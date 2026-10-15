package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// Перенос прежних откликов и приглашений (срез 38, D-240): у каждой пары «вакансия × учёный», где отклик или приглашение
// были до чата, появляется переписка с теми строками о событиях, которые можно восстановить из таблиц, в порядке времени.
// Команда одноразовая и повторяемая: пара, у которой переписка уже есть, пропускается, поэтому повторный запуск (и запуск
// после того, как новые отклики уже пишут строки сами) ничего не дублирует.
//
// Что восстановить нельзя, того нет: момент просмотра отклика (в таблице только время последней смены статуса, а оно у
// просмотренного как раз и есть момент просмотра), время отзыва приглашения (берётся время последнего изменения строки).
// Всё перенесённое считается прочитанным: люди узнали об этом по уведомлениям, и счётчики в шапке не должны вырасти
// на сотни старых «событий» в день выкладки.

// BackfillResult — что перенесено.
type BackfillResult struct {
	Pairs int // переписок заведено
	Lines int // строк о событиях записано
}

// backfillBatch — сколько пар читается за один заход.
const backfillBatch = 200

type backfillLine struct {
	at   time.Time
	kind string
	data any
}

// Backfill переносит прежние отклики и приглашения в переписки. Каждая пара переносится своей транзакцией: сбой на одной
// не отменяет остальные, повторный запуск продолжит с оставшихся.
func Backfill(ctx context.Context, db DB) (BackfillResult, error) {
	var (
		res   BackfillResult
		q     = dbgen.New(db)
		after = dbgen.ListChatBackfillPairsParams{AfterVacancy: uuid.Nil, AfterUser: uuid.Nil, RowLimit: backfillBatch}
	)
	for {
		pairs, err := q.ListChatBackfillPairs(ctx, after)
		if err != nil {
			return res, fmt.Errorf("chat: list backfill pairs: %w", err)
		}
		for _, p := range pairs {
			lines, err := backfillPair(ctx, db, p.VacancyID, p.UserID)
			if err != nil {
				return res, err
			}
			if lines > 0 {
				res.Pairs++
				res.Lines += lines
			}
		}
		if len(pairs) < backfillBatch {
			return res, nil
		}
		last := pairs[len(pairs)-1]
		after.AfterVacancy, after.AfterUser = last.VacancyID, last.UserID
	}
}

// backfillPair переносит одну пару в своей транзакции и возвращает число записанных строк.
func backfillPair(ctx context.Context, db DB, vacancyID, userID uuid.UUID) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("chat: backfill begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(db).WithTx(tx)
	n, err := backfillOne(ctx, q, vacancyID, userID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("chat: backfill commit: %w", err)
	}
	return n, nil
}

func backfillOne(ctx context.Context, q *dbgen.Queries, vacancyID, userID uuid.UUID) (int, error) {
	apps, err := q.ListBackfillApplications(ctx, dbgen.ListBackfillApplicationsParams{VacancyID: vacancyID, UserID: userID})
	if err != nil {
		return 0, fmt.Errorf("chat: backfill applications: %w", err)
	}
	var offer *dbgen.GetBackfillOfferRow
	if o, err := q.GetBackfillOffer(ctx, dbgen.GetBackfillOfferParams{VacancyID: vacancyID, UserID: userID}); err == nil {
		offer = &o
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("chat: backfill offer: %w", err)
	}
	var lines []backfillLine
	in := OpenInput{VacancyID: vacancyID, UserID: userID}
	if offer != nil {
		in.OfferID = &offer.ID
		lines = append(lines, offerLines(*offer)...)
	}
	for _, a := range apps {
		invs, err := q.ListInvitations(ctx, a.ID)
		if err != nil {
			return 0, fmt.Errorf("chat: backfill invitations: %w", err)
		}
		lines = append(lines, applicationLines(a, invs)...)
	}
	in.ApplicationID = linkedApplication(apps)
	if len(lines) == 0 {
		return 0, nil
	}
	slices.SortStableFunc(lines, func(a, b backfillLine) int { return a.at.Compare(b.at) })
	chatID, err := Open(ctx, q, in, lines[0].at)
	if err != nil {
		return 0, err
	}
	for _, l := range lines {
		if _, err := AppendSystem(ctx, q, chatID, l.kind, l.data, l.at); err != nil {
			return 0, err
		}
	}
	return len(lines), markHistoryRead(ctx, q, chatID, vacancyID, userID, lines[len(lines)-1].at)
}

// linkedApplication — отклик, за которым следует переписка: последний живой, а если все отозваны, последний.
func linkedApplication(apps []dbgen.ListBackfillApplicationsRow) *uuid.UUID {
	for i := len(apps) - 1; i >= 0; i-- {
		if apps[i].Status != "withdrawn" {
			return &apps[i].ID
		}
	}
	if len(apps) > 0 {
		return &apps[len(apps)-1].ID
	}
	return nil
}

// markHistoryRead отмечает перенесённое прочитанным у обеих сторон: у учёного и у каждого, кто сейчас ведёт отклики этой
// вакансии.
func markHistoryRead(ctx context.Context, q *dbgen.Queries, chatID, vacancyID, userID uuid.UUID, at time.Time) error {
	v, err := q.GetVacancyForApply(ctx, vacancyID)
	if err != nil {
		return fmt.Errorf("chat: backfill vacancy: %w", err)
	}
	staff, err := orgs.UsersWhoCan(ctx, q, v.OrgID, access.ViewApplications, unitOf(v.UnitID))
	if err != nil {
		return err
	}
	for _, side := range []Side{SideScientist, SideOrg} {
		if err := q.RaiseSideRead(ctx, dbgen.RaiseSideReadParams{Side: string(side), UpTo: 1 << 30, ID: chatID}); err != nil {
			return fmt.Errorf("chat: backfill side read: %w", err)
		}
	}
	for _, uid := range append([]uuid.UUID{userID}, staff...) {
		if err := q.MarkChatRead(ctx, dbgen.MarkChatReadParams{ChatID: chatID, UserID: uid, UpTo: 1 << 30, Now: at}); err != nil {
			return fmt.Errorf("chat: backfill mark read: %w", err)
		}
	}
	return nil
}

// laterOf — более позднее из двух моментов.
func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// offerLines восстанавливает строки приглашения на вакансию.
func offerLines(o dbgen.GetBackfillOfferRow) []backfillLine {
	out := []backfillLine{{at: o.CreatedAt, kind: EventOfferCreated, data: OfferCreatedData{OfferID: o.ID, Message: o.Message, Status: "pending"}}}
	switch o.Status {
	case "interested", "declined":
		at := o.UpdatedAt
		if o.AnsweredAt != nil {
			at = *o.AnsweredAt
		}
		out = append(out, backfillLine{at: at, kind: EventOfferAnswered, data: OfferAnsweredData{OfferID: o.ID, Answer: o.Status, Note: o.AnswerNote}})
	case "cancelled":
		out = append(out, backfillLine{at: o.UpdatedAt, kind: EventOfferCancelled, data: OfferData{OfferID: o.ID}})
	}
	return out
}

// applicationLines восстанавливает строки отклика и его приглашений.
func applicationLines(a dbgen.ListBackfillApplicationsRow, invs []dbgen.ApplicationInvitation) []backfillLine {
	out := []backfillLine{{at: a.CreatedAt, kind: EventApplicationSent, data: ApplicationSentData{ApplicationID: a.ID, CoverLetter: a.CoverLetter}}}
	for _, inv := range invs {
		out = append(out, backfillLine{at: inv.CreatedAt, kind: EventInvitationCreated, data: InvitationCreated(inv)})
		if inv.AnsweredAt != nil {
			out = append(out, backfillLine{at: *inv.AnsweredAt, kind: EventInvitationAnswered, data: InvitationAnswered(inv, answerOf(inv))})
			if inv.Status == invitationConfirmed && inv.AnswerAt != nil {
				// Предложенное время принято организацией: старт собеседования стал этим временем. Момент принятия не
				// раньше ответа, на что бы ни указывало время изменения строки.
				out = append(out, backfillLine{at: laterOf(inv.UpdatedAt, *inv.AnsweredAt), kind: EventInvitationAccepted,
					data: InvitationAcceptedData{ApplicationID: a.ID, InvitationID: inv.ID, StartsAt: inv.StartsAt}})
			}
		}
		if inv.Status == "cancelled" {
			out = append(out, backfillLine{at: laterOf(inv.UpdatedAt, inv.CreatedAt), kind: EventInvitationCancelled,
				data: InvitationCancelledData{ApplicationID: a.ID, InvitationID: inv.ID, Kind: inv.Kind}})
		}
	}
	switch a.Status {
	case "viewed":
		out = append(out, backfillLine{at: a.StatusChangedAt, kind: EventApplicationViewed, data: ApplicationData{ApplicationID: a.ID}})
	case "accepted", "rejected":
		out = append(out, backfillLine{at: a.StatusChangedAt, kind: EventApplicationDecided,
			data: ApplicationDecidedData{ApplicationID: a.ID, Decision: a.Status, Note: a.DecisionNote}})
	case "withdrawn":
		out = append(out, backfillLine{at: a.StatusChangedAt, kind: EventApplicationWithdrawn, data: ApplicationData{ApplicationID: a.ID}})
	}
	return out
}

package applications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/chat"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
)

// Разбор откликов организацией (срез 9, D-088…D-095): список откликов, решения, приглашения и ответы на них.
// Права организации спрашиваются только у пакета access (ViewApplications) через orgs.ActorOf; автор отклика не может
// разбирать собственный отклик, даже если стал сотрудником организации (readerOf).

const msgDecision = "Выберите решение: принять или отказать"

// staffApp загружает отклик и требует, чтобы человек был сотрудником организации с правом видеть отклики на эту вакансию.
// Всем остальным, включая автора отклика, отклика «нет».
func (s *Service) staffApp(ctx context.Context, q *dbgen.Queries, id uuid.UUID, user auth.User) (dbgen.GetApplicationRow, error) {
	a, err := s.load(ctx, q, id)
	if err != nil {
		return dbgen.GetApplicationRow{}, err
	}
	reader, err := s.readerOf(ctx, q, a, user)
	if err != nil {
		return dbgen.GetApplicationRow{}, err
	}
	if reader != files.Staff {
		return dbgen.GetApplicationRow{}, ErrNotFound
	}
	return a, nil
}

// notifyApplicant отправляет уведомление и письмо автору отклика.
func (s *Service) notifyApplicant(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, kind, title, body string) error {
	return s.notes.Emit(ctx, q, notifications.Notice{UserID: a.UserID, Kind: kind, Title: title, Body: body, Link: "/applications/" + a.ID.String()})
}

// notifyStaff сообщает всем, кто вправе видеть отклики на эту вакансию (кроме самого автора отклика).
func (s *Service) notifyStaff(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, kind, title, body string) error {
	staff, err := orgs.UsersWhoCan(ctx, q, a.OrgID, access.ViewApplications, unitOf(a.UnitID))
	if err != nil {
		return err
	}
	for _, uid := range staff {
		if uid == a.UserID {
			continue
		}
		if err := s.notes.Emit(ctx, q, notifications.Notice{UserID: uid, Kind: kind, Title: title, Body: body, Link: "/candidates/" + a.ID.String()}); err != nil {
			return err
		}
	}
	return nil
}

// chatLine дописывает в переписку по отклику строку о событии (срез 38). Вызывается после того, как событие записано, в
// той же транзакции: сначала отклик, потом переписка (D-230).
func (s *Service) chatLine(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, event string, data any, at time.Time) error {
	id := a.ID
	_, err := chat.Record(ctx, q, chat.Entry{VacancyID: a.VacancyID, UserID: a.UserID, ApplicationID: &id, Type: event, Data: data, At: at})
	return err
}

// cancelOpenInvitations закрывает открытые приглашения отклика (отказ, отзыв) и пишет о каждом строку в переписку.
func (s *Service) cancelOpenInvitations(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, now time.Time) error {
	closed, err := q.CancelOpenInvitations(ctx, dbgen.CancelOpenInvitationsParams{ApplicationID: a.ID, Now: now})
	if err != nil {
		return fmt.Errorf("applications: cancel invitations: %w", err)
	}
	return s.cancelLines(ctx, q, a, closed, now)
}

func (s *Service) cancelLines(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, closed []dbgen.CancelOpenInvitationsRow, now time.Time) error {
	for _, c := range closed {
		if err := s.chatLine(ctx, q, a, chat.EventInvitationCancelled, chat.InvitationCancelledData{ApplicationID: a.ID, InvitationID: c.ID, Kind: c.Kind}, now); err != nil {
			return err
		}
	}
	return nil
}

// record пишет в журнал действие сотрудника над откликом. Подпись объекта — название вакансии, имя соискателя берётся
// из его аккаунта при показе (у стёртого аккаунта в журнале «Аккаунт удалён»).
func record(ctx context.Context, q *dbgen.Queries, action audit.Action, a dbgen.GetApplicationRow, user auth.User, details map[string]string) error {
	return audit.Record(ctx, q, audit.Event{
		Action: action, OrgID: a.OrgID, Actor: user.ID, Subject: a.UserID,
		TargetKind: "application", TargetID: a.ID, TargetLabel: a.VacancyTitle, Details: details,
	})
}

// markViewed ставит «просмотрен», когда организация впервые открыла карточку, и сообщает об этом соискателю.
// Переход срабатывает один раз даже при одновременном открытии двумя сотрудниками.
func (s *Service) markViewed(ctx context.Context, user auth.User, a *dbgen.GetApplicationRow) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		now := s.now()
		n, err := q.SetApplicationStatusFrom(ctx, dbgen.SetApplicationStatusFromParams{ID: a.ID, ToStatus: StatusViewed, FromStatuses: []string{StatusSent}, Now: now})
		if err != nil {
			return fmt.Errorf("applications: mark viewed: %w", err)
		}
		if n == 0 {
			return nil // уже просмотрен другим сотрудником
		}
		a.Status, a.StatusChangedAt = StatusViewed, now
		if err := record(ctx, q, audit.ApplicationViewed, *a, user, nil); err != nil {
			return err
		}
		if err := s.chatLine(ctx, q, *a, chat.EventApplicationViewed, chat.ApplicationData{ApplicationID: a.ID}, now); err != nil {
			return err
		}
		return s.notifyApplicant(ctx, q, *a, "application_viewed", "Ваш отклик просмотрен",
			lines(vacancyLine(a.VacancyTitle, a.OrgName), "Организация открыла ваш отклик."))
	})
}

// ---- решение ----

// Decide ставит решение по отклику: принят или отказ, с короткой запиской соискателю. Решение окончательное (D-088).
// Отказ закрывает открытые приглашения.
func (s *Service) Decide(ctx context.Context, user auth.User, id uuid.UUID, to, note string) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.staffApp(ctx, q, id, user)
		if err != nil {
			return err
		}
		note = strings.TrimSpace(note)
		switch {
		case !isOneOf(to, decisions):
			return &auth.ValidationError{Fields: map[string]string{"status": msgDecision}}
		case utf8.RuneCountInString(note) > maxNoteRunes:
			return &auth.ValidationError{Fields: map[string]string{"note": msgNoteLong}}
		}
		now := s.now()
		n, err := q.SetApplicationDecision(ctx, dbgen.SetApplicationDecisionParams{
			ID: id, ToStatus: to, Note: note, DecidedBy: &user.ID, Now: now, FromStatuses: fromStatuses(to),
		})
		if err != nil {
			return fmt.Errorf("applications: decide: %w", err)
		}
		if n == 0 {
			return ErrBadStatus
		}
		action := audit.ApplicationAccepted
		if to == StatusRejected {
			action = audit.ApplicationRejected
		}
		if err := record(ctx, q, action, a, user, nil); err != nil {
			return err
		}
		title, body := decisionNotice(a, to, note)
		if err := s.chatLine(ctx, q, a, chat.EventApplicationDecided, chat.ApplicationDecidedData{ApplicationID: id, Decision: to, Note: note}, now); err != nil {
			return err
		}

		if to == StatusRejected {
			if err := s.cancelOpenInvitations(ctx, q, a, now); err != nil {
				return err
			}
		}
		return s.notifyApplicant(ctx, q, a, "application_"+to, title, body)
	})
}

func decisionNotice(a dbgen.GetApplicationRow, to, note string) (title, body string) {
	head := vacancyLine(a.VacancyTitle, a.OrgName)
	if to == StatusAccepted {
		return "Положительное решение по отклику", lines(head, "Организация приняла положительное решение по вашему отклику.", withNote("Сообщение организации", note))
	}
	return "Решение по отклику", lines(head, "Организация не продолжит рассмотрение вашего отклика.", withNote("Сообщение организации", note))
}

// ---- приглашения ----

// Invite отправляет приглашение. Отклик становится «приглашён»; новое собеседование заменяет прежние открытые.
func (s *Service) Invite(ctx context.Context, user auth.User, id uuid.UUID, in InvitationInput) (Invitation, error) {
	var out Invitation
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.staffApp(ctx, q, id, user)
		if err != nil {
			return err
		}
		// Учёный, заблокировавший организацию, приглашений от неё больше не получает (срез 38, D-237, T-105).
		if blocked, err := chat.Blocked(ctx, q, a.UserID, a.OrgID); err != nil {
			return err
		} else if blocked {
			return ErrBlocked
		}
		now := s.now()
		v, errs := validateInvitation(in, now)
		if len(errs) > 0 {
			return &auth.ValidationError{Fields: errs}
		}
		// Строка отклика блокируется до конца транзакции: решение и приглашение одновременно не пройдут,
		// и два приглашения одновременно не обойдут предел.
		n, err := q.MarkApplicationInvited(ctx, dbgen.MarkApplicationInvitedParams{ID: id, Now: now, FromStatuses: append(fromStatuses(StatusInvited), StatusInvited)})
		if err != nil {
			return fmt.Errorf("applications: mark invited: %w", err)
		}
		if n == 0 {
			return ErrBadStatus
		}
		total, err := q.CountInvitations(ctx, id)
		if err != nil {
			return fmt.Errorf("applications: count invitations: %w", err)
		}
		if total >= maxInvitations {
			return ErrTooManyInvitations
		}
		if v.Kind == InvInterview {
			replaced, err := q.CancelOpenInterviews(ctx, dbgen.CancelOpenInterviewsParams{ApplicationID: id, Now: now})
			if err != nil {
				return fmt.Errorf("applications: replace interviews: %w", err)
			}
			// Заменённое собеседование в переписке закрывается строкой: иначе его карточка осталась бы с кнопками.
			closed := make([]dbgen.CancelOpenInvitationsRow, 0, len(replaced))
			for _, r := range replaced {
				closed = append(closed, dbgen.CancelOpenInvitationsRow(r))
			}
			if err := s.cancelLines(ctx, q, a, closed, now); err != nil {
				return err
			}
		}
		invID, err := q.InsertInvitation(ctx, dbgen.InsertInvitationParams{
			ApplicationID: id, Kind: v.Kind, Status: InitialInvitationStatus(v.Kind), Message: v.Message, StartsAt: v.StartsAt, PlaceKind: v.PlaceKind,
			Place: v.Place, ContactName: v.ContactName, ContactEmail: v.ContactEmail, ContactPhone: v.Phone, CreatedBy: &user.ID, Now: now,
		})
		if err != nil {
			return fmt.Errorf("applications: insert invitation: %w", err)
		}
		row, err := q.GetInvitation(ctx, dbgen.GetInvitationParams{ID: invID, ApplicationID: id})
		if err != nil {
			return fmt.Errorf("applications: load invitation: %w", err)
		}
		out = invitationFrom(row, files.Staff)
		if err := record(ctx, q, audit.ApplicationInvited, a, user, map[string]string{"kind": v.Kind}); err != nil {
			return err
		}
		if err := s.chatLine(ctx, q, a, chat.EventInvitationCreated, chat.InvitationCreated(row), now); err != nil {
			return err
		}
		// Блокировка могла появиться, пока шла проверка: после замка переписки читается заново (см. offers.Invite, T-105).
		if blocked, err := chat.Blocked(ctx, q, a.UserID, a.OrgID); err != nil {
			return err
		} else if blocked {
			return ErrBlocked
		}
		title, body := invitationNotice(a.VacancyTitle, a.OrgName, v)
		return s.notifyApplicant(ctx, q, a, "invitation_"+v.Kind, title, body)
	})
	return out, err
}

// loadInvitation находит приглашение внутри отклика.
func loadInvitation(ctx context.Context, q *dbgen.Queries, appID, invID uuid.UUID) (dbgen.ApplicationInvitation, error) {
	inv, err := q.GetInvitation(ctx, dbgen.GetInvitationParams{ID: invID, ApplicationID: appID})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.ApplicationInvitation{}, ErrNotFound
	}
	if err != nil {
		return dbgen.ApplicationInvitation{}, fmt.Errorf("applications: load invitation: %w", err)
	}
	return inv, nil
}

// CancelInvitation отменяет приглашение, которое ещё открыто; соискателю приходит уведомление.
func (s *Service) CancelInvitation(ctx context.Context, user auth.User, appID, invID uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.staffApp(ctx, q, appID, user)
		if err != nil {
			return err
		}
		inv, err := loadInvitation(ctx, q, appID, invID)
		if err != nil {
			return err
		}
		n, err := q.SetInvitationStatusFrom(ctx, dbgen.SetInvitationStatusFromParams{ID: invID, ToStatus: InvCancelled, FromStatuses: openInvitation, Now: s.now()})
		if err != nil {
			return fmt.Errorf("applications: cancel invitation: %w", err)
		}
		if n == 0 {
			return ErrBadInvitation
		}
		if err := record(ctx, q, audit.ApplicationInvitationCanceled, a, user, map[string]string{"kind": inv.Kind}); err != nil {
			return err
		}
		if err := s.chatLine(ctx, q, a, chat.EventInvitationCancelled, chat.InvitationCancelledData{ApplicationID: appID, InvitationID: invID, Kind: inv.Kind}, s.now()); err != nil {
			return err
		}
		return s.notifyApplicant(ctx, q, a, "invitation_cancelled", "Приглашение отменено",
			lines(vacancyLine(a.VacancyTitle, a.OrgName), "Организация отменила приглашение: "+kindWord(inv.Kind)+"."))
	})
}

// AcceptProposal принимает время, которое предложил соискатель: оно становится временем собеседования.
func (s *Service) AcceptProposal(ctx context.Context, user auth.User, appID, invID uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.staffApp(ctx, q, appID, user)
		if err != nil {
			return err
		}
		inv, err := loadInvitation(ctx, q, appID, invID)
		if err != nil {
			return err
		}
		if !CanAcceptProposal(inv.Kind, inv.Status) {
			return ErrBadInvitation
		}
		n, err := q.AcceptProposedTime(ctx, dbgen.AcceptProposedTimeParams{ID: invID, Now: s.now()})
		if err != nil {
			return fmt.Errorf("applications: accept proposal: %w", err)
		}
		if n == 0 {
			return ErrBadInvitation // предложенное время уже прошло или приглашение изменили
		}
		if err := record(ctx, q, audit.ApplicationProposalAccepted, a, user, map[string]string{"kind": inv.Kind}); err != nil {
			return err
		}
		if err := s.chatLine(ctx, q, a, chat.EventInvitationAccepted, chat.InvitationAcceptedData{ApplicationID: appID, InvitationID: invID, StartsAt: inv.AnswerAt}, s.now()); err != nil {
			return err
		}
		return s.notifyApplicant(ctx, q, a, "invitation_confirmed", "Время собеседования согласовано",
			lines(vacancyLine(a.VacancyTitle, a.OrgName), "Собеседование: "+momentText(*inv.AnswerAt), placeText(deref(inv.PlaceKind), inv.Place)))
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Answer — ответ соискателя на приглашение: подтвердить собеседование, предложить другое время или оставить контакты.
// Отвечать можно один раз, пока приглашение ждёт ответа; организации приходит уведомление.
func (s *Service) Answer(ctx context.Context, user auth.User, appID, invID uuid.UUID, in AnswerInput) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.load(ctx, q, appID)
		if err != nil {
			return err
		}
		if a.UserID != user.ID {
			return ErrNotFound
		}
		inv, err := loadInvitation(ctx, q, appID, invID)
		if err != nil {
			return err
		}
		now := s.now()
		v, errs := validateAnswer(inv.Kind, in, now)
		if len(errs) > 0 {
			return &auth.ValidationError{Fields: errs}
		}
		n, err := q.AnswerInvitation(ctx, dbgen.AnswerInvitationParams{
			ID: invID, ToStatus: v.Status, AnswerAt: v.ProposedAt, AnswerNote: v.Note, AnswerContact: v.Contact, AnswerTime: v.Time, Now: now,
		})
		if err != nil {
			return fmt.Errorf("applications: answer invitation: %w", err)
		}
		if n == 0 {
			return ErrBadInvitation
		}
		answered, err := loadInvitation(ctx, q, appID, invID)
		if err != nil {
			return err
		}
		if err := s.chatLine(ctx, q, a, chat.EventInvitationAnswered, chat.InvitationAnswered(answered, v.Status), now); err != nil {
			return err
		}
		title, body := answerNotice(a.ApplicantName, a.VacancyTitle, inv, v)
		return s.notifyStaff(ctx, q, a, "invitation_"+v.Status, title, body)
	})
}

// ---- чтение: приглашения в карточке ----

func invitationFrom(r dbgen.ApplicationInvitation, reader files.Reader) Invitation {
	inv := Invitation{
		ID: r.ID, Kind: r.Kind, Status: r.Status, Message: r.Message, StartsAt: r.StartsAt, PlaceKind: deref(r.PlaceKind), Place: r.Place,
		ContactName: r.ContactName, ContactEmail: r.ContactEmail, ContactPhone: r.ContactPhone, CreatedAt: r.CreatedAt,
	}
	if r.AnsweredAt != nil {
		inv.Answer = &InvitationAnswer{ProposedAt: r.AnswerAt, Note: r.AnswerNote, Contact: r.AnswerContact, Time: r.AnswerTime, AnsweredAt: *r.AnsweredAt}
	}
	switch reader {
	case files.Applicant:
		inv.CanAnswer = CanAnswerInvitation(r.Status)
	case files.Staff:
		inv.CanCancel = CanCancelInvitation(r.Status)
		inv.CanAcceptProposal = CanAcceptProposal(r.Kind, r.Status)
	}
	return inv
}

func (s *Service) invitationsOf(ctx context.Context, q *dbgen.Queries, appID uuid.UUID, reader files.Reader) ([]Invitation, error) {
	rows, err := q.ListInvitations(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("applications: list invitations: %w", err)
	}
	out := make([]Invitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, invitationFrom(r, reader))
	}
	return out, nil
}

// ---- список откликов организации ----

// Candidates — список откликов на вакансии, которые человек вправе разбирать: новые сверху, с числом откликов по статусам.
func (s *Service) Candidates(ctx context.Context, user auth.User, f CandidateFilter) (CandidateList, error) {
	if f.Status != "" && !isOneOf(f.Status, Statuses) {
		return CandidateList{}, &auth.ValidationError{Fields: map[string]string{"status": "Такого статуса нет"}}
	}
	sc, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ViewApplications)
	if err != nil {
		return CandidateList{}, err
	}
	counts := map[string]int{}
	for _, st := range Statuses {
		counts[st] = 0
	}
	rows, err := s.q.CountCandidatesByStatus(ctx, dbgen.CountCandidatesByStatusParams{WholeOrgs: sc.WholeOrgs, Units: sc.Units, VacancyID: f.VacancyID})
	if err != nil {
		return CandidateList{}, fmt.Errorf("applications: count candidates: %w", err)
	}
	all := 0
	for _, r := range rows {
		counts[r.Status] = int(r.Total)
		all += int(r.Total)
	}
	total := all
	if f.Status != "" {
		total = counts[f.Status]
	}
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 20
	}
	limit, offset = min(limit, 50), max(offset, 0)
	list, err := s.q.ListCandidates(ctx, dbgen.ListCandidatesParams{
		WholeOrgs: sc.WholeOrgs, Units: sc.Units, VacancyID: f.VacancyID, Status: f.Status, RowLimit: num.Int32(limit), RowOffset: num.Int32(offset),
	})
	if err != nil {
		return CandidateList{}, fmt.Errorf("applications: list candidates: %w", err)
	}
	out := CandidateList{Items: make([]Candidate, 0, len(list)), Total: total, Counts: counts}
	ids := make([]uuid.UUID, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	links, err := chat.LinksOf(ctx, s.q, chat.SideOrg, user.ID, ids, nil)
	if err != nil {
		return CandidateList{}, err
	}
	for _, r := range list {
		unit := ""
		if r.UnitName != nil {
			unit = *r.UnitName
		}
		out.Items = append(out.Items, Candidate{
			ID: r.ID, Status: r.Status, CreatedAt: r.CreatedAt, StatusChangedAt: r.StatusChangedAt, ApplicantName: r.ApplicantName, Headline: r.Headline, Anonymized: r.Anonymized,
			Vacancy:            VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, Status: r.VacancyStatus, OrgName: r.OrgName, OrgSlug: r.OrgSlug},
			UnitName:           unit,
			References:         RefsCount{Total: int(r.RefsTotal), Received: int(r.RefsReceived)},
			PendingInvitations: int(r.InvitesPending), ProposedInvitations: int(r.InvitesProposed),
			Chat: links.Application(r.ID),
		})
	}
	return out, nil
}

// CandidateVacancies — вакансии, на которые есть отклики и которые человек вправе разбирать, с числом откликов.
func (s *Service) CandidateVacancies(ctx context.Context, user auth.User) ([]VacancyCount, error) {
	sc, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ViewApplications)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListCandidateVacancies(ctx, dbgen.ListCandidateVacanciesParams{WholeOrgs: sc.WholeOrgs, Units: sc.Units})
	if err != nil {
		return nil, fmt.Errorf("applications: list candidate vacancies: %w", err)
	}
	out := make([]VacancyCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, VacancyCount{ID: r.ID, Title: r.Title, Status: r.Status, OrgName: r.OrgName, OrgSlug: r.OrgSlug, Total: int(r.Total), New: int(r.NewCount)})
	}
	return out, nil
}

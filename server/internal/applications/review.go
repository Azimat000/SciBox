package applications

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

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

// Разбор откликов организацией (срез 9, D-088…D-095): список откликов и решения. Приглашений внутри отклика нет (D-256).
// Права организации спрашиваются только у пакета access (ViewApplications) через orgs.ActorOf; автор отклика не может
// разбирать собственный отклик, даже если стал сотрудником организации (readerOf).

const (
	msgDecision  = "Выберите решение: принять или отказать"
	msgNoteLong  = "Не больше 1000 знаков"
	maxNoteRunes = 1000
)

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

// chatLine дописывает в переписку по отклику строку о событии (срез 38). Вызывается после того, как событие записано, в
// той же транзакции: сначала отклик, потом переписка (D-230).
func (s *Service) chatLine(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, event string, data any, at time.Time) error {
	id := a.ID
	_, err := chat.Record(ctx, q, chat.Entry{VacancyID: a.VacancyID, UserID: a.UserID, ApplicationID: &id, Type: event, Data: data, At: at})
	return err
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
			Vacancy:    VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, Status: r.VacancyStatus, OrgName: r.OrgName, OrgSlug: r.OrgSlug},
			UnitName:   unit,
			References: RefsCount{Total: int(r.RefsTotal), Received: int(r.RefsReceived)},
			Chat:       links.Application(r.ID),
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

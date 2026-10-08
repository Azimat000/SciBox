package offers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
	"scibox/server/internal/privacy"
)

// kindOffer — вид счётчика частоты (таблица rate_events общая с остальными разделами).
const kindOffer = "offer"

// uniqueViolation — код ошибки PostgreSQL «нарушено уникальное ограничение».
const uniqueViolation = "23505"

var moscow = time.FixedZone("MSK", 3*60*60)

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	// Invite — сколько приглашений один аккаунт может отправить за окно (защита учёных от рассылки).
	Invite Limit
}

// DefaultConfig — боевые настройки: 20 приглашений в сутки с одного аккаунта (D-095).
func DefaultConfig() Config { return Config{Invite: Limit{Max: 20, Window: 24 * time.Hour}} }

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика приглашений.
type Service struct {
	db    DB
	q     *dbgen.Queries
	cfg   Config
	notes *notifications.Service
	now   func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, notes *notifications.Service, cfg Config) *Service {
	return newService(pool, notes, cfg)
}

func newService(db DB, notes *notifications.Service, cfg Config) *Service {
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, notes: notes, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("offers: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("offers: commit: %w", err)
	}
	return nil
}

// today — сегодняшняя дата по Москве как полночь в UTC (так сравниваются даты срока подачи, D-057).
func (s *Service) today() time.Time {
	n := s.now().In(moscow)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) spend(ctx context.Context, q *dbgen.Queries, user uuid.UUID) error {
	now := s.now()
	lim := s.cfg.Invite
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindOffer, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("offers: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindOffer, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("offers: record rate event: %w", err)
	}
	return nil
}

func unitOf(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return access.NoUnit
	}
	return *id
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// checkText проверяет необязательный текст: длина и отсутствие управляющих знаков (кроме переноса строки).
func checkText(text string, limit int, msgLong string) (string, string) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > limit {
		return text, msgLong
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return text, "В тексте есть недопустимые символы"
		}
	}
	return text, ""
}

// ---- приглашение ----

// Invite отправляет приглашение учёному на вакансию. Вести вакансию (access.ManageVacancies) должен приглашающий, профиль
// учёного должен быть ему виден (privacy), вакансия открыта, человек ещё не откликался и не приглашён.
// Всё, что человеку «не положено» знать (чужая вакансия, закрытый профиль), даёт «нет такого».
func (s *Service) Invite(ctx context.Context, user auth.User, in InviteInput) (Offer, error) {
	var out Offer
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		v, err := q.GetVacancyForApply(ctx, in.VacancyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("offers: load vacancy: %w", err)
		}
		actor, err := orgs.ActorOf(ctx, q, v.OrgID, user.ID)
		if err != nil {
			return err
		}
		if !actor.Can(access.ManageVacancies, unitOf(v.UnitID)) {
			return ErrNotFound
		}
		p, err := q.GetProfileForOffer(ctx, in.ProfileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("offers: load profile: %w", err)
		}
		// Приглашающий состоит в организации, поэтому смотрит на профиль как сотрудник.
		if vis, _ := privacy.Parse(p.Visibility); !vis.CanView(privacy.Viewer{Staff: true}) {
			return ErrNotFound
		}
		message, msgErr := checkText(in.Message, maxMessage, "Сообщение длиннее 1000 знаков. Сократите его")
		if msgErr != "" {
			return &auth.ValidationError{Fields: map[string]string{"message": msgErr}}
		}
		if p.UserID == user.ID {
			return ErrStaffInvitee
		}
		invitee, err := orgs.ActorOf(ctx, q, v.OrgID, p.UserID)
		if err != nil {
			return err
		}
		if invitee.Can(access.ViewApplications, unitOf(v.UnitID)) {
			return ErrStaffInvitee
		}
		if v.Status != "published" {
			return ErrVacancyClosed
		}
		if v.Deadline != nil && v.Deadline.Before(s.today()) {
			return ErrDeadlinePassed
		}
		if _, err := q.GetActiveApplicationForVacancy(ctx, dbgen.GetActiveApplicationForVacancyParams{VacancyID: v.ID, UserID: p.UserID}); err == nil {
			return ErrAlreadyApplied
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("offers: check application: %w", err)
		}
		if err := s.spend(ctx, q, user.ID); err != nil {
			return err
		}
		id, err := q.InsertOffer(ctx, dbgen.InsertOfferParams{VacancyID: v.ID, UserID: p.UserID, InvitedBy: &user.ID, Message: message, Now: s.now()})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrAlreadyOffered
		}
		if err != nil {
			return fmt.Errorf("offers: insert offer: %w", err)
		}
		title, body := receivedNotice(v.Title, v.OrgName, message)
		if err := s.notes.Emit(ctx, q, notifications.Notice{UserID: p.UserID, Kind: "offer_received", Title: title, Body: body, Link: "/offers/" + id.String()}); err != nil {
			return err
		}
		row, err := q.GetOffer(ctx, id)
		if err != nil {
			return fmt.Errorf("offers: load offer: %w", err)
		}
		out = offerFromRow(row, true)
		return nil
	})
	return out, err
}

// loadForStaff находит приглашение, которым вправе распоряжаться организация: ведущему вакансию оно есть, остальным нет.
func (s *Service) loadForStaff(ctx context.Context, q *dbgen.Queries, id uuid.UUID, user auth.User) (dbgen.GetOfferRow, error) {
	o, err := q.GetOffer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetOfferRow{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetOfferRow{}, fmt.Errorf("offers: load offer: %w", err)
	}
	actor, err := orgs.ActorOf(ctx, q, o.OrgID, user.ID)
	if err != nil {
		return dbgen.GetOfferRow{}, err
	}
	if !actor.Can(access.ManageVacancies, unitOf(o.UnitID)) {
		return dbgen.GetOfferRow{}, ErrNotFound
	}
	return o, nil
}

// Cancel отзывает приглашение, на которое ещё нет ответа; учёному приходит уведомление.
func (s *Service) Cancel(ctx context.Context, user auth.User, id uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		o, err := s.loadForStaff(ctx, q, id, user)
		if err != nil {
			return err
		}
		n, err := q.CancelOffer(ctx, dbgen.CancelOfferParams{ID: id, Now: s.now()})
		if err != nil {
			return fmt.Errorf("offers: cancel offer: %w", err)
		}
		if n == 0 {
			return ErrBadState
		}
		title, body := cancelledNotice(o.VacancyTitle, o.OrgName)
		return s.notes.Emit(ctx, q, notifications.Notice{UserID: o.UserID, Kind: "offer_cancelled", Title: title, Body: body, Link: "/offers/" + id.String()})
	})
}

// Answer — ответ учёного: «Интересно» или «Не сейчас», с необязательной запиской. Отвечает только приглашённый и один раз.
// Сотрудникам, которые ведут вакансию, приходит уведомление.
func (s *Service) Answer(ctx context.Context, user auth.User, id uuid.UUID, in AnswerInput) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		o, err := q.GetOffer(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && o.UserID != user.ID {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("offers: load offer: %w", err)
		}
		note, noteErr := checkText(in.Note, maxNote, "Записка длиннее 1000 знаков. Сократите её")
		switch {
		case in.Action != ActionInterested && in.Action != ActionDeclined:
			return &auth.ValidationError{Fields: map[string]string{"action": "Выберите ответ: «Интересно» или «Не сейчас»"}}
		case noteErr != "":
			return &auth.ValidationError{Fields: map[string]string{"note": noteErr}}
		}
		n, err := q.AnswerOffer(ctx, dbgen.AnswerOfferParams{ID: id, UserID: user.ID, Status: in.Action, Note: note, Now: s.now()})
		if err != nil {
			return fmt.Errorf("offers: answer offer: %w", err)
		}
		if n == 0 {
			return ErrBadState
		}
		staff, err := orgs.UsersWhoCan(ctx, q, o.OrgID, access.ManageVacancies, unitOf(o.UnitID))
		if err != nil {
			return err
		}
		title, body := answeredNotice(o.ScientistName, o.VacancyTitle, o.OrgName, in.Action, note)
		for _, uid := range staff {
			if uid == user.ID {
				continue
			}
			if err := s.notes.Emit(ctx, q, notifications.Notice{UserID: uid, Kind: "offer_" + in.Action, Title: title, Body: body, Link: "/sent-offers"}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- чтение ----

// Get — приглашение глазами учёного; чужое и несуществующее одинаково «нет такого».
func (s *Service) Get(ctx context.Context, user auth.User, id uuid.UUID) (Offer, error) {
	o, err := s.q.GetOffer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (o.UserID != user.ID || o.Status == StatusCancelled) {
		return Offer{}, ErrNotFound
	}
	if err != nil {
		return Offer{}, fmt.Errorf("offers: load offer: %w", err)
	}
	return offerFromRow(o, false), nil
}

func offerFromRow(o dbgen.GetOfferRow, forStaff bool) Offer {
	out := Offer{
		ID: o.ID, Status: o.Status, Message: o.Message, AnswerNote: o.AnswerNote, AnsweredAt: o.AnsweredAt, CreatedAt: o.CreatedAt,
		Vacancy: VacancyRef{ID: o.VacancyID, Title: o.VacancyTitle, Status: o.VacancyStatus, OrgName: o.OrgName, OrgSlug: o.OrgSlug,
			UnitName: deref(o.UnitName), City: o.VacancyCity, Deadline: dateStr(o.Deadline)},
	}
	pending := o.Status == StatusPending
	if forStaff {
		out.Scientist = &ScientistRef{ProfileID: o.ProfileID, Name: o.ScientistName}
		out.CanCancel = pending
	} else {
		out.ApplicationID = o.ApplicationID
		out.CanAnswer = pending
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pageOf(limit, offset int) (int32, int32) {
	if limit <= 0 {
		limit = 20
	}
	return num.Int32(min(limit, 50)), num.Int32(max(offset, 0))
}

// Mine — «Приглашения» учёного: новые сверху, с числом по состояниям.
func (s *Service) Mine(ctx context.Context, user auth.User, status string, limit, offset int) (MineList, error) {
	if status != "" && status != StatusPending && status != StatusInterested && status != StatusDeclined {
		return MineList{}, &auth.ValidationError{Fields: map[string]string{"status": "Такого состояния нет"}}
	}
	counts := map[string]int{StatusPending: 0, StatusInterested: 0, StatusDeclined: 0}
	rows, err := s.q.CountMyOffersByStatus(ctx, user.ID)
	if err != nil {
		return MineList{}, fmt.Errorf("offers: count offers: %w", err)
	}
	all := 0
	for _, r := range rows {
		counts[r.Status] = int(r.Total)
		all += int(r.Total)
	}
	total := all
	if status != "" {
		total = counts[status]
	}
	l, o := pageOf(limit, offset)
	list, err := s.q.ListMyOffers(ctx, dbgen.ListMyOffersParams{UserID: user.ID, Status: status, RowLimit: l, RowOffset: o})
	if err != nil {
		return MineList{}, fmt.Errorf("offers: list offers: %w", err)
	}
	out := MineList{Items: make([]Offer, 0, len(list)), Total: total, Counts: counts, Pending: counts[StatusPending]}
	for _, r := range list {
		out.Items = append(out.Items, Offer{
			ID: r.ID, Status: r.Status, Message: r.Message, AnswerNote: r.AnswerNote, AnsweredAt: r.AnsweredAt, CreatedAt: r.CreatedAt,
			Vacancy: VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, Status: r.VacancyStatus, OrgName: r.OrgName, OrgSlug: r.OrgSlug,
				City: r.VacancyCity, Deadline: dateStr(r.Deadline)},
			ApplicationID: r.ApplicationID, CanAnswer: r.Status == StatusPending,
		})
	}
	return out, nil
}

// Sent — «Отправленные приглашения» организации: по вакансиям, которые человек ведёт (access.ManageVacancies).
func (s *Service) Sent(ctx context.Context, user auth.User, f SentFilter) (SentList, error) {
	if f.Status != "" && !isOneOf(f.Status, Statuses) {
		return SentList{}, &auth.ValidationError{Fields: map[string]string{"status": "Такого состояния нет"}}
	}
	sc, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ManageVacancies)
	if err != nil {
		return SentList{}, err
	}
	counts := map[string]int{}
	for _, st := range Statuses {
		counts[st] = 0
	}
	rows, err := s.q.CountSentOffersByStatus(ctx, dbgen.CountSentOffersByStatusParams{WholeOrgs: sc.WholeOrgs, Units: sc.Units, VacancyID: f.VacancyID})
	if err != nil {
		return SentList{}, fmt.Errorf("offers: count sent offers: %w", err)
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
	l, o := pageOf(f.Limit, f.Offset)
	list, err := s.q.ListSentOffers(ctx, dbgen.ListSentOffersParams{
		WholeOrgs: sc.WholeOrgs, Units: sc.Units, VacancyID: f.VacancyID, Status: f.Status, RowLimit: l, RowOffset: o,
	})
	if err != nil {
		return SentList{}, fmt.Errorf("offers: list sent offers: %w", err)
	}
	out := SentList{Items: make([]Offer, 0, len(list)), Total: total, Counts: counts}
	for _, r := range list {
		out.Items = append(out.Items, Offer{
			ID: r.ID, Status: r.Status, Message: r.Message, AnswerNote: r.AnswerNote, AnsweredAt: r.AnsweredAt, CreatedAt: r.CreatedAt,
			Vacancy:   VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, Status: r.VacancyStatus, OrgName: r.OrgName, OrgSlug: r.OrgSlug, UnitName: deref(r.UnitName)},
			Scientist: &ScientistRef{ProfileID: r.ProfileID, Name: r.ScientistName}, CanCancel: r.Status == StatusPending,
		})
	}
	return out, nil
}

func isOneOf(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Targets — вакансии, на которые человек может пригласить этого учёного (по номеру профиля): опубликованные, срок не
// прошёл, в пределах его прав. Профиль, который человеку не виден, даёт «нет такого».
func (s *Service) Targets(ctx context.Context, user auth.User, profileID uuid.UUID) ([]Target, error) {
	p, err := s.q.GetProfileForOffer(ctx, profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("offers: load profile: %w", err)
	}
	staff, err := s.q.IsOrgStaff(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("offers: check organization staff: %w", err)
	}
	if vis, _ := privacy.Parse(p.Visibility); !vis.CanView(privacy.Viewer{Staff: staff}) {
		return nil, ErrNotFound
	}
	if p.UserID == user.ID {
		return []Target{}, nil
	}
	sc, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ManageVacancies)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListOfferTargets(ctx, dbgen.ListOfferTargetsParams{UserID: p.UserID, Today: s.today(), WholeOrgs: sc.WholeOrgs, Units: sc.Units})
	if err != nil {
		return nil, fmt.Errorf("offers: list targets: %w", err)
	}
	out := make([]Target, 0, len(rows))
	for _, r := range rows {
		out = append(out, Target{ID: r.ID, Title: r.Title, OrgName: r.OrgName, OrgSlug: r.OrgSlug, UnitName: deref(r.UnitName),
			Deadline: dateStr(r.Deadline), Offered: r.Offered, Applied: r.Applied})
	}
	return out, nil
}

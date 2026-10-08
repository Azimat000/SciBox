package references

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
)

// kindRequest — вид счётчика частоты (таблица rate_events общая с остальными разделами).
const kindRequest = "reference_request"

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	ProductName string
	PublicURL   string
	// TTL — сколько живёт ссылка рекомендателя.
	TTL time.Duration
	// ResendAfter — через сколько можно отправить просьбу повторно.
	ResendAfter time.Duration
	// Requests — сколько просьб (новых и повторных) человек может отправить за окно: защита от рассылки по чужим адресам.
	Requests Limit
}

// DefaultConfig — боевые настройки: ссылка живёт 30 дней, повторно можно через сутки, 15 просьб в сутки с одного аккаунта.
func DefaultConfig(productName, publicURL string) Config {
	return Config{ProductName: productName, PublicURL: strings.TrimRight(publicURL, "/"), TTL: 30 * 24 * time.Hour, ResendAfter: 24 * time.Hour, Requests: Limit{Max: 15, Window: 24 * time.Hour}}
}

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика рекомендательных писем.
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
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, notes: notes, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("references: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("references: commit: %w", err)
	}
	return nil
}

// spend учитывает просьбу и отказывает, если лимит исчерпан.
func (s *Service) spend(ctx context.Context, q *dbgen.Queries, user uuid.UUID) error {
	now := s.now()
	lim := s.cfg.Requests
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindRequest, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("references: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindRequest, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("references: record rate event: %w", err)
	}
	return nil
}

// openStatus: отклик ещё живой, и рекомендательные письма по нему имеют смысл.
func openStatus(status string) bool {
	switch status {
	case "sent", "viewed", "invited":
		return true
	}
	return false
}

// ---- создание просьб ----

// create кладёт просьбы в базу и письма с ссылками в очередь. q привязан к транзакции вызывающего.
func (s *Service) create(ctx context.Context, q *dbgen.Queries, app AppInfo, refs []Referee) ([]uuid.UUID, error) {
	now := s.now()
	expires := now.Add(s.cfg.TTL)
	ids := make([]uuid.UUID, 0, len(refs))
	for _, ref := range refs {
		token, hash := newToken()
		id, err := q.InsertReferenceRequest(ctx, dbgen.InsertReferenceRequestParams{
			ApplicationID: app.ID, Name: ref.Name, Email: ref.Email, Relation: ref.Relation, TokenHash: hash, Now: now, ExpiresAt: expires,
		})
		if err != nil {
			return nil, fmt.Errorf("references: insert request: %w", err)
		}
		if err := s.notes.Enqueue(ctx, q, s.requestMail(app, ref, token, expires)); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreateInTx вызывается пакетом applications при отправке отклика, в его транзакции: рекомендатели проверены Normalize.
func (s *Service) CreateInTx(ctx context.Context, q *dbgen.Queries, app AppInfo, refs []Referee) error {
	_, err := s.create(ctx, q, app, refs)
	return err
}

// loadOwn находит отклик и проверяет, что он принадлежит человеку; чужой отклик «не существует».
func (s *Service) loadOwn(ctx context.Context, q *dbgen.Queries, user auth.User, appID uuid.UUID) (dbgen.GetApplicationRow, error) {
	a, err := q.GetApplication(ctx, appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetApplicationRow{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetApplicationRow{}, fmt.Errorf("references: load application: %w", err)
	}
	if a.UserID != user.ID {
		return dbgen.GetApplicationRow{}, ErrNotFound
	}
	return a, nil
}

func appInfoOf(a dbgen.GetApplicationRow) AppInfo {
	return AppInfo{ID: a.ID, ApplicantName: a.ApplicantName, VacancyTitle: a.VacancyTitle, OrgName: a.OrgName}
}

// Add просит ещё одного рекомендателя в уже отправленном отклике.
func (s *Service) Add(ctx context.Context, user auth.User, appID uuid.UUID, in RefereeInput) (Request, error) {
	var out Request
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.loadOwn(ctx, q, user, appID)
		if err != nil {
			return err
		}
		// Блокировка отклика: две одновременные просьбы встанут в очередь и не обойдут предел.
		if _, err := q.LockApplication(ctx, appID); err != nil {
			return fmt.Errorf("references: lock application: %w", err)
		}
		if !openStatus(a.Status) {
			return ErrClosed
		}
		existing, err := q.ListReferenceRequests(ctx, appID)
		if err != nil {
			return fmt.Errorf("references: list requests: %w", err)
		}
		if len(existing) >= MaxPerApplication {
			return ErrTooMany
		}
		ref, errs := validateReferee(in, []string{user.Email, a.ContactEmail})
		for _, e := range existing {
			if strings.EqualFold(e.Email, ref.Email) {
				errs["email"] = msgDuplicate
			}
		}
		if len(errs) > 0 {
			return &auth.ValidationError{Fields: errs}
		}
		if err := s.spend(ctx, q, user.ID); err != nil {
			return err
		}
		ids, err := s.create(ctx, q, appInfoOf(a), []Referee{ref})
		if err != nil {
			return err
		}
		rows, err := q.ListReferenceRequests(ctx, appID)
		if err != nil {
			return fmt.Errorf("references: list requests: %w", err)
		}
		for _, r := range rows {
			if r.ID == ids[0] {
				out = s.requestOf(r)
			}
		}
		return nil
	})
	return out, err
}

// Resend отправляет рекомендателю новое письмо со свежей ссылкой (старая перестаёт работать).
func (s *Service) Resend(ctx context.Context, user auth.User, appID, refID uuid.UUID) (Request, error) {
	var out Request
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.loadOwn(ctx, q, user, appID)
		if err != nil {
			return err
		}
		r, err := q.LockReferenceRequest(ctx, dbgen.LockReferenceRequestParams{ID: refID, ApplicationID: appID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("references: lock request: %w", err)
		}
		if r.Status != StatusPending {
			return ErrNotPending
		}
		if !openStatus(a.Status) {
			return ErrClosed
		}
		now := s.now()
		if next := r.LastSentAt.Add(s.cfg.ResendAfter); now.Before(next) {
			return &TooSoonError{RetryAfter: next.Sub(now)}
		}
		if err := s.spend(ctx, q, user.ID); err != nil {
			return err
		}
		token, hash := newToken()
		expires := now.Add(s.cfg.TTL)
		if err := q.RenewReferenceRequest(ctx, dbgen.RenewReferenceRequestParams{ID: refID, TokenHash: hash, ExpiresAt: expires, Now: now}); err != nil {
			return fmt.Errorf("references: renew request: %w", err)
		}
		if err := s.notes.Enqueue(ctx, q, s.requestMail(appInfoOf(a), Referee{Name: r.Name, Email: r.Email, Relation: r.Relation}, token, expires)); err != nil {
			return err
		}
		rows, err := q.ListReferenceRequests(ctx, appID)
		if err != nil {
			return fmt.Errorf("references: list requests: %w", err)
		}
		for _, row := range rows {
			if row.ID == refID {
				out = s.requestOf(row)
			}
		}
		return nil
	})
	return out, err
}

// Cancel отменяет просьбу, на которую ещё не ответили (например, ошиблись в почте). Полученное письмо убрать нельзя.
func (s *Service) Cancel(ctx context.Context, user auth.User, appID, refID uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		if _, err := s.loadOwn(ctx, q, user, appID); err != nil {
			return err
		}
		r, err := q.LockReferenceRequest(ctx, dbgen.LockReferenceRequestParams{ID: refID, ApplicationID: appID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("references: lock request: %w", err)
		}
		if r.Status != StatusPending {
			return ErrNotPending
		}
		// Строка заблокирована выше, поэтому между проверкой статуса и удалением она не изменится.
		if _, err := q.DeleteReferenceRequest(ctx, dbgen.DeleteReferenceRequestParams{ID: refID, ApplicationID: appID}); err != nil {
			return fmt.Errorf("references: delete request: %w", err)
		}
		return nil
	})
}

// ---- чтение для соискателя и организации ----

func (s *Service) requestOf(r dbgen.ListReferenceRequestsRow) Request {
	req := Request{
		ID: r.ID, Name: r.Name, Email: r.Email, Relation: r.Relation, Status: r.Status,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, LastSentAt: r.LastSentAt, AnsweredAt: r.AnsweredAt,
	}
	if r.Status == StatusPending {
		if next := r.LastSentAt.Add(s.cfg.ResendAfter); s.now().Before(next) {
			req.ResendAt = &next
		} else {
			req.CanResend = true
		}
	}
	return req
}

// ListForApplicant — просьбы отклика глазами соискателя: без писем. Права проверяет вызывающий (applications).
func (s *Service) ListForApplicant(ctx context.Context, appID uuid.UUID) ([]Request, error) {
	rows, err := s.q.ListReferenceRequests(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("references: list requests: %w", err)
	}
	out := make([]Request, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.requestOf(r))
	}
	return out, nil
}

// ListForStaff — просьбы отклика глазами организации, с письмами. Права проверяет вызывающий (applications).
func (s *Service) ListForStaff(ctx context.Context, appID uuid.UUID) ([]StaffRequest, error) {
	rows, err := s.q.ListReferenceRequests(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("references: list requests: %w", err)
	}
	fs, err := s.q.ListApplicationFiles(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("references: list files: %w", err)
	}
	letterFile := map[uuid.UUID]FileInfo{}
	for _, f := range fs {
		if f.ReferenceID != nil && files.Kind(f.Kind) == files.KindReferenceLetter {
			letterFile[*f.ReferenceID] = FileInfo{ID: f.ID, Name: f.Name, Size: int(f.Size)}
		}
	}
	out := make([]StaffRequest, 0, len(rows))
	for _, r := range rows {
		sr := StaffRequest{Request: s.requestOf(r)}
		if r.Status == StatusReceived {
			sr.Letter = &Letter{Text: r.LetterText}
			if f, ok := letterFile[r.ID]; ok {
				sr.Letter.File = &f
			}
		}
		out = append(out, sr)
	}
	return out, nil
}

// ---- рекомендатель по ссылке ----

// open находит просьбу по ссылке. Ответ на живую просьбу требует, чтобы ссылка не вышла и отклик не был отозван;
// уже отвеченную просьбу можно только посмотреть.
func (s *Service) open(ctx context.Context, q *dbgen.Queries, token string) (dbgen.GetReferenceByTokenRow, error) {
	row, err := q.GetReferenceByToken(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetReferenceByTokenRow{}, ErrInvalidLink
	}
	if err != nil {
		return dbgen.GetReferenceByTokenRow{}, fmt.Errorf("references: load request: %w", err)
	}
	return row, nil
}

// answerable: на эту просьбу можно ответить сейчас.
func (s *Service) answerable(row dbgen.GetReferenceByTokenRow) error {
	switch {
	case row.Status != StatusPending:
		return ErrAlreadyAnswered
	case row.ApplicationStatus == "withdrawn":
		return ErrGone
	case !s.now().Before(row.ExpiresAt):
		return ErrExpired
	}
	return nil
}

// Lookup показывает рекомендателю, о чём его просят. Для просьбы, на которую ещё можно ответить, проверяет срок и отклик.
func (s *Service) Lookup(ctx context.Context, token string) (Info, error) {
	row, err := s.open(ctx, s.q, token)
	if err != nil {
		return Info{}, err
	}
	if row.Status == StatusPending {
		if err := s.answerable(row); err != nil {
			return Info{}, err
		}
	}
	return Info{
		RefereeName: row.Name, Relation: row.Relation, ApplicantName: row.ApplicantName, VacancyTitle: row.VacancyTitle,
		OrgName: row.OrgName, Status: row.Status, ExpiresAt: row.ExpiresAt,
	}, nil
}

// answer отмечает ответ и сообщает, кому положено. status — received или declined.
func (s *Service) answer(ctx context.Context, token, status, text string, up *files.Upload) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := s.open(ctx, q, token)
		if err != nil {
			return err
		}
		if err := s.answerable(row); err != nil {
			return err
		}
		now := s.now()
		n, err := q.AnswerReferenceRequest(ctx, dbgen.AnswerReferenceRequestParams{ID: row.ID, Status: status, LetterText: text, Now: now})
		if err != nil {
			return fmt.Errorf("references: answer request: %w", err)
		}
		if n == 0 {
			// Между проверкой и ответом ссылку использовали или она истекла.
			return ErrAlreadyAnswered
		}
		if up != nil {
			if _, err := q.InsertApplicationFile(ctx, dbgen.InsertApplicationFileParams{
				ApplicationID: row.ApplicationID, ReferenceID: &row.ID, Kind: string(files.KindReferenceLetter),
				Name: files.CleanName(up.Name), Size: num.Int32(len(up.Data)), Position: 0, Data: up.Data, Now: now,
			}); err != nil {
				return fmt.Errorf("references: save letter: %w", err)
			}
		}
		return s.announce(ctx, q, row, status)
	})
}

// announce сообщает об ответе: организации (о письме) и соискателю (что письмо пришло или рекомендатель отказался).
func (s *Service) announce(ctx context.Context, q *dbgen.Queries, row dbgen.GetReferenceByTokenRow, status string) error {
	appLink := "/applications/" + row.ApplicationID.String()
	if status == StatusDeclined {
		return s.notes.Emit(ctx, q, notifications.Notice{
			UserID: row.ApplicantID, Kind: "reference_declined", Title: "Рекомендатель не напишет письмо",
			Body: fmt.Sprintf("Рекомендатель: %s. Вакансия «%s». Можно попросить кого-то другого.", row.Name, row.VacancyTitle), Link: appLink,
		})
	}
	unit := access.NoUnit
	if row.UnitID != nil {
		unit = *row.UnitID
	}
	staff, err := orgs.UsersWhoCan(ctx, q, row.OrgID, access.ViewApplications, unit)
	if err != nil {
		return err
	}
	for _, id := range staff {
		if err := s.notes.Emit(ctx, q, notifications.Notice{
			UserID: id, Kind: "reference_received", Title: "Пришло рекомендательное письмо",
			Body: fmt.Sprintf("Рекомендатель: %s. Кандидат: %s. Вакансия «%s».", row.Name, row.ApplicantName, row.VacancyTitle),
			Link: "/candidates/" + row.ApplicationID.String(),
		}); err != nil {
			return err
		}
	}
	return s.notes.Emit(ctx, q, notifications.Notice{
		UserID: row.ApplicantID, Kind: "reference_received_mine", Title: "Рекомендательное письмо получено",
		Body: fmt.Sprintf("Рекомендатель: %s. Вакансия «%s». Содержание письма видит только организация.", row.Name, row.VacancyTitle), Link: appLink,
	})
}

// Submit принимает письмо рекомендателя: текст, PDF или и то и другое. Ссылка срабатывает один раз.
func (s *Service) Submit(ctx context.Context, token string, in LetterInput, up *files.Upload) error {
	text := strings.TrimSpace(in.Text)
	errs := map[string]string{}
	if utf8.RuneCountInString(text) > MaxLetterRunes {
		errs["text"] = fmt.Sprintf("Письмо длиннее %d знаков. Приложите его PDF-файлом", MaxLetterRunes)
	}
	if up != nil {
		if err := files.Check(*up); err != nil {
			errs["file"], _ = files.FieldMessage(err)
		}
	}
	if text == "" && up == nil {
		errs["text"] = "Напишите письмо или приложите PDF"
	}
	if len(errs) > 0 {
		return &auth.ValidationError{Fields: errs}
	}
	return s.answer(ctx, token, StatusReceived, text, up)
}

// Decline отмечает отказ рекомендателя.
func (s *Service) Decline(ctx context.Context, token string) error {
	return s.answer(ctx, token, StatusDeclined, "", nil)
}

package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/chat"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
	"scibox/server/internal/references"
)

// kindApply — вид счётчика частоты (таблица rate_events общая с остальными разделами).
const kindApply = "apply"

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
	// Apply — сколько откликов человек может отправить за окно (защита от рассылки по всем вакансиям подряд).
	Apply Limit
	// PublicURL — адрес сайта: из него строятся ссылки на отклики в файле выгрузки (срез 16).
	PublicURL string
}

// DefaultConfig — боевые настройки: 20 откликов в сутки с одного аккаунта.
func DefaultConfig() Config { return Config{Apply: Limit{Max: 20, Window: 24 * time.Hour}} }

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Profiles — откуда берётся профиль для отклика (profiles.Service в бою).
type Profiles interface {
	ForApplication(ctx context.Context, user auth.User, contactEmail string) (profiles.ApplicationPackage, error)
}

// References — рекомендательные письма отклика (references.Service в бою).
type References interface {
	CreateInTx(ctx context.Context, q *dbgen.Queries, app references.AppInfo, refs []references.Referee) error
	ListForApplicant(ctx context.Context, appID uuid.UUID) ([]references.Request, error)
	ListForStaff(ctx context.Context, appID uuid.UUID) ([]references.StaffRequest, error)
}

// Service — вся логика откликов.
type Service struct {
	db    DB
	q     *dbgen.Queries
	cfg   Config
	prof  Profiles
	refs  References
	notes *notifications.Service
	vault *files.Vault
	now   func() time.Time
}

// NewService собирает сервис. vault — склад файлов: сюда файлы приходят уже записанными (files.Vault.Put), отсюда выдаются.
func NewService(pool *pgxpool.Pool, prof *profiles.Service, refs *references.Service, notes *notifications.Service, vault *files.Vault, cfg Config) *Service {
	return newService(pool, prof, refs, notes, vault, cfg)
}

func newService(db DB, prof Profiles, refs References, notes *notifications.Service, vault *files.Vault, cfg Config) *Service {
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, prof: prof, refs: refs, notes: notes, vault: vault, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("applications: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("applications: commit: %w", err)
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
	lim := s.cfg.Apply
	// Замок на счётчик: параллельные запросы не проходят проверку с одним и тем же нулём (срез 29, CON-2).
	if err := q.LockRateKey(ctx, kindApply+":"+user.String()); err != nil {
		return fmt.Errorf("applications: lock rate key: %w", err)
	}
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindApply, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("applications: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindApply, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("applications: record rate event: %w", err)
	}
	return nil
}

func unitOf(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return access.NoUnit
	}
	return *id
}

// ---- отправка отклика ----

// Тексты ошибок полей.
const (
	msgCoverShort   = "Расскажите в сопроводительном письме, почему вы откликаетесь (не меньше 20 знаков)"
	msgCoverLong    = "Сопроводительное письмо длиннее 6000 знаков. Сократите его или приложите подробности файлом"
	msgTooManyFiles = "Можно приложить не больше пяти файлов"
	msgIncomplete   = "Профиль слишком пуст. Укажите в профиле, кто вы (поле «Должность и место работы»), и откликнитесь снова"
)

// validate проверяет поля отклика. Возвращает проверенные значения и ошибки по полям.
func validate(user auth.User, in Input, ups []files.Stored) (contact, cover string, refs []references.Referee, errs map[string]string) {
	errs = map[string]string{}
	var msg string
	if contact, msg = auth.NormalizeEmail(in.ContactEmail); msg != "" {
		errs["contact_email"] = msg
	}
	cover = strings.TrimSpace(in.CoverLetter)
	switch n := utf8.RuneCountInString(cover); {
	case n < minCoverRunes:
		errs["cover_letter"] = msgCoverShort
	case n > maxCoverRunes:
		errs["cover_letter"] = msgCoverLong
	}
	forbidden := []string{user.Email}
	if contact != "" {
		forbidden = append(forbidden, contact)
	}
	var refMsg string
	if refs, refMsg = references.Normalize(in.Referees, forbidden...); refMsg != "" {
		errs["referees"] = refMsg
	}
	if err := files.CheckSet(ups); errors.Is(err, files.ErrTooMany) {
		errs["files"] = msgTooManyFiles
	} else if err != nil {
		errs["files"], _ = files.FieldMessage(err)
	}
	return contact, cover, refs, errs
}

// checkVacancy: на вакансию можно откликаться. Черновик и архив «не существуют» для откликающегося.
func (s *Service) checkVacancy(v dbgen.GetVacancyForApplyRow) error {
	switch v.Status {
	case "published":
	case "closed":
		return ErrVacancyClosed
	default:
		return ErrNotFound
	}
	if v.Deadline != nil && v.Deadline.Before(s.today()) {
		return ErrDeadlinePassed
	}
	return nil
}

// knownRefusal — отказ, который наш код вернул до записи в базу: отклик не создан, записанные файлы больше не нужны.
// Любую другую ошибку (сбой базы, особенно при фиксации) считать отказом нельзя: возможно, транзакция всё же прошла,
// и удалённые файлы остались бы без содержимого. Такие файлы, если они действительно не нужны, уберёт чистка осиротевших.
func knownRefusal(err error) bool {
	var (
		verr *auth.ValidationError
		rerr *auth.RateLimitedError
	)
	return errors.As(err, &verr) || errors.As(err, &rerr) ||
		errors.Is(err, ErrNotFound) || errors.Is(err, ErrVacancyClosed) || errors.Is(err, ErrDeadlinePassed) ||
		errors.Is(err, ErrOwnVacancy) || errors.Is(err, ErrAlreadyApplied)
}

// CheckInput проверяет поля отклика (без файлов) до их приёма: тот же набор проверок, что и в Apply.
func (s *Service) CheckInput(user auth.User, in Input) error {
	if _, _, _, errs := validate(user, in, nil); len(errs) > 0 {
		return &auth.ValidationError{Fields: errs}
	}
	return nil
}

// Precheck — всё, что можно выяснить об отклике до приёма файлов: поля, вакансия (есть ли, открыта ли, не своя ли),
// не откликался ли человек уже и не исчерпан ли суточный предел. Те же отказы, что даёт Apply, но без записи: файлы по
// заведомо отвергаемому запросу не доходят ни до хранилища, ни до антивируса (срез 29, CON-3).
func (s *Service) Precheck(ctx context.Context, user auth.User, in Input) error {
	if err := s.CheckInput(user, in); err != nil {
		return err
	}
	v, err := s.q.GetVacancyForApply(ctx, in.VacancyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("applications: precheck vacancy: %w", err)
	}
	if err := s.checkVacancy(v); err != nil {
		return err
	}
	actor, err := orgs.ActorOf(ctx, s.q, v.OrgID, user.ID)
	if err != nil {
		return err
	}
	if actor.Can(access.ViewApplications, unitOf(v.UnitID)) {
		return ErrOwnVacancy
	}
	if _, err := s.q.GetActiveApplicationForVacancy(ctx, dbgen.GetActiveApplicationForVacancyParams{VacancyID: v.ID, UserID: user.ID}); err == nil {
		return ErrAlreadyApplied
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("applications: precheck existing application: %w", err)
	}
	now, lim := s.now(), s.cfg.Apply
	row, err := s.q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindApply, Key: user.ID.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("applications: precheck rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	return nil
}

// Apply отправляет отклик. Профиль уходит снимком (D-081): организация увидит то, что человек отправил, даже если он
// потом изменит или скроет профиль. Режим приватности профиля при этом не спрашивается: человек откликнулся сам.
// Файлы ups уже записаны в хранилище; Apply становится их владельцем: при отказе удаляет, при успехе привязывает к отклику.
func (s *Service) Apply(ctx context.Context, user auth.User, in Input, ups []files.Stored) (Detail, error) {
	staged := ups
	d, err := s.apply(ctx, user, in, ups, &staged)
	if err != nil && knownRefusal(err) {
		s.vault.Discard(ctx, staged...)
	}
	return d, err
}

func (s *Service) apply(ctx context.Context, user auth.User, in Input, ups []files.Stored, staged *[]files.Stored) (Detail, error) {
	contact, cover, refs, errs := validate(user, in, ups)
	if len(errs) > 0 {
		return Detail{}, &auth.ValidationError{Fields: errs}
	}
	pkg, err := s.prof.ForApplication(ctx, user, contact)
	if errors.Is(err, profiles.ErrIncomplete) {
		return Detail{}, &auth.ValidationError{Fields: map[string]string{"profile": msgIncomplete}}
	}
	if err != nil {
		return Detail{}, err
	}
	snapshot, _ := json.Marshal(pkg.Profile) // у View только строки, числа и списки: ошибки кодирования нет
	// Резюме собрал сам сервер: оно чисто по происхождению и идёт в хранилище без антивируса.
	cv, err := s.vault.PutTrusted(ctx, pkg.CVName, pkg.CV)
	if err != nil {
		return Detail{}, fmt.Errorf("applications: store cv: %w", err)
	}
	*staged = append([]files.Stored{cv}, ups...)

	var id uuid.UUID
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		v, err := q.GetVacancyForApply(ctx, in.VacancyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("applications: load vacancy: %w", err)
		}
		if err := s.checkVacancy(v); err != nil {
			return err
		}
		actor, err := orgs.ActorOf(ctx, q, v.OrgID, user.ID)
		if err != nil {
			return err
		}
		if actor.Can(access.ViewApplications, unitOf(v.UnitID)) {
			return ErrOwnVacancy
		}
		if _, err := q.GetActiveApplicationForVacancy(ctx, dbgen.GetActiveApplicationForVacancyParams{VacancyID: v.ID, UserID: user.ID}); err == nil {
			return ErrAlreadyApplied
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("applications: check existing application: %w", err)
		}
		if err := s.spend(ctx, q, user.ID); err != nil {
			return err
		}
		now := s.now()
		id, err = q.InsertApplication(ctx, dbgen.InsertApplicationParams{
			VacancyID: v.ID, UserID: user.ID, CoverLetter: cover, ContactEmail: contact, Profile: snapshot, Now: now,
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrAlreadyApplied
		}
		if err != nil {
			return fmt.Errorf("applications: insert: %w", err)
		}
		if err := s.saveFile(ctx, q, id, files.KindCV, 0, cv, now); err != nil {
			return err
		}
		for i, up := range ups {
			if err := s.saveFile(ctx, q, id, files.KindAttachment, i, up, now); err != nil {
				return err
			}
		}
		if err := s.refs.CreateInTx(ctx, q, references.AppInfo{ID: id, ApplicantName: user.Name, VacancyTitle: v.Title, OrgName: v.OrgName}, refs); err != nil {
			return err
		}
		// Строка в переписке пишется в той же транзакции, что и отклик: откатился отклик, откатилась и она (срез 38).
		if _, err := chat.Record(ctx, q, chat.Entry{
			VacancyID: v.ID, UserID: user.ID, ApplicationID: &id, Type: chat.EventApplicationSent, At: now,
			Data: chat.ApplicationSentData{ApplicationID: id, CoverLetter: cover},
		}); err != nil {
			return err
		}
		return s.announce(ctx, q, user, id, v)
	})
	if err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, user, id)
}

func (s *Service) saveFile(ctx context.Context, q *dbgen.Queries, appID uuid.UUID, kind files.Kind, pos int, st files.Stored, now time.Time) error {
	if _, err := q.InsertApplicationFile(ctx, dbgen.InsertApplicationFileParams{
		ApplicationID: appID, Kind: string(kind), Name: files.CleanName(st.Name), Size: num.Int32(int(st.Size)), Position: num.Int16(pos),
		StorageKey: &st.Key, Sha256: st.Sum[:], ScanStatus: string(st.Scan), ScannedAt: scannedAt(st), Now: now,
	}); err != nil {
		return fmt.Errorf("applications: save file: %w", err)
	}
	return nil
}

// scannedAt — когда файл проверен; у файла «на проверке» даты нет.
func scannedAt(st files.Stored) *time.Time {
	if st.ScannedAt.IsZero() {
		return nil
	}
	return &st.ScannedAt
}

// announce сообщает об отклике организации (тем, кто вправе видеть отклики на эту вакансию) и подтверждает его соискателю.
func (s *Service) announce(ctx context.Context, q *dbgen.Queries, user auth.User, id uuid.UUID, v dbgen.GetVacancyForApplyRow) error {
	staff, err := orgs.UsersWhoCan(ctx, q, v.OrgID, access.ViewApplications, unitOf(v.UnitID))
	if err != nil {
		return err
	}
	for _, uid := range staff {
		if err := s.notes.Emit(ctx, q, notifications.Notice{
			UserID: uid, Kind: "application_received", Title: "Новый отклик",
			Body: fmt.Sprintf("Кандидат: %s. Вакансия «%s».", user.Name, v.Title), Link: "/candidates/" + id.String(),
		}); err != nil {
			return err
		}
	}
	return s.notes.Emit(ctx, q, notifications.Notice{
		UserID: user.ID, Kind: "application_sent", Title: "Отклик отправлен",
		Body: fmt.Sprintf("Вакансия «%s», %s. Статус отклика видно в разделе «Мои отклики».", v.Title, v.OrgName), Link: "/applications/" + id.String(),
	})
}

// ---- чтение ----

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// Mine — «Мои отклики»: новые сверху.
func (s *Service) Mine(ctx context.Context, user auth.User, limit, offset int) (List, error) {
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 50)
	offset = max(offset, 0)
	rows, err := s.q.ListMyApplications(ctx, dbgen.ListMyApplicationsParams{UserID: user.ID, Lim: num.Int32(limit), Off: num.Int32(offset)})
	if err != nil {
		return List{}, fmt.Errorf("applications: list: %w", err)
	}
	total, err := s.q.CountMyApplications(ctx, user.ID)
	if err != nil {
		return List{}, fmt.Errorf("applications: count: %w", err)
	}
	out := List{Items: make([]Summary, 0, len(rows)), Total: total}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	links, err := chat.LinksOf(ctx, s.q, chat.SideScientist, user.ID, ids, nil)
	if err != nil {
		return List{}, err
	}
	for _, r := range rows {
		out.Items = append(out.Items, Summary{Chat: links.Application(r.ID),
			ID: r.ID, Status: r.Status, CreatedAt: r.CreatedAt, StatusChangedAt: r.StatusChangedAt,
			Vacancy:    VacancyRef{ID: r.VacancyID, Title: r.VacancyTitle, Status: r.VacancyStatus, OrgName: r.OrgName, OrgSlug: r.OrgSlug, Deadline: dateStr(r.Deadline)},
			References: RefsCount{Total: int(r.RefsTotal), Received: int(r.RefsReceived)}, PendingInvitations: int(r.InvitesPending),
		})
	}
	return out, nil
}

// readerOf определяет, кем человек приходится отклику. Автор отклика всегда соискатель, даже если потом стал сотрудником
// этой организации: чужих писем о себе он видеть не должен. Остальные — сотрудники, если вправе видеть отклики
// на вакансию, иначе отклика для них нет.
func (s *Service) readerOf(ctx context.Context, q *dbgen.Queries, a dbgen.GetApplicationRow, user auth.User) (files.Reader, error) {
	if a.UserID == user.ID {
		return files.Applicant, nil
	}
	actor, err := orgs.ActorOf(ctx, q, a.OrgID, user.ID)
	if err != nil {
		return 0, err
	}
	if actor.Can(access.ViewApplications, unitOf(a.UnitID)) {
		return files.Staff, nil
	}
	return 0, ErrNotFound
}

func (s *Service) load(ctx context.Context, q *dbgen.Queries, id uuid.UUID) (dbgen.GetApplicationRow, error) {
	a, err := q.GetApplication(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetApplicationRow{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetApplicationRow{}, fmt.Errorf("applications: load: %w", err)
	}
	return a, nil
}

// Get — карточка отклика. Соискатель видит свой отклик без рекомендательных писем, сотрудник организации — с письмами.
func (s *Service) Get(ctx context.Context, user auth.User, id uuid.UUID) (Detail, error) {
	a, err := s.load(ctx, s.q, id)
	if err != nil {
		return Detail{}, err
	}
	reader, err := s.readerOf(ctx, s.q, a, user)
	if err != nil {
		return Detail{}, err
	}
	// Организация, открывшая карточку впервые, отмечает отклик просмотренным (и соискатель об этом узнаёт).
	if reader == files.Staff && a.Status == StatusSent {
		if err := s.markViewed(ctx, user, &a); err != nil {
			return Detail{}, err
		}
	}
	var view profiles.View
	if err := json.Unmarshal(a.Profile, &view); err != nil {
		return Detail{}, fmt.Errorf("applications: decode profile snapshot %s: %w", a.ID, err)
	}
	fs, err := s.q.ListApplicationFiles(ctx, a.ID)
	if err != nil {
		return Detail{}, fmt.Errorf("applications: list files: %w", err)
	}
	d := Detail{Base: Base{
		ID: a.ID, Status: a.Status, CreatedAt: a.CreatedAt, StatusChangedAt: a.StatusChangedAt,
		Vacancy:       VacancyRef{ID: a.VacancyID, Title: a.VacancyTitle, Status: a.VacancyStatus, OrgName: a.OrgName, OrgSlug: a.OrgSlug, Deadline: dateStr(a.Deadline)},
		ApplicantName: a.ApplicantName, Anonymized: a.Anonymized, ContactEmail: a.ContactEmail, CoverLetter: a.CoverLetter, Profile: view, Files: []FileRef{}, DecisionNote: a.DecisionNote,
	}}
	if d.Invitations, err = s.invitationsOf(ctx, s.q, a.ID, reader); err != nil {
		return Detail{}, err
	}
	for _, f := range fs {
		kind, ok := files.Parse(f.Kind)
		// Письма рекомендателей показываются в блоке рекомендаций, а не среди файлов отклика.
		if !ok || kind == files.KindReferenceLetter || !kind.VisibleTo(reader) {
			continue
		}
		// Организация не видит заражённый файл вовсе; соискатель видит, чтобы понять, что он не принят.
		if reader == files.Staff && files.ScanStatus(f.ScanStatus) == files.ScanInfected {
			continue
		}
		ref := FileRef{ID: f.ID, Name: f.Name, Size: int(f.Size), Scan: f.ScanStatus}
		if kind == files.KindCV {
			d.CV = &ref
		} else {
			d.Files = append(d.Files, ref)
		}
	}
	side := chat.SideScientist
	if reader == files.Staff {
		side = chat.SideOrg
	}
	links, err := chat.LinksOf(ctx, s.q, side, user.ID, []uuid.UUID{a.ID}, nil)
	if err != nil {
		return Detail{}, err
	}
	d.Chat = links.Application(a.ID)
	switch reader {
	case files.Applicant:
		d.Viewer = ViewerInfo{Role: RoleApplicant, CanWithdraw: CanWithdraw(a.Status), Decisions: []string{}}
		if d.References, err = s.refs.ListForApplicant(ctx, a.ID); err != nil {
			return Detail{}, err
		}
	default:
		d.Viewer = ViewerInfo{Role: RoleStaff, Decisions: DecisionsFrom(a.Status), CanInvite: CanInvite(a.Status)}
		if d.References, err = s.refs.ListForStaff(ctx, a.ID); err != nil {
			return Detail{}, err
		}
	}
	return d, nil
}

// OpenedFile — файл отклика, открытый для выдачи. Body нужно закрыть.
type OpenedFile struct {
	Name string
	Size int64
	Body io.ReadCloser
}

// File открывает файл отклика для выдачи. Что кому положено, решает files.Kind.VisibleTo; недоступный файл «не существует».
// Файл, который ещё не проверен на вирусы, не выдаётся никому (ErrFilePending); заражённого нет.
func (s *Service) File(ctx context.Context, user auth.User, appID, fileID uuid.UUID) (OpenedFile, error) {
	a, err := s.load(ctx, s.q, appID)
	if err != nil {
		return OpenedFile{}, err
	}
	reader, err := s.readerOf(ctx, s.q, a, user)
	if err != nil {
		return OpenedFile{}, err
	}
	f, err := s.q.GetApplicationFile(ctx, dbgen.GetApplicationFileParams{ID: fileID, ApplicationID: appID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OpenedFile{}, ErrNotFound
	}
	if err != nil {
		return OpenedFile{}, fmt.Errorf("applications: load file: %w", err)
	}
	if kind, ok := files.Parse(f.Kind); !ok || !kind.VisibleTo(reader) {
		return OpenedFile{}, ErrNotFound
	}
	switch files.ScanStatus(f.ScanStatus) {
	case files.ScanClean:
	case files.ScanPending:
		return OpenedFile{}, ErrFilePending
	default:
		return OpenedFile{}, ErrNotFound
	}
	if f.StorageKey == nil {
		// Чистый файл без ключа — нарушение таблицы (ограничения базы этого не допускают), не пропускаем.
		return OpenedFile{}, fmt.Errorf("applications: file %s is clean but has no storage key", f.ID)
	}
	body, err := s.vault.Open(ctx, *f.StorageKey)
	if err != nil {
		return OpenedFile{}, fmt.Errorf("applications: open file %s: %w", f.ID, err)
	}
	// Выдача файла сотруднику организации записывается до выдачи: нет записи — нет файла. Свои файлы соискатель
	// скачивает без записи.
	if reader == files.Staff {
		if err := record(ctx, s.q, audit.ApplicationFileDownloaded, a, user, map[string]string{"kind": f.Kind}); err != nil {
			_ = body.Close()
			return OpenedFile{}, err
		}
	}
	return OpenedFile{Name: f.Name, Size: body.Size(), Body: body}, nil
}

// Withdraw отзывает отклик. Только соискатель и только пока решения нет. Организации приходит уведомление.
func (s *Service) Withdraw(ctx context.Context, user auth.User, id uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		a, err := s.load(ctx, q, id)
		if err != nil {
			return err
		}
		if a.UserID != user.ID {
			return ErrNotFound
		}
		now := s.now()
		n, err := q.SetApplicationStatusFrom(ctx, dbgen.SetApplicationStatusFromParams{ID: id, ToStatus: StatusWithdrawn, FromStatuses: withdrawable, Now: now})
		if err != nil {
			return fmt.Errorf("applications: withdraw: %w", err)
		}
		if n == 0 {
			return ErrBadStatus
		}
		if _, err := chat.Record(ctx, q, chat.Entry{
			VacancyID: a.VacancyID, UserID: a.UserID, ApplicationID: &id, Type: chat.EventApplicationWithdrawn, At: now,
			Data: chat.ApplicationData{ApplicationID: id},
		}); err != nil {
			return err
		}
		if err := s.cancelOpenInvitations(ctx, q, a, now); err != nil {
			return err
		}
		staff, err := orgs.UsersWhoCan(ctx, q, a.OrgID, access.ViewApplications, unitOf(a.UnitID))
		if err != nil {
			return err
		}
		for _, uid := range staff {
			if err := s.notes.Emit(ctx, q, notifications.Notice{
				UserID: uid, Kind: "application_withdrawn", Title: "Отклик отозван",
				Body: fmt.Sprintf("Кандидат: %s. Вакансия «%s».", a.ApplicantName, a.VacancyTitle), Link: "/candidates/" + id.String(),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ForVacancy отвечает, может ли человек откликнуться на вакансию, и если нет, то почему.
func (s *Service) ForVacancy(ctx context.Context, user auth.User, vacancyID uuid.UUID) (VacancyState, error) {
	v, err := s.q.GetVacancyForApply(ctx, vacancyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VacancyState{}, ErrNotFound
	}
	if err != nil {
		return VacancyState{}, fmt.Errorf("applications: load vacancy: %w", err)
	}
	if v.Status == "draft" || v.Status == "archived" {
		return VacancyState{}, ErrNotFound
	}
	app, err := s.q.GetActiveApplicationForVacancy(ctx, dbgen.GetActiveApplicationForVacancyParams{VacancyID: v.ID, UserID: user.ID})
	switch {
	case err == nil:
		return VacancyState{Reason: ReasonApplied, Application: &ApplicationRef{ID: app.ID, Status: app.Status}}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return VacancyState{}, fmt.Errorf("applications: check existing application: %w", err)
	}
	actor, err := orgs.ActorOf(ctx, s.q, v.OrgID, user.ID)
	if err != nil {
		return VacancyState{}, err
	}
	switch err := s.checkVacancy(v); {
	case errors.Is(err, ErrVacancyClosed):
		return VacancyState{Reason: ReasonClosed}, nil
	case errors.Is(err, ErrDeadlinePassed):
		return VacancyState{Reason: ReasonExpired}, nil
	}
	if actor.Can(access.ViewApplications, unitOf(v.UnitID)) {
		return VacancyState{Reason: ReasonOwn}, nil
	}
	return VacancyState{CanApply: true}, nil
}

package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/httpguard"
	"scibox/server/internal/mail"
)

// Назначения одноразовых ссылок и виды счётчиков частоты.
const (
	purposeConfirm     = "confirm_email"
	purposeReset       = "reset_password"
	purposeChangeEmail = "change_email"
	purposeRevertEmail = "revert_email"

	kindLoginEmail   = "login_email"
	kindLoginIP      = "login_ip"
	kindMailIP       = "mail_ip"
	kindNotice       = "register_notice"
	kindConfirm      = "register_confirm"
	kindPasswordUser = "password_user"
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrEmailNotConfirmed  = errors.New("auth: email is not confirmed")
	ErrInvalidToken       = errors.New("auth: invalid or expired token")
	ErrUnauthenticated    = errors.New("auth: not signed in")
	// ErrReauthRequired — опасное действие, а пароль в этой сессии давно не вводили (T-12, D-144).
	ErrReauthRequired = errors.New("auth: password confirmation required")
	ErrNotFound       = errors.New("auth: not found")
	ErrEmailTaken     = errors.New("auth: email already belongs to another account")
)

// RateLimitedError — слишком много попыток; RetryAfter — через сколько можно повторить.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string { return "auth: rate limited" }

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Limits — все ограничения частоты.
type Limits struct {
	LoginEmail   Limit // неудачные входы в один аккаунт
	LoginIP      Limit // неудачные входы с одного адреса
	MailIP       Limit // запросы писем (регистрация, сброс, повтор) с одного адреса
	Confirm      Limit // повторные регистрации одной неподтверждённой почты (срез 29, D-190)
	Notice       Limit // письма «вы уже зарегистрированы» на один адрес получателя (срез 29, AUT-4)
	PasswordUser Limit // неверные «текущие пароли» при смене пароля
}

// Config — настройки сервиса аккаунтов.
type Config struct {
	ProductName   string
	PublicURL     string // откуда открывается сайт: из него строятся ссылки в письмах
	PolicyVersion string // версия текста политики, на которую дано согласие

	SessionTTL     time.Duration // сколько живёт сессия
	InactiveAfter  time.Duration // без входа дольше этого срока аккаунт считается заброшенным (срез 29, D-192)
	InactiveWarn   time.Duration // за сколько до удаления неактивного аккаунта уходит письмо
	SessionMaxAge  time.Duration // предел жизни сессии с момента входа, как бы часто её ни продлевали (срез 29, AUT-11)
	SessionRefresh time.Duration // как часто продлевать сессию при активности
	ConfirmTTL     time.Duration // срок ссылки подтверждения почты
	ResetTTL       time.Duration // срок ссылки сброса пароля
	MailCooldown   time.Duration // пауза между письмами со ссылкой одному человеку
	ReauthTTL      time.Duration // сколько после ввода пароля разрешены опасные действия (D-144)
	ChangeEmailTTL time.Duration // срок ссылки подтверждения новой почты
	RevertEmailTTL time.Duration // срок ссылки «это не я» на старую почту
	DeletionGrace  time.Duration // сколько дней на отмену удаления аккаунта (D-142)
	DeviceTTL      time.Duration // сколько браузер помнится как знакомый (cookie устройства)

	Limits       Limits
	Hash         HashParams
	HashParallel int // сколько паролей хешируется одновременно
}

// PolicyVersion — версия черновика политики конфиденциальности (страница /privacy).
const PolicyVersion = "2026-10-draft"

// Сроки по умолчанию. Нулевой или отрицательный срок в настройках заменяется ими (newService):
// «0» не должен значить ни «опасные действия без пароля навсегда», ни «удалить сразу».
const (
	DefaultReauthTTL     = 10 * time.Minute
	DefaultDeletionGrace = 14 * 24 * time.Hour
	DefaultDeviceTTL     = 400 * 24 * time.Hour // столько браузеры хранят cookie самое большее
)

// DefaultConfig — боевые настройки.
func DefaultConfig(productName, publicURL string) Config {
	return Config{
		ProductName:    productName,
		PublicURL:      strings.TrimRight(publicURL, "/"),
		PolicyVersion:  PolicyVersion,
		SessionTTL:     30 * 24 * time.Hour,
		SessionMaxAge:  90 * 24 * time.Hour,
		SessionRefresh: time.Hour,
		ConfirmTTL:     48 * time.Hour,
		ResetTTL:       time.Hour,
		MailCooldown:   time.Minute,
		ReauthTTL:      DefaultReauthTTL,
		ChangeEmailTTL: 24 * time.Hour,
		RevertEmailTTL: 7 * 24 * time.Hour,
		DeletionGrace:  DefaultDeletionGrace,
		DeviceTTL:      DefaultDeviceTTL,
		Limits: Limits{
			LoginEmail:   Limit{Max: 5, Window: 15 * time.Minute},
			LoginIP:      Limit{Max: 30, Window: 15 * time.Minute},
			MailIP:       Limit{Max: 10, Window: time.Hour},
			Notice:       Limit{Max: 3, Window: 24 * time.Hour},
			Confirm:      Limit{Max: 5, Window: 24 * time.Hour},
			PasswordUser: Limit{Max: 5, Window: 15 * time.Minute},
		},
		Hash:         DefaultHashParams,
		HashParallel: 4,
	}
}

// User — человек, как его видят остальные части сервера.
type User struct {
	ID             uuid.UUID
	Email          string
	Name           string
	EmailConfirmed bool
	CreatedAt      time.Time
	// DeletionDueAt — человек попросил удалить аккаунт; в этот момент данные сотрутся. nil — не просил.
	DeletionDueAt *time.Time
}

// Session — только что созданная сессия; Token уходит в cookie и больше нигде не хранится.
type Session struct {
	Token     string
	ExpiresAt time.Time
	// Device — токен cookie устройства (знакомый браузер); пустой, если cookie менять не нужно.
	Device string
}

// Principal — кто сейчас делает запрос.
type Principal struct {
	User      User
	SessionID uuid.UUID
	// ExpiresAt — до какого момента действует сессия; Refreshed — сессию только что продлили (cookie надо обновить).
	ExpiresAt time.Time
	Refreshed bool
	// Fresh — пароль в этой сессии вводили недавно (ReauthTTL): можно опасные действия.
	Fresh bool
}

// Meta — откуда пришёл запрос.
type Meta struct {
	IP        string
	UserAgent string
	Device    string // токен из cookie устройства, если браузер его прислал
	// Binding — значение cookie «браузер регистрации» (срез 29, D-190): ссылка подтверждения почты привязывается к нему.
	Binding string
}

// DB — то, что сервису нужно от базы: запросы и транзакции. Этому интерфейсу отвечает *pgxpool.Pool;
// отдельный интерфейс нужен тестам, которые по очереди «ломают» каждый запрос и проверяют, что ошибка не теряется.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика аккаунтов.
type Service struct {
	db     DB
	q      *dbgen.Queries
	mailer mail.Sender
	hasher *Hasher
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	wg     sync.WaitGroup
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, mailer mail.Sender, cfg Config, logger *slog.Logger) *Service {
	return newService(pool, mailer, cfg, logger)
}

// NewServiceOn — то же, что NewService, поверх любой базы с запросами и транзакциями. Нужен тестам других
// пакетов, которые подставляют «ломающуюся» базу и проверяют, что сбой не теряется.
func NewServiceOn(db DB, mailer mail.Sender, cfg Config, logger *slog.Logger) *Service {
	return newService(db, mailer, cfg, logger)
}

func newService(db DB, mailer mail.Sender, cfg Config, logger *slog.Logger) *Service {
	cfg.ReauthTTL = positiveOr(cfg.ReauthTTL, DefaultReauthTTL)
	cfg.SessionMaxAge = positiveOr(cfg.SessionMaxAge, 90*24*time.Hour) // ноль не значит «без предела»
	cfg.DeletionGrace = positiveOr(cfg.DeletionGrace, DefaultDeletionGrace)
	cfg.DeviceTTL = positiveOr(cfg.DeviceTTL, DefaultDeviceTTL)
	return &Service{
		db:     db,
		q:      dbgen.New(db),
		mailer: mailer,
		hasher: NewHasher(cfg.Hash, cfg.HashParallel),
		cfg:    cfg,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func positiveOr(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

// Config возвращает настройки (нужны обработчикам: срок cookie, адрес сайта).
func (s *Service) Config() Config { return s.cfg }

// Flush ждёт, пока отправятся все письма, поставленные в очередь. Нужен тестам и мягкой остановке сервера.
func (s *Service) Flush() { s.wg.Wait() }

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}

func userFrom(u dbgen.User) User {
	return User{
		ID: u.ID, Email: u.Email, Name: u.DisplayName, EmailConfirmed: u.EmailConfirmedAt != nil, CreatedAt: u.CreatedAt,
		DeletionDueAt: u.DeletionDueAt,
	}
}

// ---- ограничение частоты ----

// reserve проверяет лимит и записывает попытку одним неделимым шагом: параллельные запросы с одним ключом стоят друг
// за другом на замке базы, поэтому пачка в тысячу запросов не пройдёт проверку с одним и тем же нулём (срез 29, AUT-1).
// Удачная попытка возвращается через refund, если она не должна считаться.
func (s *Service) reserve(ctx context.Context, kind, key string, lim Limit) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err := q.LockRateKey(ctx, kind+":"+key); err != nil {
		return fmt.Errorf("auth: lock rate key: %w", err)
	}
	now := s.now()
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kind, Key: key, At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("auth: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kind, Key: key, At: now}); err != nil {
		return fmt.Errorf("auth: record rate event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit: %w", err)
	}
	return nil
}

// refund убирает одну попытку (самую свежую) по ключу: удачный вход не считается в лимит по адресу.
func (s *Service) refund(ctx context.Context, kind, key string) error {
	if err := s.q.RefundRateEvent(ctx, dbgen.RefundRateEventParams{Kind: kind, Key: key}); err != nil {
		return fmt.Errorf("auth: refund rate event: %w", err)
	}
	return nil
}

// allowMail учитывает запрос письма с адреса ip и отказывает, если их слишком много.
func (s *Service) allowMail(ctx context.Context, ip string) error {
	return s.reserve(ctx, kindMailIP, httpguard.NetworkKey(ip), s.cfg.Limits.MailIP)
}

// ---- письма со ссылками ----

// issueToken выдаёт новую одноразовую ссылку. Если человеку уже недавно отправляли такую же (пауза MailCooldown),
// ссылка не выдаётся: ok == false.
func (s *Service) issueToken(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, purpose string, ttl time.Duration) (raw string, ok bool, err error) {
	return s.issueEmailToken(ctx, q, userID, purpose, ttl, "")
}

// issueEmailToken — как issueToken; newEmail (для смены почты) запоминается вместе со ссылкой.
func (s *Service) issueEmailToken(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, purpose string, ttl time.Duration, newEmail string) (raw string, ok bool, err error) {
	now := s.now()
	last, err := q.LatestAuthTokenAt(ctx, dbgen.LatestAuthTokenAtParams{UserID: userID, Purpose: purpose})
	if err != nil {
		return "", false, fmt.Errorf("auth: latest token: %w", err)
	}
	if now.Sub(last) < s.cfg.MailCooldown {
		return "", false, nil
	}
	if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: userID, Purpose: purpose}); err != nil {
		return "", false, fmt.Errorf("auth: supersede tokens: %w", err)
	}
	raw, err = s.createToken(ctx, q, userID, purpose, ttl, newEmail)
	return raw, err == nil, err
}

// issueConfirmToken выдаёт ссылку подтверждения почты, привязанную к браузеру регистрации (binding — значение его
// cookie; пусто — привязки нет, и такая ссылка только подтверждает почту, не входя в аккаунт). force: выдать, не глядя
// на паузу между письмами (повторная регистрация обязана погасить прежнюю ссылку, иначе она доживёт до нового пароля).
func (s *Service) issueConfirmToken(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, binding string, force bool) (raw string, ok bool, err error) {
	now := s.now()
	if !force {
		last, err := q.LatestAuthTokenAt(ctx, dbgen.LatestAuthTokenAtParams{UserID: userID, Purpose: purposeConfirm})
		if err != nil {
			return "", false, fmt.Errorf("auth: latest token: %w", err)
		}
		if now.Sub(last) < s.cfg.MailCooldown {
			return "", false, nil
		}
	}
	if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: userID, Purpose: purposeConfirm}); err != nil {
		return "", false, fmt.Errorf("auth: supersede tokens: %w", err)
	}
	raw, hash := newToken()
	var bind []byte
	if binding != "" {
		bind = hashToken(binding)
	}
	if err := q.CreateBoundAuthToken(ctx, dbgen.CreateBoundAuthTokenParams{
		UserID: userID, Purpose: purposeConfirm, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(s.cfg.ConfirmTTL), BindingHash: bind,
	}); err != nil {
		return "", false, fmt.Errorf("auth: create token: %w", err)
	}
	return raw, true, nil
}

// createToken выдаёт ссылку без паузы и не отменяя прежние (ссылка «это не я» и сброс после отката почты
// должны уйти всегда).
func (s *Service) createToken(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, purpose string, ttl time.Duration, newEmail string) (string, error) {
	now := s.now()
	raw, hash := newToken()
	var err error
	if newEmail == "" {
		err = q.CreateAuthToken(ctx, dbgen.CreateAuthTokenParams{
			UserID: userID, Purpose: purpose, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(ttl),
		})
	} else {
		err = q.CreateEmailToken(ctx, dbgen.CreateEmailTokenParams{
			UserID: userID, Purpose: purpose, TokenHash: hash, CreatedAt: now, ExpiresAt: now.Add(ttl), NewEmail: newEmail,
		})
	}
	if err != nil {
		return "", fmt.Errorf("auth: create token: %w", err)
	}
	return raw, nil
}

// sendAsync отправляет письмо в фоне: ответ человеку не ждёт почтовый сервер и не выдаёт, было ли письмо.
// Сбой отправки пишется в журнал (без адреса получателя).
func (s *Service) sendAsync(m mail.Message) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, m); err != nil {
			s.logger.Error("send mail", "subject", m.Subject, "err", err)
		}
	}()
}

// ---- регистрация ----

// RegisterInput — поля формы регистрации.
type RegisterInput struct {
	Name     string
	Email    string
	Password string
	Consent  bool
}

// Register создаёт аккаунт и отправляет письмо с подтверждением. Ответ одинаков, есть такая почта или нет:
// если аккаунт уже есть, владельцу уходит письмо об этом. Возвращает нормализованную почту.
func (s *Service) Register(ctx context.Context, in RegisterInput, meta Meta) (string, error) {
	fields := map[string]string{}
	name, msg := NormalizeName(in.Name)
	if msg != "" {
		fields["name"] = msg
	}
	email, msg := NormalizeEmail(in.Email)
	if msg != "" {
		fields["email"] = msg
	}
	if msg := ValidatePassword(in.Password, email); msg != "" {
		fields["password"] = msg
	}
	if !in.Consent {
		fields["consent"] = msgConsentRequired
	}
	if len(fields) > 0 {
		return "", &ValidationError{Fields: fields}
	}
	if err := s.allowMail(ctx, meta.IP); err != nil {
		return "", err
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return "", err
	}

	var (
		mails []mail.Message
		now   = s.now()
	)
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		user, err := q.CreateUser(ctx, dbgen.CreateUserParams{
			Email: email, DisplayName: name, PasswordHash: hash, PrivacyConsentAt: now, PrivacyPolicyVersion: s.cfg.PolicyVersion,
		})
		force := false
		if errors.Is(err, pgx.ErrNoRows) {
			// Почта занята.
			existing, err := q.GetUserByEmail(ctx, email)
			if err != nil {
				return fmt.Errorf("auth: load existing user: %w", err)
			}
			if existing.EmailConfirmedAt == nil && existing.DeletedAt == nil {
				// Повторная регистрация неподтверждённой почты. Чтобы чужие запросы не заваливали ящик и не перебивали
				// пароль без конца, их число на одну почту ограничено; сверх лимита ничего не меняем (ответ тот же).
				switch err := s.reserve(ctx, kindConfirm, email, s.cfg.Limits.Confirm); {
				case err == nil:
				case errors.As(err, new(*RateLimitedError)):
					return nil
				default:
					return err
				}
				force = true
			}
			user, err = q.ReregisterUnconfirmedUser(ctx, dbgen.ReregisterUnconfirmedUserParams{
				ID: existing.ID, DisplayName: name, PasswordHash: hash, PrivacyConsentAt: now, PrivacyPolicyVersion: s.cfg.PolicyVersion,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// Аккаунт уже подтверждён: ничего не меняем, владельцу уходит письмо.
				// Письмо владельцу уходит не чаще трёх раз в сутки: иначе чужой запрос регистрации забивал бы его ящик
				// нашими письмами. Ответ человеку, который запросил регистрацию, от этого не меняется.
				switch err := s.reserve(ctx, kindNotice, email, s.cfg.Limits.Notice); {
				case err == nil:
					mails = append(mails, s.alreadyRegisteredMail(existing))
				case !errors.As(err, new(*RateLimitedError)):
					return err
				}
				return nil
			}
			if err != nil {
				return fmt.Errorf("auth: reregister: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("auth: create user: %w", err)
		}
		raw, ok, err := s.issueConfirmToken(ctx, q, user.ID, meta.Binding, force)
		if err != nil {
			return err
		}
		if ok {
			mails = append(mails, s.confirmMail(user, raw))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	for _, m := range mails {
		s.sendAsync(m)
	}
	return email, nil
}

// ResendConfirmation повторно отправляет письмо с подтверждением. Ответ всегда одинаковый.
func (s *Service) ResendConfirmation(ctx context.Context, rawEmail string, meta Meta) error {
	return s.sendLink(ctx, rawEmail, meta, purposeConfirm, s.cfg.ConfirmTTL, func(u dbgen.User) bool { return u.EmailConfirmedAt == nil }, s.confirmMail)
}

// RequestPasswordReset отправляет ссылку для нового пароля. Ответ всегда одинаковый.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string, meta Meta) error {
	return s.sendLink(ctx, rawEmail, meta, purposeReset, s.cfg.ResetTTL, func(dbgen.User) bool { return true }, s.resetMail)
}

func (s *Service) sendLink(ctx context.Context, rawEmail string, meta Meta, purpose string, ttl time.Duration,
	eligible func(dbgen.User) bool, build func(dbgen.User, string) mail.Message) error {
	if err := s.allowMail(ctx, meta.IP); err != nil {
		return err
	}
	email, msg := NormalizeEmail(rawEmail)
	if msg != "" {
		return nil
	}
	user, err := s.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("auth: find user: %w", err)
	}
	if user.DeletedAt != nil || !eligible(user) {
		return nil
	}
	var m *mail.Message
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		var (
			raw string
			ok  bool
			err error
		)
		if purpose == purposeConfirm {
			raw, ok, err = s.issueConfirmToken(ctx, q, user.ID, meta.Binding, false)
		} else {
			raw, ok, err = s.issueToken(ctx, q, user.ID, purpose, ttl)
		}
		if err != nil {
			return err
		}
		if ok {
			built := build(user, raw)
			m = &built
		}
		return nil
	})
	if err != nil {
		return err
	}
	if m != nil {
		s.sendAsync(*m)
	}
	return nil
}

// unusablePasswordHash — настоящий хеш случайного пароля, который никто не знает: вход по нему невозможен, а проверка
// пароля работает как обычно (без ошибки «испорченный хеш»).
func (s *Service) unusablePasswordHash(ctx context.Context) (string, error) {
	raw, _ := newToken()
	h, err := s.hasher.Hash(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("auth: hash unusable password: %w", err)
	}
	return h, nil
}

// ConfirmedElsewhereError — ссылку подтверждения открыли не в том браузере, где шла регистрация. Почта подтверждена,
// но входа нет, а пароль, выбранный при регистрации, больше не годится (его мог выбрать тот, кто зарегистрировал чужую
// почту). ResetToken — ссылка «придумайте пароль», которую сайт сразу открывает; владелец почты задаёт пароль сам.
type ConfirmedElsewhereError struct{ ResetToken string }

func (*ConfirmedElsewhereError) Error() string { return "auth: email confirmed in another browser" }

// ConfirmEmail подтверждает почту по ссылке из письма. В браузере, где шла регистрация, сразу входит в аккаунт;
// в любом другом подтверждает почту, обнуляет пароль и отдаёт ссылку на выбор нового (*ConfirmedElsewhereError).
func (s *Service) ConfirmEmail(ctx context.Context, rawToken string, meta Meta) (User, Session, error) {
	var (
		user      dbgen.User
		sess      Session
		elsewhere *ConfirmedElsewhereError
		now       = s.now()
	)
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.ConsumeConfirmToken(ctx, dbgen.ConsumeConfirmTokenParams{Now: now, TokenHash: hashToken(rawToken)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("auth: consume token: %w", err)
		}
		userID := row.UserID
		before, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return fmt.Errorf("auth: load user: %w", err)
		}
		bound := len(row.BindingHash) > 0 && meta.Binding != "" &&
			subtle.ConstantTimeCompare(row.BindingHash, hashToken(meta.Binding)) == 1
		if !bound && before.EmailConfirmedAt != nil {
			// Почта уже подтверждена (например, сбросом пароля), а старую ссылку открыли в чужом браузере: ни входа, ни
			// обнуления пароля у живого аккаунта.
			return ErrInvalidToken
		}
		if err := q.ConfirmUserEmail(ctx, dbgen.ConfirmUserEmailParams{At: now, ID: userID}); err != nil {
			return fmt.Errorf("auth: confirm email: %w", err)
		}
		if !bound {
			// Пароль обнуляется: вместо него хеш от случайной строки, которую не знает никто. Новый пароль человек задаёт по
			// ссылке из ответа.
			unusable, err := s.unusablePasswordHash(ctx)
			if err != nil {
				return err
			}
			if err := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{ID: userID, PasswordHash: unusable, UpdatedAt: now}); err != nil {
				return fmt.Errorf("auth: wipe password: %w", err)
			}
			if err := q.DeleteUserSessions(ctx, userID); err != nil {
				return fmt.Errorf("auth: revoke sessions: %w", err)
			}
			raw, err := s.createToken(ctx, q, userID, purposeReset, s.cfg.ResetTTL, "")
			if err != nil {
				return err
			}
			elsewhere = &ConfirmedElsewhereError{ResetToken: raw}
			return audit.Record(ctx, q, audit.Event{Action: audit.AccountPasswordReset, Actor: userID, IP: meta.IP, Details: map[string]string{"via": "confirm_other_browser"}})
		}
		if user, err = q.GetUserByID(ctx, userID); err != nil {
			return fmt.Errorf("auth: load user: %w", err)
		}
		if sess, err = s.createSession(ctx, q, userID, meta, false); err != nil {
			return err
		}
		// Браузер, где подтвердили почту, сразу знакомый: письма «вход с нового устройства» о нём не будет.
		if sess.Device, _, err = s.rememberDevice(ctx, q, userID, meta.Device); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountLogin, Actor: userID, IP: meta.IP, Details: map[string]string{"via": "email_link"}})
	})
	if err != nil {
		return User{}, Session{}, err
	}
	if elsewhere != nil {
		return User{}, Session{}, elsewhere
	}
	return userFrom(user), sess, nil
}

// ---- вход и сессии ----

// createSession создаёт сессию. withPassword — человек только что ввёл пароль: сессия сразу «со свежим паролем».
func (s *Service) createSession(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, meta Meta, withPassword bool) (Session, error) {
	now := s.now()
	raw, hash := newToken()
	expires := now.Add(s.cfg.SessionTTL)
	var reauth *time.Time
	if withPassword {
		reauth = &now
	}
	if err := q.TouchUserActive(ctx, dbgen.TouchUserActiveParams{ID: userID, Now: now}); err != nil {
		return Session{}, fmt.Errorf("auth: touch user: %w", err)
	}
	if _, err := q.CreateSession(ctx, dbgen.CreateSessionParams{
		UserID: userID, TokenHash: hash, CreatedAt: now, ExpiresAt: expires,
		UserAgent: truncate(meta.UserAgent, 300), Ip: meta.IP, ReauthAt: reauth,
	}); err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return Session{Token: raw, ExpiresAt: expires}, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Login проверяет почту и пароль и создаёт сессию.
func (s *Service) Login(ctx context.Context, rawEmail, password string, meta Meta) (User, Session, error) {
	key := strings.ToLower(strings.TrimSpace(rawEmail))
	// Попытка записывается до проверки пароля: проверка и запись неделимы (AUT-1). Удачный вход вернёт её ниже.
	if err := s.reserve(ctx, kindLoginIP, httpguard.NetworkKey(meta.IP), s.cfg.Limits.LoginIP); err != nil {
		return User{}, Session{}, err
	}
	emailCounted := len(key) <= MaxEmailLen
	if emailCounted {
		if err := s.reserve(ctx, kindLoginEmail, key, s.cfg.Limits.LoginEmail); err != nil {
			_ = s.refund(ctx, kindLoginIP, httpguard.NetworkKey(meta.IP)) // отказ по почте не должен съедать лимит адреса
			return User{}, Session{}, err
		}
	}
	fail := func() (User, Session, error) {
		return User{}, Session{}, ErrInvalidCredentials
	}
	giveBack := func() error {
		if err := s.refund(ctx, kindLoginIP, httpguard.NetworkKey(meta.IP)); err != nil {
			return err
		}
		if emailCounted {
			return s.refund(ctx, kindLoginEmail, key)
		}
		return nil
	}
	// Слишком длинные поля не хешируем и не пишем в базу: это явно не настоящий вход.
	if len(key) > MaxEmailLen || len(password) > 4*MaxPasswordLen {
		return fail()
	}

	user, err := s.q.GetUserByEmail(ctx, key)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return User{}, Session{}, fmt.Errorf("auth: find user: %w", err)
	}
	// Стёртого аккаунта для входа нет, как и несуществующего.
	if errors.Is(err, pgx.ErrNoRows) || user.DeletedAt != nil {
		s.hasher.spendTime(ctx)
		return fail()
	}
	ok, err := s.hasher.Verify(ctx, password, user.PasswordHash)
	if err != nil {
		return User{}, Session{}, fmt.Errorf("auth: verify password: %w", err)
	}
	if !ok {
		return fail()
	}
	if user.EmailConfirmedAt == nil {
		if err := giveBack(); err != nil {
			return User{}, Session{}, err
		}
		return User{}, Session{}, ErrEmailNotConfirmed
	}

	var (
		sess      Session
		newDevice bool
	)
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := q.ClearRateEvents(ctx, dbgen.ClearRateEventsParams{Kind: kindLoginEmail, Key: key}); err != nil {
			return fmt.Errorf("auth: clear attempts: %w", err)
		}
		var err error
		if sess, err = s.createSession(ctx, q, user.ID, meta, true); err != nil {
			return err
		}
		if sess.Device, newDevice, err = s.rememberDevice(ctx, q, user.ID, meta.Device); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountLogin, Actor: user.ID, IP: meta.IP})
	})
	if err != nil {
		return User{}, Session{}, err
	}
	// Вход удался: адрес не должен терять попытки на чужих неудачах за одним NAT (почту очистила транзакция выше).
	if err := s.refund(ctx, kindLoginIP, httpguard.NetworkKey(meta.IP)); err != nil {
		return User{}, Session{}, err
	}
	if newDevice {
		s.sendAsync(s.newDeviceMail(user, meta, s.now()))
	}
	return userFrom(user), sess, nil
}

// Authenticate находит человека по токену из cookie. Просроченные сессии удаляются, живые продлеваются.
func (s *Service) Authenticate(ctx context.Context, rawToken string) (Principal, error) {
	if rawToken == "" {
		return Principal{}, ErrUnauthenticated
	}
	hash := hashToken(rawToken)
	row, err := s.q.GetSessionByTokenHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("auth: load session: %w", err)
	}
	now := s.now()
	// Абсолютный предел: украденная cookie не живёт вечно, пока ею пользуются (ASVS 3.3.2).
	if !row.ExpiresAt.After(now) || (s.cfg.SessionMaxAge > 0 && !row.SessionCreatedAt.Add(s.cfg.SessionMaxAge).After(now)) {
		if err := s.q.DeleteSessionByTokenHash(ctx, hash); err != nil {
			return Principal{}, fmt.Errorf("auth: delete expired session: %w", err)
		}
		return Principal{}, ErrUnauthenticated
	}
	p := Principal{
		SessionID: row.SessionID,
		ExpiresAt: row.ExpiresAt,
		User: User{
			ID: row.UserID, Email: row.Email, Name: row.DisplayName,
			EmailConfirmed: row.EmailConfirmedAt != nil, CreatedAt: row.UserCreatedAt,
			DeletionDueAt: row.DeletionDueAt,
		},
		Fresh: row.ReauthAt != nil && !row.ReauthAt.After(now) && now.Sub(*row.ReauthAt) < s.cfg.ReauthTTL,
	}
	if now.Sub(row.LastSeenAt) >= s.cfg.SessionRefresh {
		p.ExpiresAt = now.Add(s.cfg.SessionTTL)
		if limit := row.SessionCreatedAt.Add(s.cfg.SessionMaxAge); s.cfg.SessionMaxAge > 0 && p.ExpiresAt.After(limit) {
			p.ExpiresAt = limit
		}
		if err := s.q.TouchSession(ctx, dbgen.TouchSessionParams{ID: row.SessionID, LastSeenAt: now, ExpiresAt: p.ExpiresAt}); err != nil {
			return Principal{}, fmt.Errorf("auth: refresh session: %w", err)
		}
		if err := s.q.TouchUserActive(ctx, dbgen.TouchUserActiveParams{ID: row.UserID, Now: now}); err != nil {
			return Principal{}, fmt.Errorf("auth: touch user: %w", err)
		}
		p.Refreshed = true
	}
	return p, nil
}

// Logout завершает сессию. Если такой сессии нет, ничего не происходит.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if err := s.q.DeleteSessionByTokenHash(ctx, hashToken(rawToken)); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

// RevokeOtherSessions завершает все сессии человека, кроме текущей.
func (s *Service) RevokeOtherSessions(ctx context.Context, p Principal) error {
	if err := s.q.DeleteOtherUserSessions(ctx, dbgen.DeleteOtherUserSessionsParams{UserID: p.User.ID, ID: p.SessionID}); err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	// «Выйти на других устройствах» гасит и ожидающую смену почты: её мог попросить тот, кто сидел в другом сеансе.
	if err := s.q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: s.now(), UserID: p.User.ID, Purpose: purposeChangeEmail}); err != nil {
		return fmt.Errorf("auth: cancel email change: %w", err)
	}
	return nil
}

// ---- пароль и профиль ----

// ResetPassword задаёт новый пароль по ссылке из письма. Ссылка сгорает, все сессии завершаются.
func (s *Service) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	now := s.now()
	hash := hashToken(rawToken)
	peek, err := s.q.PeekAuthToken(ctx, dbgen.PeekAuthTokenParams{TokenHash: hash, Purpose: purposeReset, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("auth: peek token: %w", err)
	}
	if msg := ValidatePassword(newPassword, peek.Email); msg != "" {
		return newValidation("password", msg)
	}
	newHash, err := s.hasher.Hash(ctx, newPassword)
	if err != nil {
		return err
	}
	var user dbgen.User
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		userID, err := q.ConsumeAuthToken(ctx, dbgen.ConsumeAuthTokenParams{Now: now, TokenHash: hash, Purpose: purposeReset})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("auth: consume token: %w", err)
		}
		if err := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{ID: userID, PasswordHash: newHash, UpdatedAt: now}); err != nil {
			return fmt.Errorf("auth: update password: %w", err)
		}
		// Письмо со ссылкой дошло до владельца почты: значит, почта его.
		if err := q.ConfirmUserEmail(ctx, dbgen.ConfirmUserEmailParams{At: now, ID: userID}); err != nil {
			return fmt.Errorf("auth: confirm email: %w", err)
		}
		if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: userID, Purpose: purposeChangeEmail}); err != nil {
			return fmt.Errorf("auth: cancel email change: %w", err)
		}
		// Почта подтверждена этим сбросом: невостребованная ссылка подтверждения больше не нужна.
		if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: userID, Purpose: purposeConfirm}); err != nil {
			return fmt.Errorf("auth: cancel confirmation link: %w", err)
		}
		if err := q.DeleteUserSessions(ctx, userID); err != nil {
			return fmt.Errorf("auth: revoke sessions: %w", err)
		}
		if err := q.ClearRateEvents(ctx, dbgen.ClearRateEventsParams{Kind: kindLoginEmail, Key: peek.Email}); err != nil {
			return fmt.Errorf("auth: clear attempts: %w", err)
		}
		if user, err = q.GetUserByID(ctx, userID); err != nil {
			return fmt.Errorf("auth: load user: %w", err)
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountPasswordReset, Actor: userID})
	})
	if err != nil {
		return err
	}
	s.sendAsync(s.passwordChangedMail(user))
	return nil
}

// ChangePassword меняет пароль в настройках. Нужен текущий пароль; другие сессии завершаются.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	if err := s.reserve(ctx, kindPasswordUser, p.User.ID.String(), s.cfg.Limits.PasswordUser); err != nil {
		return err
	}
	user, err := s.q.GetUserByID(ctx, p.User.ID)
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	fields := map[string]string{}
	if msg := ValidatePassword(next, user.Email); msg != "" {
		fields["new_password"] = msg
	} else if next == current {
		fields["new_password"] = "Новый пароль совпадает со старым. Придумайте другой"
	}
	// Новый пароль хешируем до проверки старого: если хешировать нечем (перегрузка), ошибка возвращается сразу.
	var newHash string
	if fields["new_password"] == "" {
		if newHash, err = s.hasher.Hash(ctx, next); err != nil {
			return err
		}
	}
	ok := false
	if len(current) <= 4*MaxPasswordLen {
		if ok, err = s.hasher.Verify(ctx, current, user.PasswordHash); err != nil {
			return fmt.Errorf("auth: verify password: %w", err)
		}
	}
	if !ok {
		fields["current_password"] = "Текущий пароль указан неверно"
	} else if err := s.refund(ctx, kindPasswordUser, p.User.ID.String()); err != nil {
		return err
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{ID: user.ID, PasswordHash: newHash, UpdatedAt: s.now()}); err != nil {
			return fmt.Errorf("auth: update password: %w", err)
		}
		// Ожидающая смена почты гаснет вместе с паролем: тот, кто её просил, мог держать чужой сеанс (срез 29, AUT-8).
		if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: s.now(), UserID: user.ID, Purpose: purposeChangeEmail}); err != nil {
			return fmt.Errorf("auth: cancel email change: %w", err)
		}
		if err := q.DeleteOtherUserSessions(ctx, dbgen.DeleteOtherUserSessionsParams{UserID: user.ID, ID: p.SessionID}); err != nil {
			return fmt.Errorf("auth: revoke sessions: %w", err)
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountPasswordChanged, Actor: user.ID})
	})
	if err != nil {
		return err
	}
	s.sendAsync(s.passwordChangedMail(user))
	return nil
}

// UpdateName меняет имя человека.
func (s *Service) UpdateName(ctx context.Context, p Principal, rawName string) (User, error) {
	name, msg := NormalizeName(rawName)
	if msg != "" {
		return User{}, newValidation("name", msg)
	}
	user, err := s.q.UpdateUserName(ctx, dbgen.UpdateUserNameParams{ID: p.User.ID, DisplayName: name, UpdatedAt: s.now()})
	if err != nil {
		return User{}, fmt.Errorf("auth: update name: %w", err)
	}
	return userFrom(user), nil
}

// ---- уборка ----

// Cleanup удаляет просроченные сессии, ссылки и старые счётчики попыток.
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.now()
	if err := s.q.DeleteExpiredSessions(ctx, now); err != nil {
		return fmt.Errorf("auth: cleanup sessions: %w", err)
	}
	if err := s.q.DeleteExpiredAuthTokens(ctx, now.Add(-24*time.Hour)); err != nil {
		return fmt.Errorf("auth: cleanup tokens: %w", err)
	}
	if err := s.q.DeleteOldRateEvents(ctx, now.Add(-24*time.Hour)); err != nil {
		return fmt.Errorf("auth: cleanup rate events: %w", err)
	}
	if err := s.q.DeleteOldKnownDevices(ctx, now.Add(-s.cfg.DeviceTTL)); err != nil {
		return fmt.Errorf("auth: cleanup known devices: %w", err)
	}
	if _, _, err := s.ProcessInactive(ctx); err != nil {
		return err
	}
	if _, err := s.ProcessDueDeletions(ctx); err != nil {
		return err
	}
	return nil
}

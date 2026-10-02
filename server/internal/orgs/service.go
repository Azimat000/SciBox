// Package orgs — организации, подразделения, сотрудники и приглашения (срез 4).
//
// Критичная зона (docs/TESTING.md): здесь выдаются и забираются права на вакансии и отклики.
// Все проверки «можно ли» делает пакет access; сервис только собирает сведения о человеке (роль,
// подразделения) и спрашивает его.
package orgs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/mail"
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	ErrNotFound          = errors.New("orgs: not found")
	ErrForbidden         = errors.New("orgs: forbidden")
	ErrLastOwner         = errors.New("orgs: the last owner cannot leave or be demoted")
	ErrAlreadyMember     = errors.New("orgs: already a member")
	ErrInvalidInvitation = errors.New("orgs: invalid or expired invitation")
	ErrWrongEmail        = errors.New("orgs: invitation was sent to another address")
)

// Виды счётчиков частоты (таблица rate_events общая с аккаунтами).
const (
	kindOrgCreate  = "org_create"
	kindOrgInvite  = "org_invite"
	inviteTTLHours = 7 * 24

	// maxSlugTries — сколько раз пробуем «название», «название-2», «название-3»… прежде чем взять случайный хвост.
	maxSlugTries = 20
)

// errNoRow — запрос ничего не вернул (адрес занят).
var errNoRow = pgx.ErrNoRows

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	ProductName string
	PublicURL   string        // откуда открывается сайт: из него строятся ссылки в письмах
	InviteTTL   time.Duration // сколько живёт приглашение
	Create      Limit         // создание организаций одним человеком
	Invite      Limit         // письма-приглашения от одного человека
}

// DefaultConfig — боевые настройки.
func DefaultConfig(productName, publicURL string) Config {
	return Config{
		ProductName: productName,
		PublicURL:   strings.TrimRight(publicURL, "/"),
		InviteTTL:   inviteTTLHours * time.Hour,
		Create:      Limit{Max: 5, Window: 24 * time.Hour},
		Invite:      Limit{Max: 30, Window: time.Hour},
	}
}

// DB — то, что сервису нужно от базы: запросы и транзакции (так же, как в пакете auth).
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика организаций.
type Service struct {
	db     DB
	q      *dbgen.Queries
	mailer mail.Sender
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	wg     sync.WaitGroup
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, mailer mail.Sender, cfg Config, logger *slog.Logger) *Service {
	return newService(pool, mailer, cfg, logger)
}

func newService(db DB, mailer mail.Sender, cfg Config, logger *slog.Logger) *Service {
	return &Service{
		db:     db,
		q:      dbgen.New(db),
		mailer: mailer,
		cfg:    cfg,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Flush ждёт, пока отправятся все письма, поставленные в очередь.
func (s *Service) Flush() { s.wg.Wait() }

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("orgs: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("orgs: commit: %w", err)
	}
	return nil
}

// sendAsync отправляет письмо в фоне; сбой отправки пишется в журнал (без адреса получателя).
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

// ---- ограничение частоты ----

// spend учитывает событие вида kind по ключу key и отказывает, если лимит уже исчерпан.
func (s *Service) spend(ctx context.Context, q *dbgen.Queries, kind string, key uuid.UUID, lim Limit) error {
	now := s.now()
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kind, Key: key.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("orgs: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kind, Key: key.String(), At: now}); err != nil {
		return fmt.Errorf("orgs: record rate event: %w", err)
	}
	return nil
}

// ---- кто есть кто ----

// Viewer — что человек может в этой организации. Нужен интерфейсу, чтобы показать нужные кнопки;
// сервер всё равно перепроверяет каждое действие.
type Viewer struct {
	Role                string      `json:"role"`
	CanEditOrganization bool        `json:"can_edit_organization"`
	CanManageMembers    bool        `json:"can_manage_members"`
	CanManageUnits      bool        `json:"can_manage_units"`
	EditableUnits       []uuid.UUID `json:"editable_units"`
}

func (s *Service) loadOrg(ctx context.Context, q *dbgen.Queries, slug string) (dbgen.Organization, error) {
	org, err := q.GetOrganizationBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Organization{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Organization{}, fmt.Errorf("orgs: load organization: %w", err)
	}
	return org, nil
}

// actorOf собирает права человека в организации. Не сотрудник — нулевой Actor (ему ничего нельзя).
func (s *Service) actorOf(ctx context.Context, q *dbgen.Queries, orgID, userID uuid.UUID) (access.Actor, error) {
	m, err := q.GetMember(ctx, dbgen.GetMemberParams{OrgID: orgID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return access.Actor{}, nil
	}
	if err != nil {
		return access.Actor{}, fmt.Errorf("orgs: load member: %w", err)
	}
	actor := access.Actor{Role: access.Role(m.Role)}
	if actor.Role == access.RoleUnitHead {
		if actor.HeadOf, err = q.ListUnitIDsHeadedBy(ctx, dbgen.ListUnitIDsHeadedByParams{OrgID: orgID, HeadUserID: &userID}); err != nil {
			return access.Actor{}, fmt.Errorf("orgs: load headed units: %w", err)
		}
	}
	return actor, nil
}

// viewerOf описывает, что может человек; nil для того, кто не вошёл. Для вошедшего не сотрудника все права ложны.
func (s *Service) viewerOf(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, user *auth.User, units []dbgen.ListUnitsRow) (*Viewer, error) {
	if user == nil {
		return nil, nil
	}
	actor, err := s.actorOf(ctx, q, orgID, user.ID)
	if err != nil {
		return nil, err
	}
	v := &Viewer{
		Role:                string(actor.Role),
		CanEditOrganization: actor.Can(access.EditOrganization, access.NoUnit),
		CanManageMembers:    actor.Can(access.ManageMembers, access.NoUnit),
		CanManageUnits:      actor.Can(access.ManageUnits, access.NoUnit),
		EditableUnits:       []uuid.UUID{},
	}
	for _, u := range units {
		if actor.Can(access.EditUnit, u.ID) {
			v.EditableUnits = append(v.EditableUnits, u.ID)
		}
	}
	return v, nil
}

// require загружает организацию и проверяет право perm у человека. Не сотрудник и сотрудник без права
// получают одинаковый отказ: организация публична, поэтому «нет такой» не нужно.
func (s *Service) require(ctx context.Context, q *dbgen.Queries, slug string, user auth.User, perm access.Permission, unit uuid.UUID) (dbgen.Organization, access.Actor, error) {
	org, err := s.loadOrg(ctx, q, slug)
	if err != nil {
		return dbgen.Organization{}, access.Actor{}, err
	}
	actor, err := s.actorOf(ctx, q, org.ID, user.ID)
	if err != nil {
		return dbgen.Organization{}, access.Actor{}, err
	}
	if !actor.Can(perm, unit) {
		return dbgen.Organization{}, access.Actor{}, ErrForbidden
	}
	return org, actor, nil
}

// ---- уборка ----

// Cleanup удаляет приглашения, срок которых вышел больше 30 дней назад.
func (s *Service) Cleanup(ctx context.Context) error {
	if err := s.q.DeleteOldInvitations(ctx, s.now().Add(-30*24*time.Hour)); err != nil {
		return fmt.Errorf("orgs: cleanup invitations: %w", err)
	}
	return nil
}

// RunCleanup чистит базу раз в interval, пока не отменён ctx.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("orgs cleanup", "err", err)
			}
		}
	}
}

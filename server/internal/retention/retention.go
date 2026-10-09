// Package retention — сроки хранения персональных данных (срез 22, T-19, T-35, D-151, D-152).
//
// Критичная зона (docs/TESTING.md): ошибка здесь стирает живые данные или оставляет персональные данные навсегда.
// Раз в час проход удаляет всё, что пережило свой срок: уведомления на сайте, записи журнала действий, адреса в
// записях журнала и регистрации, которые так и не подтвердили почту. Остальные сроки (сеансы, ссылки, счётчики,
// знакомые браузеры, очередь писем, приглашения в организации) стоят рядом с кодом, который создаёт эти данные.
//
// Правила, на которых держится безопасность чистки:
//   - срок в настройке никогда не значит «без срока» и никогда не опускается ниже пола (Policy.Normalized): нулевое
//     или отрицательное значение заменяется обычным, слишком малое поднимается до пола;
//   - журнал действий дополнительно защищён триггером базы: он удаляет только записи старше 90 дней, как бы ни была
//     настроена чистка (миграция 00018);
//   - чистка идёт партиями: одна партия — одна транзакция, не больше Batch строк, поэтому ни долгих блокировок, ни
//     огромных транзакций;
//   - каждый вид чистки берёт блокировку базы на время партии: две копии сервера не дублируют работу.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
)

// Policy — сколько что хранится.
type Policy struct {
	// ReadNotifications — прочитанные уведомления на сайте (D-151).
	ReadNotifications time.Duration
	// UnreadNotifications — непрочитанные уведомления (D-151).
	UnreadNotifications time.Duration
	// AuditLogYears — записи журнала действий, в годах (D-151).
	AuditLogYears int
	// AuditAddress — через сколько записи журнала теряют адрес человека (D-152).
	AuditAddress time.Duration
	// UnconfirmedAccounts — через сколько после последней активности стирается регистрация без подтверждённой почты и без живой ссылки (D-152).
	UnconfirmedAccounts time.Duration
}

const day = 24 * time.Hour

// DefaultPolicy — решения пользователя (D-151) и техническое решение Claude (D-152).
func DefaultPolicy() Policy {
	return Policy{
		ReadNotifications:   90 * day,
		UnreadNotifications: 365 * day,
		AuditLogYears:       3,
		AuditAddress:        180 * day,
		UnconfirmedAccounts: 7 * day,
	}
}

// floors — самые короткие сроки, ниже которых настройка не опускается.
func floors() Policy {
	return Policy{
		ReadNotifications:   7 * day,
		UnreadNotifications: 30 * day,
		AuditLogYears:       1,
		AuditAddress:        30 * day,
		UnconfirmedAccounts: 2 * day, // ссылка подтверждения живёт 48 часов
	}
}

// Normalized заменяет пустые и отрицательные значения обычными, а слишком малые поднимает до пола.
func (p Policy) Normalized() Policy {
	def, low := DefaultPolicy(), floors()
	return Policy{
		ReadNotifications:   atLeast(p.ReadNotifications, def.ReadNotifications, low.ReadNotifications),
		UnreadNotifications: atLeast(p.UnreadNotifications, def.UnreadNotifications, low.UnreadNotifications),
		AuditLogYears:       int(atLeast(time.Duration(p.AuditLogYears), time.Duration(def.AuditLogYears), time.Duration(low.AuditLogYears))),
		AuditAddress:        atLeast(p.AuditAddress, def.AuditAddress, low.AuditAddress),
		UnconfirmedAccounts: atLeast(p.UnconfirmedAccounts, def.UnconfirmedAccounts, low.UnconfirmedAccounts),
	}
}

func atLeast(v, def, floor time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return max(v, floor)
}

// Config — настройки сервиса.
type Config struct {
	Policy Policy
	// Batch — сколько строк удаляется за одну транзакцию.
	Batch int32
	// MaxBatches — сколько партий одного вида берёт один проход; остальное доберёт следующий.
	MaxBatches int
}

// DefaultConfig — обычные настройки.
func DefaultConfig() Config { return Config{Policy: DefaultPolicy(), Batch: 1000, MaxBatches: 200} }

// DB — то, что сервису нужно от базы. Этому интерфейсу отвечает *pgxpool.Pool; тесты подставляют «ломающуюся» базу.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — чистка по срокам.
type Service struct {
	db     DB
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
}

// NewService собирает сервис.
func NewService(db DB, cfg Config, logger *slog.Logger) *Service {
	return newService(db, cfg, logger, time.Now)
}

func newService(db DB, cfg Config, logger *slog.Logger, now func() time.Time) *Service {
	cfg.Policy = cfg.Policy.Normalized()
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultConfig().Batch
	}
	if cfg.MaxBatches <= 0 {
		cfg.MaxBatches = DefaultConfig().MaxBatches
	}
	return &Service{db: db, cfg: cfg, logger: logger, now: now}
}

// Report — сколько строк удалил каждый вид чистки за проход.
type Report struct {
	ReadNotifications   int64
	UnreadNotifications int64
	AuditLog            int64
	AuditAddresses      int64
	UnconfirmedAccounts int64
}

// Total — всего удалённых или очищенных строк.
func (r Report) Total() int64 {
	return r.ReadNotifications + r.UnreadNotifications + r.AuditLog + r.AuditAddresses + r.UnconfirmedAccounts
}

// task — один вид чистки: один шаг удаляет не больше batch строк и сообщает, сколько удалил.
type task struct {
	name string
	// lockKey — ключ рекомендательной блокировки базы.
	lockKey int64
	step    func(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error)
	into    func(r *Report) *int64
}

// cutoffs — границы возраста на момент прохода: всё, что старше, удаляется.
type cutoffs struct {
	now                 time.Time
	readNotifications   time.Time
	unreadNotifications time.Time
	auditLog            time.Time
	auditAddress        time.Time
	unconfirmedAccounts time.Time
}

func (s *Service) cutoffs() cutoffs {
	now, p := s.now(), s.cfg.Policy
	return cutoffs{
		now:                 now,
		readNotifications:   now.Add(-p.ReadNotifications),
		unreadNotifications: now.Add(-p.UnreadNotifications),
		auditLog:            now.AddDate(-p.AuditLogYears, 0, 0),
		auditAddress:        now.Add(-p.AuditAddress),
		unconfirmedAccounts: now.Add(-p.UnconfirmedAccounts),
	}
}

// Ключи блокировок: «scibox» в шестнадцатеричной записи и номер вида.
const lockBase int64 = 0x5c1b0c0000

var tasks = []task{
	{"read notifications", lockBase + 1, func(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error) {
		return q.PurgeReadNotifications(ctx, dbgen.PurgeReadNotificationsParams{Cutoff: at.readNotifications, MaxRows: batch})
	}, func(r *Report) *int64 { return &r.ReadNotifications }},
	{"unread notifications", lockBase + 2, func(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error) {
		return q.PurgeUnreadNotifications(ctx, dbgen.PurgeUnreadNotificationsParams{Cutoff: at.unreadNotifications, MaxRows: batch})
	}, func(r *Report) *int64 { return &r.UnreadNotifications }},
	{"audit log", lockBase + 3, func(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error) {
		if err := q.AnnounceAuditPurge(ctx); err != nil {
			return 0, fmt.Errorf("announce purge: %w", err)
		}
		return q.PurgeAuditLog(ctx, dbgen.PurgeAuditLogParams{Cutoff: at.auditLog, MaxRows: batch})
	}, func(r *Report) *int64 { return &r.AuditLog }},
	{"audit addresses", lockBase + 4, func(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error) {
		return q.ScrubOldAuditAddresses(ctx, dbgen.ScrubOldAuditAddressesParams{Cutoff: at.auditAddress, MaxRows: batch})
	}, func(r *Report) *int64 { return &r.AuditAddresses }},
	{"unconfirmed accounts", lockBase + 5, purgeUnconfirmed, func(r *Report) *int64 { return &r.UnconfirmedAccounts }},
}

// purgeUnconfirmed стирает регистрации без подтверждённой почты и вместе с ними письма и счётчики попыток по их почте.
func purgeUnconfirmed(ctx context.Context, q *dbgen.Queries, at cutoffs, batch int32) (int64, error) {
	emails, err := q.PurgeUnconfirmedUsers(ctx, dbgen.PurgeUnconfirmedUsersParams{Cutoff: at.unconfirmedAccounts, Now: at.now, MaxRows: batch})
	if err != nil {
		return 0, err
	}
	for _, email := range emails {
		if _, err := q.PurgeOutboxTo(ctx, email); err != nil {
			return 0, fmt.Errorf("purge outbox: %w", err)
		}
		if _, err := q.PurgeRateEventsFor(ctx, email); err != nil {
			return 0, fmt.Errorf("purge rate events: %w", err)
		}
	}
	return int64(len(emails)), nil
}

// Pass выполняет один проход: все виды чистки, каждый партиями. Ошибка одного вида не останавливает остальные;
// все ошибки возвращаются вместе.
func (s *Service) Pass(ctx context.Context) (Report, error) {
	var (
		report Report
		errs   []error
	)
	at := s.cutoffs()
	for _, t := range tasks {
		total, err := s.runTask(ctx, t, at)
		*t.into(&report) = total
		if err != nil {
			errs = append(errs, fmt.Errorf("retention: %s: %w", t.name, err))
		}
	}
	return report, errors.Join(errs...)
}

// runTask гоняет партии одного вида, пока они полные (или не кончится лимит партий за проход).
func (s *Service) runTask(ctx context.Context, t task, at cutoffs) (int64, error) {
	var total int64
	for range s.cfg.MaxBatches {
		n, locked, err := s.batch(ctx, t, at)
		if err != nil {
			return total, err
		}
		if !locked {
			return total, nil // этим видом занята другая копия сервера
		}
		total += n
		if n < int64(s.cfg.Batch) {
			return total, nil
		}
	}
	return total, nil
}

// batch — одна партия в одной транзакции под блокировкой вида.
func (s *Service) batch(ctx context.Context, t task, at cutoffs) (n int64, locked bool, err error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)
	if locked, err = q.TryRetentionLock(ctx, t.lockKey); err != nil || !locked {
		return 0, false, err
	}
	if n, err = t.step(ctx, q, at, s.cfg.Batch); err != nil {
		return 0, true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, true, err
	}
	return n, true, nil
}

// Run гоняет проход раз в interval, пока не отменён ctx. Первый проход сразу: сервер, который простоял выключенным,
// наверстывает пропущенное.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		report, err := s.Pass(ctx)
		if err != nil && ctx.Err() == nil {
			s.logger.Error("retention pass failed", "err", err)
		}
		if report.Total() > 0 {
			s.logger.Info("retention pass", "read_notifications", report.ReadNotifications, "unread_notifications", report.UnreadNotifications,
				"audit_log", report.AuditLog, "audit_addresses", report.AuditAddresses, "unconfirmed_accounts", report.UnconfirmedAccounts)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

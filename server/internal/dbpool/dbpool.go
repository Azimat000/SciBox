// Package dbpool открывает пул соединений с PostgreSQL с настройками для работы под нагрузкой (срез 24, T-22):
// размер пула, срок жизни соединений, сроки для базы (statement_timeout, idle_in_transaction_session_timeout)
// и журнал медленных запросов. Пул один на процесс; каждая копия сервера держит свой, поэтому сумма MaxConns
// всех копий и фоновых процессов должна быть меньше max_connections базы (docs/SCALING.md).
package dbpool

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Значения по умолчанию. Для максимального числа соединений действует правило: две копии сервера и процесс фоновых
// задач по 20 соединений = 60 из 100 (max_connections по умолчанию), остаётся запас для миграций и администратора.
const (
	DefaultMaxConns         = 20
	DefaultMaxConnLifetime  = 30 * time.Minute
	DefaultMaxConnIdleTime  = 5 * time.Minute
	DefaultHealthCheck      = 30 * time.Second
	DefaultStatementTimeout = 10 * time.Second
	DefaultIdleInTxTimeout  = 30 * time.Second
	DefaultSlowQuery        = 200 * time.Millisecond
)

// CodeQueryCanceled — код PostgreSQL 57014: запрос остановлен (по statement_timeout или по просьбе клиента).
const CodeQueryCanceled = "57014"

// Config — настройки пула. Нулевое или отрицательное значение заменяется обычным: «без срока» не бывает (I-22 по духу).
// Срок, заданный в самой строке подключения (DATABASE_URL?statement_timeout=…), главнее настройки.
type Config struct {
	MaxConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	// StatementTimeout — сколько базе позволено работать над одним запросом.
	StatementTimeout time.Duration
	// IdleInTxTimeout — сколько транзакция может стоять без команд, прежде чем база оборвёт соединение и снимет блокировки.
	IdleInTxTimeout time.Duration
	// SlowQuery — запросы дольше этого срока попадают в журнал (только имя запроса и время, значения параметров — никогда).
	SlowQuery time.Duration
}

// Default — настройки по умолчанию.
func Default() Config { return Config{}.Normalized() }

// Normalized заменяет пустые и недопустимые значения обычными.
func (c Config) Normalized() Config {
	pos := func(d, def time.Duration) time.Duration {
		if d <= 0 {
			return def
		}
		return d
	}
	if c.MaxConns <= 0 {
		c.MaxConns = DefaultMaxConns
	}
	c.MaxConnLifetime = pos(c.MaxConnLifetime, DefaultMaxConnLifetime)
	c.MaxConnIdleTime = pos(c.MaxConnIdleTime, DefaultMaxConnIdleTime)
	c.HealthCheckPeriod = pos(c.HealthCheckPeriod, DefaultHealthCheck)
	c.StatementTimeout = pos(c.StatementTimeout, DefaultStatementTimeout)
	c.IdleInTxTimeout = pos(c.IdleInTxTimeout, DefaultIdleInTxTimeout)
	c.SlowQuery = pos(c.SlowQuery, DefaultSlowQuery)
	return c
}

// PoolConfig собирает настройки pgxpool из строки подключения.
func PoolConfig(databaseURL string, cfg Config, logger *slog.Logger) (*pgxpool.Config, error) {
	cfg = cfg.Normalized()
	pc, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = min(2, cfg.MaxConns)
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	// Соединения не должны устаревать все в одну минуту: разброс спасает от лавины переподключений.
	pc.MaxConnLifetimeJitter = cfg.MaxConnLifetime / 10
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	pc.HealthCheckPeriod = cfg.HealthCheckPeriod

	params := pc.ConnConfig.RuntimeParams // ParseConfig всегда создаёт таблицу
	setDefault := func(key, value string) {
		if _, ok := params[key]; !ok {
			params[key] = value
		}
	}
	setDefault("statement_timeout", strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10))
	setDefault("idle_in_transaction_session_timeout", strconv.FormatInt(cfg.IdleInTxTimeout.Milliseconds(), 10))
	setDefault("application_name", "scibox")
	// Поисковые запросы универсальные: десяток необязательных условий и порядок приходят параметрами. Общий план, который
	// PostgreSQL заводит после пяти выполнений, не знает значений и не может ни воспользоваться индексом порядка, ни
	// отбросить пустые условия; план под конкретные значения стоит доли миллисекунды и в разы быстрее.
	setDefault("plan_cache_mode", "force_custom_plan")
	// Стандартная стоимость случайного чтения (4,0) рассчитана на вращающиеся диски и заставляет планировщик читать
	// тысячи строк подряд и сортировать вместо того, чтобы идти по индексу порядка и остановиться на нужной строке. У
	// всех облачных хостингов и ноутбуков диски твёрдотельные: рекомендация PostgreSQL для них 1,1.
	setDefault("random_page_cost", "1.1")

	if logger != nil {
		pc.ConnConfig.Tracer = &SlowQueryTracer{Logger: logger, Threshold: cfg.SlowQuery, Now: time.Now}
	}
	return pc, nil
}

// New открывает пул. Соединения устанавливаются по мере надобности: сервер стартует и при лежащей базе (/api/health ответит 503).
func New(ctx context.Context, databaseURL string, cfg Config, logger *slog.Logger) (*pgxpool.Pool, error) {
	pc, err := PoolConfig(databaseURL, cfg, logger)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	return pool, nil
}

// IsTimeout сообщает, что запрос остановила база по сроку (statement_timeout), а не человек, оборвавший запрос.
// Отмену запроса клиентом (context.Canceled) распознаёт вызывающий по контексту запроса.
func IsTimeout(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == CodeQueryCanceled && strings.Contains(pgErr.Message, "statement timeout")
}

// SlowQueryTracer пишет в журнал запросы, которые шли дольше порога или остановлены по сроку. В журнал попадают имя
// запроса из комментария sqlc (`-- name: ИмяЗапроса :many`), время и число строк, но не текст запроса целиком и не
// значения параметров: в них бывают почты и тексты людей.
type SlowQueryTracer struct {
	Logger    *slog.Logger
	Threshold time.Duration
	Now       func() time.Time
}

type queryStart struct {
	at   time.Time
	name string
}

type startKey struct{}

// TraceQueryStart запоминает, когда запрос начался.
func (t *SlowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, startKey{}, queryStart{at: t.Now(), name: QueryName(data.SQL)})
}

// TraceQueryEnd пишет строку в журнал, если запрос медленный или остановлен по сроку.
func (t *SlowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(startKey{}).(queryStart)
	if !ok {
		return
	}
	took := t.Now().Sub(start.at)
	switch {
	case IsTimeout(data.Err):
		t.Logger.Error("query stopped by statement_timeout", "query", start.name, "ms", took.Milliseconds())
	case took >= t.Threshold:
		t.Logger.Warn("slow query", "query", start.name, "ms", took.Milliseconds(), "rows", data.CommandTag.RowsAffected())
	}
}

// QueryName достаёт имя запроса из комментария sqlc; у запроса без имени берёт начало текста, как есть
// (запросы пишутся с параметрами, значений в тексте нет).
func QueryName(sql string) string {
	s := strings.TrimSpace(sql)
	if rest, ok := strings.CutPrefix(s, "-- name:"); ok {
		name, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
		return cmp.Or(name, "unnamed")
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return cmp.Or(s, "unnamed")
}

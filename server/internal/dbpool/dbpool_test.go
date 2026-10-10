package dbpool_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbpool"
	"scibox/server/internal/testdb"
)

func TestNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   dbpool.Config
		want dbpool.Config
	}{
		{"пусто: всё по умолчанию", dbpool.Config{}, dbpool.Config{
			MaxConns: dbpool.DefaultMaxConns, MaxConnLifetime: dbpool.DefaultMaxConnLifetime, MaxConnIdleTime: dbpool.DefaultMaxConnIdleTime,
			HealthCheckPeriod: dbpool.DefaultHealthCheck, StatementTimeout: dbpool.DefaultStatementTimeout,
			IdleInTxTimeout: dbpool.DefaultIdleInTxTimeout, SlowQuery: dbpool.DefaultSlowQuery}},
		{"отрицательные сроки не значат «без срока»", dbpool.Config{MaxConns: -1, StatementTimeout: -time.Second, IdleInTxTimeout: -1, SlowQuery: -5},
			dbpool.Config{MaxConns: dbpool.DefaultMaxConns, MaxConnLifetime: dbpool.DefaultMaxConnLifetime, MaxConnIdleTime: dbpool.DefaultMaxConnIdleTime,
				HealthCheckPeriod: dbpool.DefaultHealthCheck, StatementTimeout: dbpool.DefaultStatementTimeout,
				IdleInTxTimeout: dbpool.DefaultIdleInTxTimeout, SlowQuery: dbpool.DefaultSlowQuery}},
		{"заданное остаётся", dbpool.Config{MaxConns: 7, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute, HealthCheckPeriod: time.Second,
			StatementTimeout: 3 * time.Second, IdleInTxTimeout: 4 * time.Second, SlowQuery: 5 * time.Millisecond},
			dbpool.Config{MaxConns: 7, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute, HealthCheckPeriod: time.Second,
				StatementTimeout: 3 * time.Second, IdleInTxTimeout: 4 * time.Second, SlowQuery: 5 * time.Millisecond}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Normalized(); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if dbpool.Default() != (dbpool.Config{}).Normalized() {
		t.Fatal("Default must be the normalized empty config")
	}
}

func TestPoolConfig(t *testing.T) {
	t.Run("сроки и размер из настроек", func(t *testing.T) {
		pc, err := dbpool.PoolConfig("postgres://u:p@localhost:5433/db", dbpool.Config{MaxConns: 9, StatementTimeout: 3 * time.Second, IdleInTxTimeout: 4 * time.Second}, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		p := pc.ConnConfig.RuntimeParams
		if pc.MaxConns != 9 || pc.MinConns != 2 || p["statement_timeout"] != "3000" || p["idle_in_transaction_session_timeout"] != "4000" || p["application_name"] != "scibox" || p["plan_cache_mode"] != "force_custom_plan" || p["random_page_cost"] != "1.1" {
			t.Fatalf("pool: max=%d min=%d params=%v", pc.MaxConns, pc.MinConns, p)
		}
		if pc.MaxConnLifetime != dbpool.DefaultMaxConnLifetime || pc.MaxConnLifetimeJitter != dbpool.DefaultMaxConnLifetime/10 || pc.MaxConnIdleTime != dbpool.DefaultMaxConnIdleTime {
			t.Fatalf("lifetimes: %v %v %v", pc.MaxConnLifetime, pc.MaxConnLifetimeJitter, pc.MaxConnIdleTime)
		}
		if pc.ConnConfig.Tracer == nil {
			t.Fatal("slow query tracer is not installed")
		}
	})
	t.Run("срок из строки подключения главнее", func(t *testing.T) {
		pc, err := dbpool.PoolConfig("postgres://u:p@localhost:5433/db?statement_timeout=99&application_name=other", dbpool.Config{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := pc.ConnConfig.RuntimeParams
		if p["statement_timeout"] != "99" || p["application_name"] != "other" || p["idle_in_transaction_session_timeout"] != "30000" {
			t.Fatalf("params: %v", p)
		}
		if pc.ConnConfig.Tracer != nil {
			t.Fatal("tracer must not be installed without a logger")
		}
	})
	t.Run("пул из одного соединения", func(t *testing.T) {
		pc, err := dbpool.PoolConfig("postgres://u:p@localhost:5433/db", dbpool.Config{MaxConns: 1}, nil)
		if err != nil || pc.MinConns != 1 {
			t.Fatalf("min=%d err=%v", pc.MinConns, err)
		}
	})
	t.Run("строка подключения не разбирается: пароль в сообщение не попадает", func(t *testing.T) {
		_, err := dbpool.PoolConfig("postgres://user:secret-pass@host:bad/db", dbpool.Config{}, nil)
		if err == nil || strings.Contains(err.Error(), "secret-pass") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("New с плохим адресом", func(t *testing.T) {
		if _, err := dbpool.New(t.Context(), "::::", dbpool.Config{}, nil); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestSettingsReachTheDatabase(t *testing.T) {
	url := testdb.Create(t, false)
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{StatementTimeout: 3 * time.Second, IdleInTxTimeout: 7 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var statement, idle, app, plans, cost string
	if err := pool.QueryRow(t.Context(), `SELECT current_setting('statement_timeout'), current_setting('idle_in_transaction_session_timeout'), current_setting('application_name'), current_setting('plan_cache_mode'), current_setting('random_page_cost')`).Scan(&statement, &idle, &app, &plans, &cost); err != nil {
		t.Fatal(err)
	}
	if statement != "3s" || idle != "7s" || app != "scibox" || plans != "force_custom_plan" || cost != "1.1" {
		t.Fatalf("statement=%q idle=%q app=%q plans=%q cost=%q", statement, idle, app, plans, cost)
	}
}

func TestStatementTimeoutStopsALongQuery(t *testing.T) {
	url := testdb.Create(t, false)
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{StatementTimeout: 150 * time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	start := time.Now()
	_, err = pool.Exec(t.Context(), `SELECT pg_sleep(5)`)
	if !dbpool.IsTimeout(err) {
		t.Fatalf("want a statement timeout, got %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the query was not stopped in time: %v", took)
	}
	// Пул после остановленного запроса работает.
	var one int
	if err := pool.QueryRow(t.Context(), `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("pool after the timeout: %v", err)
	}
}

func TestClientCancelIsNotATimeout(t *testing.T) {
	url := testdb.Create(t, false)
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = pool.Exec(ctx, `SELECT pg_sleep(5)`)
	if err == nil || dbpool.IsTimeout(err) {
		t.Fatalf("client cancel must not look like a database timeout: %v", err)
	}
	if dbpool.IsTimeout(nil) || dbpool.IsTimeout(errors.New("other")) {
		t.Fatal("IsTimeout must be false for nil and for foreign errors")
	}
}

func TestIdleTransactionIsCut(t *testing.T) {
	url := testdb.Create(t, false)
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{IdleInTxTimeout: 200 * time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) }) // вернуть соединение в пул, иначе pool.Close ждёт его вечно
	if _, err := tx.Exec(t.Context(), `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond) // транзакция «забыта»: база должна её оборвать сама
	if _, err := tx.Exec(t.Context(), `SELECT 1`); err == nil {
		t.Fatal("a forgotten transaction must be cut by the database")
	}
}

func TestSlowQueriesAreLogged(t *testing.T) {
	url := testdb.Create(t, false)
	var buf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{SlowQuery: 20 * time.Millisecond, StatementTimeout: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(t.Context(), `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "slow query") {
		t.Fatalf("a fast query was logged: %s", buf.String())
	}
	secret := "ivanov@example.ru"
	if _, err := pool.Exec(t.Context(), "-- name: SleepyQuery :exec\nSELECT pg_sleep(0.1), $1::text", secret); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "slow query") || !strings.Contains(out, "query=SleepyQuery") {
		t.Fatalf("the slow query is not in the log: %q", out)
	}
	if strings.Contains(out, secret) || strings.Contains(out, "pg_sleep") {
		t.Fatalf("the log carries the value of a parameter or the query text: %q", out)
	}
	buf.Reset()
	if _, err := pool.Exec(t.Context(), "-- name: StuckQuery :exec\nSELECT pg_sleep(5)"); !dbpool.IsTimeout(err) {
		t.Fatalf("want a timeout: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "statement_timeout") || !strings.Contains(out, "query=StuckQuery") || !strings.Contains(out, "level=ERROR") {
		t.Fatalf("the stopped query is not in the log as an error: %q", out)
	}
}

func TestTracerWithoutStartIsHarmless(t *testing.T) {
	var buf lockedBuffer
	tr := &dbpool.SlowQueryTracer{Logger: slog.New(slog.NewTextHandler(&buf, nil)), Threshold: time.Millisecond, Now: time.Now}
	tr.TraceQueryEnd(t.Context(), nil, pgx.TraceQueryEndData{})
	if buf.String() != "" {
		t.Fatalf("log: %q", buf.String())
	}
}

func TestQueryName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"-- name: SearchVacancies :many\nSELECT 1", "SearchVacancies"},
		{"  -- name: GetUser :one", "GetUser"},
		{"-- name:", "unnamed"},
		{"SELECT   id,\n name FROM users WHERE id = $1", "SELECT id, name FROM users WHERE id = $1"},
		{strings.Repeat("ы", 100), strings.Repeat("ы", 60)},
		{"", "unnamed"},
	}
	for _, tc := range tests {
		if got := dbpool.QueryName(tc.in); got != tc.want {
			t.Errorf("QueryName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// lockedBuffer — буфер журнала, безопасный для записи из нескольких горутин.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

type queryEvent struct {
	name           string
	took           time.Duration
	slow, timedOut bool
}

type recorder struct {
	mu     sync.Mutex
	events []queryEvent
}

func (r *recorder) ObserveQuery(name string, took time.Duration, slow, timedOut bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, queryEvent{name, took, slow, timedOut})
}

func (r *recorder) last() queryEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[len(r.events)-1]
}

// Наблюдатель (метрики среза 26) получает каждый запрос: имя, время, «медленный», «остановлен по сроку». Журнал при
// этом может быть выключен вовсе: метрики не зависят от него.
func TestObserverSeesEveryQueryEvenWithoutALogger(t *testing.T) {
	url := testdb.Create(t, false)
	rec := &recorder{}
	pool, err := dbpool.New(t.Context(), url, dbpool.Config{SlowQuery: 20 * time.Millisecond, StatementTimeout: time.Second, Observer: rec}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(t.Context(), "-- name: QuickQuery :exec\nSELECT 1"); err != nil {
		t.Fatal(err)
	}
	if e := rec.last(); e.name != "QuickQuery" || e.slow || e.timedOut {
		t.Errorf("quick: %+v", e)
	}
	if _, err := pool.Exec(t.Context(), "-- name: SleepyQuery :exec\nSELECT pg_sleep(0.1)"); err != nil {
		t.Fatal(err)
	}
	if e := rec.last(); e.name != "SleepyQuery" || !e.slow || e.timedOut || e.took < 100*time.Millisecond {
		t.Errorf("slow: %+v", e)
	}
	if _, err := pool.Exec(t.Context(), "-- name: StuckQuery :exec\nSELECT pg_sleep(5)"); !dbpool.IsTimeout(err) {
		t.Fatalf("want a timeout: %v", err)
	}
	if e := rec.last(); e.name != "StuckQuery" || !e.timedOut {
		t.Errorf("timeout: %+v", e)
	}
}

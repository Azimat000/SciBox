package testdb

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeTB перехватывает Fatalf, чтобы проверить ветки ошибок.
type fakeTB struct {
	fatal    string
	cleanups []func()
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatal = fmt.Sprintf(format, args...)
	panic(f)
}
func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func expectFatal(t *testing.T, fn func(*fakeTB)) string {
	t.Helper()
	ftb := &fakeTB{}
	func() {
		defer func() {
			if r := recover(); r != nil && r != ftb {
				panic(r)
			}
		}()
		fn(ftb)
	}()
	ftb.runCleanups()
	if ftb.fatal == "" {
		t.Fatal("expected Fatalf")
	}
	return ftb.fatal
}

func databaseExists(t *testing.T, name string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, AdminURL())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestNewCreatesMigratedDatabaseAndDropsIt(t *testing.T) {
	ftb := &fakeTB{}
	pool := New(ftb)
	var name string
	var exts int
	ctx := context.Background()
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_extension WHERE extname IN ('citext','pg_trgm')").Scan(&exts); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "scibox_t_") || exts != 2 {
		t.Fatalf("db %q with %d extensions", name, exts)
	}
	ftb.runCleanups()
	if databaseExists(t, name) {
		t.Fatalf("database %q not dropped", name)
	}
}

func TestAdminURLFromEnv(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "postgres://other/db")
	if AdminURL() != "postgres://other/db" {
		t.Fatal("TEST_DATABASE_URL ignored")
	}
}

func TestCreateFailures(t *testing.T) {
	t.Run("unreachable server", func(t *testing.T) {
		t.Setenv("TEST_DATABASE_URL", "postgres://x:y@127.0.0.1:1/postgres?connect_timeout=1")
		msg := expectFatal(t, func(f *fakeTB) { Create(f, false) })
		if !strings.Contains(msg, "connect admin database") || !strings.Contains(msg, "make db-up") {
			t.Fatalf("fatal = %q", msg)
		}
	})
	t.Run("random source broken", func(t *testing.T) {
		prev := randSource
		randSource = strings.NewReader("")
		defer func() { randSource = prev }()
		msg := expectFatal(t, func(f *fakeTB) { Create(f, false) })
		if !strings.Contains(msg, "random name") {
			t.Fatalf("fatal = %q", msg)
		}
	})
	t.Run("duplicate name", func(t *testing.T) {
		prev := randSource
		defer func() { randSource = prev }()
		randSource = strings.NewReader("\x01\x02\x03\x04\x05\x06\x01\x02\x03\x04\x05\x06")
		first := &fakeTB{}
		Create(first, false)
		defer first.runCleanups()
		msg := expectFatal(t, func(f *fakeTB) { Create(f, false) })
		if !strings.Contains(msg, "create database") {
			t.Fatalf("fatal = %q", msg)
		}
	})
}

func TestWithDatabase(t *testing.T) {
	got, err := withDatabase("postgres://u:p@h:5433/postgres?sslmode=disable", "abc")
	if err != nil || got != "postgres://u:p@h:5433/abc?sslmode=disable" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := withDatabase("postgres://u:p@h:bad port/x", "abc"); err == nil {
		t.Fatal("want parse error")
	}
}

func TestCreateWithBadAdminURLPath(t *testing.T) {
	// create с некорректным адресом падает на подключении, а не позже.
	_, err := create(context.Background(), "::bad::", false, func(func()) {})
	if err == nil || !strings.Contains(err.Error(), "connect admin database") {
		t.Fatalf("err = %v", err)
	}
}

func TestDropIgnoresUnreachableServer(t *testing.T) {
	if err := drop("postgres://x:y@127.0.0.1:1/postgres?connect_timeout=1", "whatever"); err == nil {
		t.Fatal("want connection error")
	}
}

package migrate_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"scibox/server/internal/migrate"
	"scibox/server/internal/testdb"
)

func extensions(t *testing.T, dbURL string) []string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT extname FROM pg_extension WHERE extname IN ('citext','pg_trgm') ORDER BY extname`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// appTables возвращает таблицы приложения (без служебной таблицы goose).
func appTables(t *testing.T, dbURL string) []string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'goose_db_version' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// Правило docs/TESTING.md: накатить на пустую базу → откатить → накатить снова.
func TestUpDownUp(t *testing.T) {
	ctx := context.Background()
	dbURL := testdb.Create(t, false)
	var out bytes.Buffer

	accountTables := []string{"auth_tokens", "rate_events", "sessions", "users"}
	orgTables := []string{"auth_tokens", "org_invitations", "org_members", "organizations", "rate_events", "sessions", "units", "users"}
	referenceTables := []string{"auth_tokens", "org_invitations", "org_members", "organizations", "positions", "rate_events", "reference_sources",
		"regions", "science_fields", "science_groups", "sessions", "specialties", "units", "users"}
	// vacancy_view — представление, но информационная схема перечисляет и его.
	allTables := []string{"auth_tokens", "org_invitations", "org_members", "organizations", "positions", "rate_events", "reference_sources",
		"regions", "science_fields", "science_groups", "sessions", "specialties", "units", "users", "vacancies", "vacancy_specialties", "vacancy_view"}
	steps := []struct {
		cmd        string
		wantOut    string
		wantExts   int
		wantTables []string
	}{
		{"status", "pending", 0, nil},
		{"up", "00005_vacancies.sql", 2, allTables},
		{"up", "no migrations to apply", 2, allTables},
		{"status", "applied", 2, allTables},
		{"down", "00005_vacancies.sql", 2, referenceTables},
		{"down", "00004_reference.sql", 2, orgTables},
		{"down", "00003_organizations.sql", 2, accountTables},
		{"down", "00002_accounts.sql", 2, nil},
		{"down", "00001_extensions.sql", 0, nil},
		{"up", "00005_vacancies.sql", 2, allTables},
		{"reset", "00001_extensions.sql", 0, nil},
		{"reset", "no migrations to apply", 0, nil},
		{"up", "00001_extensions.sql", 2, allTables},
	}
	for i, s := range steps {
		out.Reset()
		if err := migrate.Command(ctx, dbURL, s.cmd, &out); err != nil {
			t.Fatalf("step %d %s: %v", i, s.cmd, err)
		}
		if !strings.Contains(out.String(), s.wantOut) {
			t.Fatalf("step %d %s: output %q, want %q", i, s.cmd, out.String(), s.wantOut)
		}
		if got := len(extensions(t, dbURL)); got != s.wantExts {
			t.Fatalf("step %d %s: %d extensions, want %d", i, s.cmd, got, s.wantExts)
		}
		if got := appTables(t, dbURL); strings.Join(got, ",") != strings.Join(s.wantTables, ",") {
			t.Fatalf("step %d %s: tables %v, want %v", i, s.cmd, got, s.wantTables)
		}
	}
}

func TestDownOnEmptyDatabase(t *testing.T) {
	dbURL := testdb.Create(t, false)
	err := migrate.Command(context.Background(), dbURL, "down", &bytes.Buffer{})
	if !errors.Is(err, goose.ErrNoNextVersion) {
		t.Fatalf("err = %v, want ErrNoNextVersion", err)
	}
}

func TestCommandErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, url, cmd, want string
	}{
		{"unknown command", "postgres://localhost/x", "sideways", "unknown migrate command"},
		{"bad url", "::not a url::", "up", "parse database url"},
		{"unreachable db status", "postgres://nobody:x@127.0.0.1:1/none?connect_timeout=1", "status", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := migrate.Command(ctx, tc.url, tc.cmd, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestNewProviderWithoutMigrations(t *testing.T) {
	db := stdlib.OpenDB(pgx.ConnConfig{})
	defer db.Close()
	_, err := migrate.NewProviderFS(db, fstest.MapFS{})
	if !errors.Is(err, goose.ErrNoMigrations) {
		t.Fatalf("err = %v, want ErrNoMigrations", err)
	}
	var _ *sql.DB = db
}

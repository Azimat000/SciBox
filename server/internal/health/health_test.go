package health_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/health"
	"scibox/server/internal/migrate"
	"scibox/server/internal/testdb"
)

func TestCheckDatabaseReal(t *testing.T) {
	pool := testdb.New(t)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { sqlDB.Close() })
	prov, err := migrate.NewProvider(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	got, err := health.Checker{Queries: dbgen.New(pool), Migrations: prov}.CheckDatabase(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion < 1 {
		t.Fatalf("schema version = %d, want >= 1", got.SchemaVersion)
	}
	if !strings.HasPrefix(got.ServerVersion, "16.") {
		t.Fatalf("server version = %q, want PostgreSQL 16", got.ServerVersion)
	}
}

type fakeQ struct{ err error }

func (f fakeQ) ServerVersion(context.Context) (string, error) { return "16.0", f.err }

type fakeV struct{ err error }

func (f fakeV) GetDBVersion(context.Context) (int64, error) { return 3, f.err }

func TestCheckDatabaseErrors(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		c    health.Checker
		want string
	}{
		{"query fails", health.Checker{Queries: fakeQ{err: boom}, Migrations: fakeV{}}, "query database"},
		{"version fails", health.Checker{Queries: fakeQ{}, Migrations: fakeV{err: boom}}, "read schema version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.c.CheckDatabase(context.Background())
			if !errors.Is(err, boom) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

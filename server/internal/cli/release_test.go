package cli

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"scibox/server/internal/config"
	"scibox/server/internal/testdb"
)

func TestReleaseMigratesAndLoadsTheJournalsAndIsRepeatable(t *testing.T) {
	env := map[string]string{"DATABASE_URL": testdb.Create(t, false)}
	first := run(context.Background(), []string{"release"}, env, nil)
	if first.code != 0 || !strings.Contains(first.stdout, "Справочник журналов загружен") {
		t.Fatalf("first release: code %d, stdout %q, stderr %q", first.code, first.stdout, first.stderr)
	}
	// Выкладку повторяют после сбоя: второй раз ничего не ломается и ничего не грузится заново.
	second := run(context.Background(), []string{"release"}, env, nil)
	if second.code != 0 || !strings.Contains(second.stdout, "no migrations to apply") || !strings.Contains(second.stdout, "уже загружен") {
		t.Fatalf("second release: code %d, stdout %q, stderr %q", second.code, second.stdout, second.stderr)
	}
}

func TestReleaseTakesNoArguments(t *testing.T) {
	r := run(context.Background(), []string{"release", "now"}, map[string]string{}, nil)
	if r.code != 2 || !strings.Contains(r.stderr, "Использование") {
		t.Fatalf("code %d, stderr %q", r.code, r.stderr)
	}
}

func TestReleaseReportsAMigrationFailure(t *testing.T) {
	r := run(context.Background(), []string{"release"}, map[string]string{"DATABASE_URL": "::bad"}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "release: migrate") {
		t.Fatalf("code %d, stderr %q", r.code, r.stderr)
	}
}

func TestProductionRefusesDestructiveCommands(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{"development may do anything", config.Config{Env: config.EnvDevelopment}, false},
		{"production refuses", config.Config{Env: config.EnvProduction}, true},
		{"production with the explicit permission", config.Config{Env: config.EnvProduction, AllowDestructive: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := refuseDestructive(tc.cfg, logger, "migrate reset"); got != tc.want {
				t.Fatalf("refused = %v, want %v", got, tc.want)
			}
		})
	}
}

// Через настоящий Run: боевые настройки допустимы, а команды, которые стирают базу, всё равно отказывают и не
// касаются базы (адрес несуществующей базы не открывается вовсе).
func TestProductionRunRefusesToWipeOrSeed(t *testing.T) {
	prod := func(extra map[string]string) map[string]string {
		env := map[string]string{
			"SCIBOX_ENV":           "production",
			"SCIBOX_PUBLIC_URL":    "https://scibox.example",
			"DATABASE_URL":         "postgres://app:Long-Random-Secret@localhost:5433/never_opened?sslmode=disable",
			"SCIBOX_SMTP_TLS":      "starttls",
			"SCIBOX_MAIL_FROM":     "SciBox <no-reply@scibox.example>",
			"SCIBOX_S3_ENDPOINT":   "storage.example",
			"SCIBOX_S3_TLS":        "on",
			"SCIBOX_S3_ACCESS_KEY": "real-access-key",
			"SCIBOX_S3_SECRET_KEY": "real-secret-key",
			"SCIBOX_CLAMAV_ADDR":   "clamav:3310",
		}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	for _, args := range [][]string{{"migrate", "reset"}, {"migrate", "down"}, {"seed"}} {
		r := run(context.Background(), args, prod(nil), nil)
		if r.code != 1 || !strings.Contains(r.stderr, "SCIBOX_ALLOW_DESTRUCTIVE") {
			t.Errorf("%v: code %d, stderr %q", args, r.code, r.stderr)
		}
	}
	// Разрешение доходит до команды: теперь она пытается открыть базу (несуществующую) и падает уже на этом.
	r := run(context.Background(), []string{"migrate", "reset"}, prod(map[string]string{"SCIBOX_ALLOW_DESTRUCTIVE": "on"}), nil)
	if r.code != 1 || strings.Contains(r.stderr, "refused in production") {
		t.Errorf("with the permission: code %d, stderr %q", r.code, r.stderr)
	}
}

package migrate_test

import (
	"context"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	dbfiles "scibox/server/db"
	"scibox/server/internal/migrate"
	"scibox/server/internal/testdb"
)

// Выкладка без простоя и откат версии (срез 28, D-186) держатся на правиле «расширить, потом сжать»: пока новая
// версия сервера накатывает миграцию, старая копия ещё работает с той же базой, а откат на старую версию оставляет
// схему новой. Поэтому миграция после 00024 в разделе Up может только добавлять; всё, что ломает работу старой
// версии (удалить или переименовать столбец и таблицу, сменить тип, сделать столбец обязательным без значения,
// добавить ограничение), помечается строкой `-- scibox:contract <причина>` и выкладывается отдельным выпуском,
// когда ни одна копия предыдущей версии уже не нужна. Тест проверяет файлы миграций, а не базу.

// lastUncheckedMigration — последняя миграция до введения правила; прежние файлы не проверяются.
const lastUncheckedMigration = 24

const contractMarker = "-- scibox:contract"

// breaking — что в разделе Up ломает работу предыдущей версии сервера.
var breaking = []struct {
	name string
	re   *regexp.Regexp
}{
	{"DROP TABLE", regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)},
	{"DROP COLUMN", regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`)},
	{"DROP VIEW", regexp.MustCompile(`(?i)\bDROP\s+(MATERIALIZED\s+)?VIEW\b`)},
	{"DROP TYPE", regexp.MustCompile(`(?i)\bDROP\s+TYPE\b`)},
	{"DROP CONSTRAINT", regexp.MustCompile(`(?i)\bDROP\s+CONSTRAINT\b`)},
	{"DROP FUNCTION", regexp.MustCompile(`(?i)\bDROP\s+FUNCTION\b`)},
	{"DROP TRIGGER", regexp.MustCompile(`(?i)\bDROP\s+TRIGGER\b`)},
	{"RENAME", regexp.MustCompile(`(?i)\bRENAME\s+(TO|COLUMN|CONSTRAINT)\b`)},
	{"ALTER COLUMN TYPE", regexp.MustCompile(`(?i)\bALTER\s+(COLUMN\s+)?\w+\s+(SET\s+DATA\s+)?TYPE\b`)},
	{"SET NOT NULL", regexp.MustCompile(`(?i)\bSET\s+NOT\s+NULL\b`)},
	{"ADD CONSTRAINT", regexp.MustCompile(`(?i)\bADD\s+(CONSTRAINT|CHECK|UNIQUE|PRIMARY\s+KEY|FOREIGN\s+KEY|EXCLUDE)\b`)},
	{"TRUNCATE", regexp.MustCompile(`(?i)\bTRUNCATE\b`)},
	// Обязательный столбец без значения по умолчанию: вставка старой версии, которая его не знает, упадёт.
	{"ADD COLUMN ... NOT NULL without DEFAULT", regexp.MustCompile(`(?is)\bADD\s+COLUMN\s+(IF\s+NOT\s+EXISTS\s+)?[^,;]*\bNOT\s+NULL\b[^,;]*`)},
}

var (
	createIndex = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\b`)
	indexSafe   = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+(CONCURRENTLY\s+)?IF\s+NOT\s+EXISTS\b`)
	addColumn   = regexp.MustCompile(`(?i)\bADD\s+COLUMN\s+(IF\s+NOT\s+EXISTS\s+)?[^,;]*\bNOT\s+NULL\b[^,;]*`)
	hasDefault  = regexp.MustCompile(`(?i)\bDEFAULT\b`)
)

// upSection возвращает раздел Up файла goose без комментариев `--` (маркер обрабатывается отдельно).
func upSection(sql string) string {
	up := sql
	if i := strings.Index(up, "-- +goose Down"); i >= 0 {
		up = up[:i]
	}
	var b strings.Builder
	for line := range strings.SplitSeq(up, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// unsafeExpansion перечисляет нарушения правила в тексте миграции (без маркера `-- scibox:contract`).
func unsafeExpansion(sql string) []string {
	if strings.Contains(sql, contractMarker) {
		return nil
	}
	up := upSection(sql)
	var found []string
	for _, b := range breaking {
		if b.name == "ADD COLUMN ... NOT NULL without DEFAULT" {
			for _, m := range addColumn.FindAllString(up, -1) {
				if !hasDefault.MatchString(m) {
					found = append(found, b.name)
				}
			}
			continue
		}
		if b.re.MatchString(up) {
			found = append(found, b.name)
		}
	}
	// Индекс в боевую базу кладут заранее и без блокировки записи (docs/SCALING.md, «Как выкладывать индексы»), поэтому
	// в миграции он обязан быть `IF NOT EXISTS`: готовый индекс не пересоздаётся.
	if n := len(createIndex.FindAllString(up, -1)); n != len(indexSafe.FindAllString(up, -1)) {
		found = append(found, "CREATE INDEX without IF NOT EXISTS")
	}
	return found
}

func migrationNumber(t *testing.T, name string) int {
	t.Helper()
	n, _, ok := strings.Cut(name, "_")
	if !ok {
		t.Fatalf("migration %q has no number prefix", name)
	}
	v, err := strconv.Atoi(n)
	if err != nil {
		t.Fatalf("migration %q: %v", name, err)
	}
	return v
}

func TestNewMigrationsOnlyExpandTheSchema(t *testing.T) {
	entries, err := fs.ReadDir(dbfiles.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if migrationNumber(t, e.Name()) <= lastUncheckedMigration {
			continue
		}
		checked++
		raw, err := fs.ReadFile(dbfiles.Migrations, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if found := unsafeExpansion(string(raw)); len(found) > 0 {
			t.Errorf("%s breaks the previous server version during a rolling deploy: %s.\n"+
				"Split it: add first, remove in a later release, and mark the removing file with %q and the reason (docs/DEPLOY.md, «Миграции»)",
				e.Name(), strings.Join(found, ", "), contractMarker)
		}
	}
	t.Logf("checked %d migrations after %05d", checked, lastUncheckedMigration)
}

// Сам проверяющий проверен: правило не должно молча пропускать опасное и не должно ругаться на безопасное.
func TestExpansionCheckerRecognisesBreakingChanges(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string // пусто — безопасно
	}{
		{"new table", "-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Down\nDROP TABLE t;", nil},
		{"new nullable column", "-- +goose Up\nALTER TABLE t ADD COLUMN IF NOT EXISTS a text;", nil},
		{"new column with default", "-- +goose Up\nALTER TABLE t ADD COLUMN a int NOT NULL DEFAULT 0;", nil},
		{"new index", "-- +goose Up\nCREATE INDEX IF NOT EXISTS t_a_idx ON t (a);\nCREATE UNIQUE INDEX IF NOT EXISTS t_b_idx ON t (b);", nil},
		{"drop in down only", "-- +goose Up\nSELECT 1;\n-- +goose Down\nALTER TABLE t DROP COLUMN a;\nDROP TABLE t;", nil},
		{"drop only in a comment", "-- +goose Up\n-- потом: DROP COLUMN a\nSELECT 1;", nil},
		{"drop column", "-- +goose Up\nALTER TABLE t DROP COLUMN a;", []string{"DROP COLUMN"}},
		{"drop table", "-- +goose Up\nDROP TABLE IF EXISTS t;", []string{"DROP TABLE"}},
		{"rename column", "-- +goose Up\nALTER TABLE t RENAME COLUMN a TO b;", []string{"RENAME"}},
		{"rename table", "-- +goose Up\nALTER TABLE t RENAME TO u;", []string{"RENAME"}},
		{"change type", "-- +goose Up\nALTER TABLE t ALTER COLUMN a TYPE bigint;", []string{"ALTER COLUMN TYPE"}},
		{"change type, set data type", "-- +goose Up\nALTER TABLE t ALTER a SET DATA TYPE bigint;", []string{"ALTER COLUMN TYPE"}},
		{"set not null", "-- +goose Up\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", []string{"SET NOT NULL"}},
		{"column not null without default", "-- +goose Up\nALTER TABLE t ADD COLUMN a int NOT NULL;", []string{"ADD COLUMN ... NOT NULL without DEFAULT"}},
		{"second column not null without default", "-- +goose Up\nALTER TABLE t ADD COLUMN a int NOT NULL DEFAULT 0, ADD COLUMN b int NOT NULL;", []string{"ADD COLUMN ... NOT NULL without DEFAULT"}},
		{"add check", "-- +goose Up\nALTER TABLE t ADD CONSTRAINT c CHECK (a > 0);", []string{"ADD CONSTRAINT"}},
		{"add unique", "-- +goose Up\nALTER TABLE t ADD UNIQUE (a);", []string{"ADD CONSTRAINT"}},
		{"drop constraint", "-- +goose Up\nALTER TABLE t DROP CONSTRAINT c;", []string{"DROP CONSTRAINT"}},
		{"truncate", "-- +goose Up\nTRUNCATE t;", []string{"TRUNCATE"}},
		{"index that cannot be pre-created", "-- +goose Up\nCREATE INDEX t_a_idx ON t (a);", []string{"CREATE INDEX without IF NOT EXISTS"}},
		{"marked as contract", "-- +goose Up\n-- scibox:contract 00031 stopped reading a in release 1.4\nALTER TABLE t DROP COLUMN a;", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unsafeExpansion(tc.sql)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Откат версии: сервер предыдущего выпуска не знает последних миграций, но база накатана дальше. Он должен считать её
// готовой (иначе /readyz не пустит копию к работе и откат превратится в простой), а не «ожидающей миграций».
func TestAnOlderServerAcceptsADatabaseMigratedFurther(t *testing.T) {
	ctx := context.Background()
	dbURL := testdb.Create(t, true) // накатана полностью

	connCfg, err := pgx.ParseConfig(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*connCfg)
	t.Cleanup(func() { _ = db.Close() })

	// «Старая версия»: те же файлы без последней миграции.
	all, err := fs.Sub(dbfiles.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	older := fstest.MapFS{}
	for _, e := range entries[:len(entries)-1] {
		raw, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		older[e.Name()] = &fstest.MapFile{Data: raw}
	}
	p, err := migrate.NewProviderFS(db, older)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := p.HasPending(ctx)
	if err != nil {
		t.Fatalf("an older server cannot tell whether a newer database needs migrations: %v", err)
	}
	if pending {
		t.Fatal("an older server must not consider a database migrated further than it knows as needing migrations")
	}
	if _, err := p.GetDBVersion(ctx); err != nil {
		t.Fatalf("an older server cannot read the version of a newer database: %v", err)
	}
}

func TestMigrationsGiveUpWaitingForLocks(t *testing.T) {
	cfg, err := migrate.ConnConfig("postgres://u:p@db.internal/scibox?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RuntimeParams["lock_timeout"]; got != migrate.LockTimeout {
		t.Fatalf("lock_timeout = %q, want %q: a migration must not hold the site hostage behind a long query", got, migrate.LockTimeout)
	}
	// Своё значение администратора не перетирается.
	cfg, err = migrate.ConnConfig("postgres://u:p@db.internal/scibox?lock_timeout=2s")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RuntimeParams["lock_timeout"]; got != "2s" {
		t.Fatalf("lock_timeout = %q, want the administrator's 2s", got)
	}
	if _, err := migrate.ConnConfig("::bad"); err == nil {
		t.Fatal("a broken address must be an error")
	}
}

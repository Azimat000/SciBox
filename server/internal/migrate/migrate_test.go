package migrate_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
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
	defer func() { _ = conn.Close(ctx) }()
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
	defer func() { _ = conn.Close(ctx) }()
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
	vacancyTables := []string{"auth_tokens", "org_invitations", "org_members", "organizations", "positions", "rate_events", "reference_sources",
		"regions", "science_fields", "science_groups", "sessions", "specialties", "units", "users", "vacancies", "vacancy_specialties", "vacancy_view"}
	// vacancy_search — текст для поиска (00006).
	searchTables := append(slices.Clone(vacancyTables[:15]), "vacancy_search", "vacancy_specialties", "vacancy_view")
	// Профили учёных (00007): три таблицы встают между positions и rate_events.
	profileTables := slices.Concat(searchTables[:5], []string{"profile_items", "profile_specialties", "profiles"}, searchTables[5:])
	// Отклики, рекомендации, уведомления и очередь писем (00008). Информационная схема отдаёт имена по алфавиту.
	applicationTables := append(slices.Clone(profileTables), "application_files", "applications", "notifications", "outbox", "reference_requests")
	slices.Sort(applicationTables)
	// Приглашения к откликам (00009).
	reviewTables := append(slices.Clone(applicationTables), "application_invitations")
	slices.Sort(reviewTables)
	// Приглашения учёных на вакансии (00010).
	offerTables := append(slices.Clone(reviewTables), "vacancy_offers")
	slices.Sort(offerTables)
	// Избранное, сохранённые поиски, напоминания и настройки писем (00011).
	matchingTables := append(slices.Clone(offerTables), "deadline_reminders", "favorites", "notification_settings", "saved_searches")
	slices.Sort(matchingTables)
	// Справочник журналов (00012).
	journalTables := append(slices.Clone(matchingTables), "journal_issns", "journals")
	slices.Sort(journalTables)
	// Счётчики ограничения частоты (00015).
	counterTables := append(slices.Clone(journalTables), "rate_counters")
	slices.Sort(counterTables)
	// Знакомые браузеры (00016).
	deviceTables := append(slices.Clone(counterTables), "known_devices")
	slices.Sort(deviceTables)
	// Журнал действий (00017).
	auditTables := append(slices.Clone(deviceTables), "audit_log")
	slices.Sort(auditTables)
	// Файлы откликов в хранилище: очередь удаления объектов (00019).
	fileTables := append(slices.Clone(auditTables), "blob_deletions")
	slices.Sort(fileTables)
	// Текст профиля для поиска (00020); 00021 таблиц не добавляет.
	scaleTables := append(slices.Clone(fileTables), "profile_search")
	slices.Sort(scaleTables)
	// Общее расписание фоновых задач (00022).
	jobTables := append(slices.Clone(scaleTables), "job_runs")
	slices.Sort(jobTables)
	// Организации, от которых скрыт профиль (00027).
	hiddenTables := append(slices.Clone(jobTables), "profile_hidden_orgs")
	slices.Sort(hiddenTables)
	// Справочник навыков (00030).
	skillTables := append(slices.Clone(hiddenTables), "skill_terms", "skills")
	slices.Sort(skillTables)
	// Чат: переписки, сообщения, личные отметки (00031).
	chatTables := append(slices.Clone(skillTables), "chat_messages", "chat_participants", "chats")
	slices.Sort(chatTables)
	// Инструменты чата: шаблоны, блокировки, жалобы (00032).
	allTables := append(slices.Clone(chatTables), "chat_blocks", "chat_complaints", "chat_templates")
	slices.Sort(allTables)
	steps := []struct {
		cmd        string
		wantOut    string
		wantExts   int
		wantTables []string
	}{
		{"status", "pending", 0, nil},
		{"up", "00032_chat_tools.sql", 2, allTables},
		{"up", "no migrations to apply", 2, allTables},
		{"status", "applied", 2, allTables},
		// Столбцы привязки ссылки (00025) и неактивности (00026), индексы для чисел главной (00024) и метрики очереди
		// писем (00023) таблиц не добавляют; 00027 добавляет таблицу скрытых от организаций; 00028 (условия вакансии)
		// только столбцы; 00029 (ссылка на GitHub) тоже один столбец; 00030 добавляет справочник навыков; 00031 чат; 00032 инструменты чата.
		{"down", "00032_chat_tools.sql", 2, chatTables},
		{"down", "00031_chat.sql", 2, skillTables},
		{"down", "00030_skills.sql", 2, hiddenTables},
		{"down", "00029_profile_github.sql", 2, hiddenTables},
		{"down", "00028_vacancy_conditions.sql", 2, hiddenTables},
		{"down", "00027_anonymous_profile.sql", 2, jobTables},
		{"down", "00026_inactive_accounts.sql", 2, jobTables},
		{"down", "00025_confirm_binding.sql", 2, jobTables},
		{"down", "00024_load_indexes.sql", 2, jobTables},
		{"down", "00023_observability.sql", 2, jobTables},
		{"down", "00022_job_runs.sql", 2, scaleTables},
		{"down", "00021_vacancy_search_open.sql", 2, scaleTables},
		{"down", "00020_scale.sql", 2, fileTables},
		{"down", "00019_file_storage.sql", 2, auditTables},
		// Сроки хранения (00018) таблиц не добавляют: меняют защиту журнала и добавляют индексы.
		{"down", "00018_retention.sql", 2, auditTables},
		{"down", "00017_audit_log.sql", 2, deviceTables},
		{"down", "00016_account_security.sql", 2, counterTables},
		{"down", "00015_rate_counters.sql", 2, journalTables},
		// Виды вакансий и навыки (00013, 00014) таблиц не добавляют.
		{"down", "00014_profile_skills.sql", 2, journalTables},
		{"down", "00013_vacancy_kinds.sql", 2, journalTables},
		{"down", "00012_journals.sql", 2, matchingTables},
		{"down", "00011_matching.sql", 2, offerTables},
		{"down", "00010_offers.sql", 2, reviewTables},
		{"down", "00009_review.sql", 2, applicationTables},
		{"down", "00008_applications.sql", 2, profileTables},
		{"down", "00007_profiles.sql", 2, searchTables},
		{"down", "00006_search.sql", 2, vacancyTables},
		{"down", "00005_vacancies.sql", 2, referenceTables},
		{"down", "00004_reference.sql", 2, orgTables},
		{"down", "00003_organizations.sql", 2, accountTables},
		{"down", "00002_accounts.sql", 2, nil},
		{"down", "00001_extensions.sql", 0, nil},
		{"up", "00032_chat_tools.sql", 2, allTables},
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
	defer func() { _ = db.Close() }()
	_, err := migrate.NewProviderFS(db, fstest.MapFS{})
	if !errors.Is(err, goose.ErrNoMigrations) {
		t.Fatalf("err = %v, want ErrNoMigrations", err)
	}
	_ = db
}

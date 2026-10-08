package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/testdb"
)

const productConfig = "../../../config/product.json"

type result struct {
	code           int
	stdout, stderr string
}

func run(ctx context.Context, args []string, env map[string]string, onListen func(net.Addr)) result {
	var out, errb bytes.Buffer
	if _, ok := env["SCIBOX_PRODUCT_CONFIG"]; !ok {
		env["SCIBOX_PRODUCT_CONFIG"] = productConfig
	}
	code := Run(ctx, args, Env{
		Getenv:   func(k string) string { return env[k] },
		Stdout:   &out,
		Stderr:   &errb,
		OnListen: onListen,
	})
	return result{code, out.String(), errb.String()}
}

func TestCommandsWithoutDatabase(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		env      map[string]string
		code     int
		inStdout string
		inStderr string
	}{
		{"version", []string{"version"}, map[string]string{}, 0, "dev", ""},
		{"help", []string{"help"}, map[string]string{}, 0, "Использование", ""},
		{"unknown", []string{"fly"}, map[string]string{}, 2, "", "неизвестная команда"},
		{"bad product config", []string{"seed"}, map[string]string{"SCIBOX_PRODUCT_CONFIG": "/nope.json"}, 1, "", "load config"},
		{"seed bad url", []string{"seed"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "connect database"},
		{"seed without migrations", []string{"seed"}, map[string]string{"DATABASE_URL": testdb.Create(t, false)}, 1, "", "seed"},
		{"migrate without subcommand", []string{"migrate"}, map[string]string{}, 2, "", "Использование"},
		{"migrate bad url", []string{"migrate", "up"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "parse database url"},
		{"serve bad url", []string{"serve"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "connect database"},
		{"journals without subcommand", []string{"journals"}, map[string]string{}, 2, "", "Использование"},
		{"journals unknown subcommand", []string{"journals", "drop"}, map[string]string{}, 2, "", "Использование"},
		{"journals unknown flag", []string{"journals", "load", "--fast"}, map[string]string{}, 2, "", "Использование"},
		{"journals missing file", []string{"journals", "load", "/nope.csv"}, map[string]string{}, 1, "", "no such file"},
		{"journals bad url", []string{"journals", "load"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "connect database"},
		{"journals without migrations", []string{"journals", "load"}, map[string]string{"DATABASE_URL": testdb.Create(t, false)}, 1, "", "journals"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := run(context.Background(), tc.args, tc.env, nil)
			if r.code != tc.code || !strings.Contains(r.stdout, tc.inStdout) || !strings.Contains(r.stderr, tc.inStderr) {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestMigrateUp(t *testing.T) {
	dbURL := testdb.Create(t, false)
	r := run(context.Background(), []string{"migrate", "up"}, map[string]string{"DATABASE_URL": dbURL}, nil)
	if r.code != 0 || !strings.Contains(r.stdout, "00001_extensions.sql") {
		t.Fatalf("got %+v", r)
	}
}

func TestJournalsLoad(t *testing.T) {
	dbURL := testdb.Create(t, true)
	env := map[string]string{"DATABASE_URL": dbURL}
	first := run(context.Background(), []string{"journals", "load"}, env, nil)
	if first.code != 0 || !strings.Contains(first.stdout, "Справочник журналов загружен (SJR 2025") {
		t.Fatalf("first: %+v", first)
	}
	again := run(context.Background(), []string{"journals", "load"}, env, nil)
	if again.code != 0 || !strings.Contains(again.stdout, "уже загружен") {
		t.Fatalf("again: %+v", again)
	}
	forced := run(context.Background(), []string{"journals", "load", "--force"}, env, nil)
	if forced.code != 0 || !strings.Contains(forced.stdout, "загружен (SJR") {
		t.Fatalf("forced: %+v", forced)
	}
	// Свой файл (новый выпуск SCImago): справочник заменяется им.
	file := t.TempDir() + "/sjr.csv"
	csv := "Rank;Sourceid;Title;Type;Issn;SJR;SJR Best Quartile;Total Docs. (2026)\n1;7;\"Nature\";journal;\"00280836\";18,1;Q1\n"
	if err := os.WriteFile(file, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	own := run(context.Background(), []string{"journals", "load", file}, env, nil)
	if own.code != 0 || !strings.Contains(own.stdout, "SJR 2026") || !strings.Contains(own.stdout, "журналов 1,") {
		t.Fatalf("own file: %+v", own)
	}
	if err := os.WriteFile(file, []byte("not a csv"), 0o600); err != nil {
		t.Fatal(err)
	}
	if bad := run(context.Background(), []string{"journals", "load", file}, env, nil); bad.code != 1 || !strings.Contains(bad.stderr, "not a SCImago") {
		t.Fatalf("bad file: %+v", bad)
	}
}

func TestSeedLoadsDemoDataOnceAndOnlyOnce(t *testing.T) {
	dbURL := testdb.Create(t, true)
	env := map[string]string{"DATABASE_URL": dbURL}
	first := run(context.Background(), []string{"seed"}, env, nil)
	if first.code != 0 || !strings.Contains(first.stdout, "организаций 30") || !strings.Contains(first.stdout, "elena.orlova@demo.example.ru") {
		t.Fatalf("first run: %+v", first)
	}
	// Пароль не печатается в терминал.
	if strings.Contains(first.stdout, "demo-password") {
		t.Errorf("the demo password must not be printed: %s", first.stdout)
	}
	second := run(context.Background(), []string{"seed"}, env, nil)
	if second.code != 0 || !strings.Contains(second.stdout, "организаций 0") || !strings.Contains(second.stdout, "новых людей 0") {
		t.Fatalf("second run must add nothing: %+v", second)
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	count := func(q string) (n int) {
		if err := pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM organizations"); n != 30 {
		t.Errorf("%d organizations, want 30", n)
	}
	if n := count("SELECT count(*) FROM users WHERE email_confirmed_at IS NOT NULL"); n != 17 {
		t.Errorf("%d confirmed users, want 17", n)
	}
	// У каждой организации есть владелец и подразделение; руководители подразделений — её сотрудники.
	if n := count("SELECT count(*) FROM organizations o WHERE NOT EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = o.id AND m.role = 'owner') OR NOT EXISTS (SELECT 1 FROM units u WHERE u.org_id = o.id)"); n != 0 {
		t.Errorf("%d organizations without an owner or units", n)
	}
	if n := count("SELECT count(*) FROM units u WHERE u.head_user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = u.org_id AND m.user_id = u.head_user_id)"); n != 0 {
		t.Errorf("%d unit heads who are not members", n)
	}
	// Демонстрационные вакансии: все четыре типа, есть черновик и закрытая; опубликованные заполнены так, как требует публикация.
	if n := count("SELECT count(*) FROM vacancies"); n < 200 || n > 230 {
		t.Errorf("%d vacancies, want about 200 (for the search)", n)
	}
	// Поиск должен быть на чём проверить: есть вакансии с прошедшим сроком, которые он прячет, и в разных регионах.
	if n := count("SELECT count(*) FROM vacancies WHERE status = 'published' AND deadline < current_date"); n == 0 {
		t.Error("no published vacancies with an expired deadline")
	}
	if n := count("SELECT count(DISTINCT region_code) FROM vacancies WHERE status = 'published'"); n < 12 {
		t.Errorf("vacancies are spread over %d regions only", n)
	}
	if n := count("SELECT count(*) FROM vacancies WHERE status = 'published' AND work_format = 'remote'"); n == 0 {
		t.Error("no remote vacancies")
	}
	if n := count("SELECT count(*) FROM (SELECT org_id, title FROM vacancies GROUP BY 1, 2 HAVING count(*) > 1) d"); n != 0 {
		t.Errorf("%d repeated titles inside an organization", n)
	}
	if n := count("SELECT count(*) FROM vacancies v WHERE NOT EXISTS (SELECT 1 FROM vacancy_search s WHERE s.vacancy_id = v.id)"); n != 0 {
		t.Errorf("%d vacancies without search text", n)
	}
	for _, status := range []string{"draft", "published", "closed"} {
		if n := count("SELECT count(*) FROM vacancies WHERE status = '" + status + "'"); n == 0 {
			t.Errorf("no %s vacancies", status)
		}
	}
	// Все семь видов вакансий (D-134).
	if n := count("SELECT count(DISTINCT p.position_type) FROM vacancies v JOIN positions p ON p.code = v.position_code"); n != 7 {
		t.Errorf("%d position types among vacancies, want 7", n)
	}
	// Опубликованное заполнено по правилам своего вида (internal/vacancies, rulesFor).
	if n := count(`SELECT count(*) FROM vacancies v JOIN positions p ON p.code = v.position_code WHERE v.status <> 'draft' AND (
		v.summary = '' OR v.description = '' OR v.work_format IS NULL OR v.contract_type IS NULL
		OR (p.position_type IN ('research', 'teaching') AND v.career_level IS NULL)
		OR (p.position_type IN ('research', 'teaching', 'admin') AND v.rate_percent IS NULL)
		OR (p.position_type IN ('research', 'teaching', 'phd', 'masters') AND NOT EXISTS (SELECT 1 FROM vacancy_specialties s WHERE s.vacancy_id = v.id))
		OR (p.position_type IN ('teaching', 'phd', 'masters', 'project', 'internship') AND v.focus = '')
		OR (v.work_format <> 'remote' AND (v.city = '' OR v.region_code IS NULL))
		OR (v.is_competition AND v.deadline IS NULL))`); n != 0 {
		t.Errorf("%d published vacancies are not complete", n)
	}
	// Профили демо-учёных: во всех режимах приватности, заполнены, записи по порядку.
	if n := count("SELECT count(*) FROM profiles"); n != 14 {
		t.Errorf("%d profiles, want 14", n)
	}
	for _, mode := range []string{"hidden", "orgs", "public"} {
		if n := count("SELECT count(*) FROM profiles WHERE visibility = '" + mode + "'"); n == 0 {
			t.Errorf("no %s profiles", mode)
		}
	}
	if n := count("SELECT count(*) FROM profiles WHERE open_to_offers"); n == 0 {
		t.Error("nobody is open to offers")
	}
	if n := count("SELECT count(DISTINCT kind) FROM profile_items"); n != 6 {
		t.Errorf("%d kinds of profile items, want 6", n)
	}
	if n := count("SELECT count(*) FROM profiles p WHERE headline = '' OR NOT EXISTS (SELECT 1 FROM profile_specialties s WHERE s.profile_id = p.id) OR NOT EXISTS (SELECT 1 FROM profile_items i WHERE i.profile_id = p.id)"); n != 0 {
		t.Errorf("%d demo profiles are empty", n)
	}
	// Журналы демо-публикаций (срез 14): у статей настоящие ISSN из SCImago; после загрузки справочника все находятся,
	// у нескольких учёных есть статьи в Q1–Q2.
	if n := count("SELECT count(*) FROM profile_items WHERE kind = 'publication' AND data ->> 'issn' IS NOT NULL"); n < 20 {
		t.Errorf("%d demo publications with an ISSN, want at least 20", n)
	}
	if r := run(context.Background(), []string{"journals", "load"}, env, nil); r.code != 0 {
		t.Fatalf("journals: %+v", r)
	}
	if n := count("SELECT count(*) FROM profile_items i WHERE kind = 'publication' AND data ->> 'issn' IS NOT NULL AND NOT EXISTS (SELECT 1 FROM journal_issns x WHERE x.issn = i.data ->> 'issn')"); n != 0 {
		t.Errorf("%d demo ISSNs are not in the journal catalog", n)
	}
	if n := count("SELECT count(DISTINCT i.profile_id) FROM profile_items i JOIN journal_issns x ON x.issn = i.data ->> 'issn' JOIN journals j ON j.id = x.journal_id WHERE j.quartile <= 2"); n < 5 {
		t.Errorf("%d demo scientists with Q1–Q2 articles, want at least 5", n)
	}
	if n := count("SELECT count(DISTINCT j.quartile) FROM profile_items i JOIN journal_issns x ON x.issn = i.data ->> 'issn' JOIN journals j ON j.id = x.journal_id"); n != 4 {
		t.Errorf("demo journals cover %d quartiles, want all 4", n)
	}
	if n := count("SELECT count(*) FROM org_invitations"); n != 1 {
		t.Errorf("%d invitations, want 1", n)
	}
	// Демо-отклики (срез 8): разные статусы, файлы, рекомендатели во всех состояниях, уведомления организациям.
	if n := count("SELECT count(*) FROM applications"); n < 12 {
		t.Errorf("%d demo applications, want at least 12", n)
	}
	if n := count("SELECT count(DISTINCT status) FROM applications"); n != 6 {
		t.Errorf("%d statuses among demo applications, want 6", n)
	}
	for _, status := range []string{"pending", "received", "declined"} {
		if n := count("SELECT count(*) FROM reference_requests WHERE status = '" + status + "'"); n == 0 {
			t.Errorf("no %s reference requests", status)
		}
	}
	if n := count("SELECT count(*) FROM application_files WHERE kind = 'reference_letter'"); n == 0 {
		t.Error("no reference letters as files")
	}
	if n := count("SELECT count(*) FROM applications a WHERE NOT EXISTS (SELECT 1 FROM application_files f WHERE f.application_id = a.id AND f.kind = 'cv')"); n != 0 {
		t.Errorf("%d demo applications without a cv", n)
	}
	if n := count("SELECT count(*) FROM notifications WHERE kind = 'application_received'"); n == 0 {
		t.Error("organizations were not notified about demo applications")
	}
	// Разбор откликов (срез 9): приглашения всех видов и состояний, записки к решениям; открытые приглашения только у «приглашённых».
	for _, kind := range []string{"interview", "contacts", "request_contacts"} {
		if n := count("SELECT count(*) FROM application_invitations WHERE kind = '" + kind + "'"); n == 0 {
			t.Errorf("no %s demo invitations", kind)
		}
	}
	for _, status := range []string{"pending", "proposed", "confirmed", "answered", "shared"} {
		if n := count("SELECT count(*) FROM application_invitations WHERE status = '" + status + "'"); n == 0 {
			t.Errorf("no %s demo invitations", status)
		}
	}
	if n := count("SELECT count(*) FROM application_invitations i JOIN applications a ON a.id = i.application_id WHERE i.status IN ('pending', 'proposed') AND a.status <> 'invited'"); n != 0 {
		t.Errorf("%d open demo invitations on applications that are not 'invited'", n)
	}
	if n := count("SELECT count(*) FROM applications WHERE decision_note <> ''"); n == 0 {
		t.Error("no demo decision notes")
	}
	// Каталог и приглашения (срез 10): приглашения во всех состояниях, ответы учёных, никто не приглашён на вакансию с откликом.
	for _, status := range []string{"pending", "interested", "declined"} {
		if n := count("SELECT count(*) FROM vacancy_offers WHERE status = '" + status + "'"); n == 0 {
			t.Errorf("no %s demo offers", status)
		}
	}
	if n := count("SELECT count(*) FROM vacancy_offers o JOIN applications a ON a.vacancy_id = o.vacancy_id AND a.user_id = o.user_id"); n != 0 {
		t.Errorf("%d demo offers on vacancies the person already applied to", n)
	}
	if n := count("SELECT count(*) FROM vacancy_offers o JOIN profiles p ON p.user_id = o.user_id WHERE p.visibility = 'hidden'"); n != 0 {
		t.Errorf("%d demo offers to hidden profiles", n)
	}
	if n := count("SELECT count(*) FROM profiles WHERE visibility <> 'hidden' AND headline <> ''"); n < 8 {
		t.Errorf("only %d demo profiles are visible in the catalog", n)
	}
	// Избранное, поиски и сроки (срез 11).
	if n := count("SELECT count(*) FROM favorites"); n < 12 {
		t.Errorf("only %d demo favorites", n)
	}
	if n := count("SELECT count(DISTINCT user_id) FROM favorites"); n != 6 {
		t.Errorf("favorites of %d people, want 6", n)
	}
	if n := count("SELECT count(*) FROM favorites f JOIN vacancies v ON v.id = f.vacancy_id WHERE v.status <> 'published'"); n != 0 {
		t.Errorf("%d demo favorites on vacancies that are not published", n)
	}
	if n := count("SELECT count(*) FROM favorites f JOIN applications a ON a.vacancy_id = f.vacancy_id AND a.user_id = f.user_id"); n != 0 {
		t.Errorf("%d demo favorites on vacancies the person already applied to", n)
	}
	if n := count("SELECT count(*) FROM favorites f JOIN vacancies v ON v.id = f.vacancy_id WHERE v.deadline BETWEEN current_date AND current_date + 7"); n < 3 {
		t.Errorf("only %d favorites with a deadline in the coming week: the calendar would look empty", n)
	}
	if n := count("SELECT count(*) FROM saved_searches"); n != 7 {
		t.Errorf("%d demo saved searches, want 7", n)
	}
	for _, freq := range []string{"instant", "daily", "weekly", "off"} {
		if n := count("SELECT count(*) FROM saved_searches WHERE frequency = '" + freq + "'"); n == 0 {
			t.Errorf("no %s demo saved searches", freq)
		}
	}
	if n := count("SELECT count(*) FROM outbox WHERE sent_at IS NULL"); n != 0 {
		t.Errorf("%d demo mails are waiting to be sent: demo data must not send mail", n)
	}
}

func TestServeListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	r := run(context.Background(), []string{"serve"}, map[string]string{
		"DATABASE_URL":     testdb.Create(t, true),
		"SCIBOX_HTTP_ADDR": ln.Addr().String(), // порт уже занят
	}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "listen") {
		t.Fatalf("got %+v", r)
	}
}

// Сервер по умолчанию (без аргументов) поднимается, отвечает на /api/health и
// аккуратно останавливается по отмене контекста.
func TestServeHealthAndShutdown(t *testing.T) {
	dbURL := testdb.Create(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type healthBody struct {
		Status   string `json:"status"`
		Product  string `json:"product"`
		Database struct {
			SchemaVersion int64 `json:"schema_version"`
		} `json:"database"`
	}
	var got healthBody
	var status, landingStatus int
	var reqErr error

	r := run(ctx, nil, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_HTTP_ADDR": "127.0.0.1:0"}, func(addr net.Addr) {
		defer cancel()
		client := http.Client{Timeout: 5 * time.Second}
		// Числа главной страницы подключены и отвечают без входа.
		if resp, err := client.Get(fmt.Sprintf("http://%s/api/landing", addr)); err == nil {
			landingStatus = resp.StatusCode
			_ = resp.Body.Close()
		}
		resp, err := client.Get(fmt.Sprintf("http://%s/api/health", addr))
		if err != nil {
			reqErr = err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		status = resp.StatusCode
		reqErr = json.NewDecoder(resp.Body).Decode(&got)
	})
	if reqErr != nil {
		t.Fatal(reqErr)
	}
	if r.code != 0 || !strings.Contains(r.stderr, "server stopped") {
		t.Fatalf("got %+v", r)
	}
	if status != http.StatusOK || got.Status != "ok" || got.Product != "SciBox" || got.Database.SchemaVersion < 1 {
		t.Fatalf("health = %d %+v", status, got)
	}
	if landingStatus != http.StatusOK {
		t.Fatalf("landing numbers answered %d", landingStatus)
	}
}

// С SCIBOX_WEB_DIR сервер сам отдаёт собранный сайт с того же порта, что и API (D-131).
func TestServeWebDir(t *testing.T) {
	dbURL := testdb.Create(t, true)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>site</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var page string
	var apiStatus int
	r := run(ctx, nil, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_HTTP_ADDR": "127.0.0.1:0", "SCIBOX_WEB_DIR": dir}, func(addr net.Addr) {
		defer cancel()
		client := http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get(fmt.Sprintf("http://%s/vacancies/42", addr)); err == nil {
			b, _ := io.ReadAll(resp.Body)
			page = string(b)
			_ = resp.Body.Close()
		}
		if resp, err := client.Get(fmt.Sprintf("http://%s/api/nope", addr)); err == nil {
			apiStatus = resp.StatusCode
			_ = resp.Body.Close()
		}
	})
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	if page != "<title>site</title>" || apiStatus != http.StatusNotFound {
		t.Fatalf("page %q, api status %d", page, apiStatus)
	}

	// Сайт не собран — сервер не притворяется, что всё хорошо.
	r = run(context.Background(), nil, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_HTTP_ADDR": "127.0.0.1:0", "SCIBOX_WEB_DIR": t.TempDir()}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "site not built") {
		t.Fatalf("got %+v", r)
	}
}

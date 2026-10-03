package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	if n := count("SELECT count(DISTINCT p.position_type) FROM vacancies v JOIN positions p ON p.code = v.position_code"); n != 4 {
		t.Errorf("%d position types among vacancies, want 4", n)
	}
	if n := count(`SELECT count(*) FROM vacancies v JOIN positions p ON p.code = v.position_code WHERE v.status <> 'draft' AND (
		v.summary = '' OR v.description = '' OR v.work_format IS NULL OR v.contract_type IS NULL
		OR (p.position_type <> 'management' AND (v.career_level IS NULL OR NOT EXISTS (SELECT 1 FROM vacancy_specialties s WHERE s.vacancy_id = v.id)))
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
	defer ln.Close()
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
		defer resp.Body.Close()
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

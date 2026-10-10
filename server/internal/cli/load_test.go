package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/config"
	"scibox/server/internal/dbpool"
)

// Замер горячих запросов на объёмных данных (срез 24, docs/SCALING.md). Нужна база, заполненная `make load-data`;
// без переменной SCIBOX_LOAD_DATABASE_URL тест пропускается (это единственный пропуск в пакете: обычный `make test`
// не должен ждать часы на заполнение). Запуск: `make load-check`. Бюджет каждого запроса — loadBudget.
//
// Запросы идут через настоящий роутер (newApp), с настоящими сеансами и лимитами частоты, но без сети; поэтому в
// цифре всё, кроме сетевого пути: разбор запроса, права, SQL, сборка ответа.
const loadBudget = 50 * time.Millisecond

// Поиск по словам и с опечатками имеет свой, более высокий бюджет (budget в строке): он читает тексты всех подходящих профилей или
// вакансий, а их у частого слова тысячи (docs/SCALING.md, «Известные узкие места»).

// loadCase — один горячий запрос: что и каким человеком спрашивают. gen возвращает путь для i-го повторения (параметры меняются,
// чтобы замер не сводился к одной закешированной строке).
type loadCase struct {
	name   string
	cookie string // имя сеанса из loadSessions; пусто — гость
	gen    func(i int) string
	n      int
	budget time.Duration // 0 — loadBudget
}

type loadResult struct {
	name          string
	p50, p95, max time.Duration
	status        map[int]int
	bytes         int
}

func TestHotQueriesOnLoadData(t *testing.T) {
	dbURL := os.Getenv("SCIBOX_LOAD_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SCIBOX_LOAD_DATABASE_URL не задан: замеры на объёмных данных запускает `make load-check`")
	}
	ctx := t.Context()
	pool, err := dbpool.New(ctx, dbURL, dbpool.Default(), slog.New(slog.NewTextHandler(io.Discard, nil))) // тот же пул, что у сервера: сроки, план под значения, журнал медленных
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	cfg, err := config.Load(func(k string) string {
		return map[string]string{"DATABASE_URL": dbURL, "SCIBOX_PRODUCT_CONFIG": productConfig}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	// Каждый запрос попадает в новую минуту: лимиты частоты не мешают замеру, но запись счётчика остаётся в цифре.
	var tick atomic.Int64
	clock := func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(tick.Add(1)) * time.Minute)
	}
	h := newApp(cfg, pool, newTestVault(), slog.New(slog.NewTextHandler(io.Discard, nil)), okHealth{}, nil, clock).handler

	fx := loadFixtures(t, ctx, pool)

	cases := []loadCase{
		// Гость: главные публичные страницы.
		{name: "поиск вакансий: первая страница без слов", gen: func(int) string { return "/api/vacancies" }, n: 60},
		{name: "поиск вакансий: слово", gen: func(i int) string { return "/api/vacancies?q=" + fx.word(i) }, n: 60, budget: 80 * time.Millisecond},
		{name: "поиск вакансий: два слова и фильтры", gen: func(i int) string {
			return fmt.Sprintf("/api/vacancies?q=%s+%s&field=%s&format=onsite&sort=deadline", fx.word(i), fx.word(i+7), fx.field(i))
		}, n: 60},
		{name: "поиск вакансий: только фильтры (область, регион, тип)", gen: func(i int) string {
			return fmt.Sprintf("/api/vacancies?field=%s&region=%s&type=%s", fx.field(i), fx.region(i), fx.posType(i))
		}, n: 60},
		{name: "поиск вакансий: по зарплате", gen: func(i int) string { return "/api/vacancies?salary_min=80000&sort=salary&housing=1" }, n: 40},
		{name: "поиск вакансий: страница глубоко (offset 5000)", gen: func(i int) string { return "/api/vacancies?limit=20&offset=5000" }, n: 30},
		{name: "поиск вакансий: опечатка (похожие слова)", gen: func(i int) string { return "/api/vacancies?q=" + fx.typo(i) }, n: 20, budget: 120 * time.Millisecond},
		{name: "страница вакансии", gen: func(i int) string { return "/api/vacancies/" + fx.vacancy(i) }, n: 60},
		{name: "вакансии организации", gen: func(i int) string { return "/api/vacancies?org=" + fx.orgSlug(i) }, n: 40},
		{name: "главная: числа", gen: func(int) string { return "/api/landing" }, n: 30},
		{name: "каталог организаций", gen: func(i int) string { return "/api/organizations?q=" + fx.cityWord(i) }, n: 40},
		{name: "страница организации", gen: func(i int) string { return "/api/organizations/" + fx.orgSlug(i) }, n: 40},
		{name: "справочники", gen: func(int) string { return "/api/reference" }, n: 20},
		{name: "страница учёного", gen: func(i int) string { return "/api/scientists/" + fx.profile(i) }, n: 40},
		{name: "соискатели (гость): первая страница", gen: func(int) string { return "/api/scientists" }, n: 40},
		{name: "соискатели (гость): слово", gen: func(i int) string { return "/api/scientists?q=" + fx.word(i) }, n: 40, budget: 150 * time.Millisecond},
		{name: "соискатели (гость): область и регион", gen: func(i int) string {
			return fmt.Sprintf("/api/scientists?field=%s&region=%s", fx.field(i), fx.region(i))
		}, n: 40},
		{name: "соискатели (гость): h-индекс и порядок", gen: func(i int) string { return "/api/scientists?h_min=20&sort=h_index" }, n: 40},
		{name: "соискатели (гость): статьи в Q1–Q2", gen: func(i int) string { return "/api/scientists?q12_min=5&sort=h_index" }, n: 30},
		{name: "соискатели (гость): страница глубоко (offset 2000)", gen: func(int) string { return "/api/scientists?limit=20&offset=2000" }, n: 30},
		{name: "соискатели (гость): опечатка (похожие слова)", gen: func(i int) string { return "/api/scientists?q=" + fx.typo(i) }, n: 20, budget: 300 * time.Millisecond},
		// Сотрудник большой организации.
		{name: "соискатели (сотрудник): первая страница", cookie: "owner_big", gen: func(int) string { return "/api/scientists" }, n: 40},
		{name: "соискатели (сотрудник): слово и фильтр", cookie: "owner_big", gen: func(i int) string {
			return fmt.Sprintf("/api/scientists?q=%s&degree=doctor&open=1", fx.word(i))
		}, n: 40, budget: 150 * time.Millisecond},
		{name: "отклики организации: список (лидер, 7 тысяч откликов)", cookie: "owner_big", gen: func(int) string { return "/api/my/candidates" }, n: 40, budget: 80 * time.Millisecond},
		{name: "отклики организации: по статусу, страница глубже", cookie: "owner_big", gen: func(int) string { return "/api/my/candidates?status=sent&offset=200" }, n: 30},
		{name: "отклики организации: глубокая страница (лидер, offset 3000)", cookie: "owner_big", gen: func(int) string { return "/api/my/candidates?offset=3000" }, n: 20, budget: 120 * time.Millisecond},
		{name: "отклики организации: вакансии с откликами", cookie: "owner_big", gen: func(int) string { return "/api/my/candidate-vacancies" }, n: 30},
		{name: "мои вакансии (организация-лидер)", cookie: "owner_big", gen: func(int) string { return "/api/my/vacancies" }, n: 40},
		{name: "журнал действий организации (лидер)", cookie: "owner_big", gen: func(int) string { return "/api/organizations/org-1/audit" }, n: 30},
		{name: "приглашённые учёные: отправленные", cookie: "owner_big", gen: func(int) string { return "/api/my/sent-offers" }, n: 30},
		// Соискатель.
		{name: "сеанс: кто я (каждый запрос вошедшего)", cookie: "applicant", gen: func(int) string { return "/api/auth/me" }, n: 80},
		{name: "колокольчик: число непрочитанных", cookie: "applicant", gen: func(int) string { return "/api/notifications/unread-count" }, n: 80},
		{name: "колокольчик: последние уведомления", cookie: "applicant", gen: func(int) string { return "/api/notifications?limit=10" }, n: 60},
		{name: "мои отклики", cookie: "applicant", gen: func(int) string { return "/api/applications" }, n: 60},
		{name: "отклик глазами соискателя", cookie: "applicant", gen: func(int) string { return "/api/applications/" + fx.applicantApp }, n: 40},
		{name: "избранное", cookie: "applicant", gen: func(int) string { return "/api/favorites" }, n: 40},
		{name: "подходящие вам", cookie: "applicant", gen: func(int) string { return "/api/matches" }, n: 30, budget: 80 * time.Millisecond},
		{name: "сроки подачи", cookie: "applicant", gen: func(int) string { return "/api/deadlines" }, n: 30},
		{name: "приглашения учёного", cookie: "applicant", gen: func(int) string { return "/api/offers" }, n: 30},
		{name: "мой профиль", cookie: "applicant", gen: func(int) string { return "/api/profile" }, n: 40},
		{name: "поиск журналов", cookie: "applicant", gen: func(i int) string { return "/api/journals?q=" + fx.word(i) }, n: 30},
	}

	// SCIBOX_LOAD_ONLY — часть названия запроса: замерить только подходящие (при разборе медленного запроса).
	only := os.Getenv("SCIBOX_LOAD_ONLY")
	cases = slices.DeleteFunc(cases, func(c loadCase) bool { return only != "" && !strings.Contains(c.name, only) })
	var results []loadResult
	for i, c := range cases {
		results = append(results, runLoadCase(t, h, fx, c, i))
	}

	var report strings.Builder
	report.WriteString("| Запрос | p50 | p95 | максимум | ответ |\n|---|---|---|---|---|\n")
	failed := 0
	for i, r := range results {
		budget := cases[i].budget
		if budget == 0 {
			budget = loadBudget
		}
		mark := ""
		if r.p95 > budget {
			mark = " **больше бюджета**"
			failed++
		}
		fmt.Fprintf(&report, "| %s | %s | %s | %s | %s |%s\n", r.name, ms(r.p50), ms(r.p95), ms(r.max), statusSummary(r), mark)
		for code := range r.status {
			if code != http.StatusOK {
				t.Errorf("%s: ответ %v, ожидался только 200", r.name, r.status)
				break
			}
		}
	}
	t.Log("\n" + report.String())
	if path := os.Getenv("SCIBOX_LOAD_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report.String()), 0o600); err != nil { //nolint:gosec // G703: путь задаёт разработчик переменной окружения в этом тесте замеров
			t.Fatal(err)
		}
	}
	if failed > 0 {
		t.Errorf("%d запросов медленнее %s (p95)", failed, loadBudget)
	}
}

func runLoadCase(t *testing.T, h http.Handler, fx *loadFx, c loadCase, idx int) loadResult {
	t.Helper()
	var ck *http.Cookie
	if c.cookie != "" {
		ck = &http.Cookie{Name: "scibox_session", Value: fx.tokens[c.cookie]}
	}
	do := func(i int) (time.Duration, int, int) {
		remote := fmt.Sprintf("10.%d.%d.%d:4000", idx%250, (i/250)%250, i%250)
		start := time.Now()
		rec := send(h, req{path: c.gen(i), cookie: ck, remote: remote})
		return time.Since(start), rec.Code, rec.Body.Len()
	}
	// Прогрев: те же повторы с другими параметрами, без замера. Холодное чтение с диска Docker на Mac в цифру не входит: на
	// сервере, где данные помещаются в память, оно бывает только после перезапуска базы.
	slowest := time.Duration(0)
	for i := range min(c.n, 30) {
		d, _, _ := do(1000 + i)
		slowest = max(slowest, d)
		if slowest > 2*time.Second { // заведомо слишком медленно: не тратим минуты на повторы
			break
		}
	}
	n := c.n
	if slowest > 2*time.Second {
		n = 3
	}
	res := loadResult{name: c.name, status: map[int]int{}}
	times := make([]time.Duration, 0, n)
	for i := range n {
		d, code, size := do(i)
		times = append(times, d)
		res.status[code]++
		res.bytes = max(res.bytes, size)
	}
	slices.Sort(times)
	res.p50, res.p95, res.max = times[len(times)/2], times[len(times)*95/100], times[len(times)-1]
	return res
}

func ms(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%d мкс", d.Microseconds())
	}
	return fmt.Sprintf("%.1f мс", float64(d.Microseconds())/1000)
}

func statusSummary(r loadResult) string {
	var parts []string
	for code, n := range r.status {
		parts = append(parts, fmt.Sprintf("%d×%d", code, n))
	}
	slices.Sort(parts)
	return fmt.Sprintf("%s, до %d КБ", strings.Join(parts, " "), (r.bytes+1023)/1024)
}

// loadFx — данные, найденные в заполненной базе: чем спрашивать.
type loadFx struct {
	vacancies    []string
	profiles     []string
	orgs         [][2]string // id, slug
	applicantApp string
	words        []string // слова, которые есть в текстах вакансий
	fields       []string
	regions      []string
	tokens       map[string]string
}

func (f *loadFx) word(i int) string { return f.words[i%len(f.words)] }
func (f *loadFx) typo(i int) string {
	w := []rune(f.words[i%len(f.words)])
	w[len(w)/2] = 'ъ' // одна «опечатка» в середине слова
	return string(w)
}
func (f *loadFx) cityWord(i int) string {
	return []string{"Москва", "Казань", "Новосибирск", "Томск", "Самара", "Пермь", "Дубна", "Тула"}[i%8]
}
func (f *loadFx) vacancy(i int) string { return f.vacancies[i%len(f.vacancies)] }
func (f *loadFx) profile(i int) string { return f.profiles[i%len(f.profiles)] }
func (f *loadFx) orgSlug(i int) string { return f.orgs[i%len(f.orgs)][1] }
func (f *loadFx) field(i int) string   { return f.fields[i%len(f.fields)] }
func (f *loadFx) region(i int) string  { return f.regions[i%len(f.regions)] }
func (f *loadFx) posType(i int) string {
	return []string{"research", "teaching", "admin", "phd", "masters", "project", "internship"}[i%7]
}

func loadFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *loadFx {
	t.Helper()
	fx := &loadFx{tokens: map[string]string{}}
	strs := func(sql string) []string {
		rows, err := pool.Query(ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		if err := rows.Err(); err != nil || len(out) == 0 {
			t.Fatalf("нет данных для %q: %v (запустите `make load-data`)", sql, err)
		}
		return out
	}
	fx.words = strs(`SELECT DISTINCT split_part(summary, ' ', 1) FROM (SELECT summary FROM vacancies WHERE status = 'published' ORDER BY random() LIMIT 400) x`)
	fx.fields = strs(`SELECT code FROM science_groups ORDER BY random() LIMIT 12`)
	fx.regions = strs(`SELECT code FROM regions ORDER BY random() LIMIT 12`)
	fx.vacancies = strs(`SELECT id::text FROM vacancies WHERE status = 'published' ORDER BY random() LIMIT 200`)
	fx.profiles = strs(`SELECT id::text FROM profiles WHERE visibility = 'public' ORDER BY random() LIMIT 200`)
	for _, r := range strs(`SELECT id::text || ' ' || slug FROM organizations ORDER BY random() LIMIT 100`) {
		id, slug, _ := strings.Cut(r, " ")
		fx.orgs = append(fx.orgs, [2]string{id, slug})
	}
	// Соискатель: человек с несколькими откликами, не сотрудник организации.
	row := pool.QueryRow(ctx, `SELECT a.user_id::text, a.id::text FROM applications a
		WHERE NOT EXISTS (SELECT 1 FROM org_members m WHERE m.user_id = a.user_id) ORDER BY random() LIMIT 1`)
	var applicant string
	if err := row.Scan(&applicant, &fx.applicantApp); err != nil {
		t.Fatal(err)
	}
	// Владелец организации-лидера (org-1 получает больше всего вакансий и откликов, номер 1 хозяина — первый человек).
	owner := strs(`SELECT user_id::text FROM org_members WHERE role = 'owner' AND org_id = (SELECT id FROM organizations WHERE slug = 'org-1')`)[0]
	for name, user := range map[string]string{"applicant": applicant, "owner_big": owner} {
		token := "loadcheck-" + name
		fx.tokens[name] = token
		if _, err := pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, token); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, user_agent, ip)
			VALUES ($1::uuid, sha256(convert_to($2, 'UTF8')), now(), now(), now() + interval '2 days', 'loadcheck', '10.9.9.9')`, user, token); err != nil {
			t.Fatal(err)
		}
	}
	return fx
}

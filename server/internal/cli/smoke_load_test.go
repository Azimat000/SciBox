package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scibox/server/internal/config"
	"scibox/server/internal/dbpool"
)

// Дымовой нагрузочный тест (срез 27, D-178). Это не замер скорости (его делает `make load-test` на объёмных данных),
// а проверка, что сервер остаётся здоровым, когда запросы идут одновременно: в смеси «поиск, страницы, личные списки,
// решения, вход» работает несколько десятков потоков через настоящий роутер, настоящий пул соединений (те же сроки и
// размер, что в работе) и ограничение частоты. Он ловит то, что один запрос за раз не показывает: исчерпание пула
// (обработчик держит соединение и просит второе), гонки данных (тест идёт под `-race`), 5xx и 429 под одновременностью,
// утечку соединений. Задержка проверяется грубо (p99 < 3 с): под `-race` и на маленьком CI время ответа в 5–10 раз
// больше обычного, а настоящие бюджеты проверяет нагрузочный тест на большой базе.
const (
	smokeRequests = 3000
	smokeWorkers  = 24
	smokeP99      = 3 * time.Second
)

type smokeKind struct {
	name   string
	weight int
	who    string // guest, applicant, owner
	path   func(*smokeFx, *rand.Rand) string
	method string
	want   int
}

type smokeFx struct {
	vacancies []string
	profiles  []string
	orgs      []string
	words     []string
}

func (f *smokeFx) pick(r *rand.Rand, s []string) string { return s[r.IntN(len(s))] }

func TestMixedLoadStaysHealthy(t *testing.T) {
	w := newPerimeterWorld(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Тот же пул, что у сервера в работе: размер, сроки, план под значения.
	pool, err := dbpool.New(ctx, w.env["DATABASE_URL"], dbpool.Default(), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	env := map[string]string{"DATABASE_URL": w.env["DATABASE_URL"], "SCIBOX_PRODUCT_CONFIG": productConfig, "SCIBOX_TRUSTED_PROXIES": "192.0.2.1"}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	// Часы идут вперёд на минуту за каждые 200 обращений к ним: счётчики частоты каждого человека не накапливаются
	// в одном окне, как не накапливались бы за настоящие минуты, а сеансы (30 суток) за время теста не истекают.
	base := time.Date(2026, 10, 8, 12, 0, 30, 0, time.UTC)
	var ticks atomic.Int64
	clock := func() time.Time { return base.Add(time.Duration(ticks.Add(1)/200) * time.Minute) }
	h := newApp(cfg, pool, w.vault, logger, okHealth{}, w.web, clock).handler

	// Что спрашивать: идентификаторы из самой базы.
	fx := &smokeFx{}
	for _, q := range []struct {
		dst *[]string
		sql string
	}{
		{&fx.vacancies, `SELECT id::text FROM vacancies WHERE status = 'published'`},
		{&fx.profiles, `SELECT id::text FROM profiles WHERE visibility = 'public' AND headline <> ''`},
		{&fx.orgs, `SELECT slug FROM organizations`},
		{&fx.words, `SELECT DISTINCT split_part(title, ' ', 1) FROM vacancies WHERE status = 'published' LIMIT 40`},
	} {
		rows, err := w.pool.Query(ctx, q.sql)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			*q.dst = append(*q.dst, s)
		}
		rows.Close()
		if len(*q.dst) == 0 {
			t.Fatalf("в демо-данных нет строк для %q", q.sql)
		}
	}

	// Люди: учёные с откликами и владельцы организаций. Каждый входит один раз; потоки пользуются их cookie.
	emails := func(sql string) []string {
		rows, err := w.pool.Query(ctx, sql)
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
		return out
	}
	applicants := emails(`SELECT DISTINCT u.email FROM applications a JOIN users u ON u.id = a.user_id
		WHERE NOT EXISTS (SELECT 1 FROM org_members m WHERE m.user_id = u.id) LIMIT 8`)
	owners := emails(`SELECT u.email FROM org_members m JOIN users u ON u.id = m.user_id WHERE m.role = 'owner' LIMIT 4`)
	if len(applicants) < 3 || len(owners) < 2 {
		t.Fatalf("мало людей в демо-данных: учёных %d, владельцев %d", len(applicants), len(owners))
	}
	var cookies = map[string][]*http.Cookie{}
	for _, e := range applicants {
		cookies["applicant"] = append(cookies["applicant"], login(t, h, e, "192.0.2.1:4000", map[string]string{"X-Forwarded-For": "198.51.100.1"}))
	}
	for _, e := range owners {
		cookies["owner"] = append(cookies["owner"], login(t, h, e, "192.0.2.1:4000", map[string]string{"X-Forwarded-For": "198.51.100.2"}))
	}

	get := func(p string) func(*smokeFx, *rand.Rand) string {
		return func(*smokeFx, *rand.Rand) string { return p }
	}
	kinds := []smokeKind{
		{"vacancies", 10, "guest", get("/api/vacancies?limit=20"), "GET", 200},
		{"vacancies.word", 14, "guest", func(f *smokeFx, r *rand.Rand) string { return "/api/vacancies?q=" + f.pick(r, f.words) }, "GET", 200},
		{"vacancy", 18, "guest", func(f *smokeFx, r *rand.Rand) string { return "/api/vacancies/" + f.pick(r, f.vacancies) }, "GET", 200},
		{"organization", 4, "guest", func(f *smokeFx, r *rand.Rand) string { return "/api/organizations/" + f.pick(r, f.orgs) }, "GET", 200},
		{"organizations", 2, "guest", get("/api/organizations"), "GET", 200},
		{"scientists", 4, "guest", get("/api/scientists"), "GET", 200},
		{"scientist", 4, "guest", func(f *smokeFx, r *rand.Rand) string { return "/api/scientists/" + f.pick(r, f.profiles) }, "GET", 200},
		{"landing", 6, "guest", get("/api/landing"), "GET", 200},
		{"reference", 4, "guest", get("/api/reference"), "GET", 200},
		{"me", 8, "applicant", get("/api/auth/me"), "GET", 200},
		{"unread", 6, "applicant", get("/api/notifications/unread-count"), "GET", 200},
		{"applications", 3, "applicant", get("/api/applications"), "GET", 200},
		{"matches", 3, "applicant", get("/api/matches"), "GET", 200},
		{"favorites", 2, "applicant", get("/api/favorites"), "GET", 200},
		{"profile", 2, "applicant", get("/api/profile"), "GET", 200},
		{"favorite.add", 2, "applicant", func(f *smokeFx, r *rand.Rand) string { return "/api/favorites/" + f.pick(r, f.vacancies) }, "PUT", 204},
		{"candidates", 3, "owner", get("/api/my/candidates"), "GET", 200},
		{"my.vacancies", 1, "owner", get("/api/my/vacancies"), "GET", 200},
	}
	total := 0
	for _, k := range kinds {
		total += k.weight
	}

	type result struct {
		kind    string
		status  int
		elapsed time.Duration
	}
	var (
		mu      sync.Mutex
		results = make([]result, 0, smokeRequests)
		issued  atomic.Int64
		wg      sync.WaitGroup
	)
	deadline := time.Now().Add(60 * time.Second)
	for worker := range smokeWorkers {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(worker), 7)) //nolint:gosec // G404: воспроизводимый выбор запросов в тесте, не защита
			for issued.Add(1) <= smokeRequests && time.Now().Before(deadline) {
				pickN := rng.IntN(total)
				var k smokeKind
				for _, c := range kinds {
					if pickN < c.weight {
						k = c
						break
					}
					pickN -= c.weight
				}
				r := req{
					method: k.method, path: k.path(fx, rng), remote: "192.0.2.1:5000",
					header: map[string]string{"X-Forwarded-For": fmt.Sprintf("203.0.%d.%d", rng.IntN(250), 1+rng.IntN(250))},
				}
				if cs := cookies[k.who]; len(cs) > 0 {
					r.cookie = cs[rng.IntN(len(cs))]
				}
				start := time.Now()
				rec := send(h, r)
				elapsed := time.Since(start)
				mu.Lock()
				results = append(results, result{k.name, rec.Code, elapsed})
				mu.Unlock()
				if rec.Code != k.want && rec.Code < 500 {
					t.Errorf("%s %s: код %d, ждали %d: %.200s", k.method, r.path, rec.Code, k.want, rec.Body.String())
				}
			}
		})
	}
	wg.Wait()

	if len(results) < smokeRequests {
		t.Fatalf("за минуту выполнено только %d запросов из %d: сервер завис или стал очень медленным", len(results), smokeRequests)
	}
	var server, throttled int
	times := make([]time.Duration, 0, len(results))
	perKind := map[string][]time.Duration{}
	for _, r := range results {
		times = append(times, r.elapsed)
		perKind[r.kind] = append(perKind[r.kind], r.elapsed)
		switch {
		case r.status >= 500:
			server++
		case r.status == http.StatusTooManyRequests:
			throttled++
		}
	}
	slices.Sort(times)
	p := func(ts []time.Duration, q float64) time.Duration { return ts[min(len(ts)-1, int(float64(len(ts))*q))] }
	var sb strings.Builder
	for name, ts := range perKind {
		slices.Sort(ts)
		fmt.Fprintf(&sb, "\n  %-14s %4d  p50 %-9v p95 %-9v max %v", name, len(ts), p(ts, 0.5).Round(time.Microsecond), p(ts, 0.95).Round(time.Microsecond), ts[len(ts)-1].Round(time.Microsecond))
	}
	t.Logf("запросов %d, p50 %v, p95 %v, p99 %v, max %v%s", len(times), p(times, 0.5), p(times, 0.95), p(times, 0.99), times[len(times)-1], sb.String())

	if server > 0 {
		t.Errorf("ответов 5xx: %d из %d (под нагрузкой не должно быть ни одного)", server, len(results))
	}
	if throttled > 0 {
		t.Errorf("ответов 429: %d: ограничение частоты сработало на обычной смеси", throttled)
	}
	if p99 := p(times, 0.99); p99 > smokeP99 {
		t.Errorf("p99 = %v, больше %v", p99, smokeP99)
	}
	// Соединения вернулись в пул: ни один обработчик не оставил своё.
	deadlineConns := time.Now().Add(3 * time.Second)
	for pool.Stat().AcquiredConns() > 0 && time.Now().Before(deadlineConns) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("после теста в пуле заняты %d соединений: утечка", n)
	}
	// Ответы остаются настоящим JSON и после нагрузки.
	rec := send(h, req{path: "/api/landing", remote: "192.0.2.1:5000", header: map[string]string{"X-Forwarded-For": "203.0.113.77"}})
	if rec.Code != 200 || !json.Valid(rec.Body.Bytes()) {
		t.Errorf("после нагрузки /api/landing: %d %.100s", rec.Code, rec.Body)
	}
}

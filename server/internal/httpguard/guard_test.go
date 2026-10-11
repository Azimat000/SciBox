package httpguard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/apierr"
)

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clock — подставные часы, которые тест двигает сам.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testWho: вошедший человек передаётся заголовком X-Test-User.
func testWho(r *http.Request) (string, bool) {
	id := r.Header.Get("X-Test-User")
	return id, id != ""
}

var (
	testSearch = &Policy{Name: "search", Guest: Rate{Requests: 3, Per: time.Minute}, User: Rate{Requests: 2, Per: time.Minute}}
	testLinks  = &Policy{Name: "links", Guest: Rate{Requests: 1, Per: 10 * time.Second}, User: Rate{Requests: 1, Per: 10 * time.Second}}
)

func testRoutes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/api/search", Limit: testSearch},
		{Method: http.MethodGet, Pattern: "/api/search/{id}", Limit: testSearch},
		{Method: http.MethodPost, Pattern: "/api/links/{token}", Limit: testLinks},
		{Method: http.MethodPost, Pattern: "/api/upload", Body: 2 << 20, Slow: true},
		// Корень поддерева: chi.Walk пишет его с косой чертой, поиск адреса — без неё.
		{Method: http.MethodGet, Pattern: "/api/things/{id}/", Limit: testLinks},
		{Method: http.MethodGet, Pattern: "/api/groups", Limit: testLinks},
	}
}

// handled запоминает, дошёл ли запрос до обработчика и что он прочитал из тела.
type handled struct {
	mu    sync.Mutex
	calls int
	read  int64
	err   error
}

func (h *handled) handler(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	h.mu.Lock()
	h.calls++
	h.read, h.err = n, err
	h.mu.Unlock()
	if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
		apierr.WriteError(w, http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, "too big")
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]string{"ip": ClientIP(r)})
}

type testServer struct {
	h     http.Handler
	guard *Guard
	got   *handled
}

func newTestServer(t *testing.T, cfg Config) testServer {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = silent()
	}
	if cfg.Routes == nil {
		cfg.Routes = testRoutes()
	}
	got := &handled{}
	r := chi.NewRouter()
	g, err := New(cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Use(g.Perimeter)
	r.Route("/api", func(r chi.Router) {
		r.Use(g.Throttle(testWho))
		r.Get("/search", got.handler)
		r.Get("/search/{id}", got.handler)
		r.Get("/free", got.handler)
		r.Post("/free", got.handler)
		r.Post("/links/{token}", got.handler)
		r.Post("/upload", got.handler)
		r.Route("/things/{id}", func(r chi.Router) { r.Get("/", got.handler) })
		r.Route("/groups", func(r chi.Router) { r.Get("/", got.handler) })
		r.Get("/file", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'none'")
			_, _ = w.Write([]byte("%PDF-"))
		})
	})
	r.Get("/*", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<!doctype html>")) })
	return testServer{h: r, guard: g, got: got}
}

type call struct {
	method, path string
	body         io.Reader
	length       int64 // -1 — тело без длины (chunked)
	remote       string
	user         string
	header       map[string]string
}

func (s testServer) do(c call) *httptest.ResponseRecorder {
	if c.method == "" {
		c.method = http.MethodGet
	}
	req := httptest.NewRequest(c.method, c.path, c.body)
	if c.body != nil && c.length != 0 {
		req.ContentLength = c.length
	}
	if c.remote != "" {
		req.RemoteAddr = c.remote
	}
	if c.user != "" {
		req.Header.Set("X-Test-User", c.user)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) apierr.ErrorDetail {
	t.Helper()
	var body apierr.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an API error: %d %s", rec.Code, rec.Body)
	}
	return body.Error
}

func TestNewChecksConfig(t *testing.T) {
	router := chi.NewRouter()
	ok := Route{Method: http.MethodGet, Pattern: "/api/x"}
	rate := Rate{Requests: 1, Per: time.Minute}
	cases := []struct {
		name   string
		cfg    Config
		router Finder
		want   string
	}{
		{name: "no router", cfg: Config{Logger: silent()}, want: "router is required"},
		{name: "no logger", cfg: Config{}, router: router, want: "logger is required"},
		{name: "unknown method", cfg: Config{Logger: silent(), Routes: []Route{{Method: "BREW", Pattern: "/api/x"}}}, router: router, want: `unknown method "BREW"`},
		{name: "empty method", cfg: Config{Logger: silent(), Routes: []Route{{Pattern: "/api/x"}}}, router: router, want: "unknown method"},
		{name: "relative pattern", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "api/x"}}}, router: router, want: "must start with /"},
		{name: "empty pattern", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet}}}, router: router, want: "must start with /"},
		{name: "negative body", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodPost, Pattern: "/api/x", Body: -1}}}, router: router, want: "negative body limit"},
		{name: "duplicate route", cfg: Config{Logger: silent(), Routes: []Route{ok, ok}}, router: router, want: "listed twice"},
		{name: "unnamed policy", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Guest: rate, User: rate}}}}, router: router, want: "policy without a name"},
		{name: "zero guest requests", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: Rate{Per: time.Minute}, User: rate}}}}, router: router, want: "guest rate"},
		{name: "zero user rate", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: rate}}}}, router: router, want: "user rate"},
		{name: "negative requests", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: Rate{Requests: -5, Per: time.Minute}, User: rate}}}}, router: router, want: "guest rate"},
		{name: "window too short", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: Rate{Requests: 1, Per: time.Millisecond}, User: rate}}}}, router: router, want: "guest rate"},
		{name: "window too long", cfg: Config{Logger: silent(), Routes: []Route{{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: rate, User: Rate{Requests: 1, Per: 2 * time.Hour}}}}}, router: router, want: "user rate"},
		{name: "one name, two limits", cfg: Config{Logger: silent(), Routes: []Route{
			{Method: http.MethodGet, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: rate, User: rate}},
			{Method: http.MethodGet, Pattern: "/api/y", Limit: &Policy{Name: "p", Guest: Rate{Requests: 2, Per: time.Minute}, User: rate}},
		}}, router: router, want: "two different sets of limits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := New(tc.cfg, tc.router)
			if err == nil || !strings.Contains(err.Error(), tc.want) || g != nil {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}

	// Одна политика на нескольких адресах (одинаковые копии) допустима; значения по умолчанию подставлены.
	same := Policy{Name: "p", Guest: rate, User: rate}
	routes := []Route{
		{Method: http.MethodGet, Pattern: "/api/x", Limit: &same},
		{Method: http.MethodHead, Pattern: "/api/x", Limit: &Policy{Name: "p", Guest: rate, User: rate}},
		{Method: http.MethodPut, Pattern: "/api/y"}, {Method: http.MethodPatch, Pattern: "/api/y"}, {Method: http.MethodDelete, Pattern: "/api/y"},
	}
	g, err := New(Config{Logger: silent(), Routes: routes, Transfer: -time.Second}, router)
	if err != nil {
		t.Fatal(err)
	}
	if _, mem := g.cfg.Store.(*MemoryStore); !mem || g.cfg.Now == nil || g.cfg.Transfer != DefaultTimeouts().Transfer {
		t.Fatalf("defaults: store %T, now set %v, transfer %v", g.cfg.Store, g.cfg.Now != nil, g.cfg.Transfer)
	}
	if len(g.Routes()) != len(routes) {
		t.Fatalf("Routes() = %v", g.Routes())
	}
}

// Заголовки защиты (T-05) на каждом виде ответа: API, страница сайта, файл (свой CSP-песочница
// не затирается), ошибка; HSTS только при https.
func TestSecurityHeaders(t *testing.T) {
	common := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"Permissions-Policy":           permissionsPolicy,
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for _, https := range []bool{false, true} {
		s := newTestServer(t, Config{HTTPS: https})
		cases := []struct {
			name, method, path, csp string
			status                  int
		}{
			{"api json", http.MethodGet, "/api/free", apiCSP, 200},
			{"api not found", http.MethodGet, "/api/nope", apiCSP, 404},
			{"api too big", http.MethodPost, "/api/free", apiCSP, 413},
			{"file download keeps its sandbox", http.MethodGet, "/api/file", "sandbox; default-src 'none'; frame-ancestors 'none'", 200},
			{"site page", http.MethodGet, "/vacancies/42", siteCSP, 200},
			{"site root", http.MethodGet, "/", siteCSP, 200},
		}
		for _, tc := range cases {
			t.Run(tc.name+" https="+strconv.FormatBool(https), func(t *testing.T) {
				c := call{method: tc.method, path: tc.path}
				if tc.status == 413 {
					c.body, c.length = strings.NewReader("x"), DefaultBody+1
				}
				rec := s.do(c)
				if rec.Code != tc.status {
					t.Fatalf("status %d", rec.Code)
				}
				h := rec.Header()
				want := tc.csp
				if https && tc.csp == siteCSP {
					want += "; upgrade-insecure-requests"
				}
				if got := h.Get("Content-Security-Policy"); got != want {
					t.Fatalf("CSP = %q, want %q", got, want)
				}
				for k, v := range common {
					if h.Get(k) != v {
						t.Errorf("%s = %q, want %q", k, h.Get(k), v)
					}
				}
				if got := h.Get("Strict-Transport-Security"); (got != "") != https || (https && got != hsts) {
					t.Errorf("HSTS = %q with https=%v", got, https)
				}
			})
		}
	}
	// Ни скриптам, ни стилям сайта не разрешено встраивание и чужие адреса; встроить сайт в рамку нельзя.
	for _, bad := range []string{"unsafe-inline", "unsafe-eval", "http:", "https:", "*"} {
		if strings.Contains(siteCSP, bad) || strings.Contains(apiCSP, bad) {
			t.Errorf("CSP allows %q", bad)
		}
	}
	for _, csp := range []string{siteCSP, apiCSP} {
		if !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("CSP without frame-ancestors: %q", csp)
		}
	}
}

func TestIsAPIPath(t *testing.T) {
	for path, want := range map[string]bool{"/api": true, "/api/": true, "/api/x/y": true, "/apix": false, "/": false, "/vacancies": false} {
		if IsAPIPath(path) != want {
			t.Errorf("IsAPIPath(%q) = %v", path, !want)
		}
	}
}

// Лимит тела (T-09): по длине из заголовка запрос отвергается до обработчика; тело без длины
// обрезается на лимите, и обработчик получает ошибку чтения, а не гигабайты.
func TestBodyLimits(t *testing.T) {
	s := newTestServer(t, Config{})
	big := func(n int64) io.Reader { return io.LimitReader(zeros{}, n) }

	t.Run("declared length over default limit", func(t *testing.T) {
		rec := s.do(call{method: http.MethodPost, path: "/api/free", body: big(DefaultBody + 1), length: DefaultBody + 1})
		if rec.Code != http.StatusRequestEntityTooLarge || errorOf(t, rec).Code != apierr.CodePayloadTooLarge || s.got.calls != 0 {
			t.Fatalf("%d %s, handler calls %d", rec.Code, rec.Body, s.got.calls)
		}
	})
	t.Run("declared length over limit on a route without body", func(t *testing.T) {
		rec := s.do(call{method: http.MethodGet, path: "/vacancies", body: big(DefaultBody + 1), length: DefaultBody + 1})
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("exactly at the limit", func(t *testing.T) {
		rec := s.do(call{method: http.MethodPost, path: "/api/free", body: big(DefaultBody), length: DefaultBody})
		if rec.Code != http.StatusOK || s.got.read != DefaultBody || s.got.err != nil {
			t.Fatalf("%d, read %d, err %v", rec.Code, s.got.read, s.got.err)
		}
	})
	t.Run("chunked body over limit is cut", func(t *testing.T) {
		rec := s.do(call{method: http.MethodPost, path: "/api/free", body: big(10 * DefaultBody), length: -1})
		var mbe *http.MaxBytesError
		if rec.Code != http.StatusRequestEntityTooLarge || !errors.As(s.got.err, &mbe) || mbe.Limit != DefaultBody || s.got.read > DefaultBody {
			t.Fatalf("%d, read %d, err %v", rec.Code, s.got.read, s.got.err)
		}
	})
	t.Run("upload route allows its own limit", func(t *testing.T) {
		rec := s.do(call{method: http.MethodPost, path: "/api/upload", body: big(DefaultBody * 3 / 2), length: DefaultBody * 3 / 2})
		if rec.Code != http.StatusOK || s.got.read != DefaultBody*3/2 {
			t.Fatalf("%d, read %d", rec.Code, s.got.read)
		}
	})
	t.Run("upload route over its own limit", func(t *testing.T) {
		before := s.got.calls
		rec := s.do(call{method: http.MethodPost, path: "/api/upload", body: big(2<<20 + 1), length: 2<<20 + 1})
		if rec.Code != http.StatusRequestEntityTooLarge || s.got.calls != before {
			t.Fatalf("%d", rec.Code)
		}
		rec = s.do(call{method: http.MethodPost, path: "/api/upload", body: big(5 << 20), length: -1})
		if rec.Code != http.StatusRequestEntityTooLarge || s.got.read > 2<<20 {
			t.Fatalf("chunked: %d, read %d", rec.Code, s.got.read)
		}
	})
	t.Run("no body", func(t *testing.T) {
		if rec := s.do(call{method: http.MethodGet, path: "/api/free"}); rec.Code != http.StatusOK {
			t.Fatalf("%d", rec.Code)
		}
	})
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// Ограничение частоты (T-07): гость по адресу, вошедший по аккаунту, у каждой политики свой счётчик,
// 429 в формате D-032 с Retry-After до конца окна, новое окно начинает счёт заново.
func TestThrottle(t *testing.T) {
	clk := newClock()
	clk.Add(15 * time.Second) // 12:00:15 — до конца минутного окна 45 секунд
	s := newTestServer(t, Config{Now: clk.Now})
	get := func(path, remote, user string) *httptest.ResponseRecorder {
		return s.do(call{path: path, remote: remote, user: user})
	}

	for i := range 3 {
		if rec := get("/api/search", "203.0.113.7:1", ""); rec.Code != http.StatusOK {
			t.Fatalf("guest request %d: %d", i+1, rec.Code)
		}
	}
	// Другие адреса той же политики делят счётчик.
	rec := get("/api/search/42", "203.0.113.7:2", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th guest request: %d", rec.Code)
	}
	e := errorOf(t, rec)
	if e.Code != apierr.CodeRateLimited || e.RetryAfter != 45 || rec.Header().Get("Retry-After") != "45" || e.Message == "" {
		t.Fatalf("429 body %+v, Retry-After %q", e, rec.Header().Get("Retry-After"))
	}
	if s.got.calls != 3 {
		t.Fatalf("handler calls = %d: refused request must not reach it", s.got.calls)
	}
	// Другой гость, тот же гость на свободном адресе, вошедший с того же адреса — не задеты.
	if rec := get("/api/search", "198.51.100.1:1", ""); rec.Code != http.StatusOK {
		t.Fatalf("other guest: %d", rec.Code)
	}
	if rec := get("/api/free", "203.0.113.7:1", ""); rec.Code != http.StatusOK {
		t.Fatalf("unlimited route: %d", rec.Code)
	}
	if rec := s.do(call{method: http.MethodPost, path: "/api/links/abc", remote: "203.0.113.7:1"}); rec.Code != http.StatusOK {
		t.Fatalf("other policy: %d", rec.Code)
	}
	for i := range 2 {
		if rec := get("/api/search", "203.0.113.7:1", "u1"); rec.Code != http.StatusOK {
			t.Fatalf("user request %d: %d", i+1, rec.Code)
		}
	}
	// Вошедший считается по аккаунту: смена адреса не помогает, другой аккаунт не задет.
	if rec := get("/api/search", "192.0.2.99:1", "u1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd user request from another address: %d", rec.Code)
	}
	if rec := get("/api/search", "203.0.113.7:1", "u2"); rec.Code != http.StatusOK {
		t.Fatalf("other user: %d", rec.Code)
	}

	// Новое окно — счёт заново.
	clk.Add(45 * time.Second)
	if rec := get("/api/search", "203.0.113.7:1", ""); rec.Code != http.StatusOK {
		t.Fatalf("next window guest: %d", rec.Code)
	}
	if rec := get("/api/search", "192.0.2.99:1", "u1"); rec.Code != http.StatusOK {
		t.Fatalf("next window user: %d", rec.Code)
	}

	// Окно в 10 секунд: Retry-After не меньше секунды даже в самом конце окна.
	clk.Add(9*time.Second + 900*time.Millisecond)
	s.do(call{method: http.MethodPost, path: "/api/links/x", remote: "192.0.2.5:1"})
	rec = s.do(call{method: http.MethodPost, path: "/api/links/y", remote: "192.0.2.5:1"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("end of window: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// Правило находится и для корня поддерева chi, как бы ни была записана косая черта в конце.
func TestThrottleSubrouterRoots(t *testing.T) {
	s := newTestServer(t, Config{})
	for i, path := range []string{"/api/things/1", "/api/groups"} {
		remote := "192.0.2." + strconv.Itoa(i+1) + ":1"
		if rec := s.do(call{path: path, remote: remote}); rec.Code != http.StatusOK {
			t.Fatalf("%s first: %d", path, rec.Code)
		}
		if rec := s.do(call{path: path, remote: remote}); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s second: %d — the rule was not found", path, rec.Code)
		}
	}
}

// Адрес гостя для лимита — с учётом доверенных прокси: за прокси каждый человек считается сам по себе.
func TestThrottleBehindProxy(t *testing.T) {
	s := newTestServer(t, Config{Proxies: mustProxies(t, "127.0.0.1")})
	for i := range 3 {
		ip := "198.51.100." + strconv.Itoa(i+1)
		for range 3 {
			rec := s.do(call{path: "/api/search", remote: "127.0.0.1:5000", header: map[string]string{"X-Forwarded-For": ip}})
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ip) {
				t.Fatalf("client %s: %d %s", ip, rec.Code, rec.Body)
			}
		}
	}
	rec := s.do(call{path: "/api/search", remote: "127.0.0.1:5000", header: map[string]string{"X-Forwarded-For": "198.51.100.1"}})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th request of one client: %d", rec.Code)
	}
	// Подделать адрес слева нельзя: считается тот, кого записал прокси.
	rec = s.do(call{path: "/api/search", remote: "127.0.0.1:5000", header: map[string]string{"X-Forwarded-For": "8.8.8.8, 198.51.100.1"}})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forged left part: %d", rec.Code)
	}
}

type failingStore struct{}

func (failingStore) Hit(context.Context, string, string, time.Time) (int, error) {
	return 0, errors.New("counter table is broken")
}

// Сбой счётчиков — это 500, а не пропуск без счёта (I-09); обрыв клиентом — 499.
func TestThrottleStoreFailure(t *testing.T) {
	s := newTestServer(t, Config{Store: failingStore{}})
	rec := s.do(call{path: "/api/search"})
	if rec.Code != http.StatusInternalServerError || errorOf(t, rec).Code != apierr.CodeInternal || strings.Contains(rec.Body.String(), "broken") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if s.got.calls != 0 {
		t.Fatal("request reached the handler without being counted")
	}
	if rec := s.do(call{path: "/api/free"}); rec.Code != http.StatusOK {
		t.Fatalf("route without a limit must not touch the store: %d", rec.Code)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/search", nil)
	out := httptest.NewRecorder()
	s.h.ServeHTTP(out, req)
	if out.Code != apierr.StatusClientClosed {
		t.Fatalf("cancelled: %d", out.Code)
	}
}

// Throttle без Perimeter перед ним сам находит правила адреса: забытый Perimeter не отключает лимиты.
func TestThrottleWithoutPerimeter(t *testing.T) {
	r := chi.NewRouter()
	g, err := New(Config{Logger: silent(), Routes: testRoutes()}, r)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	r.With(g.Throttle(nil)).Get("/api/search", func(http.ResponseWriter, *http.Request) { calls++ })
	codes := []int{}
	for range 4 {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search", nil))
		codes = append(codes, rec.Code)
	}
	if calls != 3 || codes[3] != http.StatusTooManyRequests {
		t.Fatalf("calls %d, codes %v", calls, codes)
	}
	// who == nil: все гости, даже с заголовком «вошедшего».
	req := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	req.Header.Set("X-Test-User", "u1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("who=nil: %d", rec.Code)
	}
}

// Одновременные запросы не проскакивают мимо предела.
func TestThrottleConcurrent(t *testing.T) {
	s := newTestServer(t, Config{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 20 {
		wg.Go(func() {
			if rec := s.do(call{path: "/api/search", remote: "203.0.113.7:1"}); rec.Code == http.StatusOK {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if ok != testSearch.Guest.Requests {
		t.Fatalf("passed %d of 20, want %d", ok, testSearch.Guest.Requests)
	}
}

// Нулевой знак и невалидный UTF-8 в пути и параметрах отвергаются на периметре до обработчика (срез 29, INJ-2).
func TestPerimeterRefusesUndecodableText(t *testing.T) {
	g, err := New(Config{Logger: silent(), Routes: testRoutes()}, chi.NewRouter())
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	h := g.Perimeter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	for _, c := range []struct {
		url  string
		want int
	}{
		{"/api/vacancies?q=%D1%84%D0%B8%D0%B7", http.StatusOK},
		{"/api/vacancies?q=%00", http.StatusBadRequest},
		{"/api/vacancies?org=%ff", http.StatusBadRequest},
		{"/api/organizations/%00", http.StatusBadRequest},
		{"/api/scientists?field=1&field=a%00", http.StatusBadRequest},
	} {
		reached = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != c.want || (c.want != http.StatusOK && reached) {
			t.Errorf("%s: status %d (reached=%v), want %d", c.url, rec.Code, reached, c.want)
		}
	}
}

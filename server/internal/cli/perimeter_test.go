package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"scibox/server/internal/antivirus"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/files"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/config"
	"scibox/server/internal/health"
	"scibox/server/internal/httpapi"
	"scibox/server/internal/httpguard"
	"scibox/server/internal/testdb"
	"scibox/server/seed"
)

type okHealth struct{}

func (okHealth) CheckDatabase(context.Context) (health.Database, error) {
	return health.Database{SchemaVersion: 15, ServerVersion: "16"}, nil
}

// perimeterWorld — сервер, собранный как в работе (newApp), на базе с демо-данными.
type perimeterWorld struct {
	vault *files.Vault
	t     *testing.T
	pool  *pgxpool.Pool
	env   map[string]string
	web   http.Handler
}

func newPerimeterWorld(t *testing.T) *perimeterWorld {
	t.Helper()
	return newPerimeterWorldOn(t, newTestVault())
}

// newPerimeterWorldOn — то же на заданном складе файлов (настоящее хранилище и антивирус в полном сквозном тесте).
func newPerimeterWorldOn(t *testing.T, vault *files.Vault) *perimeterWorld {
	t.Helper()
	dbURL := testdb.Create(t, true)
	if err := runSeed(t.Context(), dbURL, vault, io.Discard); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>site</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &perimeterWorld{
		vault: vault,
		t:     t, pool: pool,
		env: map[string]string{"DATABASE_URL": dbURL, "SCIBOX_PRODUCT_CONFIG": productConfig},
		web: httpapi.WebHandler(os.DirFS(dir)),
	}
}

// handler собирает сервер с дополнительными настройками окружения.
func (w *perimeterWorld) handler(extra map[string]string) http.Handler {
	w.t.Helper()
	env := map[string]string{}
	for k, v := range w.env {
		env[k] = v
	}
	for k, v := range extra {
		env[k] = v
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		w.t.Fatal(err)
	}
	// Часы стоят: проверка частоты не должна зависеть от того, попала ли она на границу минуты.
	frozen := func() time.Time { return time.Date(2026, 10, 8, 12, 0, 30, 0, time.UTC) }
	return newApp(cfg, w.pool, w.vault, slog.New(slog.NewTextHandler(io.Discard, nil)), okHealth{}, w.web, frozen).handler
}

type req struct {
	method, path string
	body         io.Reader
	length       int64
	contentType  string
	cookie       *http.Cookie
	remote       string
	header       map[string]string
}

func send(h http.Handler, r req) *httptest.ResponseRecorder {
	if r.method == "" {
		r.method = http.MethodGet
	}
	hr := httptest.NewRequest(r.method, r.path, r.body)
	hr.ContentLength = r.length
	hr.Header.Set("Origin", "http://localhost:5173")
	if r.contentType != "" {
		hr.Header.Set("Content-Type", r.contentType)
	}
	if r.cookie != nil {
		hr.AddCookie(r.cookie)
	}
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, hr)
	return rec
}

func login(t *testing.T, h http.Handler, email, remote string, header map[string]string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, seed.DemoPassword)
	rec := send(h, req{method: http.MethodPost, path: "/api/auth/login", body: strings.NewReader(body), length: int64(len(body)),
		contentType: "application/json", remote: remote, header: header})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", email, rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "scibox_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// counter — тело запроса, которое считает, сколько из него прочитали.
type counter struct {
	prefix []byte
	size   int64
	read   int64
}

func (c *counter) Read(p []byte) (int, error) {
	if c.read >= c.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	if left := c.size - c.read; n > left {
		n = left
	}
	for i := range n {
		pos := c.read + i
		if pos < int64(len(c.prefix)) {
			p[i] = c.prefix[pos]
		} else {
			p[i] = 'a'
		}
	}
	c.read += n
	return int(n), nil
}

var param = regexp.MustCompile(`\{[^}]+\}`)

// TestPerimeter проверяет периметр на настоящей сборке сервера (newApp): таблица особых адресов совпадает
// с роутером, каждый адрес ограничивает тело (T-09), заголовки защиты на каждом виде ответа (T-05),
// частота ограничивается по счётчикам в базе (T-07), адрес за доверенным прокси доходит до сессий (T-06).
func TestPerimeter(t *testing.T) {
	w := newPerimeterWorld(t)
	h := w.handler(nil)

	var email string
	var appID, fileID uuid.UUID
	err := w.pool.QueryRow(t.Context(), `
		SELECT u.email, a.id, f.id FROM application_files f
		JOIN applications a ON a.id = f.application_id JOIN users u ON u.id = a.user_id
		WHERE f.kind = 'attachment' ORDER BY f.created_at LIMIT 1`).Scan(&email, &appID, &fileID)
	if err != nil {
		t.Fatal(err)
	}
	applicant := login(t, h, email, "192.0.2.10:1000", nil)

	routes := map[string]bool{}
	if err := chi.Walk(h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	limits := map[string]httpguard.Route{}
	for _, rt := range httpapi.PerimeterRoutes() {
		limits[rt.Method+" "+rt.Pattern] = rt
	}

	t.Run("every listed route exists in the router", func(t *testing.T) {
		if len(routes) < 80 {
			t.Fatalf("only %d routes walked: is the router fully built?", len(routes))
		}
		mux := h.(*chi.Mux)
		for key, rt := range limits {
			if !routes[key] {
				t.Errorf("perimeter lists %s, but the router has no such route", key)
			}
			// Периметр ищет правило по адресу запроса так же, как роутер: этот путь должен привести к тому же шаблону.
			path := strings.TrimSuffix(param.ReplaceAllString(rt.Pattern, "x"), "/")
			found := mux.Find(chi.NewRouteContext(), rt.Method, path)
			if strings.TrimSuffix(found, "/") != strings.TrimSuffix(rt.Pattern, "/") {
				t.Errorf("%s %s is found as %q", rt.Method, path, found)
			}
		}
	})

	t.Run("every route limits the request body", func(t *testing.T) {
		for _, key := range slices.Sorted(maps.Keys(routes)) {
			method, pattern, _ := strings.Cut(key, " ")
			path := param.ReplaceAllStringFunc(pattern, func(string) string { return uuid.NewString() })
			limit := limits[key].Body
			if limit == 0 {
				limit = httpguard.DefaultBody
			}
			for _, who := range []struct {
				name   string
				cookie *http.Cookie
			}{{"guest", nil}, {"signed in", applicant}} {
				if who.cookie != nil && (key == "POST /api/auth/logout" || key == "POST /api/account/deletion") {
					// Выход и просьба удалить аккаунт закрыли бы сессию для всех следующих адресов; тело они не читают,
					// гость проверяет их так же.
					continue
				}
				t.Run(key+" "+who.name, func(t *testing.T) {
					// Длина в заголовке больше лимита — отказ до чтения.
					declared := &counter{size: limit + 1}
					rec := send(h, req{method: method, path: path, body: declared, length: limit + 1,
						contentType: "application/json", cookie: who.cookie, remote: "192.0.2.20:1"})
					if rec.Code != http.StatusRequestEntityTooLarge || declared.read != 0 {
						t.Fatalf("declared %d bytes: %d, read %d", limit+1, rec.Code, declared.read)
					}
					// Тело без длины: обработчик читает не больше лимита, сколько бы ни прислали.
					body := &counter{prefix: []byte(`{"name":"`), size: limit + 1<<20}
					ct := "application/json"
					if limit > httpguard.DefaultBody {
						body.prefix = []byte("--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.pdf\"\r\nContent-Type: application/pdf\r\n\r\n")
						ct = "multipart/form-data; boundary=b"
					}
					rec = send(h, req{method: method, path: path, body: body, length: -1, contentType: ct,
						cookie: who.cookie, remote: "192.0.2.21:1"})
					if body.read > limit+1 {
						t.Fatalf("read %d bytes of the body, limit %d", body.read, limit)
					}
					if rec.Code >= 500 {
						t.Fatalf("oversized body gave %d %s", rec.Code, rec.Body)
					}
					if who.cookie != nil && rec.Code == http.StatusUnauthorized {
						t.Fatalf("the session was lost: the signed-in pass would check nothing")
					}
				})
			}
		}
	})

	t.Run("security headers on every kind of response", func(t *testing.T) {
		// Проход по всем адресам выше вызывал и «выйти»: входим заново.
		applicant := login(t, h, email, "192.0.2.11:1000", nil)
		cases := []struct {
			name, path, csp, contentType string
			cookie                       *http.Cookie
			status                       int
		}{
			{"api", "/api/health", "default-src 'none'", "application/json", nil, 200},
			{"api error", "/api/nope", "default-src 'none'", "application/json", nil, 404},
			{"site page", "/vacancies/42", "script-src 'self'", "text/html", nil, 200},
			{"application file", fmt.Sprintf("/api/applications/%s/files/%s", appID, fileID), "sandbox", "application/pdf", applicant, 200},
			{"cv pdf", "/api/profile/cv", "default-src 'none'", "application/pdf", applicant, 200},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := send(h, req{path: tc.path, cookie: tc.cookie, remote: "192.0.2.30:1"})
				hd := rec.Header()
				if rec.Code != tc.status || !strings.HasPrefix(hd.Get("Content-Type"), tc.contentType) {
					t.Fatalf("%d %s", rec.Code, hd.Get("Content-Type"))
				}
				csp := hd.Get("Content-Security-Policy")
				if !strings.Contains(csp, tc.csp) || !strings.Contains(csp, "frame-ancestors 'none'") {
					t.Fatalf("CSP = %q", csp)
				}
				for k, v := range map[string]string{
					"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Cross-Origin-Opener-Policy": "same-origin",
					"Referrer-Policy": "strict-origin-when-cross-origin",
				} {
					if hd.Get(k) != v {
						t.Errorf("%s = %q", k, hd.Get(k))
					}
				}
				if hd.Get("Permissions-Policy") == "" || hd.Get("Strict-Transport-Security") != "" {
					t.Errorf("Permissions-Policy %q, HSTS over http %q", hd.Get("Permissions-Policy"), hd.Get("Strict-Transport-Security"))
				}
			})
		}
		// Сайт на https получает HSTS.
		secure := w.handler(map[string]string{"SCIBOX_PUBLIC_URL": "https://scibox.example"})
		rec := send(secure, req{path: "/api/health"})
		if rec.Header().Get("Strict-Transport-Security") == "" {
			t.Fatal("no HSTS on an https site")
		}
	})

	t.Run("public reads are rate limited in the database", func(t *testing.T) {
		path := "/api/vacancies/" + uuid.NewString()
		for i := range 300 {
			if rec := send(h, req{path: path, remote: "198.51.100.77:1"}); rec.Code != http.StatusNotFound {
				t.Fatalf("guest request %d: %d", i+1, rec.Code)
			}
		}
		rec := send(h, req{path: "/api/landing", remote: "198.51.100.77:2"})
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || !strings.Contains(rec.Body.String(), `"rate_limited"`) {
			t.Fatalf("301st guest request: %d %s", rec.Code, rec.Body)
		}
		// Вторая копия сервера на той же базе видит тот же счёт.
		if rec := send(w.handler(nil), req{path: path, remote: "198.51.100.77:3"}); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("other server copy: %d", rec.Code)
		}
		// Вошедший с того же адреса считается по аккаунту.
		applicant := login(t, h, email, "192.0.2.12:1000", nil)
		if rec := send(h, req{path: path, remote: "198.51.100.77:4", cookie: applicant}); rec.Code != http.StatusNotFound {
			t.Fatalf("signed-in person from the same address: %d", rec.Code)
		}
		var n int
		if err := w.pool.QueryRow(t.Context(), `SELECT hits FROM rate_counters WHERE kind = 'public_read' AND key = 'ip:198.51.100.77'`).Scan(&n); err != nil || n < 301 {
			t.Fatalf("counter in the database = %d, %v", n, err)
		}
	})

	t.Run("client address behind a trusted proxy reaches sessions", func(t *testing.T) {
		header := map[string]string{"X-Forwarded-For": "203.0.113.55"}
		cases := []struct {
			name    string
			proxies string
			want    string
		}{
			{"no trusted proxies: header ignored", "", "127.0.0.1"},
			{"trusted proxy: client from header", "127.0.0.1", "203.0.113.55"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := w.handler(map[string]string{"SCIBOX_TRUSTED_PROXIES": tc.proxies})
				c := login(t, srv, seed.Logins()[1], "127.0.0.1:5000", header)
				var ip string
				err := w.pool.QueryRow(t.Context(), `SELECT ip FROM sessions WHERE token_hash = sha256(convert_to($1, 'UTF8'))`, c.Value).Scan(&ip)
				if err != nil {
					t.Fatal(err)
				}
				if ip != tc.want {
					t.Fatalf("session ip = %q, want %q", ip, tc.want)
				}
			})
		}
	})
}

// newTestVault — склад на хранилище в памяти и подставном антивирусе.
func newTestVault() *files.Vault {
	return files.NewVault(blobstore.NewMemory(), &antivirus.Fake{}, time.Now)
}

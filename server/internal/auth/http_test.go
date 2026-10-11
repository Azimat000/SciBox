package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// api — обработчики аккаунтов, вызываемые напрямую, со своим «браузерным» хранилищем cookie.
type api struct {
	t       *testing.T
	e       *env
	handler http.Handler
	ip      string
	origin  string // откуда «браузер» шлёт запросы
	cookies map[string]*http.Cookie
}

func routerFor(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(h.SameOrigin, h.Authenticate)
		h.Mount(r)
		r.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
			p, ok := FromContext(r.Context())
			if !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write([]byte(p.User.Email))
		})
	})
	return r
}

func newAPI(t *testing.T) *api {
	t.Helper()
	e := newEnv(t)
	return &api{t: t, e: e, handler: routerFor(NewHandler(e.svc, slog.New(slog.NewTextHandler(e.logs, nil)))), ip: e.meta().IP, origin: "http://localhost:5173", cookies: map[string]*http.Cookie{}}
}

type result struct {
	status int
	header http.Header
	body   []byte
}

func (r result) json(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal(r.body, into); err != nil {
		t.Fatalf("response %q is not the expected JSON: %v", r.body, err)
	}
}

type errBody struct {
	Error struct {
		Code       string            `json:"code"`
		Message    string            `json:"message"`
		Fields     map[string]string `json:"fields"`
		RetryAfter int               `json:"retry_after"`
	} `json:"error"`
}

func (r result) apiError(t *testing.T) errBody {
	t.Helper()
	var b errBody
	r.json(t, &b)
	if b.Error.Code == "" || b.Error.Message == "" {
		t.Fatalf("not an API error: %s", r.body)
	}
	return b
}

type reqOpt func(*http.Request)

func header(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func noHeader(k string) reqOpt  { return func(r *http.Request) { r.Header.Del(k) } }

// do отправляет запрос от имени «браузера»: с cookie, нужным Origin и JSON-телом.
func (a *api) do(method, path string, body any, opts ...reqOpt) result {
	a.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			a.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = a.ip + ":4242"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", a.origin)
	for _, c := range a.cookies {
		req.AddCookie(c)
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	res := rec.Result()
	for _, c := range res.Cookies() {
		if c.MaxAge < 0 {
			delete(a.cookies, c.Name)
		} else {
			a.cookies[c.Name] = c
		}
	}
	return result{status: res.StatusCode, header: res.Header, body: rec.Body.Bytes()}
}

func (a *api) post(path string, body any, opts ...reqOpt) result {
	a.t.Helper()
	return a.do(http.MethodPost, path, body, opts...)
}

func (a *api) get(path string, opts ...reqOpt) result {
	a.t.Helper()
	return a.do(http.MethodGet, path, nil, opts...)
}

type userResp struct {
	User *struct {
		ID             string `json:"id"`
		Email          string `json:"email"`
		Name           string `json:"name"`
		EmailConfirmed bool   `json:"email_confirmed"`
	} `json:"user"`
}

func (a *api) registerBody(email string) map[string]any {
	return map[string]any{"name": "Иван Петров", "email": email, "password": goodPassword, "consent": true}
}

// signedIn регистрирует, подтверждает и оставляет «браузер» с действующей сессией.
func (a *api) signedIn() string {
	a.t.Helper()
	email := uniqueEmail()
	if r := a.post("/api/auth/register", a.registerBody(email)); r.status != http.StatusAccepted {
		a.t.Fatalf("register: %d %s", r.status, r.body)
	}
	tok := tokenIn(a.t, a.e.lastMailTo(email))
	if r := a.post("/api/auth/confirm-email", map[string]string{"token": tok}); r.status != http.StatusOK {
		a.t.Fatalf("confirm: %d %s", r.status, r.body)
	}
	return email
}

func TestHTTPFullCycle(t *testing.T) {
	a := newAPI(t)
	email := uniqueEmail()

	r := a.post("/api/auth/register", a.registerBody(email))
	if r.status != http.StatusAccepted {
		t.Fatalf("register: %d %s", r.status, r.body)
	}
	var reg map[string]string
	r.json(t, &reg)
	if reg["email"] != email {
		t.Fatalf("register body = %v", reg)
	}

	// До подтверждения войти нельзя.
	r = a.post("/api/auth/login", map[string]string{"email": email, "password": goodPassword})
	if r.status != http.StatusForbidden || r.apiError(t).Error.Code != CodeEmailNotConfirmed {
		t.Fatalf("login before confirmation: %d %s", r.status, r.body)
	}
	if got := a.get("/api/auth/me"); got.status != 200 || !strings.Contains(string(got.body), `"user":null`) {
		t.Fatalf("anonymous me: %d %s", got.status, got.body)
	}

	tok := tokenIn(t, a.e.lastMailTo(email))
	r = a.post("/api/auth/confirm-email", map[string]string{"token": tok})
	if r.status != http.StatusOK {
		t.Fatalf("confirm: %d %s", r.status, r.body)
	}
	var u userResp
	r.json(t, &u)
	if u.User == nil || u.User.Email != email || !u.User.EmailConfirmed || u.User.Name != "Иван Петров" {
		t.Fatalf("confirm body: %s", r.body)
	}

	// Cookie сессии: недоступна из JavaScript, не уходит на чужие сайты, живёт 30 дней.
	c := a.cookies[CookieName]
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure || c.Value == "" {
		t.Fatalf("session cookie: %+v", c)
	}
	// Срок задаёт сервис по своим (в тесте подставным) часам, а Max-Age считается по настоящим: поэтому проверяем срок
	// относительно подставных часов (Max-Age через месяц после даты теста стал бы отрицательным и ронял бы тест).
	if got := c.Expires.Sub(a.e.clock.Now()); got < 29*24*time.Hour || got > 30*24*time.Hour {
		t.Fatalf("cookie lives %v from the test clock", got)
	}

	r = a.get("/api/auth/me")
	r.json(t, &u)
	if r.status != 200 || u.User == nil || u.User.Email != email {
		t.Fatalf("me: %d %s", r.status, r.body)
	}
	var raw map[string]map[string]any
	r.json(t, &raw)
	for k := range raw["user"] {
		switch k {
		case "id", "email", "name", "email_confirmed", "created_at", "deletion_due_at":
		default:
			t.Fatalf("unexpected field %q in the user object (never leak hashes or internals)", k)
		}
	}

	// Выход.
	if r = a.post("/api/auth/logout", nil); r.status != http.StatusNoContent || a.cookies[CookieName] != nil {
		t.Fatalf("logout: %d, cookie %v", r.status, a.cookies[CookieName])
	}
	if got := a.get("/api/auth/me"); !strings.Contains(string(got.body), `"user":null`) {
		t.Fatalf("me after logout: %s", got.body)
	}

	// Неверный пароль и верный.
	r = a.post("/api/auth/login", map[string]string{"email": email, "password": "wrong password!!"})
	if r.status != http.StatusUnauthorized || r.apiError(t).Error.Code != CodeInvalidCredentials {
		t.Fatalf("wrong password: %d %s", r.status, r.body)
	}
	if a.cookies[CookieName] != nil {
		t.Fatal("a failed login must not set a cookie")
	}
	if r = a.post("/api/auth/login", map[string]string{"email": email, "password": goodPassword}); r.status != 200 || a.cookies[CookieName] == nil {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	if r = a.get("/api/whoami"); string(r.body) != email {
		t.Fatalf("FromContext must expose the signed-in user: %q", r.body)
	}

	// Забыли пароль → письмо → новый пароль.
	a.e.clock.Advance(2 * time.Minute)
	if r = a.post("/api/auth/forgot-password", map[string]string{"email": email}); r.status != http.StatusAccepted {
		t.Fatalf("forgot: %d %s", r.status, r.body)
	}
	resetTok := tokenIn(t, a.e.lastMailTo(email))
	if r = a.post("/api/auth/reset-password", map[string]string{"token": resetTok, "password": "a good new passphrase"}); r.status != http.StatusNoContent {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	if a.cookies[CookieName] != nil {
		t.Fatal("after a reset the cookie must be cleared (all sessions are revoked)")
	}
	if r = a.post("/api/auth/login", map[string]string{"email": email, "password": "a good new passphrase"}); r.status != 200 {
		t.Fatalf("login with the new password: %d %s", r.status, r.body)
	}
}

func TestHTTPCookieIsSecureOverHTTPS(t *testing.T) {
	e := newEnv(t)
	e.svc.cfg.PublicURL = "https://scibox.example"
	a := &api{t: t, e: e, handler: routerFor(NewHandler(e.svc, slog.New(slog.NewTextHandler(e.logs, nil)))), ip: e.meta().IP, origin: "https://scibox.example", cookies: map[string]*http.Cookie{}}
	a.signedIn()
	if c := a.cookies[CookieName]; c == nil || !c.Secure {
		t.Fatalf("cookie must be Secure on https: %+v", c)
	}
}

func TestHTTPFieldValidationErrors(t *testing.T) {
	a := newAPI(t)
	r := a.post("/api/auth/register", map[string]any{"name": "", "email": "nope", "password": "short", "consent": false})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", r.status)
	}
	b := r.apiError(t)
	if b.Error.Code != "validation_failed" {
		t.Fatalf("code = %q", b.Error.Code)
	}
	for _, f := range []string{"name", "email", "password", "consent"} {
		if b.Error.Fields[f] == "" {
			t.Fatalf("no message for %q: %v", f, b.Error.Fields)
		}
	}
}

func TestHTTPErrorCodes(t *testing.T) {
	a := newAPI(t)
	cases := []struct {
		name   string
		call   func() result
		status int
		code   string
	}{
		{"invalid link on confirm", func() result { return a.post("/api/auth/confirm-email", map[string]string{"token": "nope"}) }, 400, CodeInvalidToken},
		{"invalid link on reset", func() result {
			return a.post("/api/auth/reset-password", map[string]string{"token": "nope", "password": "a good new passphrase"})
		}, 400, CodeInvalidToken},
		{"unknown account", func() result {
			return a.post("/api/auth/login", map[string]string{"email": uniqueEmail(), "password": goodPassword})
		}, 401, CodeInvalidCredentials},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.call()
			if r.status != tc.status || r.apiError(t).Error.Code != tc.code {
				t.Fatalf("%d %s", r.status, r.body)
			}
		})
	}
}

func TestHTTPRateLimitedResponse(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	a.post("/api/auth/logout", nil)
	for i := 0; i < a.e.svc.cfg.Limits.LoginEmail.Max; i++ {
		a.post("/api/auth/login", map[string]string{"email": email, "password": "wrong password!!"})
	}
	r := a.post("/api/auth/login", map[string]string{"email": email, "password": goodPassword})
	if r.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d", r.status)
	}
	b := r.apiError(t)
	if b.Error.Code != "rate_limited" || b.Error.RetryAfter != 900 || r.header.Get("Retry-After") != "900" {
		t.Fatalf("body %+v, Retry-After %q", b, r.header.Get("Retry-After"))
	}
}

func TestHTTPEmailOnlyEndpointsAnswerTheSameForKnownAndUnknown(t *testing.T) {
	a := newAPI(t)
	known := a.signedIn()
	a.post("/api/auth/logout", nil)
	for _, path := range []string{"/api/auth/forgot-password", "/api/auth/resend-confirmation"} {
		var bodies []string
		for _, email := range []string{known, uniqueEmail(), "garbage"} {
			r := a.post(path, map[string]string{"email": email})
			if r.status != http.StatusAccepted {
				t.Fatalf("%s %q: %d %s", path, email, r.status, r.body)
			}
			bodies = append(bodies, string(r.body))
		}
		if bodies[0] != bodies[1] || bodies[1] != bodies[2] {
			t.Fatalf("%s: answers differ and reveal which addresses exist: %q", path, bodies)
		}
	}
}

func TestHTTPSameOriginProtection(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	login := map[string]string{"email": email, "password": goodPassword}
	cases := []struct {
		name string
		opts []reqOpt
		want int
	}{
		{"site origin (dev server)", nil, 200},
		{"same host as the request", []reqOpt{header("Origin", "http://example.com")}, 200},
		{"foreign origin", []reqOpt{header("Origin", "https://evil.example")}, 403},
		{"origin null (sandboxed page)", []reqOpt{header("Origin", "null")}, 403},
		{"unparsable origin", []reqOpt{header("Origin", "://bad")}, 403},
		{"look-alike host", []reqOpt{header("Origin", "http://localhost:5173.evil.example")}, 403},
		{"no origin, cross-site fetch", []reqOpt{noHeader("Origin"), header("Sec-Fetch-Site", "cross-site")}, 403},
		{"no origin, same-site fetch", []reqOpt{noHeader("Origin"), header("Sec-Fetch-Site", "same-site")}, 403},
		{"no origin, same-origin fetch", []reqOpt{noHeader("Origin"), header("Sec-Fetch-Site", "same-origin")}, 200},
		{"no origin, address bar", []reqOpt{noHeader("Origin"), header("Sec-Fetch-Site", "none")}, 200},
		{"no origin, not a browser (curl)", []reqOpt{noHeader("Origin")}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := a.post("/api/auth/login", login, tc.opts...)
			if r.status != tc.want {
				t.Fatalf("status = %d, want %d: %s", r.status, tc.want, r.body)
			}
			if tc.want == 403 && r.apiError(t).Error.Code != "forbidden" {
				t.Fatalf("body: %s", r.body)
			}
		})
	}
	// Чтение не меняет данных, поэтому чужой Origin для него не помеха.
	if r := a.get("/api/auth/me", header("Origin", "https://evil.example")); r.status != 200 {
		t.Fatalf("GET with a foreign origin: %d", r.status)
	}
	for _, method := range []string{http.MethodPatch, http.MethodPut, http.MethodDelete} {
		if r := a.do(method, "/api/account", map[string]string{"name": "x"}, header("Origin", "https://evil.example")); r.status != 403 {
			t.Fatalf("%s with a foreign origin: %d", method, r.status)
		}
	}
	if r := a.do(http.MethodOptions, "/api/auth/login", nil, header("Origin", "https://evil.example")); r.status == 403 {
		t.Fatal("OPTIONS is a safe method and must pass the origin check")
	}
}

func TestHTTPRequestBodyRules(t *testing.T) {
	a := newAPI(t)
	cases := []struct {
		name string
		body any
		opts []reqOpt
		want int
		code string
	}{
		{"not json content type", `{"email":"a@b.ru"}`, []reqOpt{header("Content-Type", "text/plain")}, 415, CodeUnsupportedMedia},
		{"form content type", "email=a%40b.ru", []reqOpt{header("Content-Type", "application/x-www-form-urlencoded")}, 415, CodeUnsupportedMedia},
		{"json with charset is fine", `{"email":"a@b.ru","password":"x"}`, []reqOpt{header("Content-Type", "application/json; charset=utf-8")}, 401, CodeInvalidCredentials},
		{"missing content type", `{"email":"a@b.ru"}`, []reqOpt{noHeader("Content-Type")}, 415, CodeUnsupportedMedia},
		{"broken json", `{"email":`, nil, 400, "bad_request"},
		{"empty body", "", nil, 400, "bad_request"},
		{"unknown field", `{"email":"a@b.ru","password":"x","admin":true}`, nil, 400, "bad_request"},
		{"wrong type", `{"email":5}`, nil, 400, "bad_request"},
		{"too large", `{"email":"` + strings.Repeat("a", 20000) + `"}`, nil, 413, "payload_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := a.post("/api/auth/login", tc.body, tc.opts...)
			if r.status != tc.want || r.apiError(t).Error.Code != tc.code {
				t.Fatalf("%d %s", r.status, r.body)
			}
		})
	}
}

func TestHTTPEveryEndpointRejectsAnUnreadableBody(t *testing.T) {
	a := newAPI(t)
	a.signedIn()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/register"},
		{http.MethodPost, "/api/auth/confirm-email"},
		{http.MethodPost, "/api/auth/resend-confirmation"},
		{http.MethodPost, "/api/auth/forgot-password"},
		{http.MethodPost, "/api/auth/reset-password"},
		{http.MethodPost, "/api/auth/login"},
		{http.MethodPatch, "/api/account"},
		{http.MethodPost, "/api/account/password"},
		{http.MethodPost, "/api/account/reauth"},
		{http.MethodPost, "/api/account/email"},
		{http.MethodPost, "/api/auth/confirm-email-change"},
		{http.MethodPost, "/api/auth/revert-email-change"},
	} {
		r := a.do(c.method, c.path, `{"broken":`)
		if r.status != 400 || r.apiError(t).Error.Code != "bad_request" {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.status, r.body)
		}
	}
}

func TestHTTPAccountRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPatch, "/api/account"},
		{http.MethodPost, "/api/account/password"},
		{http.MethodPost, "/api/account/sessions/revoke-others"},
		{http.MethodGet, "/api/account/sessions"},
		{http.MethodDelete, "/api/account/sessions/" + uuid.NewString()},
		{http.MethodPost, "/api/account/reauth"},
		{http.MethodPost, "/api/account/email"},
		{http.MethodGet, "/api/account/deletion"},
		{http.MethodPost, "/api/account/deletion"},
		{http.MethodPost, "/api/account/deletion/cancel"},
	} {
		r := a.do(c.method, c.path, map[string]string{"name": "x"})
		if r.status != http.StatusUnauthorized || r.apiError(t).Error.Code != "unauthorized" {
			t.Fatalf("%s %s: %d %s", c.method, c.path, r.status, r.body)
		}
	}
}

func TestHTTPAccountSettings(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()

	r := a.do(http.MethodPatch, "/api/account", map[string]string{"name": "Анна Сидорова"})
	var u userResp
	r.json(t, &u)
	if r.status != 200 || u.User == nil || u.User.Name != "Анна Сидорова" || u.User.Email != email {
		t.Fatalf("rename: %d %s", r.status, r.body)
	}
	r = a.do(http.MethodPatch, "/api/account", map[string]string{"name": "  "})
	if r.status != 422 || r.apiError(t).Error.Fields["name"] == "" {
		t.Fatalf("empty name: %d %s", r.status, r.body)
	}

	// Вторая сессия «на другом устройстве».
	other := &api{t: t, e: a.e, handler: a.handler, ip: a.e.meta().IP, origin: a.origin, cookies: map[string]*http.Cookie{}}
	if r := other.post("/api/auth/login", map[string]string{"email": email, "password": goodPassword}); r.status != 200 {
		t.Fatalf("second login: %d %s", r.status, r.body)
	}
	if r := a.post("/api/account/sessions/revoke-others", nil); r.status != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", r.status, r.body)
	}
	if r := other.get("/api/auth/me"); !strings.Contains(string(r.body), `"user":null`) {
		t.Fatalf("the other device must be signed out: %s", r.body)
	}
	if r := a.get("/api/auth/me"); strings.Contains(string(r.body), `"user":null`) {
		t.Fatal("this device must stay signed in")
	}

	r = a.post("/api/account/password", map[string]string{"current_password": "wrong password!!", "new_password": "a good new passphrase"})
	if r.status != 422 || r.apiError(t).Error.Fields["current_password"] == "" {
		t.Fatalf("wrong current: %d %s", r.status, r.body)
	}
	r = a.post("/api/account/password", map[string]string{"current_password": goodPassword, "new_password": "a good new passphrase"})
	if r.status != http.StatusNoContent {
		t.Fatalf("change password: %d %s", r.status, r.body)
	}
}

func TestHTTPStaleCookieIsClearedAndRequestContinuesAnonymously(t *testing.T) {
	a := newAPI(t)
	a.cookies[CookieName] = &http.Cookie{Name: CookieName, Value: "stale-token"}
	r := a.get("/api/auth/me")
	if r.status != 200 || !strings.Contains(string(r.body), `"user":null`) {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if a.cookies[CookieName] != nil {
		t.Fatal("a stale cookie must be cleared")
	}
	if got := r.header.Values("Set-Cookie"); len(got) != 1 || !strings.Contains(got[0], "Max-Age=0") {
		t.Fatalf("Set-Cookie = %v", got)
	}
}

func TestHTTPCookieIsRefreshedWithTheSession(t *testing.T) {
	a := newAPI(t)
	a.signedIn()
	before := a.cookies[CookieName].Expires
	a.e.clock.Advance(2 * time.Hour)
	r := a.get("/api/auth/me")
	if r.status != 200 {
		t.Fatalf("%d", r.status)
	}
	if got := r.header.Values("Set-Cookie"); len(got) != 1 {
		t.Fatalf("an extended session must renew the cookie, Set-Cookie = %v", got)
	}
	if !a.cookies[CookieName].Expires.After(before) {
		t.Fatal("the cookie expiry must move forward")
	}
	// Сразу следующий запрос cookie не трогает.
	if r := a.get("/api/auth/me"); len(r.header.Values("Set-Cookie")) != 0 {
		t.Fatalf("no refresh expected: %v", r.header.Values("Set-Cookie"))
	}
}

func TestHTTPLogoutWithoutCookieIsHarmless(t *testing.T) {
	a := newAPI(t)
	if r := a.post("/api/auth/logout", nil); r.status != http.StatusNoContent {
		t.Fatalf("%d", r.status)
	}
}

func TestHTTPServerFailuresAreHiddenFromThePerson(t *testing.T) {
	broken, err := pgxpool.New(context.Background(), sharedPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	e := newEnv(t)
	e.svc = newService(broken, e.mailer, e.svc.cfg, slog.New(slog.NewTextHandler(e.logs, nil)))
	a := &api{t: t, e: e, handler: routerFor(NewHandler(e.svc, slog.New(slog.NewTextHandler(e.logs, nil)))), ip: e.meta().IP, origin: "http://localhost:5173", cookies: map[string]*http.Cookie{}}

	r := a.post("/api/auth/login", map[string]string{"email": "a@b.ru", "password": goodPassword})
	b := r.apiError(t)
	if r.status != 500 || b.Error.Code != "internal" || strings.Contains(b.Error.Message, "pool") || strings.Contains(b.Error.Message, "closed") {
		t.Fatalf("%d %+v: the person must not see internal details", r.status, b)
	}
	if !strings.Contains(e.logs.String(), "auth request failed") {
		t.Fatal("the failure must be logged")
	}
	// Та же поломка в проверке сессии.
	a.cookies[CookieName] = &http.Cookie{Name: CookieName, Value: "whatever"}
	if r := a.get("/api/auth/me"); r.status != 500 {
		t.Fatalf("session lookup failure: %d", r.status)
	}
	// И во всех остальных адресах: наружу всегда один и тот же безопасный ответ.
	delete(a.cookies, CookieName)
	for _, c := range []struct {
		path string
		body any
	}{
		{"/api/auth/register", a.registerBody(uniqueEmail())},
		{"/api/auth/confirm-email", map[string]string{"token": "x"}},
		{"/api/auth/resend-confirmation", map[string]string{"email": "a@b.ru"}},
		{"/api/auth/forgot-password", map[string]string{"email": "a@b.ru"}},
		{"/api/auth/reset-password", map[string]string{"token": "x", "password": "a good new passphrase"}},
		{"/api/auth/confirm-email-change", map[string]string{"token": "x"}},
		{"/api/auth/revert-email-change", map[string]string{"token": "x"}},
	} {
		if r := a.post(c.path, c.body); r.status != 500 || r.apiError(t).Error.Code != "internal" {
			t.Errorf("%s: %d %s", c.path, r.status, r.body)
		}
	}
	h := NewHandler(e.svc, slog.New(slog.DiscardHandler))
	logoutReq := httptest.NewRequest(http.MethodPost, "/", nil)
	logoutReq.AddCookie(&http.Cookie{Name: CookieName, Value: "x"})
	logoutRec := httptest.NewRecorder()
	h.logout(logoutRec, logoutReq)
	if logoutRec.Code != 500 {
		t.Errorf("logout with a broken database: %d", logoutRec.Code)
	}
	// Адреса настроек, когда человек уже распознан, а база отвалилась.
	signedIn := func(req *http.Request) *http.Request {
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", uuid.NewString())
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rc)
		return req.WithContext(context.WithValue(ctx, ctxKey{}, Principal{User: User{Name: "x"}, Fresh: true}))
	}
	for name, c := range map[string]struct {
		call func(http.ResponseWriter, *http.Request)
		body string
	}{
		"rename":   {h.updateAccount, `{"name":"Анна"}`},
		"password": {h.changePassword, `{"current_password":"x","new_password":"a good new passphrase"}`},
		"revoke":   {h.revokeOthers, `{}`},
		"reauth":   {h.reauth, `{"password":"x"}`},
		"sessions": {h.listSessions, ``},
		"end one":  {h.revokeSession, ``},
		"email":    {h.requestEmailChange, `{"email":"new@example.ru"}`},
		"plan":     {h.deletionPlan, ``},
		"delete":   {h.requestDeletion, ``},
		"undelete": {h.cancelDeletion, ``},
	} {
		call := c.call
		req := signedIn(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(c.body)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.1:1"
		rec := httptest.NewRecorder()
		call(rec, req)
		if rec.Code != 500 {
			t.Errorf("%s: status %d, body %s", name, rec.Code, rec.Body)
		}
	}
}

func TestHTTPFailMapsUnauthenticatedError(t *testing.T) {
	a := newAPI(t)
	h := NewHandler(a.e.svc, slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	h.fail(rec, httptest.NewRequest(http.MethodGet, "/", nil), ErrUnauthenticated)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestNewHandlerSurvivesBadPublicURL(t *testing.T) {
	e := newEnv(t)
	e.svc.cfg.PublicURL = "://not a url"
	h := NewHandler(e.svc, slog.New(slog.DiscardHandler))
	if h.secure || h.publicHost != "" {
		t.Fatalf("handler = %+v", h)
	}
}

func TestMetaTakesTheIPFromTheConnectionNotFromHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("User-Agent", "agent/1.0")
	if m := meta(req); m.IP != "203.0.113.9" || m.UserAgent != "agent/1.0" {
		t.Fatalf("meta = %+v: client-supplied headers must not choose the IP used for rate limits", m)
	}
	req.RemoteAddr = "no-port"
	if m := meta(req); m.IP != "no-port" {
		t.Fatalf("meta = %+v", m)
	}
}

func TestFromContextWithoutAuthentication(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context must have no user")
	}
}

func TestServiceConfigAccessor(t *testing.T) {
	e := newEnv(t)
	if e.svc.Config().ProductName != "SciBox" || e.svc.Config().SessionTTL != 30*24*time.Hour {
		t.Fatalf("config = %+v", e.svc.Config())
	}
	if (&RateLimitedError{}).Error() == "" {
		t.Fatal("RateLimitedError must have a message")
	}
}

// browser — второй «браузер» того же теста: своя cookie, та же база и почта.
func (a *api) browser() *api {
	return &api{t: a.t, e: a.e, handler: a.handler, ip: a.e.meta().IP, origin: a.origin, cookies: map[string]*http.Cookie{}}
}

func (a *api) login(email string) {
	a.t.Helper()
	if r := a.post("/api/auth/login", map[string]string{"email": email, "password": goodPassword}); r.status != http.StatusOK {
		a.t.Fatalf("login: %d %s", r.status, r.body)
	}
}

func (a *api) signedInAs() string {
	a.t.Helper()
	r := a.get("/api/whoami")
	if r.status != http.StatusOK {
		return ""
	}
	return string(r.body)
}

// Регистрация отвечает одинаково на свободную и на занятую почту: по ответу не узнать, есть ли аккаунт.
func TestHTTPRegisterAnswersTheSameForTakenAndFreeAddress(t *testing.T) {
	a := newAPI(t)
	taken := a.signedIn()
	a.post("/api/auth/logout", nil)
	free := uniqueEmail()
	var answers []string
	for i, email := range []string{taken, free} {
		// Каждый раз «новый» браузер без cookie: ответ зависит от браузера, но не от того, занята ли почта.
		b := a
		if i == 1 {
			b = newAPI(t)
		}
		r := b.post("/api/auth/register", b.registerBody(strings.ToUpper(email)))
		// Cookie «браузер регистрации» ставится всегда, и для занятой почты тоже: по заголовкам не узнать, есть ли аккаунт.
		if cookies := r.header.Values("Set-Cookie"); r.status != http.StatusAccepted || len(cookies) != 1 || !strings.HasPrefix(cookies[0], BindingCookieName+"=") {
			t.Fatalf("register %s: %d %s %v", email, r.status, r.body, r.header.Values("Set-Cookie"))
		}
		answers = append(answers, strings.Replace(string(r.body), email, "EMAIL", 1))
	}
	if answers[0] != answers[1] {
		t.Fatalf("answers differ: %q", answers)
	}
}

// Свежий пароль, смена почты и откат через HTTP (T-11, T-12).
func TestHTTPEmailChangeNeedsFreshPasswordAndCanBeReverted(t *testing.T) {
	a := newAPI(t)
	oldEmail := a.signedIn() // вход по ссылке из письма: пароль не вводили
	newEmail := uniqueEmail()

	r := a.post("/api/account/email", map[string]string{"email": newEmail})
	if r.status != http.StatusForbidden || r.apiError(t).Error.Code != "reauth_required" {
		t.Fatalf("change without a fresh password: %d %s", r.status, r.body)
	}
	if r = a.post("/api/account/reauth", map[string]string{"password": "wrong password here"}); r.status != 422 || r.apiError(t).Error.Fields["password"] == "" {
		t.Fatalf("wrong password: %d %s", r.status, r.body)
	}
	if r = a.post("/api/account/reauth", map[string]string{"password": goodPassword}); r.status != http.StatusNoContent {
		t.Fatalf("reauth: %d %s", r.status, r.body)
	}
	if r = a.post("/api/account/email", map[string]string{"email": "broken"}); r.status != 422 || r.apiError(t).Error.Fields["email"] == "" {
		t.Fatalf("broken email: %d %s", r.status, r.body)
	}
	if r = a.post("/api/account/email", map[string]string{"email": newEmail}); r.status != http.StatusAccepted {
		t.Fatalf("change: %d %s", r.status, r.body)
	}

	// Ссылку открывают в другом браузере, где никто не вошёл: почта меняется, входа нет.
	other := a.browser()
	tok := tokenIn(t, a.e.lastMailTo(newEmail))
	r = other.post("/api/auth/confirm-email-change", map[string]string{"token": tok})
	var u userResp
	r.json(t, &u)
	if r.status != http.StatusOK || u.User == nil || u.User.Email != newEmail || other.cookies[CookieName] != nil {
		t.Fatalf("confirm change: %d %s", r.status, r.body)
	}
	if r = other.post("/api/auth/confirm-email-change", map[string]string{"token": tok}); r.status != 400 || r.apiError(t).Error.Code != CodeInvalidToken {
		t.Fatalf("second use: %d %s", r.status, r.body)
	}
	if got := a.signedInAs(); got != newEmail {
		t.Fatalf("this browser must see the new email, got %q", got)
	}

	// «Это не я» со старого адреса: все сессии завершены.
	revert := tokenIn(t, a.e.lastMailTo(oldEmail))
	if r = other.post("/api/auth/revert-email-change", map[string]string{"token": revert}); r.status != http.StatusNoContent {
		t.Fatalf("revert: %d %s", r.status, r.body)
	}
	if got := a.signedInAs(); got != "" {
		t.Fatalf("every session must end after a revert, still %q", got)
	}
	a.login(oldEmail)
}

func TestHTTPEmailChangeToTakenAddressAfterLinkSent(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	a.post("/api/auth/logout", nil)
	a.login(email)
	newEmail := uniqueEmail()
	if r := a.post("/api/account/email", map[string]string{"email": newEmail}); r.status != http.StatusAccepted {
		t.Fatalf("change: %d %s", r.status, r.body)
	}
	tok := tokenIn(t, a.e.lastMailTo(newEmail))
	a.browser().signedInWith(newEmail)
	r := a.browser().post("/api/auth/confirm-email-change", map[string]string{"token": tok})
	if r.status != http.StatusConflict || r.apiError(t).Error.Code != CodeEmailTaken {
		t.Fatalf("taken address: %d %s", r.status, r.body)
	}
}

// signedInWith регистрирует и подтверждает человека с этой почтой.
func (a *api) signedInWith(email string) {
	a.t.Helper()
	if r := a.post("/api/auth/register", a.registerBody(email)); r.status != http.StatusAccepted {
		a.t.Fatalf("register: %d %s", r.status, r.body)
	}
	if r := a.post("/api/auth/confirm-email", map[string]string{"token": tokenIn(a.t, a.e.lastMailTo(email))}); r.status != http.StatusOK {
		a.t.Fatalf("confirm: %d %s", r.status, r.body)
	}
}

// Сессии: список, завершение чужой (своей другой) и текущей; cookie устройства (T-10, T-13).
func TestHTTPSessions(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	dev := a.cookies[DeviceCookieName]
	if dev == nil || !dev.HttpOnly || dev.SameSite != http.SameSiteLaxMode || dev.MaxAge < 399*24*3600 || dev.Value == "" {
		t.Fatalf("device cookie after confirmation: %+v", dev)
	}
	phone := a.browser()
	phone.login(email)

	r := a.get("/api/account/sessions")
	var list struct {
		Sessions []map[string]any `json:"sessions"`
	}
	r.json(t, &list)
	if r.status != http.StatusOK || len(list.Sessions) != 2 {
		t.Fatalf("sessions: %d %s", r.status, r.body)
	}
	var otherID string
	for _, s := range list.Sessions {
		for _, k := range []string{"id", "browser", "os", "mobile", "created_at", "last_seen_at", "current"} {
			if _, ok := s[k]; !ok {
				t.Fatalf("session row lacks %q: %v", k, s)
			}
		}
		if len(s) != 7 {
			t.Fatalf("session row must not carry extra fields (IP, token): %v", s)
		}
		if s["current"] == false {
			otherID = s["id"].(string)
		}
	}
	if otherID == "" {
		t.Fatal("no other session in the list")
	}
	if r = a.do(http.MethodDelete, "/api/account/sessions/not-a-number", nil); r.status != 404 {
		t.Fatalf("bad id: %d %s", r.status, r.body)
	}
	if r = a.do(http.MethodDelete, "/api/account/sessions/"+otherID, nil); r.status != http.StatusNoContent || a.cookies[CookieName] == nil {
		t.Fatalf("end the phone session: %d %s", r.status, r.body)
	}
	if phone.signedInAs() != "" || a.signedInAs() != email {
		t.Fatal("only the phone session must end")
	}
	if r = a.do(http.MethodDelete, "/api/account/sessions/"+otherID, nil); r.status != 404 {
		t.Fatalf("ending twice: %d %s", r.status, r.body)
	}
	// Завершить текущую — то же, что выйти.
	a.get("/api/account/sessions").json(t, &list)
	if r = a.do(http.MethodDelete, "/api/account/sessions/"+list.Sessions[0]["id"].(string), nil); r.status != http.StatusNoContent || a.cookies[CookieName] != nil {
		t.Fatalf("end the current session: %d %s", r.status, r.body)
	}
}

// Вход завершает прежнюю сессию этого браузера, даже если она чужая (T-10).
func TestHTTPLoginReplacesTheBrowserSession(t *testing.T) {
	a := newAPI(t)
	first := a.signedIn()
	oldCookie := a.cookies[CookieName].Value
	second := uniqueEmail()
	a.browser().signedInWith(second)
	a.login(second)
	if a.cookies[CookieName].Value == oldCookie {
		t.Fatal("login must give a new session")
	}
	if _, err := a.e.svc.Authenticate(bg, oldCookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("the previous session of this browser (%s) must end", first)
	}
}

// Удаление аккаунта через HTTP: план, просьба (выход), отмена после входа.
func TestHTTPDeletion(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	r := a.post("/api/account/deletion", nil)
	if r.status != http.StatusForbidden || r.apiError(t).Error.Code != "reauth_required" {
		t.Fatalf("deletion without a fresh password: %d %s", r.status, r.body)
	}
	a.login(email)
	var plan map[string]any
	r = a.get("/api/account/deletion")
	r.json(t, &plan)
	if r.status != http.StatusOK || plan["due_at"] != nil || plan["grace_days"] != float64(14) ||
		len(plan["blocking"].([]any)) != 0 || len(plan["orphaned"].([]any)) != 0 {
		t.Fatalf("plan: %d %s", r.status, r.body)
	}
	r = a.post("/api/account/deletion", nil)
	var due struct {
		DueAt time.Time `json:"due_at"`
	}
	r.json(t, &due)
	if r.status != http.StatusOK || due.DueAt.IsZero() || a.cookies[CookieName] != nil {
		t.Fatalf("request deletion: %d %s", r.status, r.body)
	}
	a.login(email)
	var me struct {
		User struct {
			DeletionDueAt *time.Time `json:"deletion_due_at"`
		} `json:"user"`
	}
	a.get("/api/auth/me").json(t, &me)
	if me.User.DeletionDueAt == nil || !me.User.DeletionDueAt.Equal(due.DueAt) {
		t.Fatalf("me during the grace period: %+v", me)
	}
	if r = a.post("/api/account/deletion/cancel", nil); r.status != http.StatusNoContent {
		t.Fatalf("cancel: %d %s", r.status, r.body)
	}
	a.get("/api/auth/me").json(t, &me)
	if me.User.DeletionDueAt != nil {
		t.Fatal("deletion must be cancelled")
	}
}

func TestWriteReauthRequired(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteReauthRequired(rec)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"reauth_required"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// Единственный владелец организации с сотрудниками: план называет организацию, просьба — 409 deletion_blocked.
func TestHTTPDeletionBlockedForSoleOwner(t *testing.T) {
	a := newAPI(t)
	email := a.signedIn()
	a.login(email)
	staff := a.browser()
	staffEmail := uniqueEmail()
	staff.signedInWith(staffEmail)
	var ownerID, staffID, orgID string
	if err := sharedPool.QueryRow(bg, `SELECT id FROM users WHERE email = $1`, email).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := sharedPool.QueryRow(bg, `SELECT id FROM users WHERE email = $1`, staffEmail).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	slug := "sole-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	if err := sharedPool.QueryRow(bg, `INSERT INTO organizations (slug, name, kind, city, created_by, created_at, updated_at)
		VALUES ($1, 'Институт единственного владельца', 'institute', 'Пермь', $2, now(), now()) RETURNING id`, slug, ownerID).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedPool.Exec(bg, `INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'owner', now()), ($1, $3, 'hr', now())`, orgID, ownerID, staffID); err != nil {
		t.Fatal(err)
	}
	var plan struct {
		Blocking []map[string]string `json:"blocking"`
	}
	a.get("/api/account/deletion").json(t, &plan)
	if len(plan.Blocking) != 1 || plan.Blocking[0]["slug"] != slug || plan.Blocking[0]["name"] != "Институт единственного владельца" {
		t.Fatalf("plan: %+v", plan)
	}
	r := a.post("/api/account/deletion", nil)
	if r.status != http.StatusConflict || r.apiError(t).Error.Code != CodeDeletionBlocked || a.cookies[CookieName] == nil {
		t.Fatalf("blocked deletion: %d %s", r.status, r.body)
	}
}

// Ссылка из письма, открытая в другом браузере (без cookie регистрации): почта подтверждена, сеанс не выдан, в ответе
// ссылка на выбор нового пароля (срез 29, D-190).
func TestHTTPConfirmFromAnotherBrowserGivesNoSession(t *testing.T) {
	a := newAPI(t)
	email := uniqueEmail()
	r := a.post("/api/auth/register", a.registerBody(email))
	if r.status != http.StatusAccepted {
		t.Fatalf("register: %d %s", r.status, r.body)
	}
	if c := a.cookies[BindingCookieName]; c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Value == "" {
		t.Fatalf("registration cookie: %+v", c)
	}
	token := tokenIn(t, a.e.lastMailTo(email))

	phone := newAPI(t) // другой браузер: нет cookie регистрации
	r = phone.post("/api/auth/confirm-email", map[string]string{"token": token})
	if r.status != http.StatusOK {
		t.Fatalf("confirm: %d %s", r.status, r.body)
	}
	var out map[string]string
	r.json(t, &out)
	if out["reset_token"] == "" || out["user"] != "" {
		t.Fatalf("answer: %s", r.body)
	}
	if phone.cookies[CookieName] != nil && phone.cookies[CookieName].Value != "" {
		t.Fatal("a session cookie was set for the other browser")
	}
	if phone.signedInAs() != "" {
		t.Fatal("the other browser is signed in")
	}
	// По ссылке из ответа человек задаёт пароль и входит.
	if r = phone.post("/api/auth/reset-password", map[string]string{"token": out["reset_token"], "password": "brand new password 55"}); r.status != http.StatusNoContent && r.status != http.StatusOK {
		t.Fatalf("reset: %d %s", r.status, r.body)
	}
	if r = phone.post("/api/auth/login", map[string]string{"email": email, "password": "brand new password 55"}); r.status != http.StatusOK {
		t.Fatalf("login with the new password: %d %s", r.status, r.body)
	}
}

// Чтение «с другого сайта» (переход по чужой ссылке, куки SameSite=Lax уходят) отвергается: у выгрузки данных и выдачи
// файла есть побочные действия (срез 29, WEB-5).
func TestHTTPCrossSiteReadsAreRefused(t *testing.T) {
	a := newAPI(t)
	a.signedIn()
	for name, c := range map[string]struct {
		opts []reqOpt
		want int
	}{
		"same origin":   {[]reqOpt{header("Sec-Fetch-Site", "same-origin")}, 200},
		"address bar":   {[]reqOpt{header("Sec-Fetch-Site", "none")}, 200},
		"not a browser": {nil, 200},
		"cross site":    {[]reqOpt{header("Sec-Fetch-Site", "cross-site")}, 403},
	} {
		t.Run(name, func(t *testing.T) {
			if r := a.get("/api/account/sessions", c.opts...); r.status != c.want {
				t.Fatalf("status = %d, want %d: %s", r.status, c.want, r.body)
			}
		})
	}
}

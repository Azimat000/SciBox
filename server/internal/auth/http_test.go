package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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
		case "id", "email", "name", "email_confirmed", "created_at":
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
		{"too large", `{"email":"` + strings.Repeat("a", 20000) + `"}`, nil, 400, "bad_request"},
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
		return req.WithContext(context.WithValue(req.Context(), ctxKey{}, Principal{User: User{Name: "x"}}))
	}
	for name, c := range map[string]struct {
		call func(http.ResponseWriter, *http.Request)
		body string
	}{
		"rename":   {h.updateAccount, `{"name":"Анна"}`},
		"password": {h.changePassword, `{"current_password":"x","new_password":"a good new passphrase"}`},
		"revoke":   {h.revokeOthers, `{}`},
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

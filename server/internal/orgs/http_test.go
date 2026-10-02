package orgs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/auth"
)

// api — настоящие обработчики аккаунтов и организаций за настоящим роутером.
type api struct {
	*world
	h http.Handler
}

func newAPIWith(w *world, svc *Service) *api {
	logger := slog.New(slog.NewTextHandler(w.logs, nil))
	authH := auth.NewHandler(w.auth, logger)
	orgsH := NewHandler(svc, logger, authH.RequireUser)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(authH.SameOrigin, authH.Authenticate)
		orgsH.Mount(r)
	})
	return &api{world: w, h: r}
}

func newAPI(t *testing.T) *api {
	w := newWorld(t)
	return newAPIWith(w, w.svc)
}

type reply struct {
	Code   int
	Header http.Header
	Raw    string
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Raw), &m); err != nil {
		t.Fatalf("not JSON (%d): %s", r.Code, r.Raw)
	}
	return m
}

func (r reply) errCode(t *testing.T) string {
	t.Helper()
	e, _ := r.json(t)["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (r reply) fields(t *testing.T) map[string]any {
	t.Helper()
	e, _ := r.json(t)["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	return f
}

func (a *api) do(who *person, method, path string, body any) reply {
	a.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != nil {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.Token})
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return reply{Code: rec.Code, Header: rec.Header(), Raw: rec.Body.String()}
}

func orgBodyJSON() map[string]any {
	return map[string]any{"name": "Институт тестовых наук", "kind": "institute", "city": "Новосибирск", "website": "example.ru", "description": "Описание"}
}

func unitBodyJSON() map[string]any {
	return map[string]any{"name": "Лаборатория тестов", "kind": "laboratory", "description": "О нас", "topics": []string{"Тема"}}
}

func TestHTTPFullFlow(t *testing.T) {
	a := newAPI(t)
	owner := a.user("Иван Петров")
	guest := a.user("Анна Смирнова")

	// Создание организации.
	r := a.do(&owner, "POST", "/api/organizations", orgBodyJSON())
	if r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Raw)
	}
	org := r.json(t)["organization"].(map[string]any)
	slug := org["slug"].(string)
	if org["website"] != "https://example.ru" || slug == "" {
		t.Fatalf("organization: %v", org)
	}
	base := "/api/organizations/" + slug

	// Изменение.
	body := orgBodyJSON()
	body["name"] = "Институт новых тестов"
	if r = a.do(&owner, "PATCH", base, body); r.Code != 200 || r.json(t)["organization"].(map[string]any)["name"] != "Институт новых тестов" {
		t.Fatalf("update: %d %s", r.Code, r.Raw)
	}

	// Подразделение.
	if r = a.do(&owner, "POST", base+"/units", unitBodyJSON()); r.Code != 201 {
		t.Fatalf("create unit: %d %s", r.Code, r.Raw)
	}
	unitID := r.json(t)["unit"].(map[string]any)["id"].(string)
	ub := unitBodyJSON()
	ub["name"] = "Лаборатория новых тестов"
	if r = a.do(&owner, "PATCH", base+"/units/"+unitID, ub); r.Code != 200 || r.json(t)["unit"].(map[string]any)["name"] != "Лаборатория новых тестов" {
		t.Fatalf("update unit: %d %s", r.Code, r.Raw)
	}

	// Приглашение: руководителем этого подразделения.
	r = a.do(&owner, "POST", base+"/invitations", map[string]any{"email": guest.Email, "role": "unit_head", "unit_id": unitID})
	if r.Code != 201 {
		t.Fatalf("invite: %d %s", r.Code, r.Raw)
	}
	invID := r.json(t)["invitation"].(map[string]any)["id"].(string)
	token := tokenIn(t, a.lastMailTo(guest.Email).Body)

	// Страница приглашения открывается без входа, чужой и своей почтой.
	r = a.do(nil, "POST", "/api/invitations/lookup", map[string]string{"token": token})
	if r.Code != 200 || r.json(t)["email_matches"] != nil || r.json(t)["email"] != guest.Email {
		t.Fatalf("lookup: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&guest, "POST", "/api/invitations/lookup", map[string]string{"token": token}); r.json(t)["email_matches"] != true {
		t.Fatalf("lookup as the guest: %s", r.Raw)
	}
	// В списке «Мои организации» приглашение видно по почте.
	r = a.do(&guest, "GET", "/api/my/organizations", nil)
	if r.Code != 200 || len(r.json(t)["invitations"].([]any)) != 1 || len(r.json(t)["organizations"].([]any)) != 0 {
		t.Fatalf("my organizations before accepting: %s", r.Raw)
	}
	// Принять по ссылке.
	r = a.do(&guest, "POST", "/api/invitations/accept", map[string]string{"token": token})
	if r.Code != 200 || r.json(t)["slug"] != slug || r.json(t)["role"] != "unit_head" {
		t.Fatalf("accept: %d %s", r.Code, r.Raw)
	}
	r = a.do(&guest, "GET", "/api/my/organizations", nil)
	if len(r.json(t)["organizations"].([]any)) != 1 || len(r.json(t)["invitations"].([]any)) != 0 {
		t.Fatalf("my organizations after accepting: %s", r.Raw)
	}

	// Руководитель правит своё подразделение, но не организацию.
	if r = a.do(&guest, "PATCH", base+"/units/"+unitID, unitBodyJSON()); r.Code != 200 {
		t.Errorf("head edits own unit: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&guest, "PATCH", base, orgBodyJSON()); r.Code != 403 || r.errCode(t) != "forbidden" {
		t.Errorf("head edits organization: %d %s", r.Code, r.Raw)
	}

	// Сотрудники.
	r = a.do(&owner, "GET", base+"/members", nil)
	if r.Code != 200 || len(r.json(t)["members"].([]any)) != 2 {
		t.Fatalf("members: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&owner, "PATCH", base+"/members/"+guest.ID.String(), map[string]string{"role": "hr"}); r.Code != 204 {
		t.Fatalf("change role: %d %s", r.Code, r.Raw)
	}
	// Руководителя назначает и снимает владелец.
	if r = a.do(&owner, "PUT", base+"/units/"+unitID+"/head", map[string]any{"user_id": guest.ID.String()}); r.Code != 200 || r.json(t)["unit"].(map[string]any)["head_name"] != "Анна Смирнова" {
		t.Fatalf("set head: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&owner, "PUT", base+"/units/"+unitID+"/head", map[string]any{"user_id": nil}); r.Code != 200 || r.json(t)["unit"].(map[string]any)["head_name"] != nil {
		t.Fatalf("clear head: %d %s", r.Code, r.Raw)
	}

	// Приглашение из списка принимается по номеру.
	third := a.user("Борис Третий")
	r = a.do(&owner, "POST", base+"/invitations", map[string]any{"email": third.Email, "role": "hr"})
	id2 := r.json(t)["invitation"].(map[string]any)["id"].(string)
	if r = a.do(&third, "POST", "/api/invitations/"+id2+"/accept", map[string]any{}); r.Code != 200 || r.json(t)["role"] != "hr" {
		t.Fatalf("accept by id: %d %s", r.Code, r.Raw)
	}

	// Отзыв приглашения.
	r = a.do(&owner, "POST", base+"/invitations", map[string]any{"email": "later@example.ru", "role": "hr"})
	id3 := r.json(t)["invitation"].(map[string]any)["id"].(string)
	if r = a.do(&owner, "DELETE", base+"/invitations/"+id3, nil); r.Code != 204 {
		t.Fatalf("revoke: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&owner, "DELETE", base+"/invitations/"+invID, nil); r.Code != 404 {
		t.Errorf("revoke an accepted invitation: %d %s", r.Code, r.Raw)
	}

	// Сотрудник уходит сам; владельца убрать нельзя, он последний.
	if r = a.do(&third, "DELETE", base+"/members/"+third.ID.String(), nil); r.Code != 204 {
		t.Fatalf("leave: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&owner, "DELETE", base+"/members/"+owner.ID.String(), nil); r.Code != 409 || r.errCode(t) != "last_owner" {
		t.Fatalf("the last owner: %d %s", r.Code, r.Raw)
	}
	if r = a.do(&owner, "DELETE", base+"/members/"+guest.ID.String(), nil); r.Code != 204 {
		t.Fatalf("remove: %d %s", r.Code, r.Raw)
	}

	// Удаление подразделения.
	if r = a.do(&owner, "DELETE", base+"/units/"+unitID, nil); r.Code != 204 {
		t.Fatalf("delete unit: %d %s", r.Code, r.Raw)
	}
	if r = a.do(nil, "GET", base+"/units/"+unitID, nil); r.Code != 404 {
		t.Errorf("deleted unit: %d", r.Code)
	}
}

func TestHTTPPublicPages(t *testing.T) {
	a := newAPI(t)
	owner := a.user("Иван Петров")
	org := a.org(owner)
	unit := a.unit(owner, org.Slug)
	head := a.user("Анна Смирнова")
	a.member(owner, org.Slug, head, "unit_head", &unit.ID)

	r := a.do(nil, "GET", "/api/organizations/"+org.Slug, nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	// Публичная страница не выдаёт ни почт, ни номеров аккаунтов, ни прав.
	for _, secret := range []string{owner.Email, head.Email, head.ID.String(), owner.ID.String(), "head_user_id"} {
		if strings.Contains(r.Raw, secret) {
			t.Errorf("the public organization page leaks %q:\n%s", secret, r.Raw)
		}
	}
	if r.json(t)["viewer"] != nil {
		t.Error("anonymous visitors have no viewer")
	}
	r = a.do(nil, "GET", "/api/organizations/"+org.Slug+"/units/"+unit.ID.String(), nil)
	if r.Code != 200 || strings.Contains(r.Raw, head.Email) || strings.Contains(r.Raw, head.ID.String()) || !strings.Contains(r.Raw, "Анна Смирнова") {
		t.Errorf("public unit page: %d %s", r.Code, r.Raw)
	}
	// Каталог.
	r = a.do(nil, "GET", "/api/organizations?q="+"институт&kind=institute&limit=abc&offset=-1", nil)
	if r.Code != 200 || r.json(t)["total"] == nil {
		t.Errorf("list: %d %s", r.Code, r.Raw)
	}
	// Неизвестное.
	for _, path := range []string{"/api/organizations/no-such-org", "/api/organizations/" + org.Slug + "/units/not-a-uuid", "/api/organizations/" + org.Slug + "/units/" + uuid.NewString()} {
		if r = a.do(nil, "GET", path, nil); r.Code != 404 || r.errCode(t) != "not_found" {
			t.Errorf("%s: %d %s", path, r.Code, r.Raw)
		}
	}
	// Вошедший сотрудник видит свои права.
	r = a.do(&owner, "GET", "/api/organizations/"+org.Slug, nil)
	if v := r.json(t)["viewer"].(map[string]any); v["role"] != "owner" || v["can_manage_members"] != true {
		t.Errorf("owner viewer: %v", v)
	}
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/organizations"},
		{"PATCH", "/api/organizations/x"},
		{"POST", "/api/organizations/x/units"},
		{"PATCH", "/api/organizations/x/units/" + id},
		{"DELETE", "/api/organizations/x/units/" + id},
		{"PUT", "/api/organizations/x/units/" + id + "/head"},
		{"GET", "/api/organizations/x/members"},
		{"PATCH", "/api/organizations/x/members/" + id},
		{"DELETE", "/api/organizations/x/members/" + id},
		{"POST", "/api/organizations/x/invitations"},
		{"DELETE", "/api/organizations/x/invitations/" + id},
		{"GET", "/api/my/organizations"},
		{"POST", "/api/invitations/accept"},
		{"POST", "/api/invitations/" + id + "/accept"},
	} {
		var body any
		if c.method != "GET" && c.method != "DELETE" {
			body = map[string]any{}
		}
		if r := a.do(nil, c.method, c.path, body); r.Code != 401 || r.errCode(t) != "unauthorized" {
			t.Errorf("%s %s without signing in: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
}

func TestHTTPRejectsBadRequests(t *testing.T) {
	a := newAPI(t)
	owner := a.user("Иван")
	org := a.org(owner)
	unit := a.unit(owner, org.Slug)
	base := "/api/organizations/" + org.Slug
	id := unit.ID.String()

	// Нужен JSON; сломанный и лишние поля отклоняются.
	req := httptest.NewRequest("POST", "/api/organizations", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: owner.Token})
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Errorf("form body: %d", rec.Code)
	}
	if r := a.do(&owner, "POST", "/api/organizations", "{broken"); r.Code != 400 || r.errCode(t) != "bad_request" {
		t.Errorf("broken JSON: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "POST", "/api/organizations", map[string]any{"name": "Х", "bogus": 1}); r.Code != 400 {
		t.Errorf("unknown field: %d", r.Code)
	}
	// Поля.
	if r := a.do(&owner, "POST", "/api/organizations", map[string]any{"name": "", "kind": "x", "city": ""}); r.Code != 422 || r.fields(t)["name"] == nil || r.fields(t)["kind"] == nil || r.fields(t)["city"] == nil {
		t.Errorf("fields: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "POST", base+"/units", map[string]any{"name": "", "kind": "x"}); r.Code != 422 {
		t.Errorf("unit fields: %d", r.Code)
	}
	if r := a.do(&owner, "POST", base+"/invitations", map[string]any{"email": "bad", "role": "x"}); r.Code != 422 || r.fields(t)["email"] == nil || r.fields(t)["role"] == nil {
		t.Errorf("invitation fields: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "POST", base+"/invitations", map[string]any{"email": "a@example.ru", "role": "unit_head", "unit_id": "not-a-uuid"}); r.Code != 422 || r.fields(t)["unit_id"] == nil {
		t.Errorf("bad unit id: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "PUT", base+"/units/"+id+"/head", map[string]any{"user_id": "not-a-uuid"}); r.Code != 422 || r.fields(t)["user_id"] == nil {
		t.Errorf("bad head id: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "PUT", base+"/units/"+id+"/head", map[string]any{"user_id": uuid.NewString()}); r.Code != 422 {
		t.Errorf("head who is not a member: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&owner, "PATCH", base+"/members/"+owner.ID.String(), map[string]any{"role": "boss"}); r.Code != 422 || r.fields(t)["role"] == nil {
		t.Errorf("bad role: %d %s", r.Code, r.Raw)
	}
	// Неверные номера в адресе — «такой страницы нет», а не ошибка сервера.
	for _, c := range []struct{ method, path string }{
		{"PATCH", base + "/units/zzz"}, {"DELETE", base + "/units/zzz"}, {"PUT", base + "/units/zzz/head"},
		{"PATCH", base + "/members/zzz"}, {"DELETE", base + "/members/zzz"}, {"DELETE", base + "/invitations/zzz"},
		{"POST", "/api/invitations/zzz/accept"},
	} {
		var body any = map[string]any{}
		if c.method == "DELETE" {
			body = nil
		}
		if r := a.do(&owner, c.method, c.path, body); r.Code != 404 {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	// Чужая организация и несуществующая.
	if r := a.do(&owner, "PATCH", "/api/organizations/no-such-org", orgBodyJSON()); r.Code != 404 {
		t.Errorf("missing organization: %d", r.Code)
	}
	// Тело, которого не должно быть (принять по номеру) и сломанное.
	if r := a.do(&owner, "POST", "/api/invitations/"+uuid.NewString()+"/accept", "{broken"); r.Code != 400 {
		t.Errorf("broken body for accept by id: %d", r.Code)
	}
	// Остальные методы с телом-мусором.
	for _, c := range []struct{ method, path string }{
		{"PATCH", base}, {"POST", base + "/units"}, {"PATCH", base + "/units/" + id}, {"PUT", base + "/units/" + id + "/head"},
		{"PATCH", base + "/members/" + owner.ID.String()}, {"POST", base + "/invitations"},
		{"POST", "/api/invitations/lookup"}, {"POST", "/api/invitations/accept"},
	} {
		if r := a.do(&owner, c.method, c.path, "{broken"); r.Code != 400 {
			t.Errorf("%s %s with a broken body: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPPermissionsAndInvitationErrors(t *testing.T) {
	a := newAPI(t)
	owner := a.user("Иван")
	org := a.org(owner)
	unit := a.unit(owner, org.Slug)
	hr := a.user("Кадровик")
	a.member(owner, org.Slug, hr, "hr", nil)
	stranger := a.user("Чужой")
	base := "/api/organizations/" + org.Slug

	for _, who := range []*person{&hr, &stranger} {
		for _, c := range []struct {
			method, path string
			body         any
		}{
			{"PATCH", base, orgBodyJSON()},
			{"POST", base + "/units", unitBodyJSON()},
			{"PATCH", base + "/units/" + unit.ID.String(), unitBodyJSON()},
			{"DELETE", base + "/units/" + unit.ID.String(), nil},
			{"PUT", base + "/units/" + unit.ID.String() + "/head", map[string]any{"user_id": nil}},
			{"GET", base + "/members", nil},
			{"PATCH", base + "/members/" + owner.ID.String(), map[string]any{"role": "hr"}},
			{"DELETE", base + "/members/" + owner.ID.String(), nil},
			{"POST", base + "/invitations", map[string]any{"email": "a@example.ru", "role": "hr"}},
			{"DELETE", base + "/invitations/" + uuid.NewString(), nil},
		} {
			if r := a.do(who, c.method, c.path, c.body); r.Code != 403 || r.errCode(t) != "forbidden" {
				t.Errorf("%s %s as %s: %d %s", c.method, c.path, who.Name, r.Code, r.Raw)
			}
		}
	}

	// Приглашение: чужая почта, повтор, уже сотрудник.
	guest := a.user("Гость")
	a.do(&owner, "POST", base+"/invitations", map[string]any{"email": guest.Email, "role": "hr"})
	token := tokenIn(t, a.lastMailTo(guest.Email).Body)
	if r := a.do(&stranger, "POST", "/api/invitations/accept", map[string]string{"token": token}); r.Code != 403 || r.errCode(t) != "invitation_wrong_email" {
		t.Errorf("wrong email: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&guest, "POST", "/api/invitations/accept", map[string]string{"token": "garbage"}); r.Code != 400 || r.errCode(t) != "invalid_invitation" {
		t.Errorf("garbage token: %d %s", r.Code, r.Raw)
	}
	if r := a.do(nil, "POST", "/api/invitations/lookup", map[string]string{"token": "garbage"}); r.Code != 400 || r.errCode(t) != "invalid_invitation" {
		t.Errorf("lookup garbage: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&guest, "POST", "/api/invitations/"+uuid.NewString()+"/accept", map[string]any{}); r.Code != 400 || r.errCode(t) != "invalid_invitation" {
		t.Errorf("unknown invitation id: %d %s", r.Code, r.Raw)
	}
	if _, err := sharedPool.Exec(bg, "INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'hr', now())", org.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.do(&guest, "POST", "/api/invitations/accept", map[string]string{"token": token}); r.Code != 409 || r.errCode(t) != "already_member" {
		t.Errorf("already a member: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPRateLimit(t *testing.T) {
	a := newAPI(t)
	owner := a.user("Иван")
	a.svc.cfg.Create = Limit{Max: 1, Window: time.Hour}
	if r := a.do(&owner, "POST", "/api/organizations", orgBodyJSON()); r.Code != 201 {
		t.Fatalf("first: %d %s", r.Code, r.Raw)
	}
	r := a.do(&owner, "POST", "/api/organizations", orgBodyJSON())
	if r.Code != 429 || r.errCode(t) != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Errorf("second: %d %s %v", r.Code, r.Raw, r.Header)
	}
}

// Сбой базы доходит до человека как «что-то сломалось» и попадает в журнал, а не как «нет такой страницы».
func TestHTTPDatabaseFailureIs500(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	svc, _ := w.faulty(1)
	svc.logger = slog.New(slog.NewTextHandler(w.logs, nil))
	a := newAPIWith(w, svc)
	for _, path := range []string{"/api/organizations", "/api/organizations/" + org.Slug} {
		r := a.do(nil, "GET", path, nil)
		if r.Code != 500 || r.errCode(t) != "internal" {
			t.Errorf("%s: %d %s", path, r.Code, r.Raw)
		}
		svc, _ = w.faulty(1)
		svc.logger = slog.New(slog.NewTextHandler(w.logs, nil))
		a = newAPIWith(w, svc)
	}
	if !strings.Contains(w.logs.String(), "organizations request failed") {
		t.Error("a failure must be logged")
	}
}

// Ошибка другого рода, чем известные, — тоже 500.
func TestHTTPFailMapsUnknownErrors(t *testing.T) {
	w := newWorld(t)
	h := NewHandler(w.svc, slog.New(slog.DiscardHandler), nil)
	rec := httptest.NewRecorder()
	h.fail(rec, httptest.NewRequest("GET", "/x", nil), errors.New("boom"))
	if rec.Code != 500 {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestHTTPMyOrganizationsFailures(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	w.org(owner)
	// Сбой на первом запросе («мои организации») и на втором («мои приглашения»): оба видны как 500.
	for n := int32(1); n <= 2; n++ {
		svc, _ := w.faulty(n)
		a := newAPIWith(w, svc)
		if r := a.do(&owner, "GET", "/api/my/organizations", nil); r.Code != 500 {
			t.Errorf("fault %d: %d %s", n, r.Code, r.Raw)
		}
	}
}

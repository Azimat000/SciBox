package vacancies

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/auth"
)

// api — настоящие обработчики аккаунтов и вакансий за настоящим роутером.
type api struct {
	*world
	h http.Handler
}

func newAPI(t *testing.T) *api {
	w := newWorld(t)
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(authH.SameOrigin, authH.Authenticate)
		NewHandler(w.svc, w.log, authH.RequireUser).Mount(r)
	})
	return &api{world: w, h: r}
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

func (r reply) vacancy(t *testing.T) map[string]any {
	t.Helper()
	v, _ := r.json(t)["vacancy"].(map[string]any)
	if v == nil {
		t.Fatalf("no vacancy in %d: %s", r.Code, r.Raw)
	}
	return v
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

func goodBody(org string) map[string]any {
	return map[string]any{
		"organization": org, "title": "Старший научный сотрудник", "position_code": "senior_researcher",
		"summary":     "Исследования активных центров катализаторов методами операндо-спектроскопии.",
		"description": "Работа в команде: эксперименты, анализ данных, публикации.", "career_level": 3,
		"work_format": "onsite", "region_code": "54", "city": "Новосибирск", "rate_percent": 100,
		"contract_type": "fixed", "contract_months": 36, "specialties": []string{"1.4.4"}, "deadline": "2026-12-01",
		"salary_from": 80000, "unit_id": nil,
	}
}

func TestHTTPFullCycle(t *testing.T) {
	a := newAPI(t)
	tm := a.team()

	// Создание: нужен вход; ответ — 201 и вакансия-черновик.
	if r := a.do(nil, "POST", "/api/vacancies", goodBody(tm.slug)); r.Code != http.StatusUnauthorized {
		t.Errorf("anonymous create: %d %s", r.Code, r.Raw)
	}
	r := a.do(&tm.owner, "POST", "/api/vacancies", goodBody(tm.slug))
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", r.Code, r.Raw)
	}
	v := r.vacancy(t)
	id := v["id"].(string)
	if v["status"] != "draft" || v["salary_from"].(float64) != 80000 || v["salary_to"] != nil || v["deadline"] != "2026-12-01" {
		t.Errorf("vacancy: %v", v)
	}
	if viewer := v["viewer"].(map[string]any); viewer["can_manage"] != true {
		t.Errorf("viewer: %v", viewer)
	}

	// Черновика не видит никто, кроме тех, кто ведёт вакансии.
	if r := a.do(nil, "GET", "/api/vacancies/"+id, nil); r.Code != http.StatusNotFound || r.errCode(t) != "not_found" {
		t.Errorf("anonymous sees a draft: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&tm.out, "GET", "/api/vacancies/"+id, nil); r.Code != http.StatusNotFound {
		t.Errorf("stranger sees a draft: %d", r.Code)
	}
	if r := a.do(&tm.hr, "GET", "/api/vacancies/"+id, nil); r.Code != http.StatusOK {
		t.Errorf("hr cannot see a draft: %d", r.Code)
	}

	// Публикация: чужой — 404 (черновик), потом свой — 200.
	if r := a.do(&tm.out, "POST", "/api/vacancies/"+id+"/status", map[string]string{"status": "published"}); r.Code != http.StatusNotFound {
		t.Errorf("stranger publishes: %d", r.Code)
	}
	r = a.do(&tm.hr, "POST", "/api/vacancies/"+id+"/status", map[string]string{"status": "published"})
	if r.Code != http.StatusOK || r.vacancy(t)["status"] != "published" {
		t.Fatalf("publish: %d %s", r.Code, r.Raw)
	}
	if r := a.do(nil, "GET", "/api/vacancies/"+id, nil); r.Code != http.StatusOK {
		t.Errorf("published vacancy is public: %d", r.Code)
	}
	if r := a.do(&tm.out, "POST", "/api/vacancies/"+id+"/status", map[string]string{"status": "closed"}); r.Code != http.StatusForbidden || r.errCode(t) != "forbidden" {
		t.Errorf("stranger closes: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&tm.hr, "POST", "/api/vacancies/"+id+"/status", map[string]string{"status": "draft"}); r.Code != http.StatusConflict || r.errCode(t) != CodeBadTransition {
		t.Errorf("bad transition: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&tm.hr, "DELETE", "/api/vacancies/"+id, nil); r.Code != http.StatusConflict || r.errCode(t) != CodeNotDraft {
		t.Errorf("delete published: %d %s", r.Code, r.Raw)
	}

	// Правка и списки.
	b := goodBody(tm.slug)
	delete(b, "organization")
	b["title"] = "Ведущий научный сотрудник"
	b["position_code"] = "leading_researcher"
	if r := a.do(&tm.hr, "PATCH", "/api/vacancies/"+id, b); r.Code != http.StatusOK || r.vacancy(t)["title"] != "Ведущий научный сотрудник" {
		t.Errorf("update: %d %s", r.Code, r.Raw)
	}
	if r := a.do(nil, "GET", "/api/vacancies?org="+tm.slug, nil); r.Code != http.StatusOK || r.json(t)["total"].(float64) != 1 {
		t.Errorf("public list: %d %s", r.Code, r.Raw)
	}
	mine := a.do(&tm.hr, "GET", "/api/my/vacancies?status=published", nil)
	if mine.Code != http.StatusOK || mine.json(t)["total"].(float64) != 1 || mine.json(t)["counts"].(map[string]any)["published"].(float64) != 1 {
		t.Errorf("my list: %d %s", mine.Code, mine.Raw)
	}
	if r := a.do(&tm.hr, "GET", "/api/my/vacancy-targets", nil); r.Code != http.StatusOK || len(r.json(t)["targets"].([]any)) != 1 {
		t.Errorf("targets: %d %s", r.Code, r.Raw)
	}

	// Закрыть, вернуть, убрать: и удалить черновик.
	for _, to := range []string{"closed", "archived"} {
		if r := a.do(&tm.hr, "POST", "/api/vacancies/"+id+"/status", map[string]string{"status": to}); r.Code != http.StatusOK {
			t.Fatalf("→ %s: %d %s", to, r.Code, r.Raw)
		}
	}
	if r := a.do(nil, "GET", "/api/vacancies/"+id, nil); r.Code != http.StatusNotFound {
		t.Errorf("archived is hidden from the public: %d", r.Code)
	}
	dr := a.do(&tm.owner, "POST", "/api/vacancies", goodBody(tm.slug))
	if r := a.do(&tm.owner, "DELETE", "/api/vacancies/"+dr.vacancy(t)["id"].(string), nil); r.Code != http.StatusNoContent {
		t.Errorf("delete draft: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPErrors(t *testing.T) {
	a := newAPI(t)
	tm := a.team()
	check := func(name string, r reply, code int, errCode string) {
		t.Helper()
		if r.Code != code || (errCode != "" && r.errCode(t) != errCode) {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	check("invalid form", a.do(&tm.owner, "POST", "/api/vacancies", map[string]any{"organization": tm.slug}), 422, "validation_failed")
	check("unknown organization", a.do(&tm.owner, "POST", "/api/vacancies", goodBody("no-such")), 404, "not_found")
	check("stranger creates", a.do(&tm.out, "POST", "/api/vacancies", goodBody(tm.slug)), 403, "forbidden")
	b := goodBody(tm.slug)
	b["unit_id"] = "not-a-uuid"
	check("bad unit id", a.do(&tm.owner, "POST", "/api/vacancies", b), 422, "validation_failed")
	b["unit_id"] = ""
	check("empty unit id means no unit", a.do(&tm.owner, "POST", "/api/vacancies", b), 201, "")
	check("unknown field", a.do(&tm.owner, "POST", "/api/vacancies", `{"organization":"x","bogus":1}`), 400, "bad_request")
	check("broken json", a.do(&tm.owner, "POST", "/api/vacancies", `{`), 400, "bad_request")
	check("not an id (get)", a.do(nil, "GET", "/api/vacancies/abc", nil), 404, "not_found")
	check("not an id (patch)", a.do(&tm.owner, "PATCH", "/api/vacancies/abc", goodBody(tm.slug)), 404, "not_found")
	check("not an id (delete)", a.do(&tm.owner, "DELETE", "/api/vacancies/abc", nil), 404, "not_found")
	check("not an id (status)", a.do(&tm.owner, "POST", "/api/vacancies/abc/status", map[string]string{"status": "closed"}), 404, "not_found")
	check("unknown vacancy", a.do(&tm.owner, "GET", "/api/vacancies/"+tm.owner.ID.String(), nil), 404, "not_found")
	d := a.do(&tm.owner, "POST", "/api/vacancies", goodBody(tm.slug)).vacancy(t)["id"].(string)
	b = goodBody(tm.slug)
	b["unit_id"] = "bad"
	check("update bad unit id", a.do(&tm.owner, "PATCH", "/api/vacancies/"+d, b), 422, "validation_failed")
	check("update broken json", a.do(&tm.owner, "PATCH", "/api/vacancies/"+d, `{`), 400, "bad_request")
	check("status broken json", a.do(&tm.owner, "POST", "/api/vacancies/"+d+"/status", `{`), 400, "bad_request")
	check("list: bad unit", a.do(nil, "GET", "/api/vacancies?org="+tm.slug+"&unit=zzz", nil), 404, "not_found")
	check("list: unknown organization", a.do(nil, "GET", "/api/vacancies?org=no-such", nil), 404, "not_found")
	check("mine: bad status", a.do(&tm.owner, "GET", "/api/my/vacancies?status=weird", nil), 422, "validation_failed")
	check("mine: not signed in", a.do(nil, "GET", "/api/my/vacancies", nil), 401, "unauthorized")
	check("targets: not signed in", a.do(nil, "GET", "/api/my/vacancy-targets", nil), 401, "unauthorized")
	check("list: public", a.do(nil, "GET", "/api/vacancies?limit=zz&offset=-4", nil), 200, "")

	// Лимит: 429 с Retry-After.
	a.svc.cfg.Create = Limit{Max: 1, Window: time.Hour}
	r := a.do(&tm.hr, "POST", "/api/vacancies", goodBody(tm.slug))
	check("first of the limit", r, 201, "")
	r = a.do(&tm.hr, "POST", "/api/vacancies", goodBody(tm.slug))
	check("over the limit", r, 429, "rate_limited")
	if r.Header.Get("Retry-After") == "" {
		t.Error("Retry-After is missing")
	}
}

func TestHTTPServerFailureIsHidden(t *testing.T) {
	w := newWorld(t)
	svc, _ := w.faulty(1)
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) { NewHandler(svc, w.log, authH.RequireUser).Mount(r) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/vacancies", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "injected") || !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

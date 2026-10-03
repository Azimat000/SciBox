package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
)

// api — настоящие обработчики аккаунтов и профилей за настоящим роутером.
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

func (r reply) fields(t *testing.T) map[string]any {
	t.Helper()
	e, _ := r.json(t)["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	return f
}

func (r reply) profile(t *testing.T) map[string]any {
	t.Helper()
	p, _ := r.json(t)["profile"].(map[string]any)
	if p == nil {
		t.Fatalf("no profile in %d: %s", r.Code, r.Raw)
	}
	return p
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

func TestHTTPNeedsSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid4()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/profile"}, {"PUT", "/api/profile"}, {"PUT", "/api/profile/privacy"}, {"POST", "/api/profile/items"},
		{"PUT", "/api/profile/items/" + id}, {"DELETE", "/api/profile/items/" + id}, {"GET", "/api/profile/doi?doi=10.1234/x"},
		{"GET", "/api/profile/cv"}, {"GET", "/api/scientists/" + id + "/cv"},
	} {
		if r := a.do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s без входа: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
}

func uuid4() string { return "6f1d2a3e-0b1c-4d5e-8f90-123456789abc" }

func TestHTTPOwnProfileCycle(t *testing.T) {
	a := newAPI(t)
	p := a.user("Елена Орлова")

	r := a.do(&p, "GET", "/api/profile", nil)
	if r.Code != 200 {
		t.Fatalf("own: %d %s", r.Code, r.Raw)
	}
	prof := r.profile(t)
	if prof["visibility"] != "hidden" || prof["name"] != "Елена Орлова" {
		t.Errorf("профиль: %v", prof)
	}
	if v := r.json(t)["viewer"].(map[string]any); v["is_owner"] != true {
		t.Errorf("viewer: %v", v)
	}

	// Основные поля.
	body := map[string]any{
		"headline": "Старший научный сотрудник", "city": "Новосибирск", "region_code": "54", "about": "О себе",
		"degree": "candidate", "degree_specialty_code": "1.4.4", "degree_year": 2016, "degree_institution": "Институт катализа",
		"dissertation_title": "Тема", "academic_title": "docent", "academic_title_year": 2021,
		"orcid": "0000-0002-1825-0097", "spin": "1234-5678", "scopus_id": "57190123456", "wos_id": "A-1234-2008",
		"h_rsci": 12, "h_scopus": nil, "contact_email": "orlova@example.ru", "specialties": []string{"1.4.4"},
	}
	r = a.do(&p, "PUT", "/api/profile", body)
	if r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Raw)
	}
	prof = r.profile(t)
	if prof["headline"] != "Старший научный сотрудник" || prof["h_index"].(map[string]any)["rsci"].(float64) != 12 || prof["h_index"].(map[string]any)["scopus"] != nil {
		t.Errorf("после сохранения: %v", prof)
	}
	if prof["identifiers"].(map[string]any)["spin"] != "12345678" {
		t.Errorf("идентификаторы: %v", prof["identifiers"])
	}

	// Ошибки полей: 422 и поле названо.
	bad := map[string]any{"orcid": "nope", "h_wos": 1000}
	r = a.do(&p, "PUT", "/api/profile", bad)
	if r.Code != http.StatusUnprocessableEntity || r.fields(t)["orcid"] == nil || r.fields(t)["h_wos"] == nil {
		t.Errorf("ошибки: %d %s", r.Code, r.Raw)
	}

	// Приватность.
	r = a.do(&p, "PUT", "/api/profile/privacy", map[string]any{"visibility": "public", "open_to_offers": true})
	if r.Code != 200 || r.profile(t)["visibility"] != "public" || r.profile(t)["open_to_offers"] != true {
		t.Errorf("privacy: %d %s", r.Code, r.Raw)
	}
	r = a.do(&p, "PUT", "/api/profile/privacy", map[string]any{"visibility": "friends"})
	if r.Code != http.StatusUnprocessableEntity || r.fields(t)["visibility"] == nil {
		t.Errorf("privacy bad: %d %s", r.Code, r.Raw)
	}

	// Записи: создание, правка, удаление.
	pub := map[string]any{"kind": "publication", "title": "Статья", "authors": "Орлова Е.", "pub_type": "article", "year": 2023, "doi": "10.1234/ABC"}
	r = a.do(&p, "POST", "/api/profile/items", pub)
	if r.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", r.Code, r.Raw)
	}
	item := r.json(t)["item"].(map[string]any)
	id := item["id"].(string)
	if item["kind"] != "publication" || item["doi"] != "10.1234/abc" || item["source"] != "manual" {
		t.Errorf("item: %v", item)
	}
	if r := a.do(&p, "POST", "/api/profile/items", pub); r.Code != http.StatusUnprocessableEntity || r.fields(t)["doi"] == nil {
		t.Errorf("дубль DOI: %d %s", r.Code, r.Raw)
	}
	pub["title"] = "Статья, вторая редакция"
	r = a.do(&p, "PUT", "/api/profile/items/"+id, pub)
	if r.Code != 200 || r.json(t)["item"].(map[string]any)["title"] != "Статья, вторая редакция" {
		t.Errorf("update: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "PUT", "/api/profile/items/"+id, map[string]any{"kind": "grant", "title": "x"}); r.Code != http.StatusUnprocessableEntity || r.fields(t)["kind"] == nil {
		t.Errorf("смена вида: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "POST", "/api/profile/items", map[string]any{"kind": "hobby"}); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("неизвестный вид: %d", r.Code)
	}
	if r := a.do(&p, "DELETE", "/api/profile/items/"+id, nil); r.Code != http.StatusNoContent || r.Raw != "" {
		t.Errorf("delete: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "DELETE", "/api/profile/items/"+id, nil); r.Code != http.StatusNotFound {
		t.Errorf("повторное удаление: %d", r.Code)
	}
	for _, path := range []string{"/api/profile/items/not-a-uuid", "/api/scientists/not-a-uuid", "/api/scientists/not-a-uuid/cv"} {
		method := "DELETE"
		if strings.Contains(path, "scientists") {
			method = "GET"
		}
		if r := a.do(&p, method, path, nil); r.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", method, path, r.Code)
		}
	}
	if r := a.do(&p, "PUT", "/api/profile/items/not-a-uuid", map[string]any{}); r.Code != http.StatusNotFound {
		t.Errorf("PUT с плохим номером: %d", r.Code)
	}
}

func TestHTTPBadRequests(t *testing.T) {
	a := newAPI(t)
	p := a.user("Елена")
	id := uuid4()
	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/profile"}, {"PUT", "/api/profile/privacy"}, {"POST", "/api/profile/items"}, {"PUT", "/api/profile/items/" + id},
	} {
		if r := a.do(&p, c.method, c.path, "{not json"); r.Code != http.StatusBadRequest {
			t.Errorf("%s %s: плохой JSON: %d", c.method, c.path, r.Code)
		}
		if r := a.do(&p, c.method, c.path, `{"surprise": 1}`); r.Code != http.StatusBadRequest {
			t.Errorf("%s %s: неизвестное поле: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPItemsOfOtherPeople(t *testing.T) {
	a := newAPI(t)
	owner, other := a.user("Хозяин"), a.user("Чужой")
	it := a.addItem(owner, goodPublication())
	if r := a.do(&other, "DELETE", "/api/profile/items/"+it.ID.String(), nil); r.Code != http.StatusNotFound {
		t.Errorf("чужое удаление: %d", r.Code)
	}
	if r := a.do(&other, "PUT", "/api/profile/items/"+it.ID.String(), map[string]any{"kind": "publication", "title": "Чужое"}); r.Code != http.StatusNotFound {
		t.Errorf("чужая правка: %d", r.Code)
	}
}

func TestHTTPItemLimit(t *testing.T) {
	old := maxItemsKind[KindPatent]
	maxItemsKind[KindPatent] = 1
	t.Cleanup(func() { maxItemsKind[KindPatent] = old })
	a := newAPI(t)
	p := a.user("Елена")
	body := map[string]any{"kind": "patent", "title": "Способ", "number": "RU 1", "patent_type": "invention", "year": 2022}
	if r := a.do(&p, "POST", "/api/profile/items", body); r.Code != http.StatusCreated {
		t.Fatalf("первый: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "POST", "/api/profile/items", body); r.Code != http.StatusConflict || r.errCode(t) != CodeTooMany {
		t.Errorf("второй: %d %s", r.Code, r.Raw)
	}
}

// Что видит другой человек в самом JSON: почты и служебных полей нет, пока не положено.
func TestHTTPPublicProfileLeaks(t *testing.T) {
	a := newAPI(t)
	v := a.viewers()
	if _, err := a.svc.SaveCore(bg, v.owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	a.addItem(v.owner, goodPublication())
	own, _ := a.svc.Own(bg, v.owner.User)
	path := "/api/scientists/" + own.Profile.ID.String()

	for _, mode := range []string{"hidden", "orgs", "public"} {
		a.setVisibility(v.owner, mode, false)
		for _, c := range []struct {
			name     string
			who      *person
			sees     bool
			contacts bool
		}{
			{"аноним", nil, mode == "public", false},
			{"вошедший без организации", &v.stranger, mode == "public", false},
			{"сотрудник", &v.staff, mode != "hidden", true},
			{"владелец", &v.owner, true, true},
		} {
			r := a.do(c.who, "GET", path, nil)
			if !c.sees {
				if r.Code != http.StatusNotFound || r.errCode(t) != "not_found" {
					t.Errorf("%s/%s: должно быть 404, получили %d %s", mode, c.name, r.Code, r.Raw)
				}
				for _, secret := range []string{"Владелец профиля", "orlova@example.ru", "Активные центры"} {
					if strings.Contains(r.Raw, secret) {
						t.Errorf("%s/%s: в отказе утекло %q", mode, c.name, secret)
					}
				}
				continue
			}
			if r.Code != 200 {
				t.Errorf("%s/%s: %d %s", mode, c.name, r.Code, r.Raw)
				continue
			}
			if got := strings.Contains(r.Raw, "orlova@example.ru"); got != c.contacts {
				t.Errorf("%s/%s: почта в ответе = %v, ожидали %v", mode, c.name, got, c.contacts)
			}
			if got := strings.Contains(r.Raw, `"visibility"`); got != (c.who == &v.owner) {
				t.Errorf("%s/%s: поле visibility в ответе = %v", mode, c.name, got)
			}
			// Почта аккаунта не уходит никому, кроме самого человека (и его контактная почта — только по правилам выше).
			if c.who != &v.owner && strings.Contains(r.Raw, v.owner.Email) {
				t.Errorf("%s/%s: утекла почта аккаунта", mode, c.name)
			}
		}
	}
}

func TestHTTPCV(t *testing.T) {
	a := newAPI(t)
	v := a.viewers()
	if _, err := a.svc.SaveCore(bg, v.owner.User, goodCore()); err != nil {
		t.Fatal(err)
	}
	own, _ := a.svc.Own(bg, v.owner.User)
	path := "/api/scientists/" + own.Profile.ID.String() + "/cv"

	r := a.do(&v.owner, "GET", "/api/profile/cv", nil)
	if r.Code != 200 || r.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(r.Raw, "%PDF-") {
		t.Fatalf("own cv: %d %q", r.Code, r.Header.Get("Content-Type"))
	}
	cd := r.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, `attachment; filename="cv.pdf"; filename*=UTF-8''`) || !strings.Contains(cd, "CV.pdf") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("заголовки: %q", cd)
	}
	if r.Header.Get("Content-Length") != fmt.Sprint(len(r.Raw)) {
		t.Errorf("Content-Length %q, тело %d", r.Header.Get("Content-Length"), len(r.Raw))
	}
	// Скрытый профиль: чужому 404; публичный: любому вошедшему.
	if r := a.do(&v.staff, "GET", path, nil); r.Code != http.StatusNotFound {
		t.Errorf("скрытый: %d", r.Code)
	}
	a.setVisibility(v.owner, "public", false)
	if r := a.do(&v.stranger, "GET", path, nil); r.Code != 200 || !strings.HasPrefix(r.Raw, "%PDF-") {
		t.Errorf("публичный: %d", r.Code)
	}
}

func TestHTTPDOI(t *testing.T) {
	a := newAPI(t)
	a.svc.cfg.DOI = Limit{Max: 4, Window: time.Hour}
	p := a.user("Елена")

	r := a.do(&p, "GET", "/api/profile/doi?doi=https%3A%2F%2Fdoi.org%2F10.1234%2FABC", nil)
	w, _ := r.json(t)["work"].(map[string]any)
	if r.Code != 200 || w["doi"] != "10.1234/abc" || w["title"] != "Найденная статья" || w["year"].(float64) != 2023 {
		t.Fatalf("lookup: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "GET", "/api/profile/doi?doi=nature", nil); r.Code != http.StatusUnprocessableEntity || r.fields(t)["doi"] == nil {
		t.Errorf("неверный: %d %s", r.Code, r.Raw)
	}
	if r := a.do(&p, "GET", "/api/profile/doi", nil); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("без DOI: %d", r.Code)
	}
	a.doi.err = crossref.ErrNotFound
	if r := a.do(&p, "GET", "/api/profile/doi?doi=10.1234/none", nil); r.Code != http.StatusNotFound || r.errCode(t) != CodeDOINotFound {
		t.Errorf("не найден: %d %s", r.Code, r.Raw)
	}
	a.doi.err = fmt.Errorf("%w: сбой", crossref.ErrUnavailable)
	if r := a.do(&p, "GET", "/api/profile/doi?doi=10.1234/down", nil); r.Code != http.StatusBadGateway || r.errCode(t) != CodeDOIUnavailable {
		t.Errorf("недоступен: %d %s", r.Code, r.Raw)
	}
	// Лимит исчерпан (четыре обращения уже было: 1 успех + 2 ошибки Crossref + …).
	a.doi.err = nil
	a.do(&p, "GET", "/api/profile/doi?doi=10.1234/more", nil)
	r = a.do(&p, "GET", "/api/profile/doi?doi=10.1234/limit", nil)
	if r.Code != http.StatusTooManyRequests || r.errCode(t) != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Errorf("лимит: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPInternalErrorIsHidden(t *testing.T) {
	a := newAPI(t)
	p := a.user("Елена")
	// Нарушаем данные в базе: запись с мусором вместо JSON-объекта не должна ронять ответ с подробностями.
	own, _ := a.svc.Own(bg, p.User)
	if _, err := sharedPool.Exec(bg, `INSERT INTO profile_items (profile_id, kind, data, created_at, updated_at) VALUES ($1, 'grant', '[]', now(), now())`, own.Profile.ID); err != nil {
		t.Fatal(err)
	}
	r := a.do(&p, "GET", "/api/profile", nil)
	if r.Code != http.StatusInternalServerError || r.errCode(t) != "internal" || strings.Contains(r.Raw, "json") {
		t.Errorf("%d %s", r.Code, r.Raw)
	}
}

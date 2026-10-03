package matching

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

type apiWorld struct {
	*world
	api *testkit.API
}

func newAPI(t *testing.T) *apiWorld {
	w := newWorld(t)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(w.svc, w.Log, requireUser).Mount(r)
	})
	return &apiWorld{world: w, api: api}
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/favorites"}, {"GET", "/api/favorites/ids"}, {"PUT", "/api/favorites/" + id}, {"DELETE", "/api/favorites/" + id},
		{"GET", "/api/saved-searches"}, {"POST", "/api/saved-searches"}, {"GET", "/api/saved-searches/" + id},
		{"PATCH", "/api/saved-searches/" + id}, {"DELETE", "/api/saved-searches/" + id}, {"GET", "/api/matches"}, {"GET", "/api/deadlines"},
	} {
		if r := a.api.Do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPFavorites(t *testing.T) {
	a := newAPI(t)
	anna, boris := a.User("Анна"), a.User("Борис")
	v := a.publish(vacancyOpts{})
	path := "/api/favorites/" + v.ID.String()

	if r := a.api.Do(&anna, "PUT", path, nil); r.Code != 204 {
		t.Fatalf("add: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&anna, "PUT", path, nil); r.Code != 204 {
		t.Errorf("повторное добавление: %d", r.Code)
	}
	r := a.api.Do(&anna, "GET", "/api/favorites/ids", nil)
	if ids := r.JSON(t)["ids"].([]any); r.Code != 200 || len(ids) != 1 || ids[0] != v.ID.String() {
		t.Errorf("ids: %d %s", r.Code, r.Raw)
	}
	r = a.api.Do(&anna, "GET", "/api/favorites", nil)
	items := r.JSON(t)["items"].([]any)
	if r.Code != 200 || len(items) != 1 || r.JSON(t)["total"] != float64(1) {
		t.Fatalf("list: %d %s", r.Code, r.Raw)
	}
	it := items[0].(map[string]any)
	if it["state"] != "open" || it["application_id"] != nil || it["vacancy"].(map[string]any)["id"] != v.ID.String() {
		t.Errorf("запись: %v", it)
	}
	if r := a.api.Do(&anna, "GET", "/api/favorites?limit=1&offset=5", nil); r.Code != 200 || len(r.JSON(t)["items"].([]any)) != 0 {
		t.Errorf("страница за пределами: %d %s", r.Code, r.Raw)
	}
	// Чужое избранное пусто, чужое не убирается.
	if r := a.api.Do(&boris, "GET", "/api/favorites/ids", nil); len(r.JSON(t)["ids"].([]any)) != 0 {
		t.Errorf("чужое: %s", r.Raw)
	}
	a.api.Do(&boris, "DELETE", path, nil)
	if r := a.api.Do(&anna, "GET", "/api/favorites/ids", nil); len(r.JSON(t)["ids"].([]any)) != 1 {
		t.Errorf("Борис убрал чужое: %s", r.Raw)
	}

	// Ошибки.
	draft := a.create(vacancyOpts{})
	for name, p := range map[string]string{"черновик": draft.ID.String(), "нет такой": uuid.NewString(), "не номер": "abc"} {
		if r := a.api.Do(&anna, "PUT", "/api/favorites/"+p, nil); r.Code != 404 || r.ErrCode(t) != "not_found" {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&anna, "DELETE", "/api/favorites/abc", nil); r.Code != 404 {
		t.Errorf("убрать не номер: %d", r.Code)
	}
	if r := a.api.Do(&anna, "DELETE", path, nil); r.Code != 204 {
		t.Errorf("remove: %d", r.Code)
	}
	// Предел.
	a.svc.cfg.MaxFavorites = 1
	other := a.publish(vacancyOpts{})
	a.api.Do(&anna, "PUT", path, nil)
	if r := a.api.Do(&anna, "PUT", "/api/favorites/"+other.ID.String(), nil); r.Code != 409 || r.ErrCode(t) != CodeTooManyFavorites {
		t.Errorf("предел: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPSavedSearches(t *testing.T) {
	a := newAPI(t)
	anna, boris := a.User("Анна"), a.User("Борис")

	r := a.api.Do(&anna, "POST", "/api/saved-searches", map[string]any{"name": "Химия в Новосибирске", "query": "region=54&q=химия", "frequency": "weekly"})
	if r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Raw)
	}
	s := r.JSON(t)["search"].(map[string]any)
	id := s["id"].(string)
	if s["frequency"] != "weekly" || s["name"] != "Химия в Новосибирске" || !strings.Contains(s["query"].(string), "region=54") || s["last_sent_at"] != nil {
		t.Errorf("поиск: %v", s)
	}
	if r := a.api.Do(&anna, "GET", "/api/saved-searches", nil); r.Code != 200 || len(r.JSON(t)["items"].([]any)) != 1 {
		t.Errorf("list: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&anna, "GET", "/api/saved-searches/"+id, nil); r.Code != 200 || r.JSON(t)["search"].(map[string]any)["id"] != id {
		t.Errorf("get: %d %s", r.Code, r.Raw)
	}
	r = a.api.Do(&anna, "PATCH", "/api/saved-searches/"+id, map[string]any{"name": "Переименован", "frequency": "off"})
	if r.Code != 200 || r.JSON(t)["search"].(map[string]any)["frequency"] != "off" {
		t.Errorf("patch: %d %s", r.Code, r.Raw)
	}

	// Чужой поиск для остальных не существует.
	for _, c := range []struct{ method, body string }{{"GET", ""}, {"PATCH", `{"name":"Х","frequency":"off"}`}, {"DELETE", ""}} {
		var body any
		if c.body != "" {
			body = c.body
		}
		if r := a.api.Do(&boris, c.method, "/api/saved-searches/"+id, body); r.Code != 404 || r.ErrCode(t) != "not_found" {
			t.Errorf("%s чужого: %d %s", c.method, r.Code, r.Raw)
		}
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if r := a.api.Do(&anna, method, "/api/saved-searches/abc", map[string]any{"name": "Х", "frequency": "off"}); r.Code != 404 {
			t.Errorf("%s не номер: %d", method, r.Code)
		}
	}

	// Ошибки создания.
	for _, c := range []struct {
		name  string
		body  any
		code  int
		field string
	}{
		{"без условий", map[string]any{"name": "Х", "query": ""}, 422, "query"},
		{"без названия", map[string]any{"name": "", "query": "q=a"}, 422, "name"},
		{"неверная частота", map[string]any{"name": "Х", "query": "q=a", "frequency": "x"}, 422, "frequency"},
		{"неверный JSON", "{broken", 400, ""},
		{"лишнее поле", map[string]any{"name": "Х", "query": "q=a", "surprise": 1}, 400, ""},
	} {
		r := a.api.Do(&anna, "POST", "/api/saved-searches", c.body)
		if r.Code != c.code || (c.field != "" && r.Fields(t)[c.field] == nil) {
			t.Errorf("%s: %d %s", c.name, r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&anna, "PATCH", "/api/saved-searches/"+id, map[string]any{"name": "", "frequency": "off"}); r.Code != 422 {
		t.Errorf("patch без имени: %d", r.Code)
	}
	if r := a.api.Do(&anna, "PATCH", "/api/saved-searches/"+id, "{broken"); r.Code != 400 {
		t.Errorf("patch сломанный JSON: %d", r.Code)
	}

	a.svc.cfg.MaxSearches = 1
	if r := a.api.Do(&anna, "POST", "/api/saved-searches", map[string]any{"name": "Лишний", "query": "q=a"}); r.Code != 409 || r.ErrCode(t) != CodeTooManySearches {
		t.Errorf("предел: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&anna, "DELETE", "/api/saved-searches/"+id, nil); r.Code != 204 {
		t.Errorf("delete: %d", r.Code)
	}
	if r := a.api.Do(&anna, "DELETE", "/api/saved-searches/"+id, nil); r.Code != 404 {
		t.Errorf("повторное удаление: %d", r.Code)
	}
}

func TestHTTPMatchesAndDeadlines(t *testing.T) {
	a := newAPI(t)
	p := a.seeker("Анна", profiles.CoreInput{Specialties: []string{"1.4.4"}, RegionCode: "54"})
	v := a.publish(vacancyOpts{title: "Подходящая", specialties: []string{"1.4.4"}, region: "54"})

	r := a.api.Do(&p, "GET", "/api/matches?limit=5", nil)
	if r.Code != 200 {
		t.Fatalf("matches: %d %s", r.Code, r.Raw)
	}
	body := r.JSON(t)
	items := body["items"].([]any)
	if body["ready"] != true || len(items) != 1 || body["total"] != float64(1) {
		t.Fatalf("matches: %s", r.Raw)
	}
	it := items[0].(map[string]any)
	reasons := it["reasons"].([]any)
	if it["vacancy"].(map[string]any)["id"] != v.ID.String() || it["score"] != float64(40+20) || len(reasons) != 2 {
		t.Errorf("запись подбора: %v", it)
	}
	if basis := body["basis"].(map[string]any); basis["specialties"] != float64(1) || basis["level"] != float64(1) {
		t.Errorf("основа: %v", basis)
	}

	a.addFavorite(p, v.ID)
	r = a.api.Do(&p, "GET", "/api/deadlines", nil)
	cal := r.JSON(t)
	days := cal["items"].([]any)
	if r.Code != 200 || len(days) != 1 || cal["without_deadline"] != float64(0) || days[0].(map[string]any)["days_left"].(float64) < 80 {
		t.Errorf("deadlines: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPInternalErrorsAreHidden(t *testing.T) {
	a := newAPI(t)
	p := a.User("Анна")
	saved, err := a.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a"})
	if err != nil {
		t.Fatal(err)
	}
	healthy, healthyDB := a.svc.q, a.svc.db
	id, body := saved.ID.String(), map[string]any{"name": "у", "frequency": "off"}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/favorites"}, {"GET", "/api/favorites/ids"}, {"DELETE", "/api/favorites/" + uuid.NewString()},
		{"GET", "/api/saved-searches"}, {"GET", "/api/saved-searches/" + id}, {"PATCH", "/api/saved-searches/" + id},
		{"DELETE", "/api/saved-searches/" + id}, {"GET", "/api/matches"}, {"GET", "/api/deadlines"},
	} {
		// Сломанная база: ответ 500 без подробностей.
		db, _ := testkit.Faulty(testkit.Pool, 1)
		a.svc.q, a.svc.db = dbgenOn(db), db
		r := a.api.Do(&p, c.method, c.path, body)
		a.svc.q, a.svc.db = healthy, healthyDB
		if r.Code != 500 || r.ErrCode(t) != "internal" || strings.Contains(r.Raw, "fault") {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	// Сломанная база при создании (транзакция): тоже 500.
	db, _ := testkit.Faulty(testkit.Pool, 1)
	a.svc.db = db
	r := a.api.Do(&p, "POST", "/api/saved-searches", map[string]any{"name": "х", "query": "q=a"})
	if r.Code != 500 || r.ErrCode(t) != "internal" {
		t.Errorf("создание: %d %s", r.Code, r.Raw)
	}
}

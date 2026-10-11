package vacancies

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/httpcache"
)

// Короткий кеш ответов поиска (срез 27, D-179): тот же запрос в течение срока отвечает из памяти, после срока ответ
// собирается заново, ошибки и неверные запросы не запоминаются.
func TestHTTPSearchCache(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cache := httpcache.NewKeyedWithClock(15*time.Second, 50, func() time.Time { return now })
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(authH.SameOrigin, authH.Authenticate)
		NewHandler(w.svc, w.log, authH.RequireUser).WithSearchCache(cache).Mount(r)
	})
	a := &api{world: w, h: r}
	path := "/api/vacancies?org=" + st.slug + "&sort=relevance&limit=20"

	first := a.do(nil, "GET", path, nil)
	if first.Code != http.StatusOK || !strings.Contains(first.Header.Get("Content-Type"), "application/json") || first.json(t)["total"] != float64(4) {
		t.Fatalf("first: %d %s", first.Code, first.Raw)
	}

	// Вакансию закрыли: до конца срока тот же запрос видит прежний ответ (так и задумано), после срока — новый.
	closed := titles(w.search(st, SearchParams{Limit: 1}))[0]
	var id string
	if err := sharedPool.QueryRow(bg, `SELECT id::text FROM vacancies WHERE title = $1 AND org_id = $2`, closed, st.orgID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedPool.Exec(bg, `UPDATE vacancies SET status = 'closed', closed_at = now() WHERE id = $1::uuid`, id); err != nil {
		t.Fatal(err)
	}
	if second := a.do(nil, "GET", path, nil); second.Raw != first.Raw {
		t.Errorf("within the ttl the answer comes from memory:\n%s\n%s", first.Raw, second.Raw)
	}
	// Те же условия в другом порядке параметров — тот же ключ.
	if reordered := a.do(nil, "GET", "/api/vacancies?limit=20&sort=relevance&org="+st.slug, nil); reordered.Raw != first.Raw {
		t.Errorf("parameters in another order are the same query")
	}
	// Лишние параметры и «1»/«true» не создают новых записей: кеш нельзя обойти мусорным параметром (срез 29, INJ-5).
	if junk := a.do(nil, "GET", path+"&junk=1&_="+strings.Repeat("x", 20), nil); junk.Raw != first.Raw {
		t.Errorf("an unknown parameter must not change the cache key")
	}
	if n := cache.Stats().Entries; n != 1 {
		t.Errorf("entries = %d, want 1", n)
	}
	// Другой запрос — свой ответ.
	other := a.do(nil, "GET", "/api/vacancies?org="+st.slug+"&q=катализатор", nil)
	if other.Code != http.StatusOK || other.Raw == first.Raw {
		t.Errorf("another query has its own answer: %d", other.Code)
	}

	now = now.Add(16 * time.Second)
	if third := a.do(nil, "GET", path, nil); third.json(t)["total"] != float64(3) {
		t.Errorf("after the ttl the answer is rebuilt: %s", third.Raw)
	}
	if s := cache.Stats(); s.Hits != 3 || s.Misses != 3 {
		t.Errorf("stats: %+v", s)
	}

	// Неверные параметры отвечают 422 каждый раз и в кеш не попадают.
	before := cache.Stats().Entries
	for range 2 {
		if bad := a.do(nil, "GET", "/api/vacancies?sort=random", nil); bad.Code != http.StatusUnprocessableEntity {
			t.Errorf("bad parameter: %d %s", bad.Code, bad.Raw)
		}
	}
	if cache.Stats().Entries != before {
		t.Errorf("a rejected query must not occupy the cache")
	}
}

// Сбой базы при сборке ответа доходит до человека как скрытая ошибка 500 и не запоминается.
func TestHTTPSearchCacheDoesNotRememberFailures(t *testing.T) {
	w := newWorld(t)
	svc, _ := w.faulty(1)
	cache := httpcache.NewKeyed(time.Minute, 10)
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		NewHandler(svc, w.log, authH.RequireUser).WithSearchCache(cache).Mount(r)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/vacancies", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "injected") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if n := cache.Stats().Entries; n != 0 {
		t.Errorf("a failure is remembered: %d entries", n)
	}
}

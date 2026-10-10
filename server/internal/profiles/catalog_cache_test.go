package profiles

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

// Короткий кеш каталога для гостей (срез 27, D-179): гость получает ответ из памяти, вошедший человек и сотрудник
// организации (у них своё содержимое каталога) кеша не касаются.
func TestHTTPCatalogCacheServesGuestsOnly(t *testing.T) {
	w := newWorld(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cache := httpcache.NewKeyedWithClock(15*time.Second, 50, func() time.Time { return now })
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(authH.SameOrigin, authH.Authenticate)
		NewHandler(w.svc, w.log, authH.RequireUser).WithCatalogCache(cache).Mount(r)
	})
	a := &api{world: w, h: r}
	m := marker()
	a.listed("Анна Ильина", m, "public", nil)
	path := "/api/scientists?q=" + m + "&sort=name&limit=5"

	first := a.do(nil, "GET", path, nil)
	if first.Code != http.StatusOK || first.json(t)["total"] != float64(1) {
		t.Fatalf("first: %d %s", first.Code, first.Raw)
	}
	// Новый публичный профиль гость до конца срока не видит, вошедший и сотрудник видят сразу.
	a.listed("Борис Орлов", m, "public", nil)
	if again := a.do(nil, "GET", path, nil); again.Raw != first.Raw {
		t.Errorf("within the ttl a guest gets the remembered answer")
	}
	if reordered := a.do(nil, "GET", "/api/scientists?limit=5&sort=name&q="+m, nil); reordered.Raw != first.Raw {
		t.Errorf("parameters in another order are the same query")
	}
	viewer := a.user("Зритель")
	if r := a.do(&viewer, "GET", path, nil); r.json(t)["total"] != float64(2) {
		t.Errorf("a signed-in person is not served from the guest cache: %s", r.Raw)
	}
	staff := a.staff("Сотрудник")
	if r := a.do(&staff, "GET", path, nil); r.json(t)["total"] != float64(2) {
		t.Errorf("a staff member is not served from the guest cache: %s", r.Raw)
	}

	now = now.Add(16 * time.Second)
	if later := a.do(nil, "GET", path, nil); later.json(t)["total"] != float64(2) {
		t.Errorf("after the ttl a guest sees the new profile: %s", later.Raw)
	}
	if s := cache.Stats(); s.Hits != 2 || s.Misses != 2 {
		t.Errorf("stats: %+v (signed-in requests must not touch the cache)", s)
	}
	// Неверные параметры: 422, в кеш не попадают.
	before := cache.Stats().Entries
	if bad := a.do(nil, "GET", "/api/scientists?sort=best", nil); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad parameter: %d", bad.Code)
	}
	if cache.Stats().Entries != before {
		t.Errorf("a rejected query must not occupy the cache")
	}
}

func TestHTTPCatalogCacheDoesNotRememberFailures(t *testing.T) {
	w := newWorld(t)
	svc, _ := w.faulty(1)
	cache := httpcache.NewKeyed(time.Minute, 10)
	authH := auth.NewHandler(w.auth, w.log)
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		NewHandler(svc, w.log, authH.RequireUser).WithCatalogCache(cache).Mount(r)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/scientists", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "injected") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if n := cache.Stats().Entries; n != 0 {
		t.Errorf("a failure is remembered: %d entries", n)
	}
}

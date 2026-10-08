package notifications

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/testkit"
)

func newAPI(t *testing.T) (*testkit.API, *Service) {
	w := testkit.NewWorld(t)
	svc := newSvc()
	return testkit.NewAPI(w, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(svc, w.Log, requireUser).Mount(r)
	}), svc
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a, _ := newAPI(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/notifications"}, {"GET", "/api/notifications/unread-count"},
		{"POST", "/api/notifications/read-all"}, {"POST", "/api/notifications/" + uuid.NewString() + "/read"},
	} {
		if r := a.Do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
}

func TestHTTPListReadAndCounts(t *testing.T) {
	a, svc := newAPI(t)
	me, other := a.User("Я"), a.User("Другой")
	for i := range 3 {
		emit(t, svc, Notice{UserID: me.ID, Kind: "application_received", Title: fmt.Sprintf("Событие %d", i), Link: "/candidates/1"})
	}
	emit(t, svc, Notice{UserID: other.ID, Kind: "k", Title: "чужое"})

	r := a.Do(&me, "GET", "/api/notifications?limit=2", nil)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	j := r.JSON(t)
	items := j["items"].([]any)
	if len(items) != 2 || j["total"].(float64) != 3 || j["unread"].(float64) != 3 {
		t.Errorf("list = %s", r.Raw)
	}
	first := items[0].(map[string]any)
	if first["title"] != "Событие 2" || first["read"] != false || first["link"] != "/candidates/1" {
		t.Errorf("first = %v", first)
	}
	if got := a.Do(&me, "GET", "/api/notifications/unread-count", nil).JSON(t)["unread"]; got != float64(3) {
		t.Errorf("unread-count = %v", got)
	}

	id := first["id"].(string)
	if r := a.Do(&other, "POST", "/api/notifications/"+id+"/read", nil); r.Code != 404 || r.ErrCode(t) != "not_found" {
		t.Errorf("foreign read: %d %s", r.Code, r.Raw)
	}
	if r := a.Do(&me, "POST", "/api/notifications/"+id+"/read", nil); r.Code != 204 {
		t.Errorf("read: %d %s", r.Code, r.Raw)
	}
	if got := a.Do(&me, "GET", "/api/notifications/unread-count", nil).JSON(t)["unread"]; got != float64(2) {
		t.Errorf("after read: %v", got)
	}
	only := a.Do(&me, "GET", "/api/notifications?unread=1", nil).JSON(t)["items"].([]any)
	if len(only) != 2 {
		t.Errorf("unread only = %d items", len(only))
	}
	if r := a.Do(&me, "POST", "/api/notifications/not-a-number/read", nil); r.Code != 404 {
		t.Errorf("bad id: %d", r.Code)
	}
	if r := a.Do(&me, "POST", "/api/notifications/read-all", nil); r.Code != 204 {
		t.Errorf("read-all: %d %s", r.Code, r.Raw)
	}
	if got := a.Do(&me, "GET", "/api/notifications/unread-count", nil).JSON(t)["unread"]; got != float64(0) {
		t.Errorf("after read-all: %v", got)
	}
	if got := a.Do(&other, "GET", "/api/notifications/unread-count", nil).JSON(t)["unread"]; got != float64(1) {
		t.Errorf("read-all touched another person: %v", got)
	}
	// Нечисловые параметры страницы не ломают список.
	if r := a.Do(&me, "GET", "/api/notifications?limit=abc&offset=x", nil); r.Code != 200 {
		t.Errorf("bad paging: %d", r.Code)
	}
	if r := a.Do(&me, "GET", "/api/notifications?offset=2147483648", nil); r.Code != 200 || len(r.JSON(t)["items"].([]any)) != 0 {
		t.Errorf("huge offset: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPInternalErrorsAreHidden(t *testing.T) {
	w := testkit.NewWorld(t)
	me := w.User("Я")
	emit(t, newSvc(), Notice{UserID: me.ID, Kind: "k", Title: "t"})
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/notifications"},
		{"GET", "/api/notifications/unread-count"},
		{"POST", "/api/notifications/" + id + "/read"},
		{"POST", "/api/notifications/read-all"},
	} {
		db, _ := testkit.Faulty(testkit.Pool, 1)
		svc := newService(db, Config{ProductName: "SciBox", PublicURL: "http://x"})
		a := testkit.NewAPI(w, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
			NewHandler(svc, w.Log, requireUser).Mount(r)
		})
		r := a.Do(&me, c.method, c.path, map[string]any{})
		if r.Code != 500 || r.ErrCode(t) != "internal" || len(r.Raw) > 300 {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
}

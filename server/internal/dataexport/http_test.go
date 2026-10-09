package dataexport_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/dataexport"
	"scibox/server/internal/testkit"
)

func newAPI(w *world, svc *dataexport.Service) *testkit.API {
	return testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		dataexport.NewHandler(svc, w.Log, requireUser).Mount(r)
	})
}

// session — свежий сеанс человека (вход по паролю): его токен годится для запросов.
func session(t *testing.T, w *world, p testkit.Person) testkit.Person {
	t.Helper()
	_, s, err := w.Auth.Login(bg, p.Email, testkit.Password, testkit.Meta())
	if err != nil {
		t.Fatal(err)
	}
	p.Token = s.Token
	return p
}

func TestHTTPExport(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	api := newAPI(w, w.service())
	const path = "/api/account/export"

	if r := api.Do(nil, "GET", path, nil); r.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d, want 401", r.Code)
	}
	// Сеанс из ссылки в письме не свежий: окно пароля.
	if r := api.Do(&b.other, "GET", path, nil); r.Code != http.StatusForbidden || r.ErrCode(t) != "reauth_required" {
		t.Errorf("stale session: %d %s", r.Code, r.Raw)
	}

	me := session(t, w, b.me)
	r := api.Do(&me, "GET", path, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("fresh session: %d %s", r.Code, r.Raw)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="scibox-data-`) || !strings.HasSuffix(cd, `.zip"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	a := open(t, []byte(r.Raw))
	if !strings.Contains(string(a.entries["data.json"]), b.me.Email) || strings.Contains(a.all(), b.other.Email) {
		t.Error("the archive must hold my data and not the data of another person")
	}

	// Четвёртая выгрузка за сутки (одна уже была через Prepare в build? нет: счёт идёт от этих запросов).
	for i := 2; i <= 3; i++ {
		if r := api.Do(&me, "GET", path, nil); r.Code != http.StatusOK {
			t.Fatalf("export %d: %d %s", i, r.Code, r.Raw)
		}
	}
	r = api.Do(&me, "GET", path, nil)
	secs, _ := strconv.Atoi(r.Header.Get("Retry-After"))
	if r.Code != http.StatusTooManyRequests || r.ErrCode(t) != "rate_limited" || secs < 3600 || secs > 24*3600 {
		t.Errorf("4th export: %d %s Retry-After=%q", r.Code, r.Raw, r.Header.Get("Retry-After"))
	}
	if strings.Contains(r.Raw, "PK") {
		t.Error("a refused request must not send a partial archive")
	}
}

func TestHTTPExportWithABrokenDatabaseNeverPassesForASuccess(t *testing.T) {
	w := newWorld(t)
	b := build(t, w)
	me := session(t, w, b.me)
	aborted := 0
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		return func(db testkit.DB) (err error) {
			svc := dataexport.NewService(db, w.Vault, dataexport.Config{ProductName: "SciBox", Export: dataexport.Limit{Max: 1000, Window: time.Hour}})
			defer func() {
				if rec := recover(); rec != nil {
					if !errors.Is(rec.(error), http.ErrAbortHandler) {
						panic(rec)
					}
					aborted++
					err = testkit.ErrFault
				}
			}()
			r := newAPI(w, svc).Do(&me, "GET", "/api/account/export", nil)
			if r.Code == http.StatusInternalServerError {
				if strings.Contains(r.Raw, "injected") {
					t.Errorf("the 500 answer shows the internals: %s", r.Raw)
				}
				return testkit.ErrFault
			}
			if r.Code != http.StatusOK {
				return errors.New(r.Raw)
			}
			return nil
		}
	})
	if aborted == 0 {
		t.Error("a failure while the files are being sent must abort the answer, but no case did")
	}
}

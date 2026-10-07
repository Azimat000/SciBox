package landing

import (
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

	"scibox/server/internal/privacy"
	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

var bg = testkit.BG

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clean убирает следы прошлых тестов: числа считаются по всей базе, а тест должен видеть только своё.
func clean(t *testing.T) {
	t.Helper()
	for _, q := range []string{`UPDATE vacancies SET status = 'archived'`, `UPDATE profiles SET visibility = 'hidden'`} {
		if _, err := testkit.Pool.Exec(bg, q); err != nil {
			t.Fatal(err)
		}
	}
}

func exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := testkit.Pool.Exec(bg, q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func stats(t *testing.T) Stats {
	t.Helper()
	s, err := NewService(testkit.Pool).Stats(bg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEmptyBase(t *testing.T) {
	clean(t)
	s := stats(t)
	if s.OpenVacancies != 0 || s.HiringOrganizations != 0 || s.Scientists != 0 {
		t.Fatalf("expected zeros, got %+v", s)
	}
	if s.Fields == nil || s.Types == nil {
		t.Fatal("lists must be empty, not nil: the site reads them as arrays")
	}
}

// Что считается открытой вакансией: опубликованная, срок не прошёл (или срока нет).
func TestOpenVacancies(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	tm := w.Team()
	other := w.Team()

	tm.Published(nil)
	tm.Published(nil) // вторая открытая в той же организации
	noDeadline := tm.Published(&tm.UnitA.ID)
	exec(t, `UPDATE vacancies SET deadline = NULL WHERE id = $1`, noDeadline.ID)
	other.Published(nil) // открытая в другой организации

	if _, err := w.Vac.Create(bg, tm.Owner.User, tm.Slug, testkit.VacancyInput(nil)); err != nil { // черновик
		t.Fatal(err)
	}
	closed := tm.Published(nil)
	tm.Status(closed.ID, "closed")
	archived := tm.Published(nil)
	tm.Status(archived.ID, "closed")
	tm.Status(archived.ID, "archived")
	expired := other.Published(nil)
	exec(t, `UPDATE vacancies SET deadline = '2020-01-01' WHERE id = $1`, expired.ID)

	s := stats(t)
	if s.OpenVacancies != 4 {
		t.Errorf("open vacancies = %d, want 4 (two + no deadline in one org, one in the other)", s.OpenVacancies)
	}
	if s.HiringOrganizations != 2 {
		t.Errorf("hiring organizations = %d, want 2", s.HiringOrganizations)
	}
}

// Организация, у которой остались только закрытые и просроченные вакансии, никого не ищет.
func TestOrganizationWithoutOpenVacanciesIsNotHiring(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	tm := w.Team()
	v := tm.Published(nil)
	exec(t, `UPDATE vacancies SET deadline = '2020-01-01' WHERE id = $1`, v.ID)
	tm.Status(tm.Published(nil).ID, "closed")
	if s := stats(t); s.HiringOrganizations != 0 || s.OpenVacancies != 0 {
		t.Fatalf("got %+v", s)
	}
}

func TestByFieldAndType(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	tm := w.Team()

	a := tm.Published(nil) // 1.4.4
	b := tm.Published(nil)
	exec(t, `INSERT INTO vacancy_specialties (vacancy_id, specialty_code) VALUES ($1, '1.4.3')`, b.ID) // две специальности одной области
	c := tm.Published(nil)
	exec(t, `INSERT INTO vacancy_specialties (vacancy_id, specialty_code) VALUES ($1, '2.1.1')`, c.ID) // две области
	d := tm.Published(nil)
	exec(t, `DELETE FROM vacancy_specialties WHERE vacancy_id = $1`, d.ID)
	exec(t, `INSERT INTO vacancy_specialties (vacancy_id, specialty_code) VALUES ($1, '5.1.1')`, d.ID)
	e := tm.Published(nil) // 1.4.4, но уже в архиве
	tm.Status(e.ID, "closed")
	tm.Status(e.ID, "archived")
	exec(t, `UPDATE vacancies SET position_code = 'phd_student' WHERE id = $1`, a.ID)
	exec(t, `UPDATE vacancies SET position_code = 'professor' WHERE id IN ($1, $2)`, b.ID, c.ID)
	exec(t, `UPDATE vacancies SET position_code = 'dean' WHERE id = $1`, d.ID)

	s := stats(t)
	wantFields := []FieldCount{{"1", 3}, {"2", 1}, {"5", 1}}
	if len(s.Fields) != len(wantFields) {
		t.Fatalf("fields = %+v, want %+v", s.Fields, wantFields)
	}
	for i, f := range wantFields {
		if s.Fields[i] != f {
			t.Errorf("fields[%d] = %+v, want %+v", i, s.Fields[i], f)
		}
	}
	// Виды идут в порядке справочника должностей.
	wantTypes := []TypeCount{{"teaching", 2}, {"admin", 1}, {"phd", 1}}
	if len(s.Types) != len(wantTypes) {
		t.Fatalf("types = %+v, want %+v", s.Types, wantTypes)
	}
	for i, ty := range wantTypes {
		if s.Types[i] != ty {
			t.Errorf("types[%d] = %+v, want %+v", i, s.Types[i], ty)
		}
	}
}

// Гость видит в каталоге только публичные профили с заполненной должностью: столько же показывает и главная.
func TestScientistsMatchWhatAGuestSeesInTheCatalog(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	set := func(p testkit.Person, visibility string) {
		if _, err := w.Prof.SetPrivacy(bg, p.User, visibility, false); err != nil {
			t.Fatal(err)
		}
	}
	pub1, pub2 := w.Applicant("Первый"), w.Applicant("Второй")
	set(pub1, string(privacy.Public))
	set(pub2, string(privacy.Public))
	forOrgs := w.Applicant("Организациям")
	set(forOrgs, string(privacy.Orgs))
	hidden := w.Applicant("Скрытый")
	set(hidden, string(privacy.Hidden))
	// Публичный, но без должности: в каталоге у него пустая карточка, поэтому его там нет.
	empty := w.User("Пустой")
	if _, err := w.Prof.Own(bg, empty.User); err != nil {
		t.Fatal(err)
	}
	set(empty, string(privacy.Public))

	s := stats(t)
	if s.Scientists != 2 {
		t.Fatalf("scientists = %d, want 2", s.Scientists)
	}
	// Сверка с настоящим каталогом гостя.
	page, err := w.Prof.Catalog(bg, profiles.CatalogParams{Limit: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if int64(page.Total) != s.Scientists {
		t.Errorf("catalog total %d, landing %d", page.Total, s.Scientists)
	}
}

// Срок подачи считается по московскому дню: ночью по Москве ещё идёт вчерашний день по UTC.
func TestDeadlineUsesMoscowDay(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	tm := w.Team()
	today := tm.Published(nil)
	yesterday := tm.Published(nil)
	exec(t, `UPDATE vacancies SET deadline = '2026-10-11' WHERE id = $1`, today.ID)
	exec(t, `UPDATE vacancies SET deadline = '2026-10-10' WHERE id = $1`, yesterday.ID)

	svc := NewService(testkit.Pool)
	// 22:30 по UTC 10 октября = 01:30 по Москве 11 октября.
	svc.now = func() time.Time { return time.Date(2026, 10, 10, 22, 30, 0, 0, time.UTC) }
	s, err := svc.Stats(bg)
	if err != nil {
		t.Fatal(err)
	}
	if s.OpenVacancies != 1 {
		t.Fatalf("open = %d, want 1: 10 October is over in Moscow, 11 October is the last day", s.OpenVacancies)
	}
	// Утром того же дня по UTC (09:00) в Москве 12:00 11 октября: последний день ещё считается.
	svc.now = func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	if s, _ = svc.Stats(bg); s.OpenVacancies != 1 {
		t.Fatalf("on the last day open = %d, want 1", s.OpenVacancies)
	}
	// 21:00 UTC = полночь по Москве: срок вышел.
	svc.now = func() time.Time { return time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC) }
	if s, _ = svc.Stats(bg); s.OpenVacancies != 0 {
		t.Fatalf("after the last day open = %d, want 0", s.OpenVacancies)
	}
}

func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	for n := int32(1); n <= 3; n++ {
		db, calls := testkit.Faulty(testkit.Pool, n)
		_, err := NewService(db).Stats(bg)
		if !errors.Is(err, testkit.ErrFault) {
			t.Errorf("failure at call %d: got %v", n, err)
		}
		if calls.Load() != n {
			t.Errorf("failure at call %d: made %d calls", n, calls.Load())
		}
	}
}

func router(svc *Service) http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) { NewHandler(svc, silent()).Mount(r) })
	return r
}

func TestHTTP(t *testing.T) {
	clean(t)
	w := testkit.NewWorld(t)
	w.Team().Published(nil)

	rec := httptest.NewRecorder()
	router(NewService(testkit.Pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/landing", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"open_vacancies", "hiring_organizations", "scientists", "fields", "types"} {
		if _, ok := got[key]; !ok {
			t.Errorf("answer has no %q: %s", key, rec.Body)
		}
	}
	if got["open_vacancies"].(float64) != 1 {
		t.Errorf("open_vacancies = %v", got["open_vacancies"])
	}
	// Только числа и коды: никаких названий, имён, почт.
	for _, leak := range []string{"@", "Институт", "Старший"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("answer leaks %q: %s", leak, rec.Body)
		}
	}
}

func TestHTTPHidesDatabaseFailure(t *testing.T) {
	db, _ := testkit.Faulty(testkit.Pool, 1)
	rec := httptest.NewRecorder()
	router(NewService(db)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/landing", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "injected") || !strings.Contains(rec.Body.String(), `"internal"`) {
		t.Errorf("body %s", rec.Body)
	}
}

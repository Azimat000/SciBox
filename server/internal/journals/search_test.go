package journals

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/testkit"
)

func titles(fs []Found) string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Title
	}
	return strings.Join(out, " | ")
}

func TestSearch(t *testing.T) {
	loadFixture(t, fixture)
	svc := NewService(testkit.Pool)
	run := func(q string, limit int) []Found {
		t.Helper()
		got, err := svc.Search(testkit.BG, q, limit)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Точное название первым, потом начинающиеся с запроса, потом по SJR.
	if got := titles(run("physics", 0)); got != "Physics | Physics of the Solid State | Journal of Physics: Condensed Matter | 100% Physics_Journal" {
		t.Fatalf("physics = %s", got)
	}
	// Все слова должны встретиться, порядок слов не важен, регистр тоже.
	if got := titles(run("SOLID physics", 0)); got != "Physics of the Solid State" {
		t.Fatalf("two words = %s", got)
	}
	if got := titles(run("physics chemistry", 0)); got != "" {
		t.Fatalf("missing word = %s", got)
	}
	// Знаки шаблона ищутся как обычные буквы.
	if got := titles(run("100%", 0)); got != "100% Physics_Journal" {
		t.Fatalf("percent = %s", got)
	}
	if got := titles(run("Physic_", 0)); got != "" {
		t.Fatalf("underscore must not be a wildcard: %s", got)
	}
	// Слишком короткий запрос ничего не ищет.
	if got := run(" N ", 0); len(got) != 0 {
		t.Fatalf("short = %s", titles(got))
	}
	// Предел выдачи.
	if got := run("physic", 2); len(got) != 2 {
		t.Fatalf("limit 2 = %d", len(got))
	}
	if got := run("physic", 1000); len(got) != 5 {
		t.Fatalf("limit is capped, not refused: %d", len(got))
	}
	// Длинный запрос обрезается, а не ломает поиск.
	if got := run(strings.Repeat("я", MaxQuery+50), 0); len(got) != 0 {
		t.Fatalf("long = %s", titles(got))
	}
	// Слов больше восьми: учитываются первые восемь.
	if got := titles(run("physics physics physics physics physics physics physics physics nothing-like-this", 0)); !strings.Contains(got, "Physics of the Solid State") {
		t.Fatalf("many words = %s", got)
	}
}

func TestSearchByISSN(t *testing.T) {
	loadFixture(t, fixture)
	svc := NewService(testkit.Pool)
	got, err := svc.Search(testkit.BG, "14764687", 0) // второй ISSN Nature, без дефиса
	if err != nil || len(got) != 1 {
		t.Fatalf("by issn = %v, %v", got, err)
	}
	n := got[0]
	if n.Title != "Nature" || n.ISSN != "1476-4687" || strings.Join(n.ISSNs, ",") != "0028-0836,1476-4687" || n.Quartile == nil || *n.Quartile != 1 || n.Year != 2025 || n.Publisher != "Nature Research" {
		t.Fatalf("Nature = %+v", n)
	}
	// По словам ставится первый ISSN журнала; квартиля может не быть.
	got, _ = svc.Search(testkit.BG, "Physics", 1)
	if got[0].ISSN != "2041-1723" || got[0].Quartile != nil {
		t.Fatalf("physics = %+v", got[0])
	}
	// Журнал без ISSN (в справочник его не кладёт загрузка, но база его допускает) не находится: его не связать.
	if _, err := testkit.Pool.Exec(testkit.BG, `INSERT INTO journals (id, title, data_year) VALUES (999, 'Orphan Physics', 2025)`); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Search(testkit.BG, "orphan", 0); len(got) != 0 {
		t.Fatalf("journal without issn = %v", got)
	}
	if got, _ := svc.Search(testkit.BG, "1234-5679", 0); len(got) != 0 {
		t.Fatalf("unknown issn = %v", got)
	}
}

func TestSearchNeverSwallowsADatabaseFailure(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		return func(db testkit.DB) error {
			_, err := NewService(db).Search(testkit.BG, "physics", 0)
			return err
		}
	})
}

func TestHTTP(t *testing.T) {
	loadFixture(t, fixture)
	w := testkit.NewWorld(t)
	mount := func(db testkit.DB) *testkit.API {
		return testkit.NewAPI(w, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
			NewHandler(NewService(db), w.Log, requireUser).Mount(r)
		})
	}
	api := mount(testkit.Pool)
	if res := api.Do(nil, http.MethodGet, "/api/journals?q=nature", nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("visitor = %d", res.Code)
	}
	me := w.User("Учёный")
	res := api.Do(&me, http.MethodGet, "/api/journals?q=nature&limit=5", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Raw, `"issn":"0028-0836"`) || !strings.Contains(res.Raw, `"quartile":1`) || !strings.Contains(res.Raw, `"year":2025`) {
		t.Fatalf("search = %d %s", res.Code, res.Raw)
	}
	if res := api.Do(&me, http.MethodGet, "/api/journals?q=x", nil); res.Code != http.StatusOK || res.Raw != "{\"items\":[]}\n" {
		t.Fatalf("short = %d %q", res.Code, res.Raw)
	}
	// Сбой на первом обращении самого поиска (сессию проверяет сервис аккаунтов по настоящей базе).
	broken, _ := testkit.Faulty(testkit.Pool, 1)
	brokenAPI := mount(broken)
	if res := brokenAPI.Do(&me, http.MethodGet, "/api/journals?q=nature", nil); res.Code != http.StatusInternalServerError || strings.Contains(res.Raw, "injected") {
		t.Fatalf("failure = %d %s", res.Code, res.Raw)
	}
}

package skills_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/skills"
	"scibox/server/internal/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.RunMain(m)) }

var day = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

// fixture — маленький справочник для тестов с базой.
const head = "kind;group;name;aliases\n"

const fixture = head +
	"research;Методы;ПЦР;Полимеразная цепная реакция|PCR\n" +
	"research;Методы;ПЦР в реальном времени;qPCR\n" +
	"research;Спектроскопия;Масс-спектрометрия;MS\n" +
	"research;Спектроскопия;ЯМР-спектроскопия;ЯМР|NMR\n" +
	"research;Микроскопия;Электронная микроскопия;ПЭМ\n" +
	"research;Программы;Python;Питон\n" +
	"research;Программы;R;\n" +
	"research;Программы;C++;\n" +
	"research;Программы;Chromeleon;\n" +
	"research;Методы;Хроматография;\n" +
	"general;Офис;Microsoft Excel;Excel|Эксель\n" +
	"general;Языки;Английский язык;Английский|English\n" +
	"general;Управление;Бухгалтерский учёт;Бухучёт\n" +
	"general;Компьютер;Python;\n"

func loadFixture(t *testing.T, csv string) skills.LoadResult {
	t.Helper()
	res, err := skills.Load(testkit.BG, testkit.Pool, skills.Source{Data: []byte(csv), Compiled: day}, true)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return res
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func names(r skills.SearchResult) []string {
	out := make([]string, 0, len(r.Items))
	for _, f := range r.Items {
		out = append(out, f.Name)
	}
	return out
}

func TestLoadReplacesTheDictionaryOnce(t *testing.T) {
	res := loadFixture(t, fixture)
	if res.Skills != 14 || res.Research != 9+1 || res.General != 4 || res.Terms != 14+13 || res.Unchanged || !strings.HasPrefix(res.Edition, "навыков: 14 · ") {
		t.Fatalf("first load = %+v", res)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skills`); n != 14 {
		t.Fatalf("skills = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skill_terms`); n != res.Terms {
		t.Fatalf("terms = %d, want %d", n, res.Terms)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skill_terms WHERE is_name`); n != 14 {
		t.Fatalf("names = %d", n)
	}
	// Ключ и слова хранятся так, как их считает код.
	var key, words string
	if err := testkit.Pool.QueryRow(testkit.BG, `SELECT key, words FROM skill_terms WHERE kind = 'research' AND term = 'Масс-спектрометрия'`).Scan(&key, &words); err != nil {
		t.Fatal(err)
	}
	if key != "масс-спектрометрия" || words != "масс спектрометрия" {
		t.Fatalf("key/words = %q %q", key, words)
	}
	var title, url, edition string
	var checked time.Time
	if err := testkit.Pool.QueryRow(testkit.BG, `SELECT title, url, edition, checked_on FROM reference_sources WHERE catalog = 'skills'`).Scan(&title, &url, &edition, &checked); err != nil {
		t.Fatal(err)
	}
	if title != skills.SourceTitle || url != skills.SourceURL || edition != res.Edition || !checked.Equal(day) {
		t.Fatalf("source = %q %q %q %v", title, url, edition, checked)
	}

	// Тот же файл (и сжатым) второй раз базу не трогает.
	again, err := skills.Load(testkit.BG, testkit.Pool, skills.Source{Data: gzipped(t, fixture), Compiled: day}, false)
	if err != nil || !again.Unchanged || again.Edition != res.Edition {
		t.Fatalf("same file again = %+v, %v", again, err)
	}
	// Другой файл заменяет справочник целиком.
	next := head + "research;Методы;Одно;\n"
	res2, err := skills.Load(testkit.BG, testkit.Pool, skills.Source{Data: []byte(next), Compiled: day.AddDate(1, 0, 0)}, false)
	if err != nil || res2.Unchanged || res2.Edition == res.Edition {
		t.Fatalf("new file = %+v, %v", res2, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skills`); n != 1 {
		t.Fatalf("after replace skills = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skill_terms`); n != 1 {
		t.Fatalf("after replace terms = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM reference_sources WHERE catalog = 'skills'`); n != 1 {
		t.Fatal("one source row")
	}
}

func TestLoadTheEmbeddedFile(t *testing.T) {
	res, err := skills.Load(testkit.BG, testkit.Pool, skills.Embedded(), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skills < 450 || res.Terms <= res.Skills {
		t.Fatalf("embedded = %+v", res)
	}
	got, err := skills.NewService(testkit.Pool).Search(testkit.BG, skills.KindResearch, "пцр", 0)
	if err != nil || len(got.Items) == 0 || got.Items[0].Name != "ПЦР" {
		t.Fatalf("search the embedded = %+v, %v", got, err)
	}
	again, err := skills.Load(testkit.BG, testkit.Pool, skills.Embedded(), false)
	if err != nil || !again.Unchanged {
		t.Fatalf("embedded again = %+v, %v", again, err)
	}
}

func TestLoadRefusesABrokenFile(t *testing.T) {
	loadFixture(t, fixture)
	for name, data := range map[string][]byte{
		"not csv":      []byte("hello"),
		"broken gzip":  {0x1f, 0x8b, 1, 2, 3},
		"bad row":      []byte(head + "science;Методы;ПЦР;\n"),
		"no skills":    []byte(head),
		"gzip of junk": gzipped(t, "hello"),
	} {
		if _, err := skills.Load(testkit.BG, testkit.Pool, skills.Source{Data: data, Compiled: day}, true); !errors.Is(err, skills.ErrFormat) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM skills`); n != 14 {
		t.Fatalf("a refused file must leave the dictionary alone: %d", n)
	}
}

// COPY, который база не приняла, откатывает всю замену.
func TestStoreRollsBackOnCopyFailure(t *testing.T) {
	loadFixture(t, fixture)
	bad := []skills.Parsed{
		{Skills: []skills.Skill{{Kind: skills.KindResearch, Group: "Г", Name: "", Line: 2}}},                                             // CHECK на название
		{Skills: []skills.Skill{{Kind: skills.KindResearch, Group: "Г", Name: "A"}, {Kind: skills.KindResearch, Group: "Г", Name: "a"}}}, // ключ дважды
	}
	for i, p := range bad {
		if err := skills.StoreForTest(testkit.BG, testkit.Pool, p, "bad", day); err == nil {
			t.Fatalf("case %d: store must fail", i)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM skills`); n != 14 {
			t.Fatalf("case %d: skills after a failed store = %d", i, n)
		}
	}
}

func TestLoadNeverSwallowsADatabaseFailure(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		_, _ = testkit.Pool.Exec(testkit.BG, `DELETE FROM reference_sources WHERE catalog = 'skills'`)
		return func(db testkit.DB) error {
			_, err := skills.Load(testkit.BG, db, skills.Source{Data: []byte(fixture), Compiled: day}, false)
			return err
		}
	})
}

func TestSearch(t *testing.T) {
	loadFixture(t, fixture)
	svc := skills.NewService(testkit.Pool)
	search := func(kind, q string, limit int) skills.SearchResult {
		t.Helper()
		got, err := svc.Search(testkit.BG, kind, q, limit)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if got.Items == nil {
			t.Fatalf("search %q: items must be an empty list, not null", q)
		}
		return got
	}
	for _, c := range []struct {
		name  string
		kind  string
		q     string
		want  []string // имена в порядке выдачи
		via   string   // синоним первого результата
		fuzzy bool
	}{
		{"exact name first, longer name after", skills.KindResearch, "пцр", []string{"ПЦР", "ПЦР в реальном времени"}, "", false},
		{"case does not matter", skills.KindResearch, "ПцР", []string{"ПЦР", "ПЦР в реальном времени"}, "", false},
		{"exact synonym", skills.KindGeneral, "excel", []string{"Microsoft Excel"}, "Excel", false},
		{"Latin synonym", skills.KindResearch, "PCR", []string{"ПЦР"}, "PCR", false},
		{"start of the name", skills.KindResearch, "масс-спе", []string{"Масс-спектрометрия"}, "", false},
		{"start of the second word", skills.KindResearch, "спектрометр", []string{"Масс-спектрометрия"}, "", false},
		{"start of a word of a synonym", skills.KindResearch, "цепная", []string{"ПЦР"}, "Полимеразная цепная реакция", false},
		{"every word of the query must match", skills.KindResearch, "цепная реак", []string{"ПЦР"}, "Полимеразная цепная реакция", false},
		{"word form", skills.KindResearch, "хроматографии", []string{"Хроматография"}, "", false},
		{"word form of two words", skills.KindResearch, "электронной микроскопии", []string{"Электронная микроскопия"}, "", false},
		{"yo is ye", skills.KindGeneral, "учет", []string{"Бухгалтерский учёт"}, "", false},
		{"ye matches yo", skills.KindGeneral, "бухучет", []string{"Бухгалтерский учёт"}, "Бухучёт", false},
		{"Latin letter", skills.KindResearch, "c++", []string{"C++"}, "", false},
		{"one letter: exact first", skills.KindResearch, "r", []string{"R"}, "", false},
		{"the kind separates dictionaries", skills.KindGeneral, "пцр", []string{}, "", false},
		{"nothing", skills.KindResearch, "кристаллография", []string{}, "", false},
		{"typo", skills.KindResearch, "pyton", []string{"Python"}, "", true},
		{"typo in a long word", skills.KindResearch, "хромотография", []string{"Хроматография"}, "", true},
		{"too short for typos", skills.KindResearch, "пцз", []string{}, "", false},
		{"punctuation only", skills.KindResearch, "!!", []string{}, "", false},
		{"percent is a letter", skills.KindResearch, "%", []string{}, "", false},
		{"underscore is a letter", skills.KindResearch, "_", []string{}, "", false},
		{"tsquery operators do not break the search", skills.KindResearch, "пцр & (x | !y) :*", nil, "", false},
		{"quotes and backslash", skills.KindResearch, `' OR 1=1 -- \`, []string{}, "", false},
		{"empty", skills.KindResearch, "   ", []string{}, "", false},
		{"nul and bad utf8", skills.KindResearch, "пцр\x00\xff", []string{"ПЦР", "ПЦР в реальном времени"}, "", false},
	} {
		got := search(c.kind, c.q, 0)
		if c.want == nil {
			continue
		}
		if !slices.Equal(names(got), c.want) {
			t.Errorf("%s: %q → %v, want %v", c.name, c.q, names(got), c.want)
			continue
		}
		if got.Fuzzy != c.fuzzy {
			t.Errorf("%s: fuzzy = %v", c.name, got.Fuzzy)
		}
		if c.via != "" && got.Items[0].Via != c.via {
			t.Errorf("%s: via = %q, want %q", c.name, got.Items[0].Via, c.via)
		}
		if c.via == "" && len(got.Items) > 0 && got.Items[0].Via != "" && !c.fuzzy {
			t.Errorf("%s: via = %q, want empty", c.name, got.Items[0].Via)
		}
	}
	// Группа и предел.
	if got := search(skills.KindResearch, "питон", 0); len(got.Items) != 1 || got.Items[0].Group != "Программы" || got.Items[0].Name != "Python" || got.Items[0].Via != "Питон" {
		t.Fatalf("group = %+v", got)
	}
	if got := search(skills.KindResearch, "п", 1); len(got.Items) != 1 {
		t.Fatalf("limit = %+v", got)
	}
	if got := search(skills.KindResearch, "п", 500); len(got.Items) > skills.MaxLimitForTest {
		t.Fatalf("limit above the maximum: %d", len(got.Items))
	}
	// Одно имя в двух видах: каждый вид находит своё.
	if got := search(skills.KindGeneral, "python", 0); len(got.Items) != 1 || got.Items[0].Group != "Компьютер" {
		t.Fatalf("general python = %+v", got)
	}
	// Неизвестный вид.
	if _, err := svc.Search(testkit.BG, "other", "пцр", 0); !errors.Is(err, skills.ErrKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	// Слишком длинный запрос не ломает поиск.
	if _, err := svc.Search(testkit.BG, skills.KindResearch, strings.Repeat("пцр ", 500), 0); err != nil {
		t.Fatalf("long query: %v", err)
	}
}

// Навык, у которого совпали и название, и синоним, показывается один раз.
func TestSearchShowsASkillOnce(t *testing.T) {
	loadFixture(t, head+"research;Г;Масс-спектрометрия;Масс спектрометрия|MS\n")
	got, err := skills.NewService(testkit.Pool).Search(testkit.BG, skills.KindResearch, "масс", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "Масс-спектрометрия" || got.Items[0].Via != "" {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestSearchNeverSwallowsADatabaseFailure(t *testing.T) {
	loadFixture(t, fixture)
	for _, q := range []string{"пцр", "pyton"} { // второй запрос доходит до поиска по похожим написаниям
		testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
			return func(db testkit.DB) error {
				_, err := skills.NewService(db).Search(testkit.BG, skills.KindResearch, q, 0)
				return err
			}
		})
	}
}

func TestCanonical(t *testing.T) {
	loadFixture(t, fixture)
	canon := func(kind string, in ...string) []string {
		t.Helper()
		got, err := skills.Canonical(testkit.BG, testQueries(), kind, in)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, c := range []struct {
		name string
		kind string
		in   []string
		want []string
	}{
		{"synonym becomes the name", skills.KindGeneral, []string{"excel"}, []string{"Microsoft Excel"}},
		{"case and yo", skills.KindGeneral, []string{"БУХГАЛТЕРСКИЙ УЧЕТ"}, []string{"Бухгалтерский учёт"}},
		{"two synonyms are one skill", skills.KindGeneral, []string{"Эксель", "Excel", "microsoft excel"}, []string{"Microsoft Excel"}},
		{"order and own skills stay", skills.KindResearch, []string{"Мой метод", "pcr", "Ещё метод"}, []string{"Мой метод", "ПЦР", "Ещё метод"}},
		{"the kind matters", skills.KindResearch, []string{"excel"}, []string{"excel"}},
		{"unknown stays as typed", skills.KindResearch, []string{"Криоэлектронная томография"}, []string{"Криоэлектронная томография"}},
		{"empty", skills.KindResearch, []string{}, []string{}},
	} {
		if got := canon(c.kind, c.in...); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v → %v, want %v", c.name, c.in, got, c.want)
		}
	}
	// Неизвестный вид список не меняет.
	if got := canon("other", "excel"); !slices.Equal(got, []string{"excel"}) {
		t.Errorf("unknown kind: %v", got)
	}
	// Пустой справочник: список не меняется.
	loadFixture(t, head+"research;Г;Другое;\n")
	if got := canon(skills.KindGeneral, "excel", "Excel", "Мой"); !slices.Equal(got, []string{"excel", "Мой"}) {
		t.Errorf("empty dictionary: %v", got)
	}
}

func TestCanonicalNeverSwallowsADatabaseFailure(t *testing.T) {
	loadFixture(t, fixture)
	testkit.RunFaults(t, func(t *testing.T) func(db testkit.DB) error {
		return func(db testkit.DB) error {
			_, err := skills.Canonical(testkit.BG, dbQueries(db), skills.KindGeneral, []string{"excel"})
			return err
		}
	})
}

func TestHTTP(t *testing.T) {
	loadFixture(t, fixture)
	w := testkit.NewWorld(t)
	mount := func(db testkit.DB) *testkit.API {
		return testkit.NewAPI(w, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
			skills.NewHandler(skills.NewService(db), w.Log, requireUser).Mount(r)
		})
	}
	api := mount(testkit.Pool)
	if res := api.Do(nil, http.MethodGet, "/api/skills?kind=research&q=pcr", nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("visitor = %d", res.Code)
	}
	me := w.User("Учёный")
	res := api.Do(&me, http.MethodGet, "/api/skills?kind=research&q=pcr&limit=5", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Raw, `"name":"ПЦР"`) || !strings.Contains(res.Raw, `"group":"Методы"`) || !strings.Contains(res.Raw, `"via":"PCR"`) || !strings.Contains(res.Raw, `"fuzzy":false`) {
		t.Fatalf("search = %d %s", res.Code, res.Raw)
	}
	if res := api.Do(&me, http.MethodGet, "/api/skills?kind=general&q=", nil); res.Code != http.StatusOK || !strings.Contains(res.Raw, `"items":[]`) {
		t.Fatalf("empty = %d %q", res.Code, res.Raw)
	}
	for _, path := range []string{"/api/skills?q=pcr", "/api/skills?kind=other&q=pcr"} {
		if res := api.Do(&me, http.MethodGet, path, nil); res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Raw, `"kind"`) {
			t.Fatalf("%s = %d %s", path, res.Code, res.Raw)
		}
	}
	// Сбой на первом обращении самого поиска (сессию проверяет сервис аккаунтов по настоящей базе).
	broken, _ := testkit.Faulty(testkit.Pool, 1)
	if res := mount(broken).Do(&me, http.MethodGet, "/api/skills?kind=research&q=pcr", nil); res.Code != http.StatusInternalServerError || strings.Contains(res.Raw, "injected") {
		t.Fatalf("failure = %d %s", res.Code, res.Raw)
	}
}

func testQueries() *dbgen.Queries            { return dbgen.New(testkit.Pool) }
func dbQueries(db testkit.DB) *dbgen.Queries { return dbgen.New(db) }

// Совпавшее название выше совпавшего синонима того же яруса: «Масс-спектрометрия» раньше «ИСП-МС» по слову «масс».
func TestSearchPutsANameBeforeASynonymOfTheSameTier(t *testing.T) {
	loadFixture(t, head+
		"research;Г;ИСП-МС;Масс-спектрометрия с индуктивно связанной плазмой\n"+
		"research;Г;Масс-спектрометрия;\n")
	got, err := skills.NewService(testkit.Pool).Search(testkit.BG, skills.KindResearch, "масс спектр", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names(got), []string{"Масс-спектрометрия", "ИСП-МС"}) {
		t.Fatalf("order = %v", names(got))
	}
}

package profiles

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"scibox/server/internal/crossref"
)

// Журналы справочника для тестов пакета (база общая, поэтому вставка один раз; квартили тестовые, не настоящие).
const (
	issnQ1      = "0028-0836" // журнал 1, второй ISSN issnQ1Second
	issnQ1Other = "1476-4687"
	issnQ2      = "2041-1723"
	issnQ3      = "0031-899X"
	issnNoQ     = "1063-7834" // журнал без квартиля
	issnUnknown = "1234-5679" // годный ISSN, которого нет в справочнике
)

var journalsOnce sync.Once

func ensureJournals(t *testing.T) {
	t.Helper()
	journalsOnce.Do(func() {
		_, err := sharedPool.Exec(bg, `
			INSERT INTO journals (id, title, quartile, data_year) VALUES
			  (90001, 'Test Journal One', 1, 2025), (90002, 'Test Journal Two', 2, 2025),
			  (90003, 'Test Journal Three', 3, 2025), (90004, 'Test Journal Unranked', NULL, 2025);
			INSERT INTO journal_issns (issn, journal_id, position) VALUES
			  ('0028-0836', 90001, 0), ('1476-4687', 90001, 1), ('2041-1723', 90002, 0),
			  ('0031-899X', 90003, 0), ('1063-7834', 90004, 0);`)
		if err != nil {
			t.Fatalf("journals: %v", err)
		}
	})
}

// pubIn — публикация в журнале с этим ISSN и годом.
func pubIn(issn string, year int, doi string) ItemInput {
	in := goodPublication()
	in.ISSN, in.Year, in.DOI = issn, ptr(year), doi
	return in
}

func TestQuartilesInProfile(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Елена Орлова")
	// Сегодня 3 октября 2026 по Москве: «последние пять лет» — 2022–2026.
	w.addItem(p, pubIn("00280836", 2025, "10.5555/q14x1-a"))  // Q1, свежая; ISSN без дефиса
	w.addItem(p, pubIn(issnQ1Other, 2018, "10.5555/q14x1-b")) // Q1 по второму ISSN, старая
	w.addItem(p, pubIn(issnQ2, 2022, "10.5555/q14x1-c"))      // Q2, ровно на границе
	w.addItem(p, pubIn(issnQ2, 2021, "10.5555/q14x1-d"))      // Q2, за границей
	w.addItem(p, pubIn(issnQ3, 2024, "10.5555/q14x1-e"))      // Q3 не считается
	w.addItem(p, pubIn(issnNoQ, 2024, "10.5555/q14x1-f"))     // без квартиля
	w.addItem(p, pubIn(issnUnknown, 2024, "10.5555/q14x1-g")) // журнала нет в справочнике
	w.addItem(p, pubIn("", 2024, "10.5555/q14x1-h"))          // без ISSN

	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	q := page.Profile.Quartiles
	if q.Q12Total != 4 || q.Q12Recent != 2 || q.RecentFrom != 2022 || q.Year == nil || *q.Year != 2025 {
		t.Fatalf("quartiles = %+v", q)
	}
	byDOI := map[string]Item{}
	for _, it := range page.Profile.Sections.Publications {
		byDOI[it.DOI] = it
	}
	check := func(doi, issn string, quartile int, title string) {
		t.Helper()
		it := byDOI[doi]
		if it.ISSN != issn {
			t.Errorf("%s: issn %q, want %q", doi, it.ISSN, issn)
		}
		if title == "" {
			if it.Journal != nil {
				t.Errorf("%s: journal %+v, want none", doi, it.Journal)
			}
			return
		}
		if it.Journal == nil || it.Journal.Title != title || it.Journal.ISSN != issn || it.Journal.Year != 2025 {
			t.Fatalf("%s: journal %+v", doi, it.Journal)
		}
		if (quartile == 0) != (it.Journal.Quartile == nil) || (quartile > 0 && *it.Journal.Quartile != quartile) {
			t.Errorf("%s: quartile %v, want %d", doi, it.Journal.Quartile, quartile)
		}
	}
	check("10.5555/q14x1-a", issnQ1, 1, "Test Journal One")
	check("10.5555/q14x1-b", issnQ1Other, 1, "Test Journal One")
	check("10.5555/q14x1-c", issnQ2, 2, "Test Journal Two")
	check("10.5555/q14x1-e", issnQ3, 3, "Test Journal Three")
	check("10.5555/q14x1-f", issnNoQ, 0, "Test Journal Unranked")
	check("10.5555/q14x1-g", issnUnknown, 0, "")
	check("10.5555/q14x1-h", "", 0, "")

	// Журнал в записи не хранится: только ISSN.
	var raw string
	if err := sharedPool.QueryRow(bg, `SELECT data::text FROM profile_items WHERE id = $1`, byDOI["10.5555/q14x1-a"].ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"issn": "0028-0836"`) || strings.Contains(raw, "journal") || strings.Contains(raw, "quartile") {
		t.Fatalf("stored = %s", raw)
	}

	// Другой смотрящий видит то же самое.
	w.setVisibility(p, "public", false)
	other, err := w.svc.Get(bg, page.Profile.ID, nil)
	if err != nil || other.Profile.Quartiles.Q12Total != 4 {
		t.Fatalf("anonymous = %+v, %v", other.Profile.Quartiles, err)
	}

	// Новый год по Москве наступает в 21:00 UTC 31 декабря: граница сдвигается.
	w.clock.t = time.Date(2026, 12, 31, 20, 59, 59, 0, time.UTC)
	if page, _ := w.svc.Own(bg, p.User); page.Profile.Quartiles.RecentFrom != 2022 || page.Profile.Quartiles.Q12Recent != 2 {
		t.Fatalf("before Moscow new year = %+v", page.Profile.Quartiles)
	}
	w.clock.t = time.Date(2026, 12, 31, 21, 0, 0, 0, time.UTC)
	if page, _ := w.svc.Own(bg, p.User); page.Profile.Quartiles.RecentFrom != 2023 || page.Profile.Quartiles.Q12Recent != 1 {
		t.Fatalf("after Moscow new year = %+v", page.Profile.Quartiles)
	}
}

func TestQuartilesWithoutJournals(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Без журналов")
	w.addItem(p, pubIn(issnUnknown, 2024, "10.5555/q14x2-a"))
	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	q := page.Profile.Quartiles
	if q.Q12Total != 0 || q.Q12Recent != 0 || q.Year != nil || q.RecentFrom != 2022 {
		t.Fatalf("quartiles = %+v", q)
	}
	raw, _ := json.Marshal(page.Profile)
	if !strings.Contains(string(raw), `"quartiles":{"q12_total":0,"q12_recent":0,"recent_from":2022,"year":null}`) || strings.Contains(string(raw), `"journal"`) {
		t.Fatalf("json = %s", raw)
	}
}

// Запись без года (до среза 7 год был необязательным, в базе такое возможно) считается во «всего», но не в «пять лет».
func TestQuartilesPublicationWithoutYear(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Старая запись")
	own, _ := w.svc.Own(bg, p.User)
	if _, err := sharedPool.Exec(bg, `INSERT INTO profile_items (profile_id, kind, sort_year, data, created_at, updated_at)
		VALUES ($1, 'publication', 0, '{"title":"Без года","authors":"А. А.","issn":"0028-0836"}', now(), now())`, own.Profile.ID); err != nil {
		t.Fatal(err)
	}
	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	if q := page.Profile.Quartiles; q.Q12Total != 1 || q.Q12Recent != 0 {
		t.Fatalf("quartiles = %+v", q)
	}
}

func TestISSNValidation(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Проверка ISSN")
	_, err := w.svc.AddItem(bg, p.User, pubIn("0028-0837", 2024, "10.5555/q14x3-a"))
	if fields := fieldsOf(t, err); fields["issn"] == "" {
		t.Fatalf("bad check digit must be refused: %v", err)
	}
	it := w.addItem(p, pubIn(" issn 0031-899x ", 2024, "10.5555/q14x3-b"))
	if it.ISSN != issnQ3 {
		t.Fatalf("normalized = %q", it.ISSN)
	}
	// У других видов записей поля ISSN нет.
	grant := ItemInput{Kind: KindGrant, ItemFields: validItem(KindGrant)}
	grant.ISSN = issnQ1
	if g := w.addItem(p, grant); g.ISSN != "" {
		t.Fatalf("grant kept issn %q", g.ISSN)
	}
	// Правка убирает ISSN, если его стёрли.
	upd := pubIn("", 2024, "10.5555/q14x3-b")
	got, err := w.svc.UpdateItem(bg, p.User, it.ID, upd)
	if err != nil || got.ISSN != "" {
		t.Fatalf("update = %+v, %v", got, err)
	}
}

func TestLookupDOIFindsTheJournal(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Поиск по DOI")
	cases := []struct {
		name     string
		issns    []string
		issn     string
		journal  string
		quartile int
	}{
		{"second issn is in the catalog", []string{issnUnknown, "2041 1723"}, issnQ2, "Test Journal Two", 2},
		{"first issn wins when both known", []string{issnQ3, issnQ1}, issnQ3, "Test Journal Three", 3},
		{"unknown journal keeps the first good issn", []string{"nonsense", issnUnknown}, issnUnknown, "", 0},
		{"no issn at all", []string{}, "", "", 0},
		{"only broken issn", []string{"0028-0837"}, "", "", 0},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w.doi.work = crossref.Work{Title: "Статья", Type: "article", ISSNs: c.issns}
			got, err := w.svc.LookupDOI(bg, p.User, "10.5555/q14x4-"+string(rune('a'+i)))
			if err != nil {
				t.Fatal(err)
			}
			if got.ISSN != c.issn || got.Title != "Статья" {
				t.Fatalf("work = %+v", got)
			}
			if c.journal == "" {
				if got.Journal != nil {
					t.Fatalf("journal = %+v", got.Journal)
				}
				return
			}
			if got.Journal == nil || got.Journal.Title != c.journal || *got.Journal.Quartile != c.quartile || got.Journal.ISSN != c.issn {
				t.Fatalf("journal = %+v", got.Journal)
			}
		})
	}
}

func TestHTTPDOIAndProfileCarryJournals(t *testing.T) {
	ensureJournals(t)
	a := newAPI(t)
	p := a.user("Через сайт")
	a.doi.work = crossref.Work{Title: "Статья", Type: "article", ISSNs: []string{issnQ1Other}}
	res := a.do(&p, http.MethodGet, "/api/profile/doi?doi=10.5555/q14x5-a", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Raw, `"issn":"1476-4687"`) || !strings.Contains(res.Raw, `"journal":{"title":"Test Journal One","issn":"1476-4687","quartile":1,"year":2025}`) {
		t.Fatalf("doi = %d %s", res.Code, res.Raw)
	}
	res = a.do(&p, http.MethodPost, "/api/profile/items", map[string]any{
		"kind": "publication", "title": "Статья", "authors": "А. А.", "pub_type": "article", "year": 2025, "issn": "1476-4687",
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", res.Code, res.Raw)
	}
	res = a.do(&p, http.MethodGet, "/api/profile", nil)
	if !strings.Contains(res.Raw, `"q12_total":1`) || !strings.Contains(res.Raw, `"journal":{"title":"Test Journal One"`) {
		t.Fatalf("profile = %s", res.Raw)
	}
	res = a.do(&p, http.MethodPost, "/api/profile/items", map[string]any{
		"kind": "publication", "title": "Статья 2", "authors": "А. А.", "pub_type": "article", "year": 2025, "issn": "1111",
	})
	if res.Code != http.StatusUnprocessableEntity || res.fields(t)["issn"] == nil {
		t.Fatalf("bad issn = %d %s", res.Code, res.Raw)
	}
}

func TestCatalogQuartiles(t *testing.T) {
	ensureJournals(t)
	a := newAPI(t)
	w := a.world
	m := marker()
	many := w.listed("Много статей", m, "public", nil)
	for i, y := range []int{2025, 2024, 2019} {
		w.addItem(many, pubIn(issnQ1, y, "10.5555/q14x6-m"+string(rune('a'+i))))
	}
	w.addItem(many, pubIn(issnQ3, 2025, "10.5555/q14x6-mq3"))
	one := w.listed("Одна статья", m, "public", nil)
	w.addItem(one, pubIn(issnQ2, 2010, "10.5555/q14x6-o"))
	w.addItem(one, pubIn(issnUnknown, 2025, "10.5555/q14x6-o2"))
	none := w.listed("Без статей", m, "public", nil)
	w.addItem(none, pubIn(issnNoQ, 2025, "10.5555/q14x6-n"))

	res := w.catalog(nil, CatalogParams{Query: m, Sort: SortName})
	if res.RecentFrom != 2022 || len(res.Items) != 3 {
		t.Fatalf("catalog = %+v", res)
	}
	cards := map[string]CatalogCard{}
	for _, c := range res.Items {
		cards[c.Name] = c
	}
	if c := cards["Много статей"]; c.Q12Total != 3 || c.Q12Recent != 2 || c.Publications != 4 {
		t.Fatalf("many = %+v", c)
	}
	if c := cards["Одна статья"]; c.Q12Total != 1 || c.Q12Recent != 0 {
		t.Fatalf("one = %+v", c)
	}
	if c := cards["Без статей"]; c.Q12Total != 0 || c.Q12Recent != 0 {
		t.Fatalf("none = %+v", c)
	}
	for _, c := range []struct {
		min  int
		want []string
	}{
		{0, []string{"Без статей", "Много статей", "Одна статья"}},
		{1, []string{"Много статей", "Одна статья"}},
		{3, []string{"Много статей"}},
		{4, []string{}},
	} {
		got := catalogNames(w.catalog(nil, CatalogParams{Query: m, Q12Min: c.min}))
		if !sameSet(got, c.want) {
			t.Errorf("q12_min %d = %v, want %v", c.min, got, c.want)
		}
	}
	for _, bad := range []int{-1, 301} {
		if _, err := w.svc.Catalog(bg, CatalogParams{Q12Min: bad}, nil); fieldsOf(t, err)["q12_min"] == "" {
			t.Errorf("q12_min %d must be refused: %v", bad, err)
		}
	}
	if res := a.do(nil, http.MethodGet, "/api/scientists?q="+m+"&q12_min=3", nil); res.Code != http.StatusOK ||
		!strings.Contains(res.Raw, `"q12_total":3`) || !strings.Contains(res.Raw, `"q12_recent":2`) || !strings.Contains(res.Raw, `"recent_from":2022`) || strings.Contains(res.Raw, "Одна статья") {
		t.Fatalf("http = %d %s", res.Code, res.Raw)
	}
	if res := a.do(nil, http.MethodGet, "/api/scientists?q12_min=много", nil); res.Code != http.StatusUnprocessableEntity || res.fields(t)["q12_min"] == nil {
		t.Fatalf("http bad = %d %s", res.Code, res.Raw)
	}
}

func TestCVShowsQuartiles(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Резюме с квартилями")
	w.addItem(p, pubIn(issnQ1, 2025, "10.5555/q14x7-a"))
	w.addItem(p, pubIn(issnNoQ, 2024, "10.5555/q14x7-b"))
	page, err := w.svc.Own(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	doc := cvDocument(page, "SciBox", testNow)
	meta := strings.Join(doc.Meta, "\n")
	if !strings.Contains(meta, "Статей в журналах Q1–Q2: 1, из них в 2022–2026: 1 (квартили SCImago Journal Rank 2025)") {
		t.Fatalf("meta = %s", meta)
	}
	var texts []string
	for _, s := range doc.Sections {
		for _, e := range s.Entries {
			texts = append(texts, e.Text)
		}
	}
	all := strings.Join(texts, "\n")
	if strings.Count(all, "Q1.") != 1 || strings.Contains(all, "Q0") {
		t.Fatalf("entries = %s", all)
	}
	// Нет статей в Q1–Q2 — нет строки.
	if quartileLine(QuartileStats{Year: ptr(2025)}) != "" || quartileLine(QuartileStats{Q12Total: 2}) != "" {
		t.Fatal("no line without Q1–Q2 articles or without a catalog year")
	}
}

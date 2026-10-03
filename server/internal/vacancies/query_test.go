package vacancies

import (
	"errors"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"scibox/server/internal/auth"
)

func TestParseSearchQueryReadsEveryParameter(t *testing.T) {
	q := url.Values{
		"q": {"химия"}, "field": {"1.4", "1.5"}, "region": {"54"}, "format": {"remote"}, "type": {"research"}, "level": {"2", "3"},
		"degree": {"candidate"}, "org_kind": {"institute"}, "funding": {"grant"}, "rate": {"100", "50"}, "term": {"short"},
		"salary_min": {"80000"}, "housing": {"1"}, "competition": {"true"}, "deadline": {"week"}, "sort": {"new"}, "org": {"baikal"},
		"unit": {"6f9619ff-8b86-d011-b42d-00c04fc964ff"}, "limit": {"15"}, "offset": {"30"},
	}
	p, err := ParseSearchQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if p.Query != "химия" || !reflect.DeepEqual(p.Fields, []string{"1.4", "1.5"}) || p.Region != "54" || !reflect.DeepEqual(p.Levels, []int{2, 3}) ||
		!reflect.DeepEqual(p.Rates, []int{100, 50}) || p.SalaryMin != 80000 || !p.Housing || !p.Competition || p.Deadline != "week" ||
		p.Sort != "new" || p.OrgSlug != "baikal" || p.UnitID == nil || p.Limit != 15 || p.Offset != 30 {
		t.Errorf("%+v", p)
	}
	// Пустой запрос: ничего не задано.
	if p, err := ParseSearchQuery(url.Values{}); err != nil || p.HasConditions() || p.Limit != 0 {
		t.Errorf("%+v %v", p, err)
	}
	// Галочки: только 1 и true.
	if p, _ := ParseSearchQuery(url.Values{"housing": {"yes"}, "competition": {"0"}}); p.Housing || p.Competition {
		t.Errorf("%+v", p)
	}
}

func TestParseSearchQueryErrors(t *testing.T) {
	for name, q := range map[string]url.Values{
		"уровень не число": {"level": {"x"}}, "ставка не число": {"rate": {"1.5"}}, "зарплата не число": {"salary_min": {"много"}},
	} {
		_, err := ParseSearchQuery(q)
		var verr *auth.ValidationError
		if !errors.As(err, &verr) || len(verr.Fields) != 1 {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseSearchQuery(url.Values{"unit": {"не-номер"}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("подразделение: %v", err)
	}
	// Предел страницы и смещение, не бывшие числом, молча пропускаются.
	if p, err := ParseSearchQuery(url.Values{"limit": {"x"}, "offset": {"y"}}); err != nil || p.Limit != 0 || p.Offset != 0 {
		t.Errorf("%+v %v", p, err)
	}
}

func TestHasConditions(t *testing.T) {
	for name, p := range map[string]SearchParams{
		"слова": {Query: "а"}, "область": {Fields: []string{"1"}}, "регион": {Region: "54"}, "формат": {Formats: []string{"remote"}},
		"тип": {Types: []string{"research"}}, "уровень": {Levels: []int{1}}, "степень": {Degrees: []string{"none"}},
		"тип организации": {OrgKinds: []string{"institute"}}, "финансирование": {Fundings: []string{"grant"}}, "ставка": {Rates: []int{100}},
		"срок договора": {Terms: []string{"short"}}, "зарплата": {SalaryMin: 1}, "жильё": {Housing: true}, "конкурс": {Competition: true},
		"срок подачи": {Deadline: "week"},
	} {
		if !p.HasConditions() {
			t.Errorf("%s: условие не замечено", name)
		}
	}
	for name, p := range map[string]SearchParams{
		"пусто": {}, "только порядок": {Sort: "new"}, "организация и страница": {OrgSlug: "x", Limit: 5, Offset: 5},
	} {
		if p.HasConditions() {
			t.Errorf("%s: условием не считается", name)
		}
	}
}

// Запись условий и чтение обратно дают то же самое.
func TestEncodeRoundTrip(t *testing.T) {
	p := SearchParams{
		Query: "химия катализа", Fields: []string{"1.4", "1.5.7"}, Region: "54", Formats: []string{"remote", "hybrid"}, Types: []string{"research"},
		Levels: []int{2, 3}, Degrees: []string{"candidate"}, OrgKinds: []string{"institute"}, Fundings: []string{"grant"}, Rates: []int{100, 50},
		Terms: []string{"short"}, SalaryMin: 90000, Housing: true, Competition: true, Deadline: "month", Sort: "deadline",
	}
	enc := p.Encode()
	values, err := url.ParseQuery(enc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseSearchQuery(values)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, p) {
		t.Errorf("\nбыло   %+v\nстало  %+v", p, back)
	}
	// Страница, организация и подразделение в запись не попадают.
	extra := p
	extra.OrgSlug, extra.Limit, extra.Offset = "baikal", 10, 20
	if extra.Encode() != enc {
		t.Errorf("лишнее в записи: %s", extra.Encode())
	}
	if (SearchParams{}).Encode() != "" {
		t.Error("пустые условия дают пустую запись")
	}
}

func TestValidateSearchNormalizesWords(t *testing.T) {
	p := SearchParams{Query: "  химия   катализа "}
	if err := ValidateSearch(&p); err != nil || p.Query != "химия катализа" {
		t.Errorf("%q %v", p.Query, err)
	}
	bad := SearchParams{Formats: []string{"teleport"}}
	if err := ValidateSearch(&bad); err == nil {
		t.Error("неизвестный формат принят")
	}
}

// Отбор по моменту публикации: окно (после, до] нужно рассылкам по сохранённым поискам.
func TestSearchPublishedWindow(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()
	base := testNow
	setPublished := func(title string, at time.Time) {
		if _, err := sharedPool.Exec(bg, `UPDATE vacancies SET published_at = $2 WHERE title = $1`, title, at); err != nil {
			t.Fatal(err)
		}
	}
	setPublished(st.chem, base.Add(-3*time.Hour))
	setPublished(st.math, base.Add(-2*time.Hour))
	setPublished(st.gen, base.Add(-1*time.Hour))
	setPublished(st.ckp, base)
	at := func(h int) *time.Time { t := base.Add(time.Duration(h) * time.Hour); return &t }

	for _, c := range []struct {
		name         string
		after, until *time.Time
		want         []string
	}{
		{"без границ", nil, nil, []string{st.chem, st.math, st.gen, st.ckp}},
		{"после: сама граница не входит", at(-2), nil, []string{st.gen, st.ckp}},
		{"до: сама граница входит", nil, at(-2), []string{st.chem, st.math}},
		{"окно", at(-3), at(-1), []string{st.math, st.gen}},
		{"пустое окно", at(0), at(0), nil},
		{"перевёрнутое окно", at(-1), at(-3), nil},
	} {
		res := w.search(st, SearchParams{PublishedAfter: c.after, PublishedUntil: c.until, Sort: SortNew})
		if !slices.Equal(sorted(titles(res)...), sorted(c.want...)) {
			t.Errorf("%s: %v, ожидали %v", c.name, titles(res), c.want)
		}
	}
}

func TestSearchNoFuzzy(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()
	typo := SearchParams{Query: "катализс"} // опечатка: точного совпадения нет
	res := w.search(st, typo)
	if !res.Fuzzy || res.Total == 0 {
		t.Fatalf("с запасным поиском находится: %+v", res)
	}
	typo.NoFuzzy = true
	res = w.search(st, typo)
	if res.Fuzzy || res.Total != 0 {
		t.Errorf("без запасного поиска ничего: %+v", res)
	}
}

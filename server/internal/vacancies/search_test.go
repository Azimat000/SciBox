package vacancies

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/auth"
	"scibox/server/internal/orgs"
)

// searchTeam — организация с вакансиями, по которым проверяются фильтры. «Сегодня» в тестах 3 октября 2026 (Москва).
type searchTeam struct {
	*team
	// Названия вакансий (для сравнения результатов).
	chem, math, gen, ckp string
}

func (w *world) searchTeam() *searchTeam {
	w.t.Helper()
	tm := w.team()
	st := &searchTeam{team: tm}
	n := nameSeq.Add(1)
	name := func(base string) string { return fmt.Sprintf("%s %d", base, n) }
	st.chem, st.math, st.gen, st.ckp = name("Химия катализа"), name("Доцент математики"), name("Постдок геномики"), name("Руководитель ЦКП")

	chem := goodInput()
	chem.Title, chem.Summary = st.chem, "Исследование органических катализаторов и их активных центров."
	chem.SalaryFrom, chem.SalaryTo, chem.FundingSource, chem.Degree = ptr(100000), ptr(130000), FundingGrant, DegreeCandidate
	chem.IsCompetition, chem.Deadline, chem.Housing = true, "2026-10-13", HousingService
	chem.UnitID = &tm.unitA.ID
	w.published(tm, chem)

	math := goodInput()
	math.Title, math.PositionCode, math.Summary = st.math, "docent", "Лекции по математическому анализу и руководство магистрами."
	math.Focus, math.WorkFormat, math.RegionCode, math.City = "Математический анализ", FormatHybrid, "16", "Казань"
	math.RatePercent, math.ContractType, math.ContractMonth, math.Specialties = ptr(50), ContractPermanent, nil, []string{"1.1.1"}
	math.SalaryFrom, math.FundingSource, math.Degree, math.AcademicTitle, math.Deadline = ptr(70000), FundingBudget, DegreeCandidate, TitleDocent, ""
	math.UnitID = &tm.unitB.ID
	w.published(tm, math)

	gen := goodInput()
	gen.Title, gen.PositionCode, gen.Summary = st.gen, "phd_student", "Анализ геномов и транскриптомов, работа с данными секвенирования."
	gen.Focus, gen.WorkFormat, gen.RegionCode, gen.City = "Геномика растений", FormatRemote, "", ""
	gen.CareerLevel, gen.RatePercent, gen.ContractMonth, gen.Specialties = ptr(2), nil, ptr(12), []string{"1.5.7"}
	gen.SalaryFrom, gen.SalaryTo, gen.FundingSource, gen.Degree, gen.Deadline = ptr(90000), ptr(110000), FundingGrant, DegreeDoctor, "2026-10-06"
	w.published(tm, gen)

	ckp := goodInput()
	ckp.Title, ckp.PositionCode, ckp.Summary = st.ckp, "shared_facility_head", "Управление центром коллективного пользования и закупками."
	ckp.Focus, ckp.RegionCode, ckp.City, ckp.CareerLevel, ckp.Specialties = "Руководство центром", "78", "Санкт-Петербург", nil, nil
	ckp.ContractType, ckp.ContractMonth, ckp.FundingSource, ckp.Deadline = ContractPermanent, nil, FundingOwn, "2026-10-28"
	w.published(tm, ckp)

	// Не должны находиться никогда: черновик, закрытая, архив.
	w.create(tm, tm.owner, goodInput())
	closed := w.published(tm, goodInput())
	if _, err := w.svc.SetStatus(bg, tm.owner.User, closed.ID, StatusClosed); err != nil {
		w.t.Fatal(err)
	}
	archived := w.published(tm, goodInput())
	for _, to := range []string{StatusClosed, StatusArchived} {
		if _, err := w.svc.SetStatus(bg, tm.owner.User, archived.ID, to); err != nil {
			w.t.Fatal(err)
		}
	}
	return st
}

func titles(res SearchResult) []string {
	out := make([]string, len(res.Items))
	for i, c := range res.Items {
		out[i] = c.Title
	}
	return out
}

func sorted(s ...string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func (w *world) search(st *searchTeam, p SearchParams) SearchResult {
	w.t.Helper()
	p.OrgSlug = st.slug
	res, err := w.svc.Search(bg, p)
	if err != nil {
		w.t.Fatalf("search %+v: %v", p, err)
	}
	return res
}

func TestSearchFilters(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()
	all := []string{st.chem, st.math, st.gen, st.ckp}
	cases := []struct {
		name string
		p    SearchParams
		want []string
	}{
		{"no filters: only published", SearchParams{}, all},
		{"field: group prefix", SearchParams{Fields: []string{"1.4"}}, []string{st.chem}},
		{"field: exact specialty", SearchParams{Fields: []string{"1.4.4"}}, []string{st.chem}},
		{"field: other specialty of a group", SearchParams{Fields: []string{"1.4.3"}}, nil},
		{"field: whole science", SearchParams{Fields: []string{"1"}}, []string{st.chem, st.math, st.gen}},
		{"field: two groups", SearchParams{Fields: []string{"1.1", "1.5"}}, []string{st.math, st.gen}},
		{"field: a code is not a substring of another", SearchParams{Fields: []string{"1.1.10"}}, nil},
		{"region", SearchParams{Region: "16"}, []string{st.math}},
		{"region: remote work has no region", SearchParams{Region: "54"}, []string{st.chem}},
		{"format onsite", SearchParams{Formats: []string{FormatOnsite}}, []string{st.chem, st.ckp}},
		{"format remote or hybrid", SearchParams{Formats: []string{FormatRemote, FormatHybrid}}, []string{st.math, st.gen}},
		{"type teaching", SearchParams{Types: []string{TypeTeaching}}, []string{st.math}},
		{"type research or admin", SearchParams{Types: []string{TypeResearch, TypeAdmin}}, []string{st.chem, st.ckp}},
		{"level 3", SearchParams{Levels: []int{3}}, []string{st.chem, st.math}},
		{"level 2 or 4", SearchParams{Levels: []int{2, 4}}, []string{st.gen}},
		{"degree doctor", SearchParams{Degrees: []string{DegreeDoctor}}, []string{st.gen}},
		{"degree none", SearchParams{Degrees: []string{DegreeNone}}, []string{st.ckp}},
		{"degree candidate", SearchParams{Degrees: []string{DegreeCandidate}}, []string{st.chem, st.math}},
		{"org kind", SearchParams{OrgKinds: []string{orgs.KindInstitute}}, all},
		{"org kind: another", SearchParams{OrgKinds: []string{orgs.KindUniversity, orgs.KindTechnopark}}, nil},
		{"funding grant", SearchParams{Fundings: []string{FundingGrant}}, []string{st.chem, st.gen}},
		{"funding own or budget", SearchParams{Fundings: []string{FundingOwn, FundingBudget}}, []string{st.math, st.ckp}},
		{"rate 50", SearchParams{Rates: []int{50}}, []string{st.math}},
		{"rate 100", SearchParams{Rates: []int{100}}, []string{st.chem, st.ckp}},
		{"term permanent", SearchParams{Terms: []string{TermPermanent}}, []string{st.math, st.ckp}},
		{"term short (to a year)", SearchParams{Terms: []string{TermShort}}, []string{st.gen}},
		{"term medium (to three years)", SearchParams{Terms: []string{TermMedium}}, []string{st.chem}},
		{"term long (over three years)", SearchParams{Terms: []string{TermLong}}, nil},
		{"salary min: upper bound counts", SearchParams{SalaryMin: 120000}, []string{st.chem}},
		{"salary min: lower bound only", SearchParams{SalaryMin: 70000}, []string{st.chem, st.math, st.gen}},
		{"salary min: above everything", SearchParams{SalaryMin: 200000}, nil},
		{"housing", SearchParams{Housing: true}, []string{st.chem}},
		{"competition", SearchParams{Competition: true}, []string{st.chem}},
		{"deadline week", SearchParams{Deadline: DeadlineWeek}, []string{st.gen}},
		{"deadline month", SearchParams{Deadline: DeadlineMonth}, []string{st.chem, st.gen, st.ckp}},
		{"deadline none", SearchParams{Deadline: DeadlineNone}, []string{st.math}},
		{"filters combine with AND", SearchParams{Types: []string{TypeResearch, TypePhD}, Fundings: []string{FundingGrant}, Levels: []int{2}}, []string{st.gen}},
		{"unit", SearchParams{UnitID: &st.unitA.ID}, []string{st.chem}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := w.search(st, c.p)
			if !slices.Equal(sorted(titles(res)...), sorted(c.want...)) || res.Total != len(c.want) || res.Fuzzy {
				t.Errorf("got %v (total %d, fuzzy %v), want %v", titles(res), res.Total, res.Fuzzy, c.want)
			}
		})
	}
}

func TestSearchText(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()
	cases := []struct {
		name  string
		q     string
		want  []string
		fuzzy bool
	}{
		{"word forms: «катализатор» finds «катализаторов»", "катализатор", []string{st.chem}, false},
		{"another case: «математический» finds «математическому»", "математический", []string{st.math}, false},
		{"upper case and ё", "КАЧЕСТВО ёж", nil, false},
		{"all words must match", "органических катализаторов", []string{st.chem}, false},
		{"one wrong word kills an exact match, then similar ones are tried", "органических фортепиано", nil, false},
		{"organization name", "Институт вакансий", []string{st.chem, st.math, st.gen, st.ckp}, false},
		{"city of the vacancy", "Казань", []string{st.math}, false},
		{"region name", "Татарстан", []string{st.math}, false},
		{"unit name", "Лаборатория", []string{st.chem, st.math}, false},
		{"specialty name", "Физическая химия", []string{st.chem}, false},
		{"position name", "постдок", []string{st.gen}, false},
		{"description (the same in all of them)", "десяти человек", []string{st.chem, st.math, st.gen, st.ckp}, false},
		{"web-search quoting: exclusion", "катализ -органических", nil, false},
		{"abbreviation", "ЦКП", []string{st.ckp}, false},
		{"typo in a word", "Постдок геномкии", []string{st.gen}, true},
		{"typo: a word with a letter missing", "Доцент матемтики", []string{st.math}, true},
		{"only stop words match nothing", "и в на", nil, false},
		{"only punctuation", "!!! ???", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := w.search(st, SearchParams{Query: c.q})
			got := sorted(titles(res)...)
			want := sorted(c.want...)
			if !slices.Equal(got, want) || res.Fuzzy != c.fuzzy {
				t.Errorf("%q: got %v fuzzy=%v, want %v fuzzy=%v", c.q, got, res.Fuzzy, want, c.fuzzy)
			}
		})
	}
}

func TestSearchRelevanceAndSort(t *testing.T) {
	w := newWorld(t)
	st := w.searchTeam()

	// По совпадению: слово в названии весит больше, чем то же слово в описании.
	tm := w.team()
	inTitle := goodInput()
	inTitle.Title, inTitle.Description = "Исследователь люминесценции кристаллов", "Работа в лаборатории."
	inBody := goodInput()
	inBody.Title, inBody.Description = "Старший научный сотрудник лаборатории", "Нужен опыт измерения люминесценции кристаллов."
	w.published(tm, inBody)
	w.published(tm, inTitle)
	res, err := w.svc.Search(bg, SearchParams{Query: "люминесценция", OrgSlug: tm.slug})
	if err != nil || len(res.Items) != 2 || res.Items[0].Title != inTitle.Title {
		t.Fatalf("relevance: %v %v", titles(res), err)
	}
	// Тот же поиск «сначала новые»: новее та, что опубликована позже (её описание у inTitle).
	w.clock.Advance(time.Hour)
	res, _ = w.svc.Search(bg, SearchParams{Query: "люминесценция", OrgSlug: tm.slug, Sort: SortNew})
	if len(res.Items) != 2 {
		t.Fatalf("new: %v", titles(res))
	}

	// Без слов поиска «по совпадению» значит «сначала новые».
	byRelevance := w.search(st, SearchParams{Sort: SortRelevance})
	byDefault := w.search(st, SearchParams{})
	byNew := w.search(st, SearchParams{Sort: SortNew})
	if !reflect.DeepEqual(titles(byRelevance), titles(byDefault)) || !reflect.DeepEqual(titles(byDefault), titles(byNew)) {
		t.Errorf("default order differs: %v %v %v", titles(byRelevance), titles(byDefault), titles(byNew))
	}

	// По сроку: ближайший первым, без срока в конце. По зарплате: больше первой, без зарплаты в конце.
	if got := titles(w.search(st, SearchParams{Sort: SortDeadline})); !reflect.DeepEqual(got, []string{st.gen, st.chem, st.ckp, st.math}) {
		t.Errorf("deadline order: %v", got)
	}
	if got := titles(w.search(st, SearchParams{Sort: SortSalary})); !reflect.DeepEqual(got, []string{st.chem, st.gen, st.math, st.ckp}) {
		t.Errorf("salary order: %v", got)
	}
}

func TestSearchHidesExpiredButNotToday(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.Deadline = "2026-10-04"
	d := w.published(tm, in)
	forever := w.published(tm, func() Input { i := goodInput(); i.Deadline = ""; return i }())

	count := func() int {
		res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug})
		if err != nil {
			t.Fatal(err)
		}
		return res.Total
	}
	if count() != 2 {
		t.Fatalf("both visible before the deadline: %d", count())
	}
	// Срок «включительно»: 4 октября вакансия видна весь день по Москве (до 00:00 5 октября).
	w.clock.Advance(24*time.Hour + 8*time.Hour) // 4 октября, 23:00 по Москве
	if count() != 2 {
		t.Errorf("the deadline day is still open: %d", count())
	}
	w.clock.Advance(2 * time.Hour) // 5 октября, 01:00 по Москве
	if count() != 1 {
		t.Errorf("after the deadline: %d", count())
	}
	// Страница вакансии остаётся: срок прошёл, а вакансия опубликована (D-057).
	if _, err := w.svc.Get(bg, d.ID, nil); err != nil {
		t.Errorf("expired page: %v", err)
	}
	_ = forever
}

func TestSearchPaging(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	for i := range 5 {
		in := goodInput()
		in.Title = fmt.Sprintf("Вакансия номер %d", i)
		w.published(tm, in)
		w.clock.Advance(time.Minute)
	}
	page := func(limit, offset int) SearchResult {
		res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Limit: limit, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	p1, p2, p3 := page(2, 0), page(2, 2), page(2, 4)
	if p1.Total != 5 || len(p1.Items) != 2 || len(p2.Items) != 2 || len(p3.Items) != 1 {
		t.Fatalf("pages: %d/%d/%d total %d", len(p1.Items), len(p2.Items), len(p3.Items), p1.Total)
	}
	if p1.Items[0].Title != "Вакансия номер 4" || p3.Items[0].Title != "Вакансия номер 0" {
		t.Errorf("newest first: %v … %v", titles(p1), titles(p3))
	}
	// Страница дальше последней: пусто, но общее число известно (чтобы показать «страница 9 из 3»).
	if far := page(2, 100); len(far.Items) != 0 || far.Total != 5 {
		t.Errorf("far page: %+v", far)
	}
	// Пустая выдача и смещение: общее число 0.
	if res, _ := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Query: "несуществующеесловоqzx", Offset: 40}); res.Total != 0 || len(res.Items) != 0 {
		t.Errorf("empty far page: %+v", res)
	}
	if def := page(0, -5); len(def.Items) != 5 {
		t.Errorf("default limit and negative offset: %d", len(def.Items))
	}
	if big := page(MaxLimit*10, 0); len(big.Items) != 5 {
		t.Errorf("huge limit: %d", len(big.Items))
	}
}

func TestSearchValidation(t *testing.T) {
	w := newWorld(t)
	many := make([]string, maxFilterItems+1)
	for i := range many {
		many[i] = FormatOnsite
	}
	cases := []struct {
		name  string
		p     SearchParams
		field string
	}{
		{"long query", SearchParams{Query: strings.Repeat("я", MaxQueryLen+1)}, "q"},
		{"format", SearchParams{Formats: []string{"onsite", "moon"}}, "format"},
		{"too many values", SearchParams{Formats: many}, "format"},
		{"type", SearchParams{Types: []string{"nobody"}}, "type"},
		{"degree", SearchParams{Degrees: []string{"master"}}, "degree"},
		{"org kind", SearchParams{OrgKinds: []string{"circus"}}, "org_kind"},
		{"funding", SearchParams{Fundings: []string{"gift"}}, "funding"},
		{"term", SearchParams{Terms: []string{"forever"}}, "term"},
		{"field code", SearchParams{Fields: []string{"1.4.x"}}, "field"},
		{"field code with SQL", SearchParams{Fields: []string{"1.4%"}}, "field"},
		{"too many fields", SearchParams{Fields: append(slices.Repeat([]string{"1.4"}, maxFilterItems), "1.1")}, "field"},
		{"level", SearchParams{Levels: []int{1, 5}}, "level"},
		{"rate", SearchParams{Rates: []int{60}}, "rate"},
		{"region", SearchParams{Region: "5"}, "region"},
		{"region with letters", SearchParams{Region: "5a"}, "region"},
		{"negative salary", SearchParams{SalaryMin: -1}, "salary_min"},
		{"huge salary", SearchParams{SalaryMin: MaxSalary + 1}, "salary_min"},
		{"deadline", SearchParams{Deadline: "tomorrow"}, "deadline"},
		{"sort", SearchParams{Sort: "random"}, "sort"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.svc.Search(bg, c.p)
			var verr *auth.ValidationError
			if !errors.As(err, &verr) || verr.Fields[c.field] == "" {
				t.Fatalf("want a field error for %q, got %v", c.field, err)
			}
		})
	}
	// Всё допустимое проходит.
	ok := SearchParams{
		Query: "  химия   ", Fields: []string{"1", "1.4", "1.4.4"}, Region: "54", Formats: WorkFormats, Types: []string{TypeResearch},
		Levels: []int{1, 2, 3, 4}, Degrees: Degrees, OrgKinds: orgs.OrgKinds, Fundings: FundingSources, Rates: Rates,
		Terms: Terms, SalaryMin: 1, Housing: true, Competition: true, Deadline: DeadlineWeek, Sort: SortSalary,
	}
	if _, err := w.svc.Search(bg, ok); err != nil {
		t.Errorf("valid params: %v", err)
	}
}

func TestSearchUnknownOrganizationAndUnit(t *testing.T) {
	w := newWorld(t)
	tm, other := w.team(), w.team()
	if _, err := w.svc.Search(bg, SearchParams{OrgSlug: "no-such-organization"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown organization: %v", err)
	}
	if _, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, UnitID: &other.unitA.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a unit of another organization: %v", err)
	}
}

// Текст для поиска пересобирается сам: при правке вакансии, смене специальностей, переименовании организации и
// подразделения, удалении вакансии.
func TestSearchIndexFollowsChanges(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.Title, in.UnitID = "Научный сотрудник по гравитации", &tm.unitA.ID
	d := w.published(tm, in)
	found := func(q string) bool {
		res, err := w.svc.Search(bg, SearchParams{Query: q, OrgSlug: tm.slug})
		if err != nil {
			t.Fatal(err)
		}
		return res.Total > 0 && !res.Fuzzy
	}
	if !found("гравитация") || found("квазары") {
		t.Fatal("initial index")
	}

	in.Title = "Научный сотрудник по квазарам"
	if _, err := w.svc.Update(bg, tm.owner.User, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if found("гравитация") || !found("квазары") {
		t.Error("the title change is not indexed")
	}

	in.Specialties = []string{"1.3.8"}
	if _, err := w.svc.Update(bg, tm.owner.User, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if found("Физическая химия") || !found("Физика конденсированного состояния") {
		t.Error("the specialties change is not indexed")
	}

	if _, err := w.orgs.UpdateOrganization(bg, tm.owner.User, tm.slug, orgs.OrgInput{Name: "Обсерватория Гелиос", Kind: orgs.KindInstitute, City: "Томск"}); err != nil {
		t.Fatal(err)
	}
	if !found("Гелиос") || !found("Томск") || found("Институт вакансий") {
		t.Error("the organization rename is not indexed")
	}
	if _, err := w.orgs.UpdateUnit(bg, tm.owner.User, tm.slug, tm.unitA.ID, orgs.UnitInput{Name: "Отдел тёмной материи", Kind: orgs.UnitDivision}); err != nil {
		t.Fatal(err)
	}
	if !found("тёмной материи") {
		t.Error("the unit rename is not indexed")
	}

	// Закрытая вакансия не находится, хотя текст остался.
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusClosed); err != nil {
		t.Fatal(err)
	}
	if found("квазары") {
		t.Error("closed vacancy is found")
	}
	if n := countRows(t, "SELECT count(*) FROM vacancy_search WHERE vacancy_id = $1", d.ID); n != 1 {
		t.Errorf("index rows of a closed vacancy: %d", n)
	}

	// Черновик удаляется вместе со своим текстом.
	draft := w.create(tm, tm.owner, goodInput())
	if n := countRows(t, "SELECT count(*) FROM vacancy_search WHERE vacancy_id = $1", draft.ID); n != 1 {
		t.Fatalf("a draft has no search text: %d", n)
	}
	if err := w.svc.Delete(bg, tm.owner.User, draft.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, "SELECT count(*) FROM vacancy_search WHERE vacancy_id = $1", draft.ID); n != 0 {
		t.Errorf("the search text of a deleted draft stays: %d", n)
	}
}

func TestHTTPSearch(t *testing.T) {
	a := newAPI(t)
	st := a.searchTeam()
	get := func(q url.Values) reply {
		q.Set("org", st.slug)
		return a.do(nil, "GET", "/api/vacancies?"+q.Encode(), nil)
	}
	titlesOf := func(r reply) []string {
		var out []string
		items, _ := r.json(t)["items"].([]any)
		for _, it := range items {
			out = append(out, it.(map[string]any)["title"].(string))
		}
		return out
	}

	r := get(url.Values{"type": {"research", "admin"}, "format": {"onsite"}, "level": {"3"}, "rate": {"100"}, "term": {"medium", "permanent"}})
	if r.Code != http.StatusOK || !reflect.DeepEqual(titlesOf(r), []string{st.chem}) || r.json(t)["total"].(float64) != 1 || r.json(t)["fuzzy"] != false {
		t.Errorf("filters over HTTP: %d %s", r.Code, r.Raw)
	}
	r = get(url.Values{"q": {"катализатор"}, "field": {"1.4", "1.1"}, "region": {"54"}, "degree": {"candidate"}, "org_kind": {"institute"},
		"funding": {"grant"}, "salary_min": {"90000"}, "housing": {"1"}, "competition": {"true"}, "deadline": {"month"}, "sort": {"relevance"}})
	if !reflect.DeepEqual(titlesOf(r), []string{st.chem}) {
		t.Errorf("every parameter at once: %s", r.Raw)
	}
	if r := get(url.Values{"q": {"геномкии"}}); r.json(t)["fuzzy"] != true || !reflect.DeepEqual(titlesOf(r), []string{st.gen}) {
		t.Errorf("fuzzy flag: %s", r.Raw)
	}
	if r := get(url.Values{"housing": {"0"}, "competition": {"no"}}); len(titlesOf(r)) != 4 {
		t.Errorf("flags that are off: %s", r.Raw)
	}

	if r := get(url.Values{"unit": {st.unitA.ID.String()}}); !reflect.DeepEqual(titlesOf(r), []string{st.chem}) {
		t.Errorf("unit filter: %s", r.Raw)
	}

	// Неверные значения: 422 с названием поля, не молчаливое «любые».
	for param, value := range map[string]string{
		"format": "moon", "level": "high", "rate": "x", "salary_min": "much", "sort": "random", "deadline": "soon", "field": "1.x",
	} {
		r := get(url.Values{param: {value}})
		fields, _ := r.json(t)["error"].(map[string]any)["fields"].(map[string]any)
		if r.Code != http.StatusUnprocessableEntity || r.errCode(t) != "validation_failed" || fields[param] == nil {
			t.Errorf("%s=%s: %d %s", param, value, r.Code, r.Raw)
		}
	}
	// Подразделение не номер или чужое: такой выдачи нет.
	if r := a.do(nil, "GET", "/api/vacancies?org="+st.slug+"&unit=not-a-uuid", nil); r.Code != http.StatusNotFound {
		t.Errorf("bad unit: %d", r.Code)
	}
}

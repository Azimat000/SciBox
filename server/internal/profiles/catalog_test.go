package profiles

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/auth"
)

// База в пакете общая, поэтому каждый тест ищет по своей метке: латинскому слову, которого нет в чужих профилях.
// Метка случайная и не похожая на другие, чтобы поиск «по похожим словам» не подбирал чужие профили.
func marker() string { return strings.ToLower(rand.Text()[:12]) }

// listed — человек с профилем: заполнена должность (с меткой), заданы режим приватности и, при желании, остальное.
func (w *world) listed(name, mark, vis string, mod func(*CoreInput)) person {
	w.t.Helper()
	p := w.user(name)
	in := CoreInput{Headline: "Исследователь " + mark, City: "Москва", RegionCode: "77"}
	if mod != nil {
		mod(&in)
	}
	if _, err := w.svc.SaveCore(bg, p.User, in); err != nil {
		w.t.Fatalf("save core: %v", err)
	}
	w.setVisibility(p, vis, false)
	return p
}

func (w *world) catalog(viewer *auth.User, p CatalogParams) CatalogResult {
	w.t.Helper()
	res, err := w.svc.Catalog(bg, p, viewer)
	if err != nil {
		w.t.Fatalf("catalog: %v", err)
	}
	return res
}

func catalogNames(res CatalogResult) []string {
	out := make([]string, 0, len(res.Items))
	for _, c := range res.Items {
		out = append(out, c.Name)
	}
	return out
}

func sameSet(got, want []string) bool {
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	return slices.Equal(g, w)
}

// Кто что видит в каталоге: каждая пара «смотрящий × режим» записана явно (D-097).
func TestCatalogWhoSeesWhat(t *testing.T) {
	w := newWorld(t)
	m := marker()
	w.listed("Скрытая", m, "hidden", nil)
	w.listed("Для организаций", m, "orgs", nil)
	pub := w.listed("Публичная", m, "public", nil)
	staff, stranger := w.staff("Сотрудник"), w.user("Вошедший без организации")
	// Сотрудник, у которого тоже есть публичный профиль: себя в каталоге он не видит.
	staffSci := w.staff("Сотрудник-учёный")
	if _, err := w.svc.SaveCore(bg, staffSci.User, CoreInput{Headline: "Исследователь " + m}); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(staffSci, "public", false)

	tests := []struct {
		name string
		who  *auth.User
		want []string
	}{
		{"аноним", nil, []string{"Публичная", "Сотрудник-учёный"}},
		{"вошедший без организации", &stranger.User, []string{"Публичная", "Сотрудник-учёный"}},
		{"сотрудник организации", &staff.User, []string{"Для организаций", "Публичная", "Сотрудник-учёный"}},
		{"владелец публичного профиля", &pub.User, []string{"Сотрудник-учёный"}},
		{"сотрудник со своим публичным профилем", &staffSci.User, []string{"Для организаций", "Публичная"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := w.catalog(tc.who, CatalogParams{Query: m})
			if got := catalogNames(res); !sameSet(got, tc.want) || res.Total != len(tc.want) {
				t.Errorf("в каталоге %v (всего %d), ожидали %v", got, res.Total, tc.want)
			}
		})
	}
}

// Скрытый профиль не попадает в каталог ни при каких фильтрах, словах и «похожих словах».
func TestCatalogNeverShowsHidden(t *testing.T) {
	w := newWorld(t)
	m := marker()
	w.listed("Зюзюкина", m, "hidden", func(c *CoreInput) { c.Specialties = []string{"1.4.4"} })
	staff := w.staff("Сотрудник")
	for _, p := range []CatalogParams{
		{Query: m}, {Query: m, Fields: []string{"1.4"}}, {Query: "Зюзюкинв"}, {Query: "Зюзюкина " + m, Sort: SortName}, {Query: m, OpenOnly: false, HMin: 0},
	} {
		for _, who := range []*auth.User{nil, &staff.User} {
			if res := w.catalog(who, p); res.Total != 0 || len(res.Items) != 0 {
				t.Errorf("скрытый профиль найден по %+v: %v", p, catalogNames(res))
			}
		}
	}
}

// Профиль без должности в каталог не попадает: карточка была бы пустой.
func TestCatalogNeedsAHeadline(t *testing.T) {
	w := newWorld(t)
	m := marker()
	p := w.user("Без должности")
	if _, err := w.svc.SaveCore(bg, p.User, CoreInput{About: "Пишу о " + m}); err != nil {
		t.Fatal(err)
	}
	w.setVisibility(p, "public", false)
	if res := w.catalog(nil, CatalogParams{Query: m}); res.Total != 0 {
		t.Errorf("профиль без должности в каталоге: %v", catalogNames(res))
	}
	if _, err := w.svc.SaveCore(bg, p.User, CoreInput{Headline: "Теперь есть", About: "Пишу о " + m}); err != nil {
		t.Fatal(err)
	}
	if res := w.catalog(nil, CatalogParams{Query: m}); res.Total != 1 {
		t.Errorf("с должностью профиль должен появиться: %v", catalogNames(res))
	}
}

// Каждый фильтр отдельной строкой таблицы и несколько вместе.
func TestCatalogFilters(t *testing.T) {
	w := newWorld(t)
	m := marker()
	w.listed("Анна", m, "public", func(c *CoreInput) {
		c.RegionCode, c.Degree, c.DegreeSpecialty, c.AcademicTitle = "54", DegreeDoctor, "1.4.4", TitleProfessor
		c.HRsci, c.HScopus, c.Specialties = ptr(40), ptr(22), []string{"1.4.4"}
	})
	b := w.listed("Борис", m, "public", func(c *CoreInput) {
		c.RegionCode, c.Degree, c.DegreeSpecialty, c.AcademicTitle = "16", DegreeCandidate, "1.4.1", TitleDocent
		c.HScholar, c.Specialties = ptr(5), []string{"1.4.1", "1.2.1"}
	})
	w.setVisibility(b, "public", true)
	w.listed("Виктор", m, "public", func(c *CoreInput) { c.Specialties = []string{"1.2.1"} })

	tests := []struct {
		name string
		p    CatalogParams
		want []string
	}{
		{"без фильтров", CatalogParams{}, []string{"Анна", "Борис", "Виктор"}},
		{"раздел науки", CatalogParams{Fields: []string{"1"}}, []string{"Анна", "Борис", "Виктор"}},
		{"группа специальностей", CatalogParams{Fields: []string{"1.4"}}, []string{"Анна", "Борис"}},
		{"другая группа", CatalogParams{Fields: []string{"1.2"}}, []string{"Борис", "Виктор"}},
		{"специальность", CatalogParams{Fields: []string{"1.4.4"}}, []string{"Анна"}},
		{"специальность не подстрока другой", CatalogParams{Fields: []string{"1.4.4"}}, []string{"Анна"}},
		{"две области «или»", CatalogParams{Fields: []string{"1.4.4", "1.2.1"}}, []string{"Анна", "Борис", "Виктор"}},
		{"область без людей", CatalogParams{Fields: []string{"2.1"}}, nil},
		{"регион", CatalogParams{Region: "54"}, []string{"Анна"}},
		{"регион без людей", CatalogParams{Region: "02"}, nil},
		{"степень доктор", CatalogParams{Degrees: []string{"doctor"}}, []string{"Анна"}},
		{"степень кандидат или без степени", CatalogParams{Degrees: []string{"candidate", "none"}}, []string{"Борис", "Виктор"}},
		{"звание профессор", CatalogParams{Titles: []string{"professor"}}, []string{"Анна"}},
		{"звание без звания", CatalogParams{Titles: []string{"none"}}, []string{"Виктор"}},
		{"доцент или профессор", CatalogParams{Titles: []string{"docent", "professor"}}, []string{"Анна", "Борис"}},
		{"открыт к предложениям", CatalogParams{OpenOnly: true}, []string{"Борис"}},
		{"h-index от 30 (лучшее из значений)", CatalogParams{HMin: 30}, []string{"Анна"}},
		{"h-index от 5 (Google Scholar тоже считается)", CatalogParams{HMin: 5}, []string{"Анна", "Борис"}},
		{"h-index от 41", CatalogParams{HMin: 41}, nil},
		{"h-index от 0 — без ограничения", CatalogParams{HMin: 0}, []string{"Анна", "Борис", "Виктор"}},
		{"несколько фильтров вместе", CatalogParams{Fields: []string{"1.4"}, Degrees: []string{"candidate", "doctor"}, Region: "54"}, []string{"Анна"}},
		{"несколько фильтров дают пусто", CatalogParams{Fields: []string{"1.2"}, Degrees: []string{"doctor"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.p.Query = m
			res := w.catalog(nil, tc.p)
			if got := catalogNames(res); !sameSet(got, tc.want) || res.Total != len(tc.want) {
				t.Errorf("найдено %v (всего %d), ожидали %v", got, res.Total, tc.want)
			}
		})
	}
}

// Слова поиска: морфология, имя, город, регион, специальность, исключение, стоп-слова, опечатки.
func TestCatalogWords(t *testing.T) {
	w := newWorld(t)
	m := marker()
	w.listed("Анна Ильина", m, "public", func(c *CoreInput) {
		c.Headline = "Исследователь " + m + ": катализаторы и кинетика"
		c.City, c.RegionCode, c.Specialties = "Новосибирск", "54", []string{"1.4.4"}
		c.Degree, c.DegreeInstitution, c.About = DegreeDoctor, "Институт катализа", "Занимаюсь операндо-спектроскопией"
	})
	w.listed("Борис Орлов", m, "public", func(c *CoreInput) {
		c.Headline = "Исследователь " + m + ": неорганическая химия, синтез материалов"
		c.City, c.RegionCode, c.Specialties = "Казань", "16", []string{"1.4.1"}
		c.Degree, c.DegreeInstitution = DegreeCandidate, "Казанский университет"
	})
	w.listed("Виктор Седов", m, "public", func(c *CoreInput) {
		c.Headline = "Исследователь " + m + ": машинное обучение"
	})

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"только метка", m, []string{"Анна Ильина", "Борис Орлов", "Виктор Седов"}},
		{"словоформа: катализатор находит катализаторы", m + " катализатор", []string{"Анна Ильина"}},
		{"имя", m + " Борис", []string{"Борис Орлов"}},
		{"город", m + " новосибирск", []string{"Анна Ильина"}},
		{"название региона", m + " татарстан", []string{"Борис Орлов"}},
		{"название специальности", m + " неорганическая", []string{"Борис Орлов"}},
		{"организация степени", m + " университет", []string{"Борис Орлов"}},
		{"о себе", m + " спектроскопия", []string{"Анна Ильина"}},
		{"два слова — «и»", m + " химия синтез", []string{"Борис Орлов"}},
		{"исключение слова", m + " -синтез", []string{"Анна Ильина", "Виктор Седов"}},
		{"стоп-слова не мешают", m + " и в на", []string{"Анна Ильина", "Борис Орлов", "Виктор Седов"}},
		{"пробелы по краям и внутри", "  " + m + "   борис ", []string{"Борис Орлов"}},
		{"регистр не важен", strings.ToUpper(m) + " БОРИС", []string{"Борис Орлов"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := w.catalog(nil, CatalogParams{Query: tc.query})
			if got := catalogNames(res); !sameSet(got, tc.want) || res.Total != len(tc.want) {
				t.Errorf("найдено %v (всего %d), ожидали %v", got, res.Total, tc.want)
			}
			if res.Fuzzy {
				t.Errorf("точный поиск не должен быть «по похожим словам»")
			}
		})
	}
}

// Если точных совпадений нет, ищутся похожие слова (опечатки) среди тех же видимых профилей.
func TestCatalogFuzzy(t *testing.T) {
	w := newWorld(t)
	m := marker()
	w.listed("Зюзюкина Полина", m, "public", nil)
	w.listed("Зюзюкина Скрытая", m, "hidden", nil)
	res := w.catalog(nil, CatalogParams{Query: "Зюзюкинв"})
	if !res.Fuzzy || res.Total != 1 || res.Items[0].Name != "Зюзюкина Полина" {
		t.Fatalf("похожие слова: fuzzy=%v, %v", res.Fuzzy, catalogNames(res))
	}
	// Фильтры действуют и в запасном поиске.
	if res := w.catalog(nil, CatalogParams{Query: "Зюзюкинв", Region: "02"}); res.Total != 0 || res.Fuzzy {
		t.Errorf("фильтр не применился к похожим словам: %v", catalogNames(res))
	}
	// Совсем непохожее ничего не находит.
	if res := w.catalog(nil, CatalogParams{Query: "Qwertyuiop"}); res.Total != 0 || res.Fuzzy {
		t.Errorf("выдуманное слово: %v", catalogNames(res))
	}
}

// Порядок: недавно обновлённые, h-index, имя, совпадение.
func TestCatalogSorting(t *testing.T) {
	w := newWorld(t)
	m := marker()
	// Профили создаются по очереди, и каждый обновляется позже предыдущего.
	w.listed("Борис", m, "public", func(c *CoreInput) {
		c.HRsci = ptr(5)
		c.Headline = "Исследователь " + m + " кинетика"
	})
	w.clock.Advance(time.Hour)
	w.listed("Анна", m, "public", func(c *CoreInput) {
		c.HRsci = ptr(40)
		c.Headline = "Исследователь " + m + " кинетика кинетика кинетика кинетика"
	})
	w.clock.Advance(time.Hour)
	w.listed("Виктор", m, "public", func(c *CoreInput) { c.Headline = "Исследователь " + m })

	tests := []struct {
		name string
		p    CatalogParams
		want []string
	}{
		{"по умолчанию: недавно обновлённые", CatalogParams{Query: m}, []string{"Виктор", "Анна", "Борис"}},
		{"явно недавно обновлённые", CatalogParams{Query: m, Sort: SortUpdated}, []string{"Виктор", "Анна", "Борис"}},
		{"по h-index, без h-index в конце", CatalogParams{Query: m, Sort: SortHIndex}, []string{"Анна", "Борис", "Виктор"}},
		{"по алфавиту", CatalogParams{Query: m, Sort: SortName}, []string{"Анна", "Борис", "Виктор"}},
		{"по совпадению (слово чаще — выше)", CatalogParams{Query: m + " кинетика", Sort: SortRelevance}, []string{"Анна", "Борис"}},
		{"без слов «по совпадению» — как обновлённые", CatalogParams{Query: m, Sort: SortRelevance}, []string{"Виктор", "Анна", "Борис"}},
		{"со словами по умолчанию — по совпадению", CatalogParams{Query: m + " кинетика"}, []string{"Анна", "Борис"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := w.catalog(nil, tc.p)
			if got := catalogNames(res); !slices.Equal(got, tc.want) {
				t.Errorf("порядок %v, ожидали %v", got, tc.want)
			}
		})
	}
}

func TestCatalogPaging(t *testing.T) {
	w := newWorld(t)
	m := marker()
	for i := range 5 {
		w.listed(fmt.Sprintf("Учёный %d", i), m, "public", nil)
		w.clock.Advance(time.Minute)
	}
	tests := []struct {
		name        string
		limit, off  int
		wantItems   int
		wantTotal   int
		wantLastNam string
	}{
		{"первая страница", 2, 0, 2, 5, ""},
		{"вторая страница", 2, 2, 2, 5, ""},
		{"последняя неполная", 2, 4, 1, 5, ""},
		{"дальше последней: число находит пробный запрос", 2, 10, 0, 5, ""},
		{"отрицательное смещение считается нулём", 2, -3, 2, 5, ""},
		{"предел по умолчанию", 0, 0, 5, 5, ""},
		{"слишком большой предел урезается", 1000, 0, 5, 5, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := w.catalog(nil, CatalogParams{Query: m, Limit: tc.limit, Offset: tc.off})
			if len(res.Items) != tc.wantItems || res.Total != tc.wantTotal {
				t.Errorf("на странице %d, всего %d; ожидали %d и %d", len(res.Items), res.Total, tc.wantItems, tc.wantTotal)
			}
		})
	}
	// Страницы не пересекаются и идут подряд.
	first := catalogNames(w.catalog(nil, CatalogParams{Query: m, Limit: 3}))
	second := catalogNames(w.catalog(nil, CatalogParams{Query: m, Limit: 3, Offset: 3}))
	if len(first) != 3 || len(second) != 2 || slices.ContainsFunc(second, func(n string) bool { return slices.Contains(first, n) }) {
		t.Errorf("страницы: %v и %v", first, second)
	}
	// Предел страницы: не больше 50.
	for i := range 52 {
		w.listed(fmt.Sprintf("Массовка %d", i), m+"b", "public", nil)
	}
	if res := w.catalog(nil, CatalogParams{Query: m + "b", Limit: 500}); len(res.Items) != 50 || res.Total != 52 {
		t.Errorf("предел страницы: %d из %d", len(res.Items), res.Total)
	}
}

// Карточка: всё, что нужно показать, и никаких контактов.
func TestCatalogCard(t *testing.T) {
	w := newWorld(t)
	m := marker()
	p := w.user("Елена Орлова")
	in := goodCore()
	in.Headline = "Старший научный сотрудник " + m
	if _, err := w.svc.SaveCore(bg, p.User, in); err != nil {
		t.Fatal(err)
	}
	w.addItem(p, goodPublication())
	other := goodPublication()
	other.DOI = "10.1234/second"
	w.addItem(p, other)
	w.setVisibility(p, "public", true)
	staff := w.staff("Сотрудник")
	w.listed("Без подробностей", m, "public", nil)

	for _, who := range []*auth.User{nil, &staff.User} {
		res := w.catalog(who, CatalogParams{Query: m, Sort: SortName})
		if len(res.Items) != 2 {
			t.Fatalf("в каталоге %v", catalogNames(res))
		}
		var c CatalogCard
		for _, it := range res.Items {
			if it.Name == "Елена Орлова" {
				c = it
			}
		}
		own, _ := w.svc.Own(bg, p.User)
		if c.ID != own.Profile.ID || c.Headline != in.Headline || c.City != "Новосибирск" || c.Region != "Новосибирская область" ||
			c.Degree != DegreeCandidate || c.AcademicTitle != TitleDocent || !c.OpenToOffers || c.Publications != 2 {
			t.Errorf("карточка: %+v", c)
		}
		if c.HIndex == nil || *c.HIndex != 15 {
			t.Errorf("h-index = %v, ожидали лучшее из значений (15)", c.HIndex)
		}
		if len(c.Specialties) != 2 || c.Specialties[0].Code != "1.4.1" || c.Specialties[1].Code != "1.4.4" || c.Specialties[0].Name == "" {
			t.Errorf("специальности: %+v", c.Specialties)
		}
		raw, _ := json.Marshal(res)
		for _, secret := range []string{"orlova@example.ru", "Orlova@Example.ru", "contact", "email", p.Email, "0000-0002-1825-0097"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(secret)) {
				t.Errorf("в каталоге есть %q: %s", secret, raw)
			}
		}
	}
	// Без подробностей: пустые списки, а не null; нет h-index.
	res := w.catalog(nil, CatalogParams{Query: m + " подробностей"})
	if len(res.Items) != 1 {
		t.Fatalf("профиль без подробностей: %v", catalogNames(res))
	}
	if c := res.Items[0]; c.Specialties == nil || len(c.Specialties) != 0 || c.HIndex != nil || c.Publications != 0 {
		t.Errorf("пустая карточка: %+v", c)
	}
	if raw, _ := json.Marshal(res); strings.Contains(string(raw), `"specialties":null`) || strings.Contains(string(raw), `"items":null`) {
		t.Errorf("пустые списки должны быть [], не null: %s", raw)
	}
	if raw, _ := json.Marshal(w.catalog(nil, CatalogParams{Query: m + " nonexistentxyz"})); strings.Contains(string(raw), `"items":null`) {
		t.Errorf("пустой список должен быть []: %s", raw)
	}
}

func TestCatalogValidation(t *testing.T) {
	w := newWorld(t)
	many := make([]string, 31)
	for i := range many {
		many[i] = "1.4"
	}
	manyDegrees := make([]string, 31)
	for i := range manyDegrees {
		manyDegrees[i] = "doctor"
	}
	tests := []struct {
		name  string
		p     CatalogParams
		field string
	}{
		{"слишком длинный запрос", CatalogParams{Query: strings.Repeat("я", 201)}, "q"},
		{"область не число", CatalogParams{Fields: []string{"химия"}}, "field"},
		{"область с лишней точкой", CatalogParams{Fields: []string{"1..4"}}, "field"},
		{"область из четырёх частей", CatalogParams{Fields: []string{"1.4.4.1"}}, "field"},
		{"область с нуля", CatalogParams{Fields: []string{"0.1"}}, "field"},
		{"слишком много областей", CatalogParams{Fields: many}, "field"},
		{"регион из одного знака", CatalogParams{Region: "5"}, "region"},
		{"регион буквами", CatalogParams{Region: "ab"}, "region"},
		{"неизвестная степень", CatalogParams{Degrees: []string{"phd"}}, "degree"},
		{"слишком много степеней", CatalogParams{Degrees: manyDegrees}, "degree"},
		{"неизвестное звание", CatalogParams{Titles: []string{"academician"}}, "title"},
		{"h-index меньше нуля", CatalogParams{HMin: -1}, "h_min"},
		{"h-index больше предела", CatalogParams{HMin: 301}, "h_min"},
		{"неизвестный порядок", CatalogParams{Sort: "best"}, "sort"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.svc.Catalog(bg, tc.p, nil)
			var verr *auth.ValidationError
			if !errors.As(err, &verr) || verr.Fields[tc.field] == "" {
				t.Errorf("ожидали ошибку поля %q, получили %v", tc.field, err)
			}
		})
	}
	// Границы допустимого проходят.
	for _, p := range []CatalogParams{
		{Query: strings.Repeat("я", 200)}, {Fields: []string{"1", "1.4", "1.4.4", "12.34.56"}}, {Region: "00"}, {HMin: 300}, {Sort: SortHIndex},
		{Degrees: []string{"none", "candidate", "doctor"}}, {Titles: []string{"none", "docent", "professor"}},
	} {
		if _, err := w.svc.Catalog(bg, p, nil); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
}

func TestHTTPCatalog(t *testing.T) {
	a := newAPI(t)
	m := marker()
	a.listed("Анна Ильина", m, "public", func(c *CoreInput) {
		c.Specialties, c.ContactEmail = []string{"1.4.4"}, "anna.private@example.ru"
	})
	a.listed("Для организаций", m, "orgs", nil)
	a.listed("Скрытая", m, "hidden", nil)
	staff := a.staff("Сотрудник")

	r := a.do(nil, "GET", "/api/scientists?q="+m+"&field=1.4&sort=name&limit=5&offset=0&open=0", nil)
	if r.Code != 200 {
		t.Fatalf("аноним: %d %s", r.Code, r.Raw)
	}
	if items := r.json(t)["items"].([]any); len(items) != 1 || items[0].(map[string]any)["name"] != "Анна Ильина" || r.json(t)["total"] != float64(1) {
		t.Errorf("аноним видит: %s", r.Raw)
	}
	if strings.Contains(r.Raw, "anna.private") {
		t.Errorf("в каталоге есть контактная почта: %s", r.Raw)
	}
	if r := a.do(&staff, "GET", "/api/scientists?q="+m, nil); r.Code != 200 || r.json(t)["total"] != float64(2) {
		t.Errorf("сотрудник: %d %s", r.Code, r.Raw)
	}
	if r := a.do(nil, "GET", "/api/scientists?q="+m+"&open=1", nil); r.Code != 200 || r.json(t)["total"] != float64(0) {
		t.Errorf("open=1: %d %s", r.Code, r.Raw)
	}
	if r := a.do(nil, "GET", "/api/scientists?q="+m+"&open=true&h_min=0&degree=none&title=none&region=77", nil); r.Code != 200 {
		t.Errorf("параметры: %d %s", r.Code, r.Raw)
	}
	for _, c := range []struct{ query, field string }{
		{"h_min=много", "h_min"}, {"limit=x", "limit"}, {"offset=y", "offset"}, {"sort=best", "sort"}, {"degree=phd", "degree"}, {"field=abc", "field"},
	} {
		if r := a.do(nil, "GET", "/api/scientists?"+c.query, nil); r.Code != http.StatusUnprocessableEntity || r.fields(t)[c.field] == nil {
			t.Errorf("%s: %d %s", c.query, r.Code, r.Raw)
		}
	}
}

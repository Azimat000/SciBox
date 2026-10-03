package profiles

import (
	"strings"
	"testing"
)

func fullPage() Page {
	return Page{
		Viewer: ViewerInfo{IsOwner: true, CanSeeContacts: true},
		Profile: View{
			Name: "Елена Орлова", Headline: "Старший научный сотрудник", City: "Новосибирск", Region: &Code{Code: "54", Name: "Новосибирская область"},
			About: "Изучаю катализ.", ContactEmail: "orlova@example.ru",
			Degree:        DegreeInfo{Level: DegreeDoctor, Specialty: &Code{Code: "1.4.4", Name: "Физическая химия"}, Year: ptr(2016), Institution: "Институт катализа", Dissertation: "Центры катализа"},
			AcademicTitle: TitleProfessor, AcademicTitleYear: ptr(2021),
			Identifiers: Identifiers{ORCID: "0000-0002-1825-0097", SPIN: "12345678", ScopusID: "57190123456", WosID: "A-1234-2008"},
			HIndex:      HIndex{RSCI: ptr(12), Scopus: ptr(9), WoS: ptr(7), Scholar: ptr(15)},
			Specialties: []Code{{"1.4.1", "Неорганическая химия"}, {"1.4.4", "Физическая химия"}},
			Sections: Sections{
				Education:    []Item{{Kind: KindEducation, ItemFields: ItemFields{Institution: "НГУ", Program: "Аспирантура", YearFrom: ptr(2010), YearTo: ptr(2014)}}},
				Experience:   []Item{{Kind: KindExperience, ItemFields: ItemFields{Organization: "Институт катализа", Position: "СНС", YearFrom: ptr(2018), Description: "Группа"}}},
				Publications: []Item{{Kind: KindPublication, ItemFields: goodPublication().ItemFields}, {Kind: KindPublication, ItemFields: ItemFields{Title: "Препринт", Authors: "Орлова Е.", PubType: "preprint", Year: ptr(2024)}}},
				Grants:       []Item{{Kind: KindGrant, ItemFields: ItemFields{Title: "Грант", Funder: "РНФ", Number: "24-13-1", Role: "lead", YearFrom: ptr(2024), YearTo: ptr(2024)}}},
				Patents:      []Item{{Kind: KindPatent, ItemFields: ItemFields{Title: "Способ", Authors: "Орлова Е.", Number: "RU 1", Office: "Роспатент", PatentType: "invention", Year: ptr(2022)}}},
				Teaching:     []Item{{Kind: KindTeaching, ItemFields: ItemFields{Course: "Физхимия", Institution: "НГУ", Level: "bachelor", YearFrom: ptr(2019), YearTo: ptr(2022)}}},
			},
		},
	}
}

func TestCVDocumentFull(t *testing.T) {
	doc := cvDocument(fullPage(), "SciBox", testNow)
	if doc.Title != "Елена Орлова" || doc.Subtitle != "Старший научный сотрудник" || doc.Footer != "SciBox · 3 октября 2026" {
		t.Errorf("шапка: %+v", doc)
	}
	meta := strings.Join(doc.Meta, "\n")
	for _, want := range []string{
		"Новосибирск, Новосибирская область", "Почта: orlova@example.ru", "ORCID 0000-0002-1825-0097", "SPIN 1234-5678",
		"Scopus ID 57190123456", "WoS ResearcherID A-1234-2008", "h-index: РИНЦ 12  ·  Scopus 9  ·  Web of Science 7  ·  Google Scholar 15",
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("в шапке нет %q:\n%s", want, meta)
		}
	}
	var heads []string
	for _, s := range doc.Sections {
		heads = append(heads, s.Heading)
	}
	want := "О себе|Степень и звание|Научные специальности|Образование|Опыт работы|Публикации|Гранты|Патенты и программы|Преподавание"
	if strings.Join(heads, "|") != want {
		t.Errorf("разделы: %v", heads)
	}
	sec := map[string]int{}
	for i, s := range doc.Sections {
		sec[s.Heading] = i
	}
	deg := doc.Sections[sec["Степень и звание"]].Entries
	if len(deg) != 2 || deg[0].Lead != "Доктор наук, 1.4.4 Физическая химия" || deg[0].Label != "2016" ||
		deg[0].Text != "Диссертация: «Центры катализа»\nИнститут катализа" || deg[1].Lead != "Профессор" || deg[1].Label != "2021" {
		t.Errorf("степень: %+v", deg)
	}
	pubs := doc.Sections[sec["Публикации"]].Entries
	if len(pubs) != 2 || pubs[0].Label != "1." || pubs[1].Label != "2." {
		t.Fatalf("публикации: %+v", pubs)
	}
	wantPub := "Орлова Е. А., Белов И. С. Журнал физической химии, 2023. Т. 97, № 4, С. 512–520. DOI: 10.1234/abc.2023"
	if pubs[0].Text != wantPub {
		t.Errorf("публикация:\n%q\nожидали\n%q", pubs[0].Text, wantPub)
	}
	if pubs[1].Text != "Орлова Е. 2024. (Препринт)" {
		t.Errorf("публикация без издания: %q", pubs[1].Text)
	}
	if got := doc.Sections[sec["Опыт работы"]].Entries[0]; got.Label != "2018 — н. в." || got.Lead != "СНС" || got.Text != "Институт катализа\nГруппа" {
		t.Errorf("опыт: %+v", got)
	}
	if got := doc.Sections[sec["Образование"]].Entries[0]; got.Label != "2010 — 2014" || got.Lead != "НГУ" || got.Text != "Аспирантура" {
		t.Errorf("образование: %+v", got)
	}
	if got := doc.Sections[sec["Гранты"]].Entries[0]; got.Label != "2024" || got.Text != "РНФ, № 24-13-1, Руководитель" {
		t.Errorf("грант: %+v", got)
	}
	if got := doc.Sections[sec["Патенты и программы"]].Entries[0]; got.Label != "2022" || got.Text != "Изобретение, № RU 1, Роспатент\nОрлова Е." {
		t.Errorf("патент: %+v", got)
	}
	if got := doc.Sections[sec["Преподавание"]].Entries[0]; got.Label != "2019 — 2022" || got.Text != "НГУ, Бакалавриат" {
		t.Errorf("преподавание: %+v", got)
	}
	if got := doc.Sections[sec["Научные специальности"]].Paragraph; got != "1.4.1 Неорганическая химия\n1.4.4 Физическая химия" {
		t.Errorf("специальности: %q", got)
	}
}

func TestCVDocumentHidesContactsFromThoseWhoMayNotSeeThem(t *testing.T) {
	page := fullPage()
	page.Viewer = ViewerInfo{CanSeeContacts: false}
	page.Profile.ContactEmail = "" // сервис уже убрал почту; проверяем и флаг, и значение
	if meta := strings.Join(cvDocument(page, "SciBox", testNow).Meta, "\n"); strings.Contains(meta, "Почта") {
		t.Errorf("почта в чужом резюме: %s", meta)
	}
	// Даже если почта по ошибке осталась в данных, без права на контакты она в файл не попадёт.
	page.Profile.ContactEmail = "orlova@example.ru"
	if meta := strings.Join(cvDocument(page, "SciBox", testNow).Meta, "\n"); strings.Contains(meta, "orlova@example.ru") {
		t.Errorf("почта просочилась: %s", meta)
	}
}

func TestCVDocumentEmptyProfile(t *testing.T) {
	doc := cvDocument(Page{Profile: View{Name: "Пустой"}}, "SciBox", testNow)
	if doc.Title != "Пустой" || len(doc.Meta) != 0 || len(doc.Sections) != 0 {
		t.Errorf("%+v", doc)
	}
	pdf, err := renderCV(Page{Profile: View{Name: "Пустой"}}, "SciBox", testNow)
	if err != nil || string(pdf[:5]) != "%PDF-" {
		t.Errorf("PDF пустого профиля: %v", err)
	}
}

func TestCVRenderFull(t *testing.T) {
	pdf, err := renderCV(fullPage(), "SciBox", testNow)
	if err != nil || string(pdf[:5]) != "%PDF-" {
		t.Fatal(err)
	}
}

func TestCVHelpers(t *testing.T) {
	if got := years(nil, nil); got != "" {
		t.Errorf("years(nil,nil) = %q", got)
	}
	if got := years(nil, ptr(2020)); got != "2020" {
		t.Errorf("years(nil,2020) = %q", got)
	}
	if got := years(ptr(2020), ptr(2020)); got != "2020" {
		t.Errorf("одинаковые годы: %q", got)
	}
	if got := location("Москва", &Code{Name: "Москва"}); got != "Москва" {
		t.Errorf("Москва: %q", got)
	}
	if got := location("Казань", &Code{Name: "Республика Татарстан"}); got != "Казань, Республика Татарстан" {
		t.Errorf("Казань: %q", got)
	}
	if got := location("", &Code{Name: "Томская область"}); got != "Томская область" {
		t.Errorf("без города: %q", got)
	}
	if got := location("Томск", nil); got != "Томск" {
		t.Errorf("без региона: %q", got)
	}
	if yearOf(nil) != "" || yearOf(ptr(2020)) != "2020" || hLine("X", nil) != "" || hLine("X", ptr(3)) != "X 3" {
		t.Error("пустые значения")
	}
	if spinText("12345678") != "1234-5678" || spinText("1234") != "1234" || spinText("1234567a") != "1234567a" {
		t.Error("spinText")
	}
	if wrapQuote("") != "" || wrapQuote("а") != "а»" {
		t.Error("wrapQuote")
	}
	if withDot("") != "" || withDot("Журнал.") != "Журнал." || withDot("Журнал") != "Журнал." {
		t.Error("withDot")
	}
	if pubTypeNote("article") != "" || pubTypeNote("book") != "(Книга)" {
		t.Error("pubTypeNote")
	}
	if prefixed("a ", "") != "" || prefixed("a ", "b") != "a b" {
		t.Error("prefixed")
	}
	// Степень без специальности и диссертации.
	got := degreeEntries(View{Degree: DegreeInfo{Level: DegreeCandidate}})
	if len(got) != 1 || got[0].Lead != "Кандидат наук" || got[0].Text != "" || got[0].Label != "" {
		t.Errorf("степень без подробностей: %+v", got)
	}
	if got := degreeEntries(View{AcademicTitle: TitleDocent}); len(got) != 1 || got[0].Lead != "Доцент" {
		t.Errorf("только звание: %+v", got)
	}
	if got := degreeEntries(View{Degree: DegreeInfo{Level: DegreeNone}, AcademicTitle: TitleNone}); len(got) != 0 {
		t.Errorf("ничего: %+v", got)
	}
}

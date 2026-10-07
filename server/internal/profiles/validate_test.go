package profiles

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeORCID(t *testing.T) {
	tests := []struct {
		in, want, msg string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"0000-0002-1825-0097", "0000-0002-1825-0097", ""},
		{"0000000218250097", "0000-0002-1825-0097", ""},
		{"0000 0002 1825 0097", "0000-0002-1825-0097", ""},
		{"https://orcid.org/0000-0002-1825-0097", "0000-0002-1825-0097", ""},
		{"HTTP://www.orcid.org/0000-0002-1825-0097", "0000-0002-1825-0097", ""},
		{"0000-0002-1694-233x", "0000-0002-1694-233X", ""}, // контрольный знак X
		{"0000-0002-1825-0098", "", msgORCIDChecksum},
		{"0000-0002-1825-009", "", msgORCIDInvalid},
		{"0000-0002-1825-00977", "", msgORCIDInvalid},
		{"abcd-0002-1825-0097", "", msgORCIDInvalid},
		{"0000-0002-1825-00X7", "", msgORCIDInvalid},
	}
	for _, tc := range tests {
		got, msg := normalizeORCID(tc.in)
		if got != tc.want || msg != tc.msg {
			t.Errorf("normalizeORCID(%q) = %q, %q; ожидали %q, %q", tc.in, got, msg, tc.want, tc.msg)
		}
	}
}

func TestORCIDCheckDigit(t *testing.T) {
	// Известные настоящие ORCID iD из документации (образцы: Josiah Carberry и др.).
	for _, id := range []string{"0000-0002-1825-0097", "0000-0001-5109-3700", "0000-0002-1694-233X", "0000-0003-1415-9269"} {
		if got := orcidCheckDigit(id); got != id[len(id)-1] {
			t.Errorf("контрольный знак %s: %c", id, got)
		}
	}
}

func TestValidateCoreValid(t *testing.T) {
	f, errs := validateCore(goodCore(), testNow)
	if len(errs) != 0 {
		t.Fatalf("ошибки: %v", errs)
	}
	if f.orcid != "0000-0002-1825-0097" || f.spin != "12345678" || f.scopusID != "57190123456" || f.wosID != "A-1234-2008" {
		t.Errorf("идентификаторы: %+v", f)
	}
	if f.contactEmail != "orlova@example.ru" || *f.degreeYear != 2016 || *f.hRsci != 12 || len(f.specialties) != 2 {
		t.Errorf("поля: %+v", f)
	}
	if *f.regionCode != "54" || *f.degreeSpecialty != "1.4.4" {
		t.Errorf("справочники: %+v", f)
	}
}

func TestValidateCoreEmptyIsFine(t *testing.T) {
	f, errs := validateCore(CoreInput{}, testNow)
	if len(errs) != 0 {
		t.Fatalf("пустой профиль должен сохраняться: %v", errs)
	}
	if f.degree != DegreeNone || f.academicTitle != TitleNone || f.regionCode != nil || f.hRsci != nil || len(f.specialties) != 0 {
		t.Errorf("пустые значения: %+v", f)
	}
}

func TestValidateCoreDropsDetailsWithoutDegree(t *testing.T) {
	in := goodCore()
	in.Degree, in.AcademicTitle = DegreeNone, TitleNone
	f, errs := validateCore(in, testNow)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if f.degreeSpecialty != nil || f.degreeYear != nil || f.degreeInstitution != "" || f.dissertation != "" || f.academicTitleYear != nil {
		t.Errorf("подробности без степени и звания должны исчезнуть: %+v", f)
	}
}

func TestValidateCoreErrors(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	tests := []struct {
		name   string
		edit   func(*CoreInput)
		field  string
		substr string
	}{
		{"должность длинная", func(c *CoreInput) { c.Headline = long(201) }, "headline", "не больше 200"},
		{"должность с управляющим знаком", func(c *CoreInput) { c.Headline = "a\x00b" }, "headline", "недопустимые"},
		{"город без букв", func(c *CoreInput) { c.City = "12345" }, "city", "буквы"},
		{"город длинный", func(c *CoreInput) { c.City = long(101) }, "city", "не больше 100"},
		{"о себе длинно", func(c *CoreInput) { c.About = long(3001) }, "about", "не больше 3000"},
		{"о себе с управляющим", func(c *CoreInput) { c.About = "a\x07b" }, "about", "недопустимые"},
		{"степень неизвестная", func(c *CoreInput) { c.Degree = "phd" }, "degree", "из списка"},
		{"звание неизвестное", func(c *CoreInput) { c.AcademicTitle = "academician" }, "academic_title", "из списка"},
		{"год степени в будущем", func(c *CoreInput) { c.DegreeYear = ptr(2027) }, "degree_year", "от 1950 до 2026"},
		{"год степени давно", func(c *CoreInput) { c.DegreeYear = ptr(1949) }, "degree_year", "от 1950"},
		{"год звания", func(c *CoreInput) { c.AcademicTitleYear = ptr(3000) }, "academic_title_year", "от 1950"},
		{"организация степени длинная", func(c *CoreInput) { c.DegreeInstitution = long(201) }, "degree_institution", "не больше 200"},
		{"тема диссертации длинная", func(c *CoreInput) { c.Dissertation = long(501) }, "dissertation_title", "не больше 500"},
		{"ORCID", func(c *CoreInput) { c.ORCID = "nope" }, "orcid", "ORCID"},
		{"ORCID сумма", func(c *CoreInput) { c.ORCID = "0000-0002-1825-0098" }, "orcid", "ошибка"},
		{"SPIN короткий", func(c *CoreInput) { c.SPIN = "123" }, "spin", "SPIN"},
		{"SPIN буквы", func(c *CoreInput) { c.SPIN = "12ab5678" }, "spin", "SPIN"},
		{"Scopus короткий", func(c *CoreInput) { c.ScopusID = "1234567" }, "scopus_id", "Scopus"},
		{"Scopus длинный", func(c *CoreInput) { c.ScopusID = "1234567890123" }, "scopus_id", "Scopus"},
		{"WoS", func(c *CoreInput) { c.WosID = "1234-2008" }, "wos_id", "ResearcherID"},
		{"WoS год", func(c *CoreInput) { c.WosID = "A-1234-1850" }, "wos_id", "ResearcherID"},
		{"h-index РИНЦ", func(c *CoreInput) { c.HRsci = ptr(-1) }, "h_rsci", "от 0 до 300"},
		{"h-index Scopus", func(c *CoreInput) { c.HScopus = ptr(301) }, "h_scopus", "от 0 до 300"},
		{"h-index WoS", func(c *CoreInput) { c.HWos = ptr(1000) }, "h_wos", "от 0 до 300"},
		{"h-index Scholar", func(c *CoreInput) { c.HScholar = ptr(-5) }, "h_scholar", "от 0 до 300"},
		{"почта", func(c *CoreInput) { c.ContactEmail = "not-an-email" }, "contact_email", "опечатк"},
		{"специальностей много", func(c *CoreInput) { c.Specialties = []string{"1.1.1", "1.1.2", "1.1.3", "1.1.4", "1.1.5", "1.1.6"} }, "specialties", "не больше пяти"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := goodCore()
			tc.edit(&in)
			_, errs := validateCore(in, testNow)
			if !strings.Contains(errs[tc.field], tc.substr) {
				t.Errorf("поле %s: %q (все ошибки: %v), ожидали «%s»", tc.field, errs[tc.field], errs, tc.substr)
			}
			if len(errs) != 1 {
				t.Errorf("ожидали ровно одну ошибку, получили %v", errs)
			}
		})
	}
}

func TestValidateCoreSpecialtiesDeduped(t *testing.T) {
	in := goodCore()
	in.Specialties = []string{" 1.4.4 ", "1.4.4", "", "1.4.1", "1.4.1"}
	f, errs := validateCore(in, testNow)
	if len(errs) != 0 || strings.Join(f.specialties, ",") != "1.4.4,1.4.1" {
		t.Errorf("%v %v", f.specialties, errs)
	}
}

func validItem(kind string) ItemFields {
	switch kind {
	case KindEducation:
		return ItemFields{Institution: "НГУ", Program: "Аспирантура, физика", YearFrom: ptr(2010), YearTo: ptr(2014), Description: "Диссертация по катализу"}
	case KindExperience:
		return ItemFields{Organization: "Институт катализа", Position: "Старший научный сотрудник", YearFrom: ptr(2018), Description: "Руководство группой"}
	case KindPublication:
		return goodPublication().ItemFields
	case KindGrant:
		return ItemFields{Title: "Катализаторы нового поколения", Funder: "РНФ", Number: "24-13-00123", Role: "lead", YearFrom: ptr(2024), YearTo: ptr(2026)}
	case KindPatent:
		return ItemFields{Title: "Способ получения катализатора", Authors: "Орлова Е. А.", Number: "RU 2 745 123", Office: "Роспатент", PatentType: "invention", Year: ptr(2022)}
	case KindTeaching:
		return ItemFields{Course: "Физическая химия", Institution: "НГУ", Level: "bachelor", YearFrom: ptr(2019), YearTo: ptr(2022)}
	}
	return ItemFields{}
}

func TestValidateItemValid(t *testing.T) {
	for _, kind := range Kinds {
		t.Run(kind, func(t *testing.T) {
			f, errs := validateItem(kind, validItem(kind), testNow)
			if len(errs) != 0 {
				t.Fatalf("ошибки: %v", errs)
			}
			if f.sortYear == 0 {
				t.Errorf("год для порядка не посчитан")
			}
		})
	}
}

func TestValidateItemSortYear(t *testing.T) {
	ongoing := validItem(KindExperience)
	ongoing.YearTo = nil
	finished := validItem(KindExperience)
	finished.YearTo = ptr(2020)
	if f, _ := validateItem(KindExperience, ongoing, testNow); f.sortYear != sortOngoing {
		t.Errorf("незавершённое: %d", f.sortYear)
	}
	if f, _ := validateItem(KindExperience, finished, testNow); f.sortYear != 2020 {
		t.Errorf("завершённое: %d", f.sortYear)
	}
	if f, _ := validateItem(KindPublication, validItem(KindPublication), testNow); f.sortYear != 2023 {
		t.Errorf("публикация: %d", f.sortYear)
	}
	if f, _ := validateItem(KindPatent, validItem(KindPatent), testNow); f.sortYear != 2022 {
		t.Errorf("патент: %d", f.sortYear)
	}
	// Нет года начала: порядок 0 (ошибка и так будет).
	noFrom := validItem(KindEducation)
	noFrom.YearFrom = nil
	if f, errs := validateItem(KindEducation, noFrom, testNow); f.sortYear != 0 || errs["year_from"] == "" {
		t.Errorf("без года начала: %d %v", f.sortYear, errs)
	}
}

func TestValidateItemDropsForeignFields(t *testing.T) {
	in := validItem(KindEducation)
	in.Title, in.DOI, in.Funder, in.Course = "чужое", "10.1234/x", "чужое", "чужое"
	f, errs := validateItem(KindEducation, in, testNow)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if f.fields.Title != "" || f.fields.DOI != "" || f.fields.Funder != "" || f.fields.Course != "" {
		t.Errorf("поля чужого вида должны отбрасываться: %+v", f.fields)
	}
}

func TestValidateItemErrors(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	type edit func(*ItemFields)
	tests := []struct {
		kind   string
		name   string
		edit   edit
		field  string
		substr string
	}{
		{KindEducation, "нет учебного заведения", func(f *ItemFields) { f.Institution = " " }, "institution", "Укажите"},
		{KindEducation, "заведение без букв", func(f *ItemFields) { f.Institution = "123" }, "institution", "буквы"},
		{KindEducation, "заведение длинное", func(f *ItemFields) { f.Institution = long(201) }, "institution", "не больше 200"},
		{KindEducation, "программа длинная", func(f *ItemFields) { f.Program = long(201) }, "program", "не больше 200"},
		{KindEducation, "нет года начала", func(f *ItemFields) { f.YearFrom = nil }, "year_from", "Укажите год"},
		{KindEducation, "год начала давно", func(f *ItemFields) { f.YearFrom = ptr(1900) }, "year_from", "от 1930"},
		{KindEducation, "окончание раньше начала", func(f *ItemFields) { f.YearTo = ptr(2000) }, "year_to", "раньше"},
		{KindEducation, "описание с управляющим", func(f *ItemFields) { f.Description = "a\x00" }, "description", "недопустимые"},
		{KindEducation, "описание длинное", func(f *ItemFields) { f.Description = long(1001) }, "description", "не больше 1000"},
		{KindExperience, "нет организации", func(f *ItemFields) { f.Organization = "" }, "organization", "Укажите организацию"},
		{KindExperience, "нет должности", func(f *ItemFields) { f.Position = "" }, "position", "Укажите должность"},
		{KindExperience, "год в будущем", func(f *ItemFields) { f.YearFrom = ptr(2027) }, "year_from", "до 2026"},
		{KindPublication, "нет названия", func(f *ItemFields) { f.Title = "" }, "title", "Укажите название"},
		{KindPublication, "название длинное", func(f *ItemFields) { f.Title = long(501) }, "title", "не больше 500"},
		{KindPublication, "нет авторов", func(f *ItemFields) { f.Authors = "" }, "authors", "Укажите авторов"},
		{KindPublication, "авторы длинные", func(f *ItemFields) { f.Authors = long(1001) }, "authors", "не больше 1000"},
		{KindPublication, "издание длинное", func(f *ItemFields) { f.Venue = long(301) }, "venue", "не больше 300"},
		{KindPublication, "нет типа", func(f *ItemFields) { f.PubType = "" }, "pub_type", "Выберите тип"},
		{KindPublication, "тип неизвестный", func(f *ItemFields) { f.PubType = "poem" }, "pub_type", "из списка"},
		{KindPublication, "нет года", func(f *ItemFields) { f.Year = nil }, "year", "Укажите год"},
		{KindPublication, "год далеко в будущем", func(f *ItemFields) { f.Year = ptr(2028) }, "year", "до 2027"},
		{KindPublication, "том длинный", func(f *ItemFields) { f.Volume = long(41) }, "volume", "не больше 40"},
		{KindPublication, "выпуск длинный", func(f *ItemFields) { f.Issue = long(41) }, "issue", "не больше 40"},
		{KindPublication, "страницы длинные", func(f *ItemFields) { f.Pages = long(41) }, "pages", "не больше 40"},
		{KindPublication, "источник неизвестный", func(f *ItemFields) { f.Source = "scopus" }, "source", "из списка"},
		{KindPublication, "DOI неверный", func(f *ItemFields) { f.DOI = "nature" }, "doi", "DOI записывают"},
		{KindPublication, "ссылка без http", func(f *ItemFields) { f.URL = "ftp://example.ru/x" }, "url", "http"},
		{KindPublication, "ссылка без адреса", func(f *ItemFields) { f.URL = "https://" }, "url", "http"},
		{KindPublication, "ссылка с пробелом", func(f *ItemFields) { f.URL = "https://example.ru/a b" }, "url", "http"},
		{KindPublication, "ссылка с управляющим", func(f *ItemFields) { f.URL = "https://example.ru/\x01" }, "url", "http"},
		{KindPublication, "ссылка длинная", func(f *ItemFields) { f.URL = "https://example.ru/" + strings.Repeat("a", 500) }, "url", "не больше 500"},
		{KindGrant, "нет названия", func(f *ItemFields) { f.Title = "" }, "title", "Укажите название"},
		{KindGrant, "нет фонда", func(f *ItemFields) { f.Funder = "" }, "funder", "Укажите, кто"},
		{KindGrant, "номер длинный", func(f *ItemFields) { f.Number = long(101) }, "number", "не больше 100"},
		{KindGrant, "нет роли", func(f *ItemFields) { f.Role = "" }, "role", "Выберите роль"},
		{KindGrant, "роль неизвестная", func(f *ItemFields) { f.Role = "boss" }, "role", "из списка"},
		{KindGrant, "год начала давно", func(f *ItemFields) { f.YearFrom = ptr(1980) }, "year_from", "от 1990"},
		{KindGrant, "окончание раньше начала", func(f *ItemFields) { f.YearTo = ptr(2020) }, "year_to", "раньше"},
		{KindPatent, "нет названия", func(f *ItemFields) { f.Title = "" }, "title", "Укажите название"},
		{KindPatent, "нет номера", func(f *ItemFields) { f.Number = "" }, "number", "Укажите номер"},
		{KindPatent, "авторы длинные", func(f *ItemFields) { f.Authors = long(501) }, "authors", "не больше 500"},
		{KindPatent, "ведомство длинное", func(f *ItemFields) { f.Office = long(101) }, "office", "не больше 100"},
		{KindPatent, "нет вида", func(f *ItemFields) { f.PatentType = "" }, "patent_type", "Выберите вид"},
		{KindPatent, "вид неизвестный", func(f *ItemFields) { f.PatentType = "trademark" }, "patent_type", "из списка"},
		{KindPatent, "нет года", func(f *ItemFields) { f.Year = nil }, "year", "Укажите год"},
		{KindTeaching, "нет курса", func(f *ItemFields) { f.Course = "" }, "course", "Укажите название курса"},
		{KindTeaching, "нет заведения", func(f *ItemFields) { f.Institution = "" }, "institution", "Укажите учебное"},
		{KindTeaching, "нет уровня", func(f *ItemFields) { f.Level = "" }, "level", "Выберите уровень"},
		{KindTeaching, "уровень неизвестный", func(f *ItemFields) { f.Level = "school" }, "level", "из списка"},
		{KindTeaching, "нет года начала", func(f *ItemFields) { f.YearFrom = nil }, "year_from", "Укажите год"},
	}
	for _, tc := range tests {
		t.Run(tc.kind+"/"+tc.name, func(t *testing.T) {
			in := validItem(tc.kind)
			tc.edit(&in)
			_, errs := validateItem(tc.kind, in, testNow)
			if !strings.Contains(errs[tc.field], tc.substr) {
				t.Errorf("поле %s: %q (все ошибки: %v), ожидали «%s»", tc.field, errs[tc.field], errs, tc.substr)
			}
			if len(errs) != 1 {
				t.Errorf("ожидали ровно одну ошибку, получили %v", errs)
			}
		})
	}
}

func TestValidateItemUnknownKind(t *testing.T) {
	_, errs := validateItem("hobby", ItemFields{}, testNow)
	if errs["kind"] != msgKindInvalid {
		t.Errorf("%v", errs)
	}
}

func TestValidateItemNormalizesDOIAndSource(t *testing.T) {
	in := validItem(KindPublication)
	in.DOI = " https://doi.org/10.1234/ABC.2023. "
	in.Source = ""
	f, errs := validateItem(KindPublication, in, testNow)
	if len(errs) != 0 || f.fields.DOI != "10.1234/abc.2023" || f.fields.Source != SourceManual {
		t.Errorf("%+v %v", f.fields, errs)
	}
	in.Source = SourceCrossref
	if f, _ := validateItem(KindPublication, in, testNow); f.fields.Source != SourceCrossref {
		t.Errorf("источник: %q", f.fields.Source)
	}
}

func TestValidateYearBoundsFollowTheClock(t *testing.T) {
	in := validItem(KindPublication)
	in.Year = ptr(2027)
	if _, errs := validateItem(KindPublication, in, testNow); errs["year"] != "" {
		t.Errorf("следующий год допустим: %v", errs)
	}
	later := testNow.AddDate(1, 0, 0)
	in.Year = ptr(2028)
	if _, errs := validateItem(KindPublication, in, later); errs["year"] != "" {
		t.Errorf("через год следующий — уже 2028: %v", errs)
	}
}

// Навыки (D-136): пробелы убираются, пустые и повторы (без учёта регистра) отбрасываются, порядок сохраняется.
func TestValidateSkills(t *testing.T) {
	long := strings.Repeat("я", MaxSkillLen+1)
	many := make([]string, MaxSkills+1)
	for i := range many {
		many[i] = fmt.Sprintf("навык %d", i)
	}
	exact := many[:MaxSkills]
	tests := []struct {
		name string
		in   []string
		want []string
		msg  string
	}{
		{"нет навыков", nil, []string{}, ""},
		{"чистка", []string{"  ПЦР  в реальном   времени ", "", "  ", "Python", "python", "ПЦР в реальном времени"}, []string{"ПЦР в реальном времени", "Python"}, ""},
		{"ровно предел длины", []string{strings.Repeat("я", MaxSkillLen)}, []string{strings.Repeat("я", MaxSkillLen)}, ""},
		{"слишком длинный", []string{"Excel", long}, []string{}, fmt.Sprintf(msgSkillLong, long, MaxSkillLen)},
		{"управляющие символы", []string{"Word\x00"}, []string{}, msgSkillInvalid},
		{"ровно предел числа", exact, exact, ""},
		{"слишком много", many, []string{}, fmt.Sprintf(msgSkillsMany, MaxSkills)},
		{"повторы не считаются в предел", append(slices.Clone(exact), "НАВЫК 0"), exact, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := fieldErrors{}
			got := errs.skills("general_skills", tc.in)
			if !slices.Equal(got, tc.want) || errs["general_skills"] != tc.msg {
				t.Errorf("получили %q, %q; ожидали %q, %q", got, errs["general_skills"], tc.want, tc.msg)
			}
		})
	}
	// Ошибка привязана к своему списку.
	in := goodCore()
	in.ResearchSkills = many
	if _, errs := validateCore(in, testNow); errs["research_skills"] == "" || errs["general_skills"] != "" {
		t.Errorf("ошибки: %v", errs)
	}
}

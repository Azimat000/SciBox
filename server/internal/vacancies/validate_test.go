package vacancies

import (
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

func check(t *testing.T, in Input, positionType string, mode checkMode) (fields, map[string]string) {
	t.Helper()
	return validate(in, positionType, mode, today)
}

func TestGoodInputPassesEveryMode(t *testing.T) {
	for _, mode := range []checkMode{modeDraft, modeStrict, modePublish} {
		f, errs := check(t, goodInput(), TypeResearch, mode)
		if len(errs) != 0 {
			t.Fatalf("mode %d: %v", mode, errs)
		}
		if f.title == "" || f.deadline == nil || *f.rate != 100 || *f.contractMonths != 36 || len(f.specialties) != 1 {
			t.Errorf("mode %d: fields not filled: %+v", mode, f)
		}
		if f.housing != HousingNone || f.degree != DegreeNone || f.academicTitle != TitleNone {
			t.Errorf("defaults not applied: %+v", f)
		}
	}
}

// Черновик может быть почти пустым: нужны только название (и должность, её проверяет сервис).
func TestDraftNeedsOnlyATitle(t *testing.T) {
	if _, errs := check(t, Input{Title: "Вакансия"}, "", modeDraft); len(errs) != 0 {
		t.Errorf("draft with a title only: %v", errs)
	}
	_, errs := check(t, Input{}, "", modeDraft)
	if errs["title"] != msgTitleRequired || len(errs) != 1 {
		t.Errorf("empty draft: %v", errs)
	}
}

// Что обязательно при публикации для каждого типа должности: каждая строка — поле, которое убрали, и ожидаемый отказ.
func TestStrictRequiredFieldsByType(t *testing.T) {
	type row struct {
		typ    string
		code   string
		mutate func(in *Input)
		field  string
	}
	teaching := func(in *Input) {
		in.PositionCode, in.Focus = "docent", "Общая физика, электродинамика"
	}
	early := func(in *Input) {
		in.PositionCode, in.Focus, in.RatePercent = "phd_student", "Операндо-спектроскопия катализаторов", nil
	}
	mgmt := func(in *Input) { in.PositionCode, in.CareerLevel, in.Specialties = "grant_manager", nil, nil }
	rows := []row{
		{TypeResearch, "", func(in *Input) { in.Summary = "" }, "summary"},
		{TypeResearch, "", func(in *Input) { in.Summary = "Коротко" }, "summary"},
		{TypeResearch, "", func(in *Input) { in.Description = "" }, "description"},
		{TypeResearch, "", func(in *Input) { in.CareerLevel = nil }, "career_level"},
		{TypeResearch, "", func(in *Input) { in.WorkFormat = "" }, "work_format"},
		{TypeResearch, "", func(in *Input) { in.City = "" }, "city"},
		{TypeResearch, "", func(in *Input) { in.RegionCode = "" }, "region_code"},
		{TypeResearch, "", func(in *Input) { in.RatePercent = nil }, "rate_percent"},
		{TypeResearch, "", func(in *Input) { in.ContractType = "" }, "contract_type"},
		{TypeResearch, "", func(in *Input) { in.ContractMonth = nil }, "contract_months"},
		{TypeResearch, "", func(in *Input) { in.Specialties = nil }, "specialties"},
		{TypeResearch, "", func(in *Input) { in.IsCompetition, in.Deadline = true, "" }, "deadline"},
		{TypeTeaching, "", func(in *Input) { teaching(in); in.Focus = "" }, "focus"},
		{TypeTeaching, "", func(in *Input) { teaching(in); in.RatePercent = nil }, "rate_percent"},
		{TypeTeaching, "", func(in *Input) { teaching(in); in.Specialties = nil }, "specialties"},
		{TypeEarlyCareer, "", func(in *Input) { early(in); in.Focus = "" }, "focus"},
		{TypeEarlyCareer, "", func(in *Input) { early(in); in.CareerLevel = nil }, "career_level"},
		{TypeEarlyCareer, "", func(in *Input) { early(in); in.Specialties = nil }, "specialties"},
		{TypeManagement, "", func(in *Input) { mgmt(in); in.RatePercent = nil }, "rate_percent"},
	}
	for i, r := range rows {
		in := goodInput()
		switch r.typ {
		case TypeTeaching:
			teaching(&in)
		case TypeEarlyCareer:
			early(&in)
		case TypeManagement:
			mgmt(&in)
		}
		// Сначала убеждаемся, что без правки вакансия проходит, затем правим.
		if _, errs := check(t, in, r.typ, modeStrict); len(errs) != 0 {
			t.Fatalf("row %d (%s): the base input must be valid, got %v", i, r.typ, errs)
		}
		in = goodInput()
		r.mutate(&in)
		if _, errs := check(t, in, r.typ, modeStrict); errs[r.field] == "" {
			t.Errorf("row %d (%s, %s): strict mode must reject, got %v", i, r.typ, r.field, errs)
		}
		if _, errs := check(t, in, r.typ, modeDraft); errs[r.field] != "" && r.field != "deadline" {
			t.Errorf("row %d (%s, %s): a draft must accept an empty field, got %v", i, r.typ, r.field, errs)
		}
	}
}

// Для вакансии без обязательных полей управления (грант-менеджер) уровень и специальности не нужны, зато ставка нужна.
func TestManagementNeedsNoLevelNoSpecialties(t *testing.T) {
	in := goodInput()
	in.PositionCode, in.CareerLevel, in.Specialties = "grant_manager", nil, nil
	if _, errs := check(t, in, TypeManagement, modeStrict); len(errs) != 0 {
		t.Errorf("%v", errs)
	}
}

func TestRemoteNeedsNoPlace(t *testing.T) {
	in := goodInput()
	in.WorkFormat, in.City, in.RegionCode = FormatRemote, "", ""
	if _, errs := check(t, in, TypeResearch, modeStrict); len(errs) != 0 {
		t.Errorf("remote work needs no city or region: %v", errs)
	}
}

func TestEarlyCareerNeedsNoRate(t *testing.T) {
	in := goodInput()
	in.PositionCode, in.Focus, in.RatePercent = "postdoc", "Нейровизуализация", nil
	if _, errs := check(t, in, TypeEarlyCareer, modeStrict); len(errs) != 0 {
		t.Errorf("%v", errs)
	}
}

// Что допустимо только для некоторых типов должностей.
func TestTypeRestrictions(t *testing.T) {
	cases := []struct {
		name   string
		typ    string
		mutate func(in *Input)
		field  string
		ok     bool
	}{
		{"competition for research", TypeResearch, func(in *Input) { in.IsCompetition = true }, "is_competition", true},
		{"competition for teaching", TypeTeaching, func(in *Input) { in.IsCompetition = true }, "is_competition", true},
		{"competition for early career", TypeEarlyCareer, func(in *Input) { in.IsCompetition = true }, "is_competition", false},
		{"competition for management", TypeManagement, func(in *Input) { in.IsCompetition = true }, "is_competition", false},
		{"title for teaching", TypeTeaching, func(in *Input) { in.AcademicTitle = TitleProfessor }, "title_required", true},
		{"title for research", TypeResearch, func(in *Input) { in.AcademicTitle = TitleDocent }, "title_required", false},
		{"title for early career", TypeEarlyCareer, func(in *Input) { in.AcademicTitle = TitleDocent }, "title_required", false},
		{"title for management", TypeManagement, func(in *Input) { in.AcademicTitle = TitleDocent }, "title_required", false},
		{"no title is fine everywhere", TypeManagement, func(in *Input) { in.AcademicTitle = TitleNone }, "title_required", true},
	}
	for _, c := range cases {
		in := goodInput()
		c.mutate(&in)
		_, errs := check(t, in, c.typ, modeDraft)
		if (errs[c.field] == "") != c.ok {
			t.Errorf("%s: ok=%v, errors %v", c.name, c.ok, errs)
		}
	}
	// Тип неизвестен (должность не из справочника): правила по типу не применяются, об ошибке скажет сервис.
	in := goodInput()
	in.IsCompetition, in.AcademicTitle = true, TitleDocent
	if _, errs := check(t, in, "", modeDraft); errs["is_competition"] != "" || errs["title_required"] != "" {
		t.Errorf("unknown type: %v", errs)
	}
}

func TestFieldFormats(t *testing.T) {
	long := func(n int) string { return strings.Repeat("я", n) }
	cases := []struct {
		name   string
		mutate func(in *Input)
		field  string
		msg    string
	}{
		{"title short", func(in *Input) { in.Title = "аб" }, "title", msgTitleShort},
		{"title long", func(in *Input) { in.Title = long(201) }, "title", msgTitleLong},
		{"title no letters", func(in *Input) { in.Title = "12345" }, "title", msgTitleInvalid},
		{"title control char", func(in *Input) { in.Title = "Лаборант\x07 науки" }, "title", msgTitleInvalid},
		{"summary long", func(in *Input) { in.Summary = long(601) }, "summary", msgSummaryLong},
		{"summary control char", func(in *Input) { in.Summary = "плохой\x00 текст" }, "summary", msgTextInvalid},
		{"description long", func(in *Input) { in.Description = long(10001) }, "description", msgDescLong},
		{"requirements long", func(in *Input) { in.Requirements = long(5001) }, "requirements", msgReqLong},
		{"focus long", func(in *Input) { in.Focus = long(301) }, "focus", msgFocusLong},
		{"focus control", func(in *Input) { in.Focus = "тема\n\x01" }, "focus", msgTextInvalid},
		{"level 0", func(in *Input) { in.CareerLevel = ptr(0) }, "career_level", msgLevelInvalid},
		{"level 5", func(in *Input) { in.CareerLevel = ptr(5) }, "career_level", msgLevelInvalid},
		{"format unknown", func(in *Input) { in.WorkFormat = "teleport" }, "work_format", msgFormatInvalid},
		{"city long", func(in *Input) { in.City = long(101) }, "city", msgCityLong},
		{"city no letters", func(in *Input) { in.City = "---" }, "city", msgCityInvalid},
		{"housing unknown", func(in *Input) { in.Housing = "castle" }, "housing", msgHousingInvalid},
		{"rate 60", func(in *Input) { in.RatePercent = ptr(60) }, "rate_percent", msgRateInvalid},
		{"salary zero", func(in *Input) { in.SalaryFrom = ptr(0) }, "salary_from", msgSalaryInvalid},
		{"salary negative", func(in *Input) { in.SalaryTo = ptr(-5) }, "salary_to", msgSalaryInvalid},
		{"salary huge", func(in *Input) { in.SalaryTo = ptr(MaxSalary + 1) }, "salary_to", msgSalaryBig},
		{"salary order", func(in *Input) { in.SalaryFrom, in.SalaryTo = ptr(90000), ptr(60000) }, "salary_to", msgSalaryOrder},
		{"contract unknown", func(in *Input) { in.ContractType = "forever" }, "contract_type", msgContractInvalid},
		{"months 0", func(in *Input) { in.ContractMonth = ptr(0) }, "contract_months", msgMonthsInvalid},
		{"months 121", func(in *Input) { in.ContractMonth = ptr(121) }, "contract_months", msgMonthsInvalid},
		{"months for permanent", func(in *Input) { in.ContractType = ContractPermanent }, "contract_months", msgMonthsOnlyFixed},
		{"funding unknown", func(in *Input) { in.FundingSource = "lottery" }, "funding_source", msgFundingInvalid},
		{"funding note long", func(in *Input) { in.FundingNote = long(201) }, "funding_note", msgFundingNoteLong},
		{"degree unknown", func(in *Input) { in.Degree = "wizard" }, "degree_required", msgDegreeInvalid},
		{"title required unknown", func(in *Input) { in.AcademicTitle = "king" }, "title_required", msgTitleReqInvalid},
		{"deadline format", func(in *Input) { in.Deadline = "01.12.2026" }, "deadline", msgDeadlineInvalid},
		{"deadline impossible date", func(in *Input) { in.Deadline = "2026-02-30" }, "deadline", msgDeadlineInvalid},
		{"deadline far future", func(in *Input) { in.Deadline = "2200-01-01" }, "deadline", msgDeadlineInvalid},
		{"too many specialties", func(in *Input) { in.Specialties = []string{"1.1.1", "1.1.2", "1.1.3", "1.1.4", "1.1.5", "1.1.6"} }, "specialties", msgSpecsMany},
	}
	for _, c := range cases {
		in := goodInput()
		c.mutate(&in)
		if c.name == "months for permanent" {
			in.ContractMonth = ptr(12)
		}
		_, errs := check(t, in, TypeResearch, modeDraft)
		if errs[c.field] != c.msg {
			t.Errorf("%s: field %s = %q, want %q (all: %v)", c.name, c.field, errs[c.field], c.msg, errs)
		}
	}
}

func TestTextsAreCleaned(t *testing.T) {
	in := goodInput()
	in.Title = "  Старший   научный \t сотрудник  "
	in.Description = "\r\nСтрока один\r\nСтрока два\r\n"
	in.City = "  Санкт-  Петербург "
	in.Specialties = []string{" 1.4.4 ", "1.4.4", "", "1.4.1"}
	f, errs := check(t, in, TypeResearch, modeStrict)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if f.title != "Старший научный сотрудник" || f.description != "Строка один\nСтрока два" || f.city != "Санкт- Петербург" {
		t.Errorf("not cleaned: %q %q %q", f.title, f.description, f.city)
	}
	if len(f.specialties) != 2 || f.specialties[0] != "1.4.4" || f.specialties[1] != "1.4.1" {
		t.Errorf("specialties: %v", f.specialties)
	}
}

// Срок подачи: при публикации не раньше сегодняшнего дня; сегодняшний день ещё подходит.
func TestDeadlineAgainstToday(t *testing.T) {
	cases := []struct {
		deadline string
		mode     checkMode
		ok       bool
	}{
		{"2026-10-02", modePublish, false},
		{"2026-10-03", modePublish, true},
		{"2026-10-04", modePublish, true},
		{"2026-10-02", modeStrict, true}, // правка опубликованной вакансии с прошедшим сроком разрешена
		{"2026-10-02", modeDraft, true},
	}
	for _, c := range cases {
		in := goodInput()
		in.Deadline = c.deadline
		_, errs := check(t, in, TypeResearch, c.mode)
		if (errs["deadline"] == "") != c.ok {
			t.Errorf("deadline %s, mode %d: errors %v", c.deadline, c.mode, errs)
		}
	}
}

func TestSalaryRangeAllowsOneBound(t *testing.T) {
	for _, in := range []Input{{SalaryFrom: ptr(50000)}, {SalaryTo: ptr(90000)}, {SalaryFrom: ptr(50000), SalaryTo: ptr(50000)}} {
		in.Title = "Вакансия"
		if _, errs := check(t, in, TypeResearch, modeDraft); len(errs) != 0 {
			t.Errorf("%+v: %v", in, errs)
		}
	}
}

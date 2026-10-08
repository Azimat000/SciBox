package vacancies

import (
	"scibox/server/internal/num"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Режимы проверки полей.
type checkMode int

const (
	// modeDraft — черновик: проверяем только то, что заполнено (формат, длину, допустимые значения).
	modeDraft checkMode = iota
	// modeStrict — вакансия должна быть готова к показу людям: обязательные поля заполнены.
	modeStrict
	// modePublish — как modeStrict, и срок подачи ещё не прошёл (публикация и повторное открытие).
	modePublish
)

// Тексты ошибок полей: показываются человеку как есть.
const (
	msgTitleRequired    = "Укажите название вакансии"
	msgTitleShort       = "Название слишком короткое: не меньше 3 знаков"
	msgTitleLong        = "Название слишком длинное: не больше 200 знаков"
	msgTitleInvalid     = "В названии должны быть буквы"
	msgPositionUnknown  = "Выберите должность из списка"
	msgUnitUnknown      = "Такого подразделения в организации нет"
	msgSummaryRequired  = "Напишите короткую аннотацию: о чём работа, в 2–3 предложениях"
	msgSummaryShort     = "Аннотация слишком короткая: не меньше 20 знаков"
	msgSummaryLong      = "Аннотация слишком длинная: не больше 600 знаков"
	msgDescRequired     = "Опишите вакансию: задачи, команду, условия"
	msgDescLong         = "Описание слишком длинное: не больше 10 000 знаков"
	msgReqLong          = "Требования слишком длинные: не больше 5000 знаков"
	msgTextInvalid      = "В тексте есть недопустимые символы"
	msgFocusRequired    = "Заполните это поле"
	msgFocusLong        = "Слишком длинно: не больше 300 знаков"
	msgLevelRequired    = "Выберите уровень исследователя"
	msgLevelInvalid     = "Выберите уровень из списка: R1–R4"
	msgFormatRequired   = "Выберите формат работы"
	msgFormatInvalid    = "Выберите формат работы из списка"
	msgRegionRequired   = "Выберите регион"
	msgCityRequired     = "Укажите город"
	msgCityLong         = "Название города слишком длинное: не больше 100 знаков"
	msgCityInvalid      = "В названии города должны быть буквы"
	msgHousingInvalid   = "Выберите вариант из списка"
	msgRateRequired     = "Выберите ставку"
	msgRateInvalid      = "Ставка: 0,25, 0,5, 0,75 или 1"
	msgSalaryInvalid    = "Сумма должна быть целым числом рублей больше нуля"
	msgSalaryBig        = "Слишком большая сумма"
	msgSalaryOrder      = "«До» не может быть меньше, чем «от»"
	msgContractRequired = "Выберите вид договора"
	msgContractInvalid  = "Выберите вид договора из списка"
	msgMonthsRequired   = "Укажите срок договора в месяцах"
	msgMonthsInvalid    = "Срок договора: от 1 до 120 месяцев"
	msgMonthsOnlyFixed  = "Срок указывают только для срочного договора"
	msgFundingInvalid   = "Выберите источник из списка"
	msgFundingNoteLong  = "Слишком длинно: не больше 200 знаков"
	msgDegreeInvalid    = "Выберите степень из списка"
	msgTitleReqInvalid  = "Выберите звание из списка"
	msgTitleReqTeaching = "Звание можно требовать только у преподавателей"
	msgCompetitionOnly  = "Отметка «конкурс» только для научных работников и преподавателей"
	msgDeadlineInvalid  = "Дата должна быть вида 2026-11-14"
	msgDeadlineRequired = "Для конкурса укажите срок подачи документов"
	msgDeadlinePast     = "Срок подачи уже прошёл: укажите сегодняшнюю или более позднюю дату"
	msgSpecsMany        = "Специальностей не больше пяти"
	msgSpecsUnknown     = "Выберите специальности из списка"
	msgSpecsRequired    = "Выберите хотя бы одну научную специальность"
)

// typeRules — что требуется от вакансии каждого типа при публикации и что для неё допустимо.
type typeRules struct {
	needRate         bool // ставка обязательна
	needLevel        bool // уровень R1–R4 обязателен
	needSpecialties  bool // нужна хотя бы одна научная специальность
	needFocus        bool // нужно поле «что предстоит делать»
	allowCompetition bool // можно отметить «конкурс»
	allowTitle       bool // можно требовать учёное звание
}

func rulesFor(positionType string) typeRules {
	switch positionType {
	case TypeResearch:
		return typeRules{needRate: true, needLevel: true, needSpecialties: true, allowCompetition: true}
	case TypeTeaching:
		return typeRules{needRate: true, needLevel: true, needSpecialties: true, needFocus: true, allowCompetition: true, allowTitle: true}
	case TypeAdmin:
		return typeRules{needRate: true}
	case TypePhD, TypeMasters:
		return typeRules{needSpecialties: true, needFocus: true}
	case TypeProject, TypeInternship:
		return typeRules{needFocus: true}
	}
	return typeRules{}
}

// fields — проверенные и приведённые к хранимому виду поля вакансии.
type fields struct {
	title          string
	positionCode   string
	unitID         *uuid.UUID
	summary        string
	description    string
	requirements   string
	focus          string
	careerLevel    *int16
	workFormat     *string
	regionCode     *string
	city           string
	housing        string
	rate           *int16
	salaryFrom     *int32
	salaryTo       *int32
	contractType   *string
	contractMonths *int16
	fundingSource  *string
	fundingNote    string
	degree         string
	academicTitle  string
	isCompetition  bool
	deadline       *time.Time
	specialties    []string
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func hasControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if allowNewline && (r == '\n' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isOneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// text приводит переводы строк к \n, режет пустоту по краям и проверяет длину. Пустая строка допустима.
func text(raw string, max int, tooLong string) (string, string) {
	d := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	if hasControl(d, true) {
		return "", msgTextInvalid
	}
	if utf8.RuneCountInString(d) > max {
		return "", tooLong
	}
	return d, ""
}

// oneLine — короткая строка без переносов.
func oneLine(raw string, max int, tooLong string) (string, string) {
	d := collapse(raw)
	if hasControl(d, false) {
		return "", msgTextInvalid
	}
	if utf8.RuneCountInString(d) > max {
		return "", tooLong
	}
	return d, ""
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intIn(n *int, lo, hi int) bool { return n != nil && *n >= lo && *n <= hi }

// validate проверяет форму вакансии. positionType — тип выбранной должности ("", если должности нет в справочнике:
// об этом скажет вызывающий, а правила по типу пропускаются). today — сегодняшняя дата по Москве (полночь в UTC).
// Возвращает приведённые поля и ошибки по полям (пусто, если всё хорошо).
func validate(in Input, positionType string, mode checkMode, today time.Time) (fields, map[string]string) {
	errs := map[string]string{}
	var f fields
	var msg string
	strict := mode != modeDraft
	rules := rulesFor(positionType)

	// Название.
	f.title = collapse(in.Title)
	switch n := utf8.RuneCountInString(f.title); {
	case n == 0:
		errs["title"] = msgTitleRequired
	case n < MinTitleLen:
		errs["title"] = msgTitleShort
	case n > MaxTitleLen:
		errs["title"] = msgTitleLong
	case hasControl(f.title, false) || !hasLetter(f.title):
		errs["title"] = msgTitleInvalid
	}
	f.positionCode = in.PositionCode
	f.unitID = in.UnitID

	// Тексты.
	if f.summary, msg = text(in.Summary, MaxSummaryLen, msgSummaryLong); msg != "" {
		errs["summary"] = msg
	} else if strict && f.summary == "" {
		errs["summary"] = msgSummaryRequired
	} else if f.summary != "" && utf8.RuneCountInString(f.summary) < MinSummaryLen && strict {
		errs["summary"] = msgSummaryShort
	}
	if f.description, msg = text(in.Description, MaxDescription, msgDescLong); msg != "" {
		errs["description"] = msg
	} else if strict && f.description == "" {
		errs["description"] = msgDescRequired
	}
	if f.requirements, msg = text(in.Requirements, MaxRequirements, msgReqLong); msg != "" {
		errs["requirements"] = msg
	}
	if f.focus, msg = oneLine(in.Focus, MaxFocusLen, msgFocusLong); msg != "" {
		errs["focus"] = msg
	} else if strict && rules.needFocus && f.focus == "" {
		errs["focus"] = msgFocusRequired
	}

	// Уровень.
	switch {
	case in.CareerLevel != nil && !intIn(in.CareerLevel, 1, 4):
		errs["career_level"] = msgLevelInvalid
	case in.CareerLevel != nil:
		f.careerLevel = num.Int16Ptr(in.CareerLevel)
	case strict && rules.needLevel:
		errs["career_level"] = msgLevelRequired
	}

	// Формат, место.
	switch {
	case in.WorkFormat != "" && !isOneOf(in.WorkFormat, WorkFormats):
		errs["work_format"] = msgFormatInvalid
	case in.WorkFormat == "" && strict:
		errs["work_format"] = msgFormatRequired
	default:
		f.workFormat = optionalString(in.WorkFormat)
	}
	f.regionCode = optionalString(in.RegionCode)
	var city string
	if city, msg = oneLine(in.City, MaxCityLen, msgCityLong); msg != "" {
		errs["city"] = msg
	} else if city != "" && !hasLetter(city) {
		errs["city"] = msgCityInvalid
	}
	f.city = city
	if strict && in.WorkFormat != FormatRemote {
		if f.city == "" && errs["city"] == "" {
			errs["city"] = msgCityRequired
		}
		if f.regionCode == nil {
			errs["region_code"] = msgRegionRequired
		}
	}

	// Условия.
	f.housing = in.Housing
	if f.housing == "" {
		f.housing = HousingNone
	}
	if !isOneOf(f.housing, Housings) {
		errs["housing"] = msgHousingInvalid
	}
	switch {
	case in.RatePercent != nil && !isOneOfInt(*in.RatePercent, Rates):
		errs["rate_percent"] = msgRateInvalid
	case in.RatePercent != nil:
		f.rate = num.Int16Ptr(in.RatePercent)
	case strict && rules.needRate:
		errs["rate_percent"] = msgRateRequired
	}
	f.salaryFrom = salary(in.SalaryFrom, "salary_from", errs)
	f.salaryTo = salary(in.SalaryTo, "salary_to", errs)
	if f.salaryFrom != nil && f.salaryTo != nil && *f.salaryTo < *f.salaryFrom {
		errs["salary_to"] = msgSalaryOrder
	}

	// Договор.
	switch {
	case in.ContractType != "" && !isOneOf(in.ContractType, ContractTypes):
		errs["contract_type"] = msgContractInvalid
	case in.ContractType == "" && strict:
		errs["contract_type"] = msgContractRequired
	default:
		f.contractType = optionalString(in.ContractType)
	}
	switch {
	case in.ContractMonth != nil && !intIn(in.ContractMonth, 1, MaxContractMon):
		errs["contract_months"] = msgMonthsInvalid
	case in.ContractMonth != nil && in.ContractType != ContractFixed:
		errs["contract_months"] = msgMonthsOnlyFixed
	case in.ContractMonth != nil:
		f.contractMonths = num.Int16Ptr(in.ContractMonth)
	case strict && in.ContractType == ContractFixed:
		errs["contract_months"] = msgMonthsRequired
	}

	// Финансирование, требования к человеку.
	if in.FundingSource != "" && !isOneOf(in.FundingSource, FundingSources) {
		errs["funding_source"] = msgFundingInvalid
	} else {
		f.fundingSource = optionalString(in.FundingSource)
	}
	if f.fundingNote, msg = oneLine(in.FundingNote, MaxFundingNote, msgFundingNoteLong); msg != "" {
		errs["funding_note"] = msg
	}
	f.degree = in.Degree
	if f.degree == "" {
		f.degree = DegreeNone
	}
	if !isOneOf(f.degree, Degrees) {
		errs["degree_required"] = msgDegreeInvalid
	}
	f.academicTitle = in.AcademicTitle
	if f.academicTitle == "" {
		f.academicTitle = TitleNone
	}
	switch {
	case !isOneOf(f.academicTitle, Titles):
		errs["title_required"] = msgTitleReqInvalid
	case f.academicTitle != TitleNone && positionType != "" && !rules.allowTitle:
		errs["title_required"] = msgTitleReqTeaching
	}
	f.isCompetition = in.IsCompetition
	if f.isCompetition && positionType != "" && !rules.allowCompetition {
		errs["is_competition"] = msgCompetitionOnly
	}

	// Срок подачи.
	if in.Deadline != "" {
		d, err := time.Parse("2006-01-02", in.Deadline)
		switch {
		case err != nil || d.Year() < 2000 || d.Year() > 2100:
			errs["deadline"] = msgDeadlineInvalid
		case mode == modePublish && d.Before(today):
			errs["deadline"] = msgDeadlinePast
		default:
			f.deadline = &d
		}
	} else if strict && f.isCompetition {
		errs["deadline"] = msgDeadlineRequired
	}

	// Научные специальности.
	f.specialties = dedupe(in.Specialties)
	switch {
	case len(f.specialties) > MaxSpecialties:
		errs["specialties"] = msgSpecsMany
	case strict && rules.needSpecialties && len(f.specialties) == 0:
		errs["specialties"] = msgSpecsRequired
	}
	return f, errs
}

func salary(v *int, field string, errs map[string]string) *int32 {
	switch {
	case v == nil:
		return nil
	case *v <= 0:
		errs[field] = msgSalaryInvalid
		return nil
	case *v > MaxSalary:
		errs[field] = msgSalaryBig
		return nil
	}
	n := int32(*v)
	return &n
}

func isOneOfInt(v int, set []int) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

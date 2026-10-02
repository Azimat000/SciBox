package orgs

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"scibox/server/internal/auth"
)

// Виды организаций (D-006 и ROADMAP, срез 4). Строки лежат в базе.
const (
	KindUniversity    = "university"     // вуз
	KindInstitute     = "institute"      // НИИ / институт РАН
	KindScienceCenter = "science_center" // научный центр
	KindRDCompany     = "rd_company"     // компания с R&D
	KindTechnopark    = "technopark"     // технопарк / инновационная инфраструктура
	KindOther         = "other"          // другое
)

// OrgKinds — все виды организаций.
var OrgKinds = []string{KindUniversity, KindInstitute, KindScienceCenter, KindRDCompany, KindTechnopark, KindOther}

// Виды подразделений.
const (
	UnitDepartment     = "department"      // кафедра
	UnitLaboratory     = "laboratory"      // лаборатория
	UnitDivision       = "division"        // отдел
	UnitSharedFacility = "shared_facility" // ЦКП (центр коллективного пользования)
)

// UnitKinds — все виды подразделений.
var UnitKinds = []string{UnitDepartment, UnitLaboratory, UnitDivision, UnitSharedFacility}

// Пределы полей.
const (
	MinNameLen        = 2
	MaxNameLen        = 200
	MaxCityLen        = 100
	MaxWebsiteLen     = 300
	MaxOrgDescription = 5000
	MaxUnitDesc       = 3000
	MaxTopics         = 10
	MinTopicLen       = 2
	MaxTopicLen       = 80
	maxQueryLen       = 100
)

// Тексты ошибок полей: показываются человеку как есть.
const (
	msgNameRequired   = "Укажите название"
	msgNameShort      = "Название слишком короткое: не меньше 2 знаков"
	msgNameLong       = "Название слишком длинное: не больше 200 знаков"
	msgNameInvalid    = "В названии должны быть буквы"
	msgOrgKind        = "Выберите тип организации из списка"
	msgUnitKind       = "Выберите вид подразделения из списка"
	msgCityRequired   = "Укажите город"
	msgCityLong       = "Название города слишком длинное: не больше 100 знаков"
	msgCityInvalid    = "В названии города должны быть буквы"
	msgWebsiteInvalid = "Адрес сайта не похож на настоящий. Он должен выглядеть так: https://example.ru"
	msgWebsiteLong    = "Адрес сайта слишком длинный: не больше 300 знаков"
	msgDescOrgLong    = "Описание слишком длинное: не больше 5000 знаков"
	msgDescUnitLong   = "Описание слишком длинное: не больше 3000 знаков"
	msgDescInvalid    = "В описании есть недопустимые символы"
	msgTopicsMany     = "Тем не больше десяти"
	msgTopicShort     = "Каждая тема: не меньше 2 знаков"
	msgTopicLong      = "Каждая тема: не больше 80 знаков"
	msgRoleInvalid    = "Выберите роль из списка"
	msgUnitForHeadsOn = "Подразделение указывают только для руководителя"
	msgUnitUnknown    = "Такого подразделения в организации нет"
	msgAlreadyMember  = "Этот человек уже работает в вашей организации"
	msgHeadNotMember  = "Руководителем может быть только сотрудник организации"
)

func isOneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
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

// collapse убирает лишние пробелы по краям и внутри.
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

// normalizeName проверяет название организации или подразделения.
func normalizeName(raw string) (string, string) {
	name := collapse(raw)
	n := utf8.RuneCountInString(name)
	switch {
	case n == 0:
		return "", msgNameRequired
	case n < MinNameLen:
		return "", msgNameShort
	case n > MaxNameLen:
		return "", msgNameLong
	case hasControl(name, false) || !hasLetter(name):
		return "", msgNameInvalid
	}
	return name, ""
}

func normalizeCity(raw string) (string, string) {
	city := collapse(raw)
	switch {
	case city == "":
		return "", msgCityRequired
	case utf8.RuneCountInString(city) > MaxCityLen:
		return "", msgCityLong
	case hasControl(city, false) || !hasLetter(city):
		return "", msgCityInvalid
	}
	return city, ""
}

// normalizeWebsite принимает адрес с http(s):// или без него. Пустой адрес допустим. Хранится с https:// или http://.
func normalizeWebsite(raw string) (string, string) {
	site := strings.TrimSpace(raw)
	if site == "" {
		return "", ""
	}
	if len(site) > MaxWebsiteLen {
		return "", msgWebsiteLong
	}
	if strings.ContainsAny(site, " \t\r\n<>\"") {
		return "", msgWebsiteInvalid
	}
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", msgWebsiteInvalid
	}
	host := u.Hostname()
	if host == "" || !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return "", msgWebsiteInvalid
	}
	if len(u.String()) > MaxWebsiteLen {
		return "", msgWebsiteLong
	}
	return u.String(), ""
}

// normalizeDescription приводит переводы строк к \n и режет пустоту по краям.
func normalizeDescription(raw string, max int, tooLong string) (string, string) {
	d := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	if hasControl(d, true) {
		return "", msgDescInvalid
	}
	if utf8.RuneCountInString(d) > max {
		return "", tooLong
	}
	return d, ""
}

// normalizeTopics убирает пустые и повторяющиеся темы (без учёта регистра), проверяет число и длину.
func normalizeTopics(raw []string) ([]string, string) {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, r := range raw {
		topic := collapse(r)
		if topic == "" {
			continue
		}
		n := utf8.RuneCountInString(topic)
		switch {
		case n < MinTopicLen:
			return nil, msgTopicShort
		case n > MaxTopicLen:
			return nil, msgTopicLong
		case hasControl(topic, false):
			return nil, msgTopicShort
		}
		key := strings.ToLower(topic)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, topic)
	}
	if len(out) > MaxTopics {
		return nil, msgTopicsMany
	}
	return out, ""
}

// OrgInput — поля формы организации.
type OrgInput struct {
	Name        string
	Kind        string
	City        string
	Website     string
	Description string
}

type orgFields struct{ name, kind, city, website, description string }

func validateOrg(in OrgInput) (orgFields, error) {
	fields := map[string]string{}
	var out orgFields
	var msg string
	if out.name, msg = normalizeName(in.Name); msg != "" {
		fields["name"] = msg
	}
	if !isOneOf(in.Kind, OrgKinds) {
		fields["kind"] = msgOrgKind
	}
	out.kind = in.Kind
	if out.city, msg = normalizeCity(in.City); msg != "" {
		fields["city"] = msg
	}
	if out.website, msg = normalizeWebsite(in.Website); msg != "" {
		fields["website"] = msg
	}
	if out.description, msg = normalizeDescription(in.Description, MaxOrgDescription, msgDescOrgLong); msg != "" {
		fields["description"] = msg
	}
	if len(fields) > 0 {
		return orgFields{}, &auth.ValidationError{Fields: fields}
	}
	return out, nil
}

// UnitInput — поля формы подразделения.
type UnitInput struct {
	Name        string
	Kind        string
	Description string
	Topics      []string
}

type unitFields struct {
	name, kind, description string
	topics                  []string
}

func validateUnit(in UnitInput) (unitFields, error) {
	fields := map[string]string{}
	var out unitFields
	var msg string
	if out.name, msg = normalizeName(in.Name); msg != "" {
		fields["name"] = msg
	}
	if !isOneOf(in.Kind, UnitKinds) {
		fields["kind"] = msgUnitKind
	}
	out.kind = in.Kind
	if out.description, msg = normalizeDescription(in.Description, MaxUnitDesc, msgDescUnitLong); msg != "" {
		fields["description"] = msg
	}
	if out.topics, msg = normalizeTopics(in.Topics); msg != "" {
		fields["topics"] = msg
	}
	if len(fields) > 0 {
		return unitFields{}, &auth.ValidationError{Fields: fields}
	}
	return out, nil
}

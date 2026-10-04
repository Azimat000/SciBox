package profiles

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
	"scibox/server/internal/issn"
)

// Тексты ошибок полей: показываются человеку как есть.
const (
	msgTooLong         = "Слишком длинно: не больше %d знаков"
	msgTextInvalid     = "В тексте есть недопустимые символы"
	msgNeedLetters     = "Здесь должны быть буквы"
	msgPickFromList    = "Выберите вариант из списка"
	msgYearInvalid     = "Укажите год числом от %d до %d"
	msgYearOrder       = "Год окончания не может быть раньше года начала"
	msgRegionUnknown   = "Выберите регион из списка"
	msgSpecsMany       = "Специальностей не больше пяти"
	msgSpecsUnknown    = "Выберите специальности из списка"
	msgSpecUnknown     = "Выберите специальность из списка"
	msgDegreeNeeded    = "Сначала выберите степень"
	msgORCIDInvalid    = "ORCID записывают так: 0000-0002-1825-0097 (16 цифр, последняя может быть X)"
	msgORCIDChecksum   = "В номере ORCID ошибка: проверьте цифры"
	msgSPINInvalid     = "SPIN-код: от 4 до 8 цифр, например 1234-5678"
	msgScopusInvalid   = "Scopus Author ID: только цифры, от 8 до 12"
	msgWosInvalid      = "ResearcherID записывают так: A-1234-2008"
	msgHIndexInvalid   = "h-index: целое число от 0 до %d"
	msgEmailInvalid    = "Похоже на опечатку в адресе почты"
	msgURLInvalid      = "Ссылка должна начинаться с http:// или https://"
	msgDOIInvalid      = "DOI записывают так: 10.1038/nature12373"
	msgISSNInvalid     = "ISSN записывают так: 0028-0836 (последняя цифра проверочная, проверьте номер)"
	msgDOIDuplicate    = "Публикация с этим DOI уже есть в вашем профиле"
	msgKindInvalid     = "Неизвестный раздел профиля"
	msgKindMismatch    = "Вид записи менять нельзя"
	maxHIndex          = 300
	yearMin            = 1900
	maxHeadline        = 200
	maxCity            = 100
	maxAbout           = 3000
	maxInstitution     = 200
	maxDissertation    = 500
	maxIdentifierShort = 40
)

// fieldErrors — ошибки по именам полей формы.
type fieldErrors map[string]string

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

// textSpec — правила короткого однострочного поля.
type textSpec struct {
	required string // текст ошибки, если поле пустое; пусто — поле необязательное
	max      int
	letters  bool // в тексте должна быть хотя бы одна буква
}

// line проверяет однострочное поле и возвращает приведённое значение.
func (e fieldErrors) line(name, raw string, spec textSpec) string {
	v := collapse(raw)
	switch {
	case v == "":
		if spec.required != "" {
			e[name] = spec.required
		}
		return ""
	case hasControl(v, false):
		e[name] = msgTextInvalid
	case utf8.RuneCountInString(v) > spec.max:
		e[name] = fmt.Sprintf(msgTooLong, spec.max)
	case spec.letters && !hasLetter(v):
		e[name] = msgNeedLetters
	default:
		return v
	}
	return ""
}

// paragraph проверяет текст с переводами строк.
func (e fieldErrors) paragraph(name, raw string, max int) string {
	v := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	switch {
	case hasControl(v, true):
		e[name] = msgTextInvalid
	case utf8.RuneCountInString(v) > max:
		e[name] = fmt.Sprintf(msgTooLong, max)
	default:
		return v
	}
	return ""
}

// choice проверяет значение из закрытого списка. Пустое значение допустимо только у необязательного поля (required пуст).
func (e fieldErrors) choice(name, v string, set []string, required string) string {
	switch {
	case v == "" && required != "":
		e[name] = required
	case v != "" && !isOneOf(v, set):
		e[name] = msgPickFromList
	default:
		return v
	}
	return ""
}

// year проверяет год. Пустой год допустим, если required пуст.
func (e fieldErrors) year(name string, v *int, lo, hi int, required string) *int {
	switch {
	case v == nil:
		if required != "" {
			e[name] = required
		}
		return nil
	case *v < lo || *v > hi:
		e[name] = fmt.Sprintf(msgYearInvalid, lo, hi)
		return nil
	}
	return v
}

// ---- основные поля ----

// coreFields — проверенные основные поля, готовые к записи в базу.
type coreFields struct {
	headline, city    string
	regionCode        *string
	about             string
	degree            string
	degreeSpecialty   *string
	degreeYear        *int16
	degreeInstitution string
	dissertation      string
	academicTitle     string
	academicTitleYear *int16
	orcid, spin       string
	scopusID, wosID   string
	hRsci, hScopus    *int16
	hWos, hScholar    *int16
	contactEmail      string
	specialties       []string
}

func int16p(v *int) *int16 {
	if v == nil {
		return nil
	}
	n := int16(*v)
	return &n
}

func strp(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var (
	spinShape    = regexp.MustCompile(`^[0-9]{4,8}$`)
	scopusShape  = regexp.MustCompile(`^[0-9]{8,12}$`)
	wosShape     = regexp.MustCompile(`^[A-Z]{1,3}-[0-9]{4}-(19|20)[0-9]{2}$`)
	orcidPrefix  = regexp.MustCompile(`(?i)^https?://(www\.)?orcid\.org/`)
	digitsOnly16 = regexp.MustCompile(`^[0-9]{15}[0-9Xx]$`)
)

// normalizeORCID приводит ORCID iD к виду 0000-0002-1825-0097. Принимает адрес orcid.org и запись без дефисов.
func normalizeORCID(raw string) (string, string) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", ""
	}
	v = orcidPrefix.ReplaceAllString(v, "")
	v = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(v))
	if !digitsOnly16.MatchString(v) {
		return "", msgORCIDInvalid
	}
	v = v[0:4] + "-" + v[4:8] + "-" + v[8:12] + "-" + v[12:16]
	if orcidCheckDigit(v) != v[len(v)-1] {
		return "", msgORCIDChecksum
	}
	return v, ""
}

// orcidCheckDigit считает контрольный знак по ISO 7064 MOD 11-2 (по первым пятнадцати цифрам).
func orcidCheckDigit(orcid string) byte {
	total := 0
	for _, r := range orcid[:len(orcid)-1] {
		if r == '-' {
			continue
		}
		total = (total + int(r-'0')) * 2
	}
	digit := (12 - total%11) % 11
	if digit == 10 {
		return 'X'
	}
	return byte('0' + digit)
}

// normalizeDigits убирает пробелы и дефисы из номера, который записывают по-разному (1234-5678 и 12345678).
func normalizeDigits(raw string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(raw))
}

// validateCore проверяет основные поля, не обращаясь к базе: справочники (регион, специальности) проверяет сервис.
func validateCore(in CoreInput, now time.Time) (coreFields, fieldErrors) {
	errs := fieldErrors{}
	var f coreFields
	thisYear := now.Year()

	f.headline = errs.line("headline", in.Headline, textSpec{max: maxHeadline})
	f.city = errs.line("city", in.City, textSpec{max: maxCity, letters: true})
	f.regionCode = strp(strings.TrimSpace(in.RegionCode))
	f.about = errs.paragraph("about", in.About, maxAbout)

	f.degree = errs.choice("degree", in.Degree, degrees, "")
	if f.degree == "" {
		f.degree = DegreeNone
	}
	if f.degree != DegreeNone {
		f.degreeSpecialty = strp(strings.TrimSpace(in.DegreeSpecialty))
		f.degreeYear = int16p(errs.year("degree_year", in.DegreeYear, 1950, thisYear, ""))
		f.degreeInstitution = errs.line("degree_institution", in.DegreeInstitution, textSpec{max: maxInstitution})
		f.dissertation = errs.line("dissertation_title", in.Dissertation, textSpec{max: maxDissertation})
	}
	f.academicTitle = errs.choice("academic_title", in.AcademicTitle, titles, "")
	if f.academicTitle == "" {
		f.academicTitle = TitleNone
	}
	if f.academicTitle != TitleNone {
		f.academicTitleYear = int16p(errs.year("academic_title_year", in.AcademicTitleYear, 1950, thisYear, ""))
	}

	var msg string
	if f.orcid, msg = normalizeORCID(in.ORCID); msg != "" {
		errs["orcid"] = msg
	}
	if v := normalizeDigits(in.SPIN); v != "" {
		if spinShape.MatchString(v) {
			f.spin = v
		} else {
			errs["spin"] = msgSPINInvalid
		}
	}
	if v := normalizeDigits(in.ScopusID); v != "" {
		if scopusShape.MatchString(v) {
			f.scopusID = v
		} else {
			errs["scopus_id"] = msgScopusInvalid
		}
	}
	if v := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.WosID), " ", "")); v != "" {
		if wosShape.MatchString(v) {
			f.wosID = v
		} else {
			errs["wos_id"] = msgWosInvalid
		}
	}
	f.hRsci = int16p(hIndex(errs, "h_rsci", in.HRsci))
	f.hScopus = int16p(hIndex(errs, "h_scopus", in.HScopus))
	f.hWos = int16p(hIndex(errs, "h_wos", in.HWos))
	f.hScholar = int16p(hIndex(errs, "h_scholar", in.HScholar))

	if raw := strings.TrimSpace(in.ContactEmail); raw != "" {
		if email, msg := auth.NormalizeEmail(raw); msg != "" {
			errs["contact_email"] = msgEmailInvalid
		} else {
			f.contactEmail = email
		}
	}

	f.specialties = dedupe(in.Specialties)
	if len(f.specialties) > MaxSpecialties {
		errs["specialties"] = msgSpecsMany
	}
	return f, errs
}

func hIndex(errs fieldErrors, name string, v *int) *int {
	if v != nil && (*v < 0 || *v > maxHIndex) {
		errs[name] = fmt.Sprintf(msgHIndexInvalid, maxHIndex)
		return nil
	}
	return v
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---- записи разделов ----

// itemFields — запись, готовая к сохранению, и год для сортировки.
type itemFields struct {
	fields   ItemFields
	sortYear int16
}

// Что обязательно у каждого вида записи.
var (
	reqInstitution  = textSpec{required: "Укажите учебное заведение", max: 200, letters: true}
	reqOrganization = textSpec{required: "Укажите организацию", max: 200, letters: true}
	reqPosition     = textSpec{required: "Укажите должность", max: 200, letters: true}
	reqPubTitle     = textSpec{required: "Укажите название", max: 500, letters: true}
	reqAuthors      = textSpec{required: "Укажите авторов", max: 1000}
	reqFunder       = textSpec{required: "Укажите, кто финансирует (фонд, программа)", max: 200, letters: true}
	reqNumber       = textSpec{required: "Укажите номер", max: 100}
	reqCourse       = textSpec{required: "Укажите название курса", max: 200, letters: true}
)

// validateItem проверяет запись раздела. Поля, которых у этого вида записи нет, отбрасываются.
func validateItem(kind string, in ItemFields, now time.Time) (itemFields, fieldErrors) {
	errs := fieldErrors{}
	var f ItemFields
	thisYear := now.Year()
	var sort int16

	switch kind {
	case KindEducation:
		f.Institution = errs.line("institution", in.Institution, reqInstitution)
		f.Program = errs.line("program", in.Program, textSpec{max: 200})
		f.YearFrom, f.YearTo = period(errs, in, 1930, thisYear+8, "Укажите год начала")
		f.Description = errs.paragraph("description", in.Description, 1000)
		sort = periodSort(f)
	case KindExperience:
		f.Organization = errs.line("organization", in.Organization, reqOrganization)
		f.Position = errs.line("position", in.Position, reqPosition)
		f.YearFrom, f.YearTo = period(errs, in, 1930, thisYear, "Укажите год начала")
		f.Description = errs.paragraph("description", in.Description, 1000)
		sort = periodSort(f)
	case KindPublication:
		f.Title = errs.line("title", in.Title, reqPubTitle)
		f.Authors = errs.line("authors", in.Authors, reqAuthors)
		f.Venue = errs.line("venue", in.Venue, textSpec{max: 300})
		f.PubType = errs.choice("pub_type", in.PubType, pubTypes, "Выберите тип публикации")
		f.Year = errs.year("year", in.Year, yearMin, thisYear+1, "Укажите год")
		f.Volume = errs.line("volume", in.Volume, textSpec{max: maxIdentifierShort})
		f.Issue = errs.line("issue", in.Issue, textSpec{max: maxIdentifierShort})
		f.Pages = errs.line("pages", in.Pages, textSpec{max: maxIdentifierShort})
		f.Source = errs.choice("source", in.Source, pubSources, "")
		if f.Source == "" {
			f.Source = SourceManual
		}
		f.URL = errs.link("url", in.URL)
		if raw := strings.TrimSpace(in.DOI); raw != "" {
			if doi, ok := crossref.NormalizeDOI(raw); ok {
				f.DOI = doi
			} else {
				errs["doi"] = msgDOIInvalid
			}
		}
		if raw := strings.TrimSpace(in.ISSN); raw != "" {
			if n, ok := issn.Normalize(raw); ok {
				f.ISSN = n
			} else {
				errs["issn"] = msgISSNInvalid
			}
		}
		if f.Year != nil {
			sort = int16(*f.Year)
		}
	case KindGrant:
		f.Title = errs.line("title", in.Title, reqPubTitle)
		f.Funder = errs.line("funder", in.Funder, reqFunder)
		f.Number = errs.line("number", in.Number, textSpec{max: 100})
		f.Role = errs.choice("role", in.Role, grantRoles, "Выберите роль в проекте")
		f.YearFrom, f.YearTo = period(errs, in, 1990, thisYear+10, "Укажите год начала")
		f.Description = errs.paragraph("description", in.Description, 1000)
		sort = periodSort(f)
	case KindPatent:
		f.Title = errs.line("title", in.Title, reqPubTitle)
		f.Authors = errs.line("authors", in.Authors, textSpec{max: 500})
		f.Number = errs.line("number", in.Number, reqNumber)
		f.Office = errs.line("office", in.Office, textSpec{max: 100})
		f.PatentType = errs.choice("patent_type", in.PatentType, patentTypes, "Выберите вид охранного документа")
		f.Year = errs.year("year", in.Year, yearMin, thisYear+1, "Укажите год")
		if f.Year != nil {
			sort = int16(*f.Year)
		}
	case KindTeaching:
		f.Course = errs.line("course", in.Course, reqCourse)
		f.Institution = errs.line("institution", in.Institution, reqInstitution)
		f.Level = errs.choice("level", in.Level, teachLevels, "Выберите уровень")
		f.YearFrom, f.YearTo = period(errs, in, 1930, thisYear, "Укажите год начала")
		f.Description = errs.paragraph("description", in.Description, 1000)
		sort = periodSort(f)
	default:
		errs["kind"] = msgKindInvalid
	}
	return itemFields{fields: f, sortYear: sort}, errs
}

// period проверяет «с какого года» и «по какой» (пусто — по настоящее время).
func period(errs fieldErrors, in ItemFields, lo, hi int, required string) (from, to *int) {
	from = errs.year("year_from", in.YearFrom, lo, hi, required)
	to = errs.year("year_to", in.YearTo, lo, hi, "")
	if from != nil && to != nil && *to < *from {
		errs["year_to"] = msgYearOrder
		to = nil
	}
	return from, to
}

// periodSort: незавершённое идёт первым, остальное по году окончания.
func periodSort(f ItemFields) int16 {
	switch {
	case f.YearFrom == nil:
		return 0
	case f.YearTo == nil:
		return sortOngoing
	}
	return int16(*f.YearTo)
}

// link проверяет необязательную ссылку: только http и https, с адресом сайта.
func (e fieldErrors) link(name, raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if utf8.RuneCountInString(v) > 500 {
		e[name] = fmt.Sprintf(msgTooLong, 500)
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || hasControl(v, false) || strings.ContainsAny(v, " \t\r\n") {
		e[name] = msgURLInvalid
		return ""
	}
	return v
}

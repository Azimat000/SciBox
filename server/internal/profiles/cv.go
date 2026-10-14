package profiles

import (
	"fmt"
	"strings"
	"time"

	"scibox/server/internal/cv"
)

// Подписи для резюме. Показываются человеку как есть.
var (
	degreeLabel = map[string]string{DegreeCandidate: "Кандидат наук", DegreeDoctor: "Доктор наук"}
	titleLabel  = map[string]string{TitleDocent: "Доцент", TitleProfessor: "Профессор"}
	pubTypeName = map[string]string{
		"book": "Книга", "chapter": "Глава в книге", "conference": "Доклад на конференции",
		"preprint": "Препринт", "thesis": "Диссертация", "other": "Другое",
	}
	grantRoleName    = map[string]string{"lead": "Руководитель", "participant": "Исполнитель"}
	patentTypeName   = map[string]string{"invention": "Изобретение", "utility_model": "Полезная модель", "software": "Программа для ЭВМ", "database": "База данных", "other": "Другое"}
	teachLevelName   = map[string]string{"bachelor": "Бакалавриат", "master": "Магистратура", "postgraduate": "Аспирантура", "continuing": "Повышение квалификации", "other": "Другое"}
	russianMonthsGen = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
)

// years: «2018 — н. в.», «2015 — 2018», «2023».
func years(from, to *int) string {
	switch {
	case from == nil && to == nil:
		return ""
	case from == nil:
		return fmt.Sprint(*to)
	case to == nil:
		return fmt.Sprintf("%d — н. в.", *from)
	case *from == *to:
		return fmt.Sprint(*from)
	}
	return fmt.Sprintf("%d — %d", *from, *to)
}

func join(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// location: «Город, Регион»; одинаковые названия («Москва, Москва») не повторяются.
func location(city string, region *Code) string {
	if region == nil || strings.EqualFold(region.Name, city) {
		return city
	}
	return join(", ", city, region.Name)
}

func yearOf(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}

// publication: «Авторы. Издание, год. Т. 5, № 2, С. 10–20. DOI: …» (название выносится в жирную строку записи).
func publicationText(it Item) string {
	biblio := join(", ", strings.TrimRight(it.Venue, "."), yearOf(it.Year))
	vol := join(", ", prefixed("Т. ", it.Volume), prefixed("№ ", it.Issue), prefixed("С. ", it.Pages))
	return join(" ", withDot(it.Authors), withDot(biblio), withDot(vol), prefixed("DOI: ", it.DOI), pubTypeNote(it.PubType), quartileNote(it.Journal))
}

// quartileNote: «Q1» у публикации в журнале справочника с квартилем.
func quartileNote(j *JournalRef) string {
	if j == nil || j.Quartile == nil {
		return ""
	}
	return fmt.Sprintf("Q%d.", *j.Quartile)
}

// quartileLine: «Статей в журналах Q1–Q2: 7, из них в 2022–2026: 4 (квартили SCImago Journal Rank 2025)». Без статей
// в Q1–Q2 строки нет.
func quartileLine(q QuartileStats) string {
	if q.Q12Total == 0 || q.Year == nil {
		return ""
	}
	return fmt.Sprintf("Статей в журналах Q1–Q2: %d, из них в %d–%d: %d (квартили SCImago Journal Rank %d)",
		q.Q12Total, q.RecentFrom, q.RecentFrom+RecentYears-1, q.Q12Recent, *q.Year)
}

func pubTypeNote(t string) string {
	if n, ok := pubTypeName[t]; ok {
		return "(" + n + ")"
	}
	return ""
}

func prefixed(prefix, v string) string {
	if v == "" {
		return ""
	}
	return prefix + v
}

func withDot(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

// cvDocument превращает профиль (уже отфильтрованный по приватности) в описание документа.
func cvDocument(page Page, product string, now time.Time) cv.Document {
	v := page.Profile
	doc := cv.Document{
		Title:    v.Name,
		Subtitle: v.Headline,
		Footer:   fmt.Sprintf("%s · %d %s %d", product, now.Day(), russianMonthsGen[now.Month()-1], now.Year()),
		Created:  now,
	}
	if loc := location(v.City, v.Region); loc != "" {
		doc.Where = append(doc.Where, loc)
	}
	if page.Viewer.CanSeeContacts && v.ContactEmail != "" {
		doc.Where = append(doc.Where, v.ContactEmail)
	}
	if v.Anonymous {
		// Безымянная карточка (D-199): в резюме только научные данные, как на странице.
		doc.Title = "Анонимный соискатель"
		doc.Subtitle = anonymousSubtitle(v)
	}
	doc.IDs = idFacts(v.Identifiers)
	if line := hIndexLine(v.HIndex); line != "" {
		// Показатель есть не у всех, поэтому не отдельная полоса, а такая же строка, как идентификаторы (D-208).
		doc.IDs = append(doc.IDs, cv.Fact{Label: "h-index", Value: line})
	}
	if line := quartileLine(v.Quartiles); line != "" {
		doc.Notes = append(doc.Notes, line)
	}
	if v.PublicationCount != nil && *v.PublicationCount > 0 {
		doc.Notes = append(doc.Notes, fmt.Sprintf("Публикаций: %d", *v.PublicationCount))
	}

	add := func(s cv.Section) {
		if s.Paragraph != "" || len(s.Entries) > 0 {
			doc.Sections = append(doc.Sections, s)
		}
	}
	add(cv.Section{Heading: "О себе", Paragraph: v.About})
	add(cv.Section{Heading: "Степень и звание", Entries: degreeEntries(v)})
	if len(v.Specialties) > 0 {
		var names []string
		for _, s := range v.Specialties {
			names = append(names, s.Code+" "+s.Name)
		}
		add(cv.Section{Heading: "Научные специальности", Paragraph: strings.Join(names, "\n")})
	}
	add(cv.Section{Heading: "Научные навыки", Paragraph: strings.Join(v.Skills.Research, ", ")})
	add(cv.Section{Heading: "Общие навыки", Paragraph: strings.Join(v.Skills.General, ", ")})

	s := v.Sections
	var edu, exp, pubs, grants, patents, teach []cv.Entry
	for _, it := range s.Education {
		edu = append(edu, cv.Entry{Label: years(it.YearFrom, it.YearTo), Lead: it.Institution, Text: join("\n", it.Program, it.Description)})
	}
	for _, it := range s.Experience {
		exp = append(exp, cv.Entry{Label: years(it.YearFrom, it.YearTo), Lead: it.Position, Text: join("\n", it.Organization, it.Description)})
	}
	for i, it := range s.Publications {
		pubs = append(pubs, cv.Entry{Label: fmt.Sprintf("%d.", i+1), Lead: it.Title, Text: publicationText(it)})
	}
	for _, it := range s.Grants {
		grants = append(grants, cv.Entry{
			Label: years(it.YearFrom, it.YearTo), Lead: it.Title,
			Text: join("\n", join(", ", it.Funder, prefixed("№ ", it.Number), grantRoleName[it.Role]), it.Description),
		})
	}
	for _, it := range s.Patents {
		patents = append(patents, cv.Entry{
			Label: yearOf(it.Year), Lead: it.Title,
			Text: join("\n", join(", ", patentTypeName[it.PatentType], prefixed("№ ", it.Number), it.Office), it.Authors),
		})
	}
	for _, it := range s.Teaching {
		teach = append(teach, cv.Entry{
			Label: years(it.YearFrom, it.YearTo), Lead: it.Course,
			Text: join("\n", join(", ", it.Institution, teachLevelName[it.Level]), it.Description),
		})
	}
	add(cv.Section{Heading: "Образование", Entries: edu})
	add(cv.Section{Heading: "Опыт работы", Entries: exp})
	add(cv.Section{Heading: "Публикации", Entries: pubs})
	add(cv.Section{Heading: "Гранты", Entries: grants})
	add(cv.Section{Heading: "Патенты и программы", Entries: patents})
	add(cv.Section{Heading: "Преподавание", Entries: teach})
	return doc
}

// spinText: SPIN-код из восьми цифр читается привычно, 1234-5678.
func spinText(spin string) string {
	if len(spin) != 8 || strings.Trim(spin, "0123456789") != "" {
		return spin
	}
	return spin[:4] + "-" + spin[4:]
}

// hIndexLine: «РИНЦ 12, Scopus 9»; только заполненные базы, без единой строка пустая.
func hIndexLine(h HIndex) string {
	var parts []string
	for _, it := range []struct {
		base string
		v    *int
	}{{"РИНЦ", h.RSCI}, {"Scopus", h.Scopus}, {"Web of Science", h.WoS}, {"Google Scholar", h.Scholar}} {
		if it.v != nil {
			parts = append(parts, fmt.Sprintf("%s %d", it.base, *it.v))
		}
	}
	return strings.Join(parts, ", ")
}

// idFacts: ORCID, SPIN, Scopus ID, WoS ResearcherID, GitHub, только заполненные.
func idFacts(ids Identifiers) []cv.Fact {
	var out []cv.Fact
	for _, it := range []cv.Fact{
		{Label: "ORCID", Value: ids.ORCID}, {Label: "SPIN", Value: spinText(ids.SPIN)},
		{Label: "Scopus ID", Value: ids.ScopusID}, {Label: "WoS ResearcherID", Value: ids.WosID},
		{Label: "GitHub", Value: githubText(ids.GitHubURL)},
	} {
		if it.Value != "" {
			out = append(out, it)
		}
	}
	return out
}

// githubText: в резюме ссылка читается как github.com/логин, без схемы.
func githubText(link string) string { return strings.TrimPrefix(link, "https://") }

func degreeEntries(v View) []cv.Entry {
	var out []cv.Entry
	if label, ok := degreeLabel[v.Degree.Level]; ok {
		lead := label
		if v.Degree.Specialty != nil {
			lead += ", " + v.Degree.Specialty.Code + " " + v.Degree.Specialty.Name
		}
		text := join("\n", prefixed("Диссертация: «", wrapQuote(v.Degree.Dissertation)), v.Degree.Institution)
		out = append(out, cv.Entry{Label: yearOf(v.Degree.Year), Lead: lead, Text: text})
	}
	if label, ok := titleLabel[v.AcademicTitle]; ok {
		out = append(out, cv.Entry{Label: yearOf(v.AcademicTitleYear), Lead: label})
	}
	return out
}

// wrapQuote дописывает закрывающую кавычку к названию; у пустого названия ничего нет.
func wrapQuote(s string) string {
	if s == "" {
		return ""
	}
	return s + "»"
}

// anonymousSubtitle: «Кандидат наук, 1.4.4», у анонимного профиля это всё, что можно сказать о человеке в заголовке.
func anonymousSubtitle(v View) string {
	var parts []string
	if label, ok := degreeLabel[v.Degree.Level]; ok {
		parts = append(parts, label)
		if v.Degree.Specialty != nil {
			parts = append(parts, v.Degree.Specialty.Code)
		}
	}
	if label, ok := titleLabel[v.AcademicTitle]; ok {
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func renderCV(page Page, product string, now time.Time) ([]byte, error) {
	return cv.Render(cvDocument(page, product, now))
}

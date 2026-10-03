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
	return join(" ", withDot(it.Authors), withDot(biblio), withDot(vol), prefixed("DOI: ", it.DOI), pubTypeNote(it.PubType))
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
		doc.Meta = append(doc.Meta, loc)
	}
	if page.Viewer.CanSeeContacts && v.ContactEmail != "" {
		doc.Meta = append(doc.Meta, "Почта: "+v.ContactEmail)
	}
	ids := v.Identifiers
	if line := join("  ·  ", prefixed("ORCID ", ids.ORCID), prefixed("SPIN ", spinText(ids.SPIN)), prefixed("Scopus ID ", ids.ScopusID), prefixed("WoS ResearcherID ", ids.WosID)); line != "" {
		doc.Meta = append(doc.Meta, line)
	}
	h := v.HIndex
	if line := join("  ·  ", hLine("РИНЦ", h.RSCI), hLine("Scopus", h.Scopus), hLine("Web of Science", h.WoS), hLine("Google Scholar", h.Scholar)); line != "" {
		doc.Meta = append(doc.Meta, "h-index: "+line)
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

func hLine(base string, v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%s %d", base, *v)
}

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

func renderCV(page Page, product string, now time.Time) ([]byte, error) {
	return cv.Render(cvDocument(page, product, now))
}

package vacancies

import (
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
)

// ParseSearchQuery переводит строку запроса (параметры адреса поиска) в параметры поиска. Ошибку формата (не число, не
// номер подразделения) отдаёт как ошибку поля; допустимость значений проверяет сам поиск (ValidateSearch).
func ParseSearchQuery(q url.Values) (SearchParams, error) {
	errs := map[string]string{}
	ints := func(name string) []int {
		var out []int
		for _, raw := range q[name] {
			n, err := strconv.Atoi(raw)
			if err != nil {
				errs[name] = "Нужно число: " + raw
				return nil
			}
			out = append(out, n)
		}
		return out
	}
	flag := func(name string) bool { return q.Get(name) == "1" || q.Get(name) == "true" }
	p := SearchParams{
		Query: q.Get("q"), Fields: q["field"], Region: q.Get("region"), Formats: q["format"], Types: q["type"],
		Levels: ints("level"), Degrees: q["degree"], OrgKinds: q["org_kind"], Fundings: q["funding"],
		Rates: ints("rate"), Terms: q["term"], Housing: flag("housing"), Competition: flag("competition"),
		Deadline: q.Get("deadline"), Sort: q.Get("sort"), OrgSlug: q.Get("org"),
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil {
		p.Limit = n
	}
	if n, err := strconv.Atoi(q.Get("offset")); err == nil {
		p.Offset = n
	}
	if raw := q.Get("salary_min"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			errs["salary_min"] = "Нужно число: " + raw
		}
		p.SalaryMin = n
	}
	if raw := q.Get("unit"); raw != "" {
		u, err := uuid.Parse(raw)
		if err != nil {
			return SearchParams{}, ErrNotFound
		}
		p.UnitID = &u
	}
	if len(errs) > 0 {
		return SearchParams{}, &auth.ValidationError{Fields: errs}
	}
	return p, nil
}

// ValidateSearch проверяет и приводит в порядок условия поиска (лишние пробелы в словах). Ошибки по полям, как в Search.
func ValidateSearch(p *SearchParams) error { return validateSearch(p) }

// HasConditions — задано ли хоть что-то, кроме порядка: слова или хотя бы один фильтр. Поиск «всё подряд» не сохраняется.
func (p SearchParams) HasConditions() bool {
	return p.Query != "" || len(p.Fields) > 0 || p.Region != "" || len(p.Formats) > 0 || len(p.Types) > 0 ||
		len(p.Levels) > 0 || len(p.Degrees) > 0 || len(p.OrgKinds) > 0 || len(p.Fundings) > 0 || len(p.Rates) > 0 ||
		len(p.Terms) > 0 || p.SalaryMin > 0 || p.Housing || p.Competition || p.Deadline != ""
}

// Encode записывает условия поиска строкой запроса, как они стоят в адресе страницы поиска: параметры по алфавиту, без
// страницы, организации и подразделения. Пустые условия не пишутся. Обратное действие — ParseSearchQuery.
func (p SearchParams) Encode() string {
	v := url.Values{}
	if p.Query != "" {
		v.Set("q", p.Query)
	}
	for _, f := range p.Fields {
		v.Add("field", f)
	}
	if p.Region != "" {
		v.Set("region", p.Region)
	}
	for _, x := range p.Formats {
		v.Add("format", x)
	}
	for _, x := range p.Types {
		v.Add("type", x)
	}
	for _, x := range p.Levels {
		v.Add("level", strconv.Itoa(x))
	}
	for _, x := range p.Degrees {
		v.Add("degree", x)
	}
	for _, x := range p.OrgKinds {
		v.Add("org_kind", x)
	}
	for _, x := range p.Fundings {
		v.Add("funding", x)
	}
	for _, x := range p.Rates {
		v.Add("rate", strconv.Itoa(x))
	}
	for _, x := range p.Terms {
		v.Add("term", x)
	}
	if p.SalaryMin > 0 {
		v.Set("salary_min", strconv.Itoa(p.SalaryMin))
	}
	if p.Housing {
		v.Set("housing", "1")
	}
	if p.Competition {
		v.Set("competition", "1")
	}
	if p.Deadline != "" {
		v.Set("deadline", p.Deadline)
	}
	if p.Sort != "" {
		v.Set("sort", p.Sort)
	}
	return v.Encode()
}

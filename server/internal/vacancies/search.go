package vacancies

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// Сроки договора для фильтра: бессрочный и три «корзины» срочных договоров.
const (
	TermPermanent = "permanent" // бессрочный
	TermShort     = "short"     // срочный, до года включительно
	TermMedium    = "medium"    // срочный, больше года и до трёх лет включительно
	TermLong      = "long"      // срочный, больше трёх лет
)

// Terms — все значения фильтра «срок договора».
var Terms = []string{TermPermanent, TermShort, TermMedium, TermLong}

// Срок подачи в фильтре.
const (
	DeadlineWeek  = "week"  // в ближайшие 7 дней
	DeadlineMonth = "month" // в ближайшие 30 дней
	DeadlineNone  = "none"  // срок не указан
)

// Deadlines — все значения фильтра «срок подачи».
var Deadlines = []string{DeadlineWeek, DeadlineMonth, DeadlineNone}

// Порядок в выдаче.
const (
	SortRelevance = "relevance" // по совпадению со словами поиска (без слов — как «новые»)
	SortNew       = "new"       // сначала новые
	SortDeadline  = "deadline"  // сначала ближайший срок подачи, без срока в конце
	SortSalary    = "salary"    // сначала больше зарплата, без зарплаты в конце
)

// Sorts — все порядки выдачи.
var Sorts = []string{SortRelevance, SortNew, SortDeadline, SortSalary}

// Пределы поиска.
const (
	MaxQueryLen    = 200
	maxFilterItems = 30
)

// fieldCode — код области науки: «1» (раздел), «1.4» (группа) или «1.4.4» (специальность).
var fieldCode = regexp.MustCompile(`^[1-9][0-9]*(\.[0-9]+){0,2}$`)

// SearchParams — что ищет человек. Пустое поле значит «любые». Несколько значений одного фильтра — «или».
type SearchParams struct {
	Query       string
	Fields      []string // коды: «1.4» находит все специальности группы
	Region      string   // код региона
	Formats     []string
	Types       []string // типы позиций
	Levels      []int    // 1–4
	Degrees     []string
	OrgKinds    []string
	Fundings    []string
	Rates       []int
	Terms       []string
	SalaryMin   int  // рублей в месяц; 0 — без ограничения
	Housing     bool // только с жильём
	Competition bool // только конкурсы
	Deadline    string
	Sort        string

	// PublishedAfter и PublishedUntil — отбор по моменту первой публикации: после первого (не включая) и до второго
	// (включая). Из адреса не читаются: нужны рассылкам по сохранённым поискам (срез 11).
	PublishedAfter, PublishedUntil *time.Time
	// NoFuzzy запрещает запасной поиск «по похожим словам»: рассылка не должна присылать неточные совпадения.
	NoFuzzy bool

	// Организация и подразделение: для страниц организаций. Если указана организация, которой нет, или подразделение не
	// из неё, вакансий «нет» (ErrNotFound).
	OrgSlug string
	UnitID  *uuid.UUID

	Limit  int
	Offset int
}

// SearchResult — страница выдачи.
type SearchResult struct {
	Items []Card `json:"items"`
	Total int    `json:"total"`
	// Fuzzy — точных совпадений со словами поиска не нашлось, показаны похожие (с опечатками).
	Fuzzy bool `json:"fuzzy"`
}

func normalizeQuery(q string) string {
	return strings.Join(strings.Fields(q), " ")
}

// validateSearch проверяет значения фильтров. Неизвестное значение — ошибка поля, а не молчаливое «любые»:
// так видно, что ссылка устарела или набрана неверно.
func validateSearch(p *SearchParams) error {
	errs := map[string]string{}
	p.Query = normalizeQuery(p.Query)
	if utf8.RuneCountInString(p.Query) > MaxQueryLen {
		errs["q"] = "Слишком длинный запрос: не больше 200 знаков"
	}
	check := func(field string, values []string, allowed []string) {
		if len(values) > maxFilterItems {
			errs[field] = "Слишком много значений"
			return
		}
		for _, v := range values {
			if !isOneOf(v, allowed) {
				errs[field] = "Неизвестное значение: " + v
				return
			}
		}
	}
	check("format", p.Formats, WorkFormats)
	check("type", p.Types, PositionTypes)
	check("degree", p.Degrees, Degrees)
	check("org_kind", p.OrgKinds, orgs.OrgKinds)
	check("funding", p.Fundings, FundingSources)
	check("term", p.Terms, Terms)
	if len(p.Fields) > maxFilterItems {
		errs["field"] = "Слишком много значений"
	}
	for _, f := range p.Fields {
		if !fieldCode.MatchString(f) {
			errs["field"] = "Неизвестная область науки: " + f
			break
		}
	}
	for _, l := range p.Levels {
		if !isOneOfInt(l, []int{1, 2, 3, 4}) {
			errs["level"] = "Уровень от 1 до 4"
			break
		}
	}
	for _, r := range p.Rates {
		if !isOneOfInt(r, Rates) {
			errs["rate"] = "Неизвестная ставка: " + strconv.Itoa(r)
			break
		}
	}
	if p.Region != "" && (len(p.Region) != 2 || strings.Trim(p.Region, "0123456789") != "") {
		errs["region"] = "Неизвестный регион"
	}
	if p.SalaryMin < 0 || p.SalaryMin > MaxSalary {
		errs["salary_min"] = "Зарплата от 0 до 100 000 000"
	}
	if p.Deadline != "" && !isOneOf(p.Deadline, Deadlines) {
		errs["deadline"] = "Неизвестное значение: " + p.Deadline
	}
	if p.Sort != "" && !isOneOf(p.Sort, Sorts) {
		errs["sort"] = "Неизвестный порядок: " + p.Sort
	}
	if len(errs) > 0 {
		return &auth.ValidationError{Fields: errs}
	}
	return nil
}

func ints32(in []int) []int32 {
	out := make([]int32, len(in))
	for i, n := range in {
		out[i] = int32(n)
	}
	return out
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Search — поиск среди опубликованных вакансий. Вакансии с прошедшим сроком подачи не показываются. Если слов поиска
// совпадений нет, повторяет поиск с допуском опечаток и говорит об этом (Fuzzy).
func (s *Service) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	if err := validateSearch(&p); err != nil {
		return SearchResult{}, err
	}
	var orgID *uuid.UUID
	if p.OrgSlug != "" {
		org, err := s.q.GetOrganizationBySlug(ctx, p.OrgSlug)
		if errors.Is(err, pgx.ErrNoRows) {
			return SearchResult{}, ErrNotFound
		}
		if err != nil {
			return SearchResult{}, fmt.Errorf("vacancies: load organization: %w", err)
		}
		orgID = &org.ID
		if p.UnitID != nil {
			if _, err := s.q.GetUnitInOrg(ctx, dbgen.GetUnitInOrgParams{ID: *p.UnitID, OrgID: org.ID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return SearchResult{}, ErrNotFound
				}
				return SearchResult{}, fmt.Errorf("vacancies: load unit: %w", err)
			}
		}
	}

	today := s.today()
	limit, offset := pageOf(p.Limit, p.Offset)
	// Без выбранного порядка со словами поиска идут по совпадению, без слов — новые сверху.
	sort := p.Sort
	if sort == "" && p.Query != "" {
		sort = SortRelevance
	}
	arg := dbgen.SearchVacanciesParams{
		Today: today, OrgID: orgID, UnitID: p.UnitID, Q: p.Query,
		Fields: nonNil(p.Fields), Region: p.Region, Formats: nonNil(p.Formats), Types: nonNil(p.Types),
		Levels: ints32(p.Levels), Degrees: nonNil(p.Degrees), OrgKinds: nonNil(p.OrgKinds), Fundings: nonNil(p.Fundings),
		Rates: ints32(p.Rates), Terms: nonNil(p.Terms), SalaryMin: int32(p.SalaryMin), Housing: p.Housing,
		Competition: p.Competition, NoDeadline: p.Deadline == DeadlineNone, Sort: sort, RowLimit: limit, RowOffset: offset,
		PublishedAfter: p.PublishedAfter, PublishedUntil: p.PublishedUntil,
	}
	switch p.Deadline {
	case DeadlineWeek:
		d := today.AddDate(0, 0, 7)
		arg.DeadlineTo = &d
	case DeadlineMonth:
		d := today.AddDate(0, 0, 30)
		arg.DeadlineTo = &d
	}

	rows, total, err := s.searchPage(ctx, arg)
	if err != nil {
		return SearchResult{}, err
	}
	fuzzy := false
	if total == 0 && p.Query != "" && !p.NoFuzzy {
		arg.Fuzzy = true
		if rows, total, err = s.searchPage(ctx, arg); err != nil {
			return SearchResult{}, err
		}
		fuzzy = total > 0
	}
	views := make([]dbgen.VacancyView, len(rows))
	for i, r := range rows {
		views[i] = r.VacancyView
	}
	items, err := cardsFrom(ctx, s.q, views)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Items: items, Total: total, Fuzzy: fuzzy}, nil
}

// searchPage читает страницу выдачи и общее число найденного. Если страница пуста, а смещение не нулевое (страница
// дальше последней), число находит отдельным пробным запросом на первую строку.
func (s *Service) searchPage(ctx context.Context, arg dbgen.SearchVacanciesParams) ([]dbgen.SearchVacanciesRow, int, error) {
	rows, err := s.q.SearchVacancies(ctx, arg)
	if err != nil {
		return nil, 0, fmt.Errorf("vacancies: search: %w", err)
	}
	if len(rows) > 0 {
		return rows, int(rows[0].Total), nil
	}
	if arg.RowOffset == 0 {
		return rows, 0, nil
	}
	arg.RowOffset, arg.RowLimit = 0, 1
	probe, err := s.q.SearchVacancies(ctx, arg)
	if err != nil {
		return nil, 0, fmt.Errorf("vacancies: search: %w", err)
	}
	if len(probe) == 0 {
		return rows, 0, nil
	}
	return rows, int(probe[0].Total), nil
}

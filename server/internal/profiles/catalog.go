package profiles

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
	"scibox/server/internal/privacy"
)

// Каталог учёных (срез 10, D-097): поиск по профилям, которые смотрящему положено видеть. Какие режимы приватности ему
// видны, решает privacy.CatalogModes; здесь только запрос и проверка условий. Контактной почты в карточках нет вообще.

// Порядок в каталоге.
const (
	SortRelevance = "relevance" // по совпадению со словами поиска (без слов — как «недавно обновлены»)
	SortUpdated   = "updated"   // сначала недавно обновлённые профили
	SortHIndex    = "h_index"   // сначала больше h-index (лучшее из четырёх значений)
	SortName      = "name"      // по алфавиту
)

// CatalogSorts — все порядки каталога.
var CatalogSorts = []string{SortRelevance, SortUpdated, SortHIndex, SortName}

// Пределы каталога.
const (
	MaxCatalogQuery = 200
	maxFilterValues = 30
	defaultPage     = 20
	maxPage         = 50
)

// fieldCode — код области науки: «1» (раздел), «1.4» (группа) или «1.4.4» (специальность).
var fieldCode = regexp.MustCompile(`^[1-9][0-9]*(\.[0-9]+){0,2}$`)

// CatalogParams — что ищет человек. Пустое поле значит «любые». Несколько значений одного фильтра — «или».
type CatalogParams struct {
	Query    string
	Fields   []string // коды областей науки: «1.4» находит все специальности группы
	Region   string   // код региона
	Degrees  []string // none | candidate | doctor
	Titles   []string // none | docent | professor
	OpenOnly bool     // только «открыт к предложениям»
	HMin     int      // h-index не меньше (лучшее из четырёх значений); 0 — без ограничения
	Q12Min   int      // статей в журналах Q1–Q2 не меньше (за всё время); 0 — без ограничения
	Sort     string
	Limit    int
	Offset   int
}

// CatalogCard — карточка учёного в каталоге: только то, что видно из любого режима, и без контактов.
type CatalogCard struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Headline      string    `json:"headline"`
	City          string    `json:"city"`
	Region        string    `json:"region"`
	Degree        string    `json:"degree"`
	AcademicTitle string    `json:"academic_title"`
	OpenToOffers  bool      `json:"open_to_offers"`
	HIndex        *int      `json:"h_index"` // лучшее из введённых значений; нет, если ничего не указано
	Publications  int       `json:"publications"`
	Q12Total      int       `json:"q12_total"`  // статей в журналах Q1–Q2
	Q12Recent     int       `json:"q12_recent"` // из них с года RecentFrom ответа
	Specialties   []Code    `json:"specialties"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CatalogResult — страница каталога.
type CatalogResult struct {
	Items []CatalogCard `json:"items"`
	Total int           `json:"total"`
	// Fuzzy — точных совпадений со словами поиска не нашлось, показаны похожие (с опечатками).
	Fuzzy bool `json:"fuzzy"`
	// RecentFrom — с какого года считается q12_recent в карточках (последние пять лет по Москве).
	RecentFrom int `json:"recent_from"`
}

func (p *CatalogParams) validate() error {
	errs := map[string]string{}
	p.Query = strings.Join(strings.Fields(p.Query), " ")
	if utf8.RuneCountInString(p.Query) > MaxCatalogQuery {
		errs["q"] = "Слишком длинный запрос: не больше 200 знаков"
	}
	if len(p.Fields) > maxFilterValues {
		errs["field"] = "Слишком много значений"
	}
	for _, f := range p.Fields {
		if !fieldCode.MatchString(f) {
			errs["field"] = "Неизвестная область науки: " + f
			break
		}
	}
	check := func(field string, values, allowed []string) {
		if len(values) > maxFilterValues {
			errs[field] = "Слишком много значений"
			return
		}
		for _, v := range values {
			if !contains(allowed, v) {
				errs[field] = "Неизвестное значение: " + v
				return
			}
		}
	}
	check("degree", p.Degrees, degrees)
	check("title", p.Titles, titles)
	if p.Region != "" && (len(p.Region) != 2 || strings.Trim(p.Region, "0123456789") != "") {
		errs["region"] = "Неизвестный регион"
	}
	if p.HMin < 0 || p.HMin > maxHIndex {
		errs["h_min"] = fmt.Sprintf("h-index от 0 до %d", maxHIndex)
	}
	if p.Q12Min < 0 || p.Q12Min > maxItemsKind[KindPublication] {
		errs["q12_min"] = fmt.Sprintf("Число статей от 0 до %d", maxItemsKind[KindPublication])
	}
	if p.Sort != "" && !contains(CatalogSorts, p.Sort) {
		errs["sort"] = "Неизвестный порядок: " + p.Sort
	}
	if len(errs) > 0 {
		return &auth.ValidationError{Fields: errs}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Catalog ищет учёных глазами смотрящего (nil — аноним). Профили вне разрешённых ему режимов приватности, без должности
// и собственный профиль смотрящего в выдачу не попадают.
func (s *Service) Catalog(ctx context.Context, p CatalogParams, viewer *auth.User) (CatalogResult, error) {
	if err := p.validate(); err != nil {
		return CatalogResult{}, err
	}
	who := privacy.Viewer{}
	var exclude *uuid.UUID
	if viewer != nil {
		staff, err := s.q.IsOrgStaff(ctx, viewer.ID)
		if err != nil {
			return CatalogResult{}, fmt.Errorf("profiles: check organization staff: %w", err)
		}
		who.Staff = staff
		exclude = &viewer.ID
	}
	modes := make([]string, 0, len(privacy.Modes))
	for _, m := range privacy.CatalogModes(who) {
		modes = append(modes, string(m))
	}

	limit, offset := p.Limit, p.Offset
	if limit <= 0 {
		limit = defaultPage
	}
	limit, offset = min(limit, maxPage), max(offset, 0)
	// Без выбранного порядка со словами поиска идут по совпадению, без слов — недавно обновлённые.
	sort := p.Sort
	if sort == "" && p.Query != "" {
		sort = SortRelevance
	}
	arg := dbgen.SearchScientistsParams{
		Modes: modes, ExcludeUser: exclude, Q: p.Query, Fields: nonNil(p.Fields), Region: p.Region,
		Degrees: nonNil(p.Degrees), Titles: nonNil(p.Titles), OpenOnly: p.OpenOnly, HMin: num.Int32(p.HMin),
		Q12Min: num.Int32(p.Q12Min), RecentFrom: num.Int32(s.recentFrom()),
		Sort: sort, RowLimit: num.Int32(limit), RowOffset: num.Int32(offset),
	}
	rows, total, err := s.catalogPage(ctx, arg)
	if err != nil {
		return CatalogResult{}, err
	}
	fuzzy := false
	if total == 0 && p.Query != "" {
		arg.Fuzzy = true
		if rows, total, err = s.catalogPage(ctx, arg); err != nil {
			return CatalogResult{}, err
		}
		fuzzy = total > 0
	}

	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	specs, err := s.q.ListSpecialtiesForProfiles(ctx, ids)
	if err != nil {
		return CatalogResult{}, fmt.Errorf("profiles: load catalog specialties: %w", err)
	}
	byProfile := map[uuid.UUID][]Code{}
	for _, sp := range specs {
		byProfile[sp.ProfileID] = append(byProfile[sp.ProfileID], Code{Code: sp.Code, Name: sp.Name})
	}
	out := CatalogResult{Items: make([]CatalogCard, 0, len(rows)), Total: total, Fuzzy: fuzzy, RecentFrom: int(arg.RecentFrom)}
	for _, r := range rows {
		c := CatalogCard{
			ID: r.ID, Name: r.DisplayName, Headline: r.Headline, City: r.City, Degree: r.Degree, AcademicTitle: r.AcademicTitle,
			OpenToOffers: r.OpenToOffers, Publications: int(r.Publications), Q12Total: int(r.Q12Total), Q12Recent: int(r.Q12Recent),
			Specialties: byProfile[r.ID], UpdatedAt: r.UpdatedAt,
		}
		if c.Specialties == nil {
			c.Specialties = []Code{}
		}
		if r.RegionName != nil {
			c.Region = *r.RegionName
		}
		if r.HMax > 0 {
			h := int(r.HMax)
			c.HIndex = &h
		}
		out.Items = append(out.Items, c)
	}
	return out, nil
}

// catalogPage читает страницу выдачи и общее число найденного. Если страница пуста, а смещение не нулевое (страница
// дальше последней), число находит отдельным пробным запросом на первую строку.
func (s *Service) catalogPage(ctx context.Context, arg dbgen.SearchScientistsParams) ([]dbgen.SearchScientistsRow, int, error) {
	rows, err := s.q.SearchScientists(ctx, arg)
	if err != nil {
		return nil, 0, fmt.Errorf("profiles: search catalog: %w", err)
	}
	if len(rows) > 0 {
		return rows, int(rows[0].Total), nil
	}
	if arg.RowOffset == 0 {
		return rows, 0, nil
	}
	arg.RowOffset, arg.RowLimit = 0, 1
	probe, err := s.q.SearchScientists(ctx, arg)
	if err != nil {
		return nil, 0, fmt.Errorf("profiles: search catalog: %w", err)
	}
	if len(probe) == 0 {
		return rows, 0, nil
	}
	return rows, int(probe[0].Total), nil
}

package matching

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/orgs"
	"scibox/server/internal/vacancies"
)

// Ошибки раздела.
var (
	// ErrNotFound — вакансии или поиска нет, или они чужие (одинаковый ответ).
	ErrNotFound = errors.New("matching: not found")
	// ErrTooManyFavorites — в избранном уже предельное число вакансий.
	ErrTooManyFavorites = errors.New("matching: too many favorites")
	// ErrTooManySearches — сохранено предельное число поисков.
	ErrTooManySearches = errors.New("matching: too many saved searches")
)

// Config — настройки сервиса.
type Config struct {
	// MaxFavorites и MaxSearches — пределы на человека.
	MaxFavorites, MaxSearches int
	// Settle — запас перед «сейчас» при рассылке: вакансия, которую публикуют прямо сейчас, попадёт в следующий проход, а не
	// потеряется между ними.
	Settle time.Duration
	// MaxListed — сколько вакансий перечислять в одном письме о новых; остальные учитываются числом.
	MaxListed int
	// Batch — сколько записей фоновый цикл берёт за один раз.
	Batch int
}

// DefaultConfig — боевые настройки: 200 избранных и 20 поисков на человека, письмо перечисляет до 10 вакансий.
func DefaultConfig() Config {
	return Config{MaxFavorites: 200, MaxSearches: 20, Settle: time.Minute, MaxListed: 10, Batch: 100}
}

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Searcher — поиск вакансий (его даёт пакет vacancies); подменяется в тестах.
type Searcher interface {
	Search(ctx context.Context, p vacancies.SearchParams) (vacancies.SearchResult, error)
}

// Service — вся логика раздела.
type Service struct {
	db    DB
	q     *dbgen.Queries
	cfg   Config
	vac   Searcher
	notes *notifications.Service
	log   *slog.Logger
	now   func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, vac *vacancies.Service, notes *notifications.Service, cfg Config, log *slog.Logger) *Service {
	return newService(pool, vac, notes, cfg, log)
}

func newService(db DB, vac Searcher, notes *notifications.Service, cfg Config, log *slog.Logger) *Service {
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, vac: vac, notes: notes, log: log, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("matching: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("matching: commit: %w", err)
	}
	return nil
}

func (s *Service) today() time.Time { return moscowToday(s.now()) }

func pageOf(limit, offset int) (int32, int32) {
	if limit <= 0 {
		limit = 20
	}
	return int32(min(limit, 50)), int32(max(offset, 0))
}

// ---- избранное ----

// FavoriteItem — вакансия в избранном.
type FavoriteItem struct {
	Vacancy vacancies.Card `json:"vacancy"`
	AddedAt time.Time      `json:"added_at"`
	// State: open (можно откликнуться), expired (срок подачи прошёл), closed (набор закончен).
	State string `json:"state"`
	// ApplicationID — живой отклик человека на эту вакансию, если он есть.
	ApplicationID *uuid.UUID `json:"application_id"`
}

// FavoriteList — страница избранного.
type FavoriteList struct {
	Items []FavoriteItem `json:"items"`
	Total int            `json:"total"`
}

// Состояния вакансии в избранном.
const (
	StateOpen    = "open"
	StateExpired = "expired"
	StateClosed  = "closed"
)

// daysLeft — сколько дней до последнего дня подачи (0 — срок сегодня). Нет срока — 0.
func daysLeft(today time.Time, deadline *time.Time) int {
	if deadline == nil {
		return 0
	}
	return int(deadline.Sub(today).Hours() / 24)
}

func stateOf(status string, deadline *time.Time, today time.Time) string {
	switch {
	case status != vacancies.StatusPublished:
		return StateClosed
	case deadline != nil && deadline.Before(today):
		return StateExpired
	}
	return StateOpen
}

// AddFavorite кладёт вакансию в избранное. Вакансия должна быть публичной (опубликована или закрыта), иначе «нет такой».
// Повторное добавление ничего не меняет. Больше MaxFavorites не помещается.
func (s *Service) AddFavorite(ctx context.Context, user auth.User, vacancyID uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		status, err := q.GetVacancyStatusByID(ctx, vacancyID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !vacancies.PubliclyVisible(status)) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("matching: load vacancy: %w", err)
		}
		// Строка человека заперта: два запроса одновременно не пройдут мимо предела.
		if _, err := q.LockUser(ctx, user.ID); err != nil {
			return fmt.Errorf("matching: lock user: %w", err)
		}
		n, err := q.AddFavorite(ctx, dbgen.AddFavoriteParams{UserID: user.ID, VacancyID: vacancyID, Now: s.now()})
		if err != nil {
			return fmt.Errorf("matching: add favorite: %w", err)
		}
		if n == 0 {
			return nil // уже в избранном
		}
		total, err := q.CountFavorites(ctx, user.ID)
		if err != nil {
			return fmt.Errorf("matching: count favorites: %w", err)
		}
		if int(total) > s.cfg.MaxFavorites {
			return ErrTooManyFavorites // транзакция откатится вместе с добавлением
		}
		return nil
	})
}

// RemoveFavorite убирает вакансию из избранного. Если её там не было, ничего не происходит.
func (s *Service) RemoveFavorite(ctx context.Context, user auth.User, vacancyID uuid.UUID) error {
	if _, err := s.q.RemoveFavorite(ctx, dbgen.RemoveFavoriteParams{UserID: user.ID, VacancyID: vacancyID}); err != nil {
		return fmt.Errorf("matching: remove favorite: %w", err)
	}
	return nil
}

// FavoriteIDs — номера всех избранных вакансий: сайт отмечает ими закладки в списках.
func (s *Service) FavoriteIDs(ctx context.Context, user auth.User) ([]uuid.UUID, error) {
	ids, err := s.q.ListFavoriteIDs(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("matching: list favorite ids: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

// Favorites — «Избранное»: сначала вакансии, на которые ещё можно откликнуться, внутри групп новые добавления сверху.
// Вакансии в архиве не показываются (открыть их нельзя), но из избранного не пропадают.
func (s *Service) Favorites(ctx context.Context, user auth.User, limit, offset int) (FavoriteList, error) {
	lim, off := pageOf(limit, offset)
	today := s.today()
	rows, err := s.q.ListFavorites(ctx, dbgen.ListFavoritesParams{UserID: user.ID, Today: today, RowLimit: lim, RowOffset: off})
	if err != nil {
		return FavoriteList{}, fmt.Errorf("matching: list favorites: %w", err)
	}
	views := make([]dbgen.VacancyView, len(rows))
	for i, r := range rows {
		views[i] = r.VacancyView
	}
	cards, err := vacancies.CardsFrom(ctx, s.q, views)
	if err != nil {
		return FavoriteList{}, err
	}
	out := FavoriteList{Items: make([]FavoriteItem, len(rows))}
	for i, r := range rows {
		out.Items[i] = FavoriteItem{Vacancy: cards[i], AddedAt: r.AddedAt, State: stateOf(r.VacancyView.Status, r.VacancyView.Deadline, today), ApplicationID: r.ApplicationID}
	}
	if len(rows) > 0 {
		out.Total = int(rows[0].Total)
	} else if off > 0 {
		// Страница дальше последней: общее число берём отдельным запросом, чтобы сайт мог предложить вернуться.
		probe, err := s.q.ListFavorites(ctx, dbgen.ListFavoritesParams{UserID: user.ID, Today: today, RowLimit: 1, RowOffset: 0})
		if err != nil {
			return FavoriteList{}, fmt.Errorf("matching: list favorites: %w", err)
		}
		if len(probe) > 0 {
			out.Total = int(probe[0].Total)
		}
	}
	return out, nil
}

// ---- календарь сроков ----

// DeadlineItem — вакансия из избранного со сроком подачи.
type DeadlineItem struct {
	Vacancy       vacancies.Card `json:"vacancy"`
	DaysLeft      int            `json:"days_left"`
	ApplicationID *uuid.UUID     `json:"application_id"`
}

// DeadlineCalendar — сроки избранного: ближайшие первыми. WithoutDeadline — сколько открытых вакансий в избранном без срока.
type DeadlineCalendar struct {
	Items           []DeadlineItem `json:"items"`
	WithoutDeadline int            `json:"without_deadline"`
}

// Deadlines собирает календарь сроков: опубликованные вакансии из избранного, срок которых сегодня или позже.
func (s *Service) Deadlines(ctx context.Context, user auth.User) (DeadlineCalendar, error) {
	today := s.today()
	rows, err := s.q.ListFavoriteDeadlines(ctx, dbgen.ListFavoriteDeadlinesParams{UserID: user.ID, Today: today})
	if err != nil {
		return DeadlineCalendar{}, fmt.Errorf("matching: list deadlines: %w", err)
	}
	views := make([]dbgen.VacancyView, len(rows))
	for i, r := range rows {
		views[i] = r.VacancyView
	}
	cards, err := vacancies.CardsFrom(ctx, s.q, views)
	if err != nil {
		return DeadlineCalendar{}, err
	}
	none, err := s.q.CountFavoritesWithoutDeadline(ctx, user.ID)
	if err != nil {
		return DeadlineCalendar{}, fmt.Errorf("matching: count undated: %w", err)
	}
	out := DeadlineCalendar{Items: make([]DeadlineItem, len(rows)), WithoutDeadline: int(none)}
	for i, r := range rows {
		out.Items[i] = DeadlineItem{Vacancy: cards[i], DaysLeft: daysLeft(today, r.VacancyView.Deadline), ApplicationID: r.ApplicationID}
	}
	return out, nil
}

// ---- подбор ----

// MatchItem — вакансия в подборке с причинами.
type MatchItem struct {
	Vacancy vacancies.Card `json:"vacancy"`
	Score   int            `json:"score"`
	Reasons []string       `json:"reasons"`
}

// MatchBasis — на что опирается подбор: сайт объясняет это человеку.
type MatchBasis struct {
	Specialties int  `json:"specialties"`
	Level       int  `json:"level"`
	HasRegion   bool `json:"has_region"`
	HasDegree   bool `json:"has_degree"`
}

// MatchList — «Подходящие вам». Ready false: в профиле нет областей науки, подбирать не по чему.
type MatchList struct {
	Items []MatchItem `json:"items"`
	Total int         `json:"total"`
	Ready bool        `json:"ready"`
	Basis MatchBasis  `json:"basis"`
}

// maxCandidates — сколько новейших вакансий из нужных разделов науки проверяют правила.
const maxCandidates = 500

// Matches подбирает вакансии по правилам (rules.go): общая группа специальностей обязательна, степень и звание не выше, чем у человека,
// дальше баллы за точность области, уровень R, степень и регион. Не показываются вакансии, на которые человек откликнулся,
// и вакансии организаций, где он сам разбирает отклики.
func (s *Service) Matches(ctx context.Context, user auth.User, limit, offset int) (MatchList, error) {
	prof, err := s.matchProfile(ctx, user)
	if err != nil {
		return MatchList{}, err
	}
	out := MatchList{
		Items: []MatchItem{}, Ready: prof.Ready(),
		Basis: MatchBasis{Specialties: len(prof.Specialties), Level: prof.Level(), HasRegion: prof.Region != "", HasDegree: prof.Degree != "none"},
	}
	if !out.Ready {
		return out, nil
	}
	own, err := orgs.ScopeOf(ctx, s.q, user.ID, access.ViewApplications)
	if err != nil {
		return MatchList{}, err
	}
	rows, err := s.q.ListMatchCandidates(ctx, dbgen.ListMatchCandidatesParams{
		Today: s.today(), Groups: prof.Groups(), UserID: user.ID, OwnOrgs: own.WholeOrgs, OwnUnits: own.Units, Batch: maxCandidates,
	})
	if err != nil {
		return MatchList{}, fmt.Errorf("matching: list candidates: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	codes, err := s.q.ListSpecialtyCodesOfVacancies(ctx, ids)
	if err != nil {
		return MatchList{}, fmt.Errorf("matching: list vacancy specialties: %w", err)
	}
	specs := map[uuid.UUID][]string{}
	for _, c := range codes {
		specs[c.VacancyID] = append(specs[c.VacancyID], c.SpecialtyCode)
	}
	type scored struct {
		row dbgen.VacancyView
		m   Match
	}
	var found []scored
	for _, r := range rows {
		op := Opening{Specialties: specs[r.ID], Degree: r.DegreeRequired, Title: r.TitleRequired, Region: deref(r.RegionCode), Format: deref(r.WorkFormat)}
		if r.CareerLevel != nil {
			lvl := int(*r.CareerLevel)
			op.CareerLevel = &lvl
		}
		if m, ok := Score(prof, op); ok {
			found = append(found, scored{r, m})
		}
	}
	// Лучшие первыми; при равных баллах раньше тот, у кого ближе срок (без срока в конце), затем новые (порядок запроса).
	sortStable(found, func(a, b scored) bool {
		if a.m.Score != b.m.Score {
			return a.m.Score > b.m.Score
		}
		switch {
		case a.row.Deadline != nil && b.row.Deadline != nil && !a.row.Deadline.Equal(*b.row.Deadline):
			return a.row.Deadline.Before(*b.row.Deadline)
		case (a.row.Deadline != nil) != (b.row.Deadline != nil):
			return a.row.Deadline != nil
		}
		return false
	})
	out.Total = len(found)
	lim, off := pageOf(limit, offset)
	from := min(int(off), len(found))
	to := min(from+int(lim), len(found))
	page := found[from:to]
	views := make([]dbgen.VacancyView, len(page))
	for i, f := range page {
		views[i] = f.row
	}
	cards, err := vacancies.CardsFrom(ctx, s.q, views)
	if err != nil {
		return MatchList{}, err
	}
	for i, f := range page {
		out.Items = append(out.Items, MatchItem{Vacancy: cards[i], Score: f.m.Score, Reasons: f.m.Reasons})
	}
	return out, nil
}

func (s *Service) matchProfile(ctx context.Context, user auth.User) (Profile, error) {
	p, err := s.q.GetMatchProfile(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{Degree: "none", Title: "none"}, nil // профиля ещё нет: подбирать не по чему
	}
	if err != nil {
		return Profile{}, fmt.Errorf("matching: load profile: %w", err)
	}
	codes, err := s.q.ListProfileSpecialtyCodes(ctx, p.ID)
	if err != nil {
		return Profile{}, fmt.Errorf("matching: load profile specialties: %w", err)
	}
	return Profile{Specialties: codes, Degree: p.Degree, Title: p.AcademicTitle, Region: deref(p.RegionCode)}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- сохранённые поиски ----

// SavedSearch — сохранённый поиск.
type SavedSearch struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Query      string     `json:"query"`
	Frequency  string     `json:"frequency"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSentAt *time.Time `json:"last_sent_at"`
}

func searchFrom(r dbgen.SavedSearch) SavedSearch {
	return SavedSearch{ID: r.ID, Name: r.Name, Query: r.Query, Frequency: r.Frequency, CreatedAt: r.CreatedAt, LastSentAt: r.LastSentAt}
}

// SearchInput — что человек присылает, когда сохраняет поиск или правит сохранённый. Query — строка запроса со страницы
// поиска без знака «?»; при правке не читается (условия поиска не меняются, меняют имя и частоту).
type SearchInput struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	Frequency string `json:"frequency"`
}

const (
	maxNameLen  = 120
	maxQueryLen = 2000
)

// checkName проверяет название: от 1 до 120 знаков без управляющих.
func checkName(name string) (string, string) {
	name = oneLine(name)
	switch {
	case name == "":
		return name, "Назовите поиск, чтобы потом найти его в списке"
	case utf8.RuneCountInString(name) > maxNameLen:
		return name, "Название слишком длинное: не больше 120 знаков"
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return name, "В названии есть недопустимые символы"
		}
	}
	return name, ""
}

// CreateSearch сохраняет поиск. Условия берутся из строки запроса, проверяются как в самом поиске и записываются в
// каноническом виде. Поиск без единого условия (всё подряд) не сохраняется. Нужный запас вакансий считается с этого момента:
// о том, что уже есть, письмо не придёт.
func (s *Service) CreateSearch(ctx context.Context, user auth.User, in SearchInput) (SavedSearch, error) {
	errs := map[string]string{}
	name, msg := checkName(in.Name)
	if msg != "" {
		errs["name"] = msg
	}
	freq := in.Frequency
	if freq == "" {
		freq = DefaultFrequency
	}
	if !validFrequency(freq) {
		errs["frequency"] = "Выберите, как сообщать о новых вакансиях"
	}
	query, qmsg := canonicalQuery(in.Query)
	if qmsg != "" {
		errs["query"] = qmsg
	}
	if len(errs) > 0 {
		return SavedSearch{}, &auth.ValidationError{Fields: errs}
	}
	var saved SavedSearch
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		if _, err := q.LockUser(ctx, user.ID); err != nil {
			return fmt.Errorf("matching: lock user: %w", err)
		}
		n, err := q.CountSavedSearches(ctx, user.ID)
		if err != nil {
			return fmt.Errorf("matching: count searches: %w", err)
		}
		if int(n) >= s.cfg.MaxSearches {
			return ErrTooManySearches
		}
		now := s.now()
		row, err := q.InsertSavedSearch(ctx, dbgen.InsertSavedSearchParams{
			UserID: user.ID, Name: name, Query: query, Frequency: freq, Now: now, NextRunAt: NextRun(freq, now),
		})
		if err != nil {
			return fmt.Errorf("matching: insert search: %w", err)
		}
		saved = searchFrom(row)
		return nil
	})
	return saved, err
}

// canonicalQuery разбирает строку запроса, проверяет значения и пишет их по каноническому образцу.
func canonicalQuery(raw string) (string, string) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "?")
	if utf8.RuneCountInString(raw) > maxQueryLen {
		return "", "Условия поиска слишком длинные"
	}
	values, err := parseValues(raw)
	if err != nil {
		return "", "Условия поиска записаны неверно"
	}
	p, err := vacancies.ParseSearchQuery(values)
	if err == nil {
		err = vacancies.ValidateSearch(&p)
	}
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		return "", firstMessage(verr.Fields)
	case err != nil:
		return "", "Условия поиска записаны неверно"
	case !p.HasConditions():
		return "", "Введите слова или выберите хотя бы один фильтр: поиск «всех вакансий» сохранять незачем"
	}
	enc := p.Encode()
	if utf8.RuneCountInString(enc) > maxQueryLen {
		return "", "Условия поиска слишком длинные"
	}
	return enc, ""
}

// Searches — сохранённые поиски человека, новые сверху.
func (s *Service) Searches(ctx context.Context, user auth.User) ([]SavedSearch, error) {
	rows, err := s.q.ListSavedSearches(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("matching: list searches: %w", err)
	}
	out := make([]SavedSearch, len(rows))
	for i, r := range rows {
		out[i] = searchFrom(r)
	}
	return out, nil
}

// Search — один сохранённый поиск; чужого и несуществующего нет.
func (s *Service) Search(ctx context.Context, user auth.User, id uuid.UUID) (SavedSearch, error) {
	row, err := s.q.GetSavedSearch(ctx, dbgen.GetSavedSearchParams{ID: id, UserID: user.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedSearch{}, ErrNotFound
	}
	if err != nil {
		return SavedSearch{}, fmt.Errorf("matching: load search: %w", err)
	}
	return searchFrom(row), nil
}

// UpdateSearch меняет название и частоту своего поиска. Когда рассылка включается после паузы («не сообщать»), счёт новых
// вакансий начинается заново: письмо не засыплет всем, что набралось за время паузы.
func (s *Service) UpdateSearch(ctx context.Context, user auth.User, id uuid.UUID, in SearchInput) (SavedSearch, error) {
	errs := map[string]string{}
	name, msg := checkName(in.Name)
	if msg != "" {
		errs["name"] = msg
	}
	if !validFrequency(in.Frequency) {
		errs["frequency"] = "Выберите, как сообщать о новых вакансиях"
	}
	if len(errs) > 0 {
		return SavedSearch{}, &auth.ValidationError{Fields: errs}
	}
	var saved SavedSearch
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		old, err := q.GetSavedSearch(ctx, dbgen.GetSavedSearchParams{ID: id, UserID: user.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("matching: load search: %w", err)
		}
		now := s.now()
		next := old.NextRunAt
		var restart *time.Time
		if in.Frequency != old.Frequency {
			next = NextRun(in.Frequency, now)
			if old.Frequency == FreqOff {
				restart = &now
			}
		}
		row, err := q.UpdateSavedSearch(ctx, dbgen.UpdateSavedSearchParams{
			ID: id, UserID: user.ID, Name: name, Frequency: in.Frequency, NextRunAt: next, Now: now, CheckedAt: restart,
		})
		if err != nil {
			return fmt.Errorf("matching: update search: %w", err)
		}
		saved = searchFrom(row)
		return nil
	})
	return saved, err
}

// DeleteSearch удаляет свой поиск. Чужого или несуществующего нет.
func (s *Service) DeleteSearch(ctx context.Context, user auth.User, id uuid.UUID) error {
	n, err := s.q.DeleteSavedSearch(ctx, dbgen.DeleteSavedSearchParams{ID: id, UserID: user.ID})
	if err != nil {
		return fmt.Errorf("matching: delete search: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

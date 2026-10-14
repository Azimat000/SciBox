package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/issn"
	"scibox/server/internal/privacy"
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	// ErrNotFound — профиля или записи нет, либо смотрящему они «не существуют» (скрытый профиль, чужая запись).
	ErrNotFound = errors.New("profiles: not found")
	// ErrTooMany — в разделе уже максимум записей.
	ErrTooMany = errors.New("profiles: too many items in the section")
	// ErrDOINotFound — Crossref не знает такого DOI.
	ErrDOINotFound = errors.New("profiles: doi not found")
	// ErrDOIUnavailable — Crossref недоступен: человек заполняет публикацию вручную.
	ErrDOIUnavailable = errors.New("profiles: crossref is unavailable")
)

// kindDOILookup — вид счётчика частоты (таблица rate_events общая с остальными разделами).
const (
	kindDOILookup   = "doi_lookup"
	kindProfileView = "profile_view"
)

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	ProductName string
	// DOI — сколько раз в час человек может искать публикации по DOI (бережём чужой сервис).
	DOI Limit
	// Views — сколько чужих закрытых профилей (с контактами) сотрудник организации может открыть за сутки (срез 29, D-191):
	// организация создаётся за минуту без проверки, и без предела любой её сотрудник выгрузил бы базу контактов.
	Views Limit
	// MaxResults — сколько первых результатов каталога доступно для листания; 0 — MaxCatalogResults (в тестах задаётся малое число).
	MaxResults int
}

// maxResults — предел выдачи каталога.
func (c Config) maxResults() int {
	if c.MaxResults > 0 {
		return c.MaxResults
	}
	return MaxCatalogResults
}

// DefaultConfig — боевые настройки: 60 поисков по DOI в час и 150 просмотров чужих профилей в сутки с одного аккаунта.
func DefaultConfig(productName string) Config {
	return Config{ProductName: productName, DOI: Limit{Max: 60, Window: time.Hour}, Views: Limit{Max: 150, Window: 24 * time.Hour}}
}

// views — предел просмотров с нулём в настройках не значит «без предела».
func (c Config) views() Limit {
	if c.Views.Max > 0 && c.Views.Window > 0 {
		return c.Views
	}
	return DefaultConfig("").Views
}

// DOIResolver ищет публикацию по DOI (crossref.Client в бою, подставной в тестах).
type DOIResolver interface {
	Lookup(ctx context.Context, doi string) (crossref.Work, error)
}

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика профилей.
type Service struct {
	db  DB
	q   *dbgen.Queries
	cfg Config
	doi DOIResolver
	now func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, doi DOIResolver, cfg Config) *Service {
	return newService(pool, doi, cfg)
}

func newService(db DB, doi DOIResolver, cfg Config) *Service {
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, doi: doi, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("profiles: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("profiles: commit: %w", err)
	}
	return nil
}

// ensure создаёт профиль человека, если его ещё нет, и берёт его под блокировку: две правки одного профиля идут по очереди.
func (s *Service) ensure(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) (uuid.UUID, error) {
	if err := q.EnsureProfile(ctx, dbgen.EnsureProfileParams{UserID: userID, Now: s.now()}); err != nil {
		return uuid.Nil, fmt.Errorf("profiles: ensure profile: %w", err)
	}
	id, err := q.LockProfileByUser(ctx, userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("profiles: lock profile: %w", err)
	}
	return id, nil
}

// ---- чтение ----

// Own — собственный профиль человека; создаётся пустым и скрытым при первом обращении.
func (s *Service) Own(ctx context.Context, user auth.User) (Page, error) {
	if err := s.q.EnsureProfile(ctx, dbgen.EnsureProfileParams{UserID: user.ID, Now: s.now()}); err != nil {
		return Page{}, fmt.Errorf("profiles: ensure profile: %w", err)
	}
	row, err := s.q.GetProfileByUser(ctx, user.ID)
	if err != nil {
		return Page{}, fmt.Errorf("profiles: load profile: %w", err)
	}
	page, err := s.build(ctx, s.q, row.Profile, row.DisplayName, row.RegionName, row.DegreeSpecialtyName, privacy.Viewer{Owner: true})
	if err != nil {
		return Page{}, err
	}
	hidden, err := s.q.ListHiddenOrgs(ctx, row.Profile.ID)
	if err != nil {
		return Page{}, fmt.Errorf("profiles: list hidden organizations: %w", err)
	}
	page.HiddenOrgs = make([]HiddenOrg, 0, len(hidden))
	for _, h := range hidden {
		page.HiddenOrgs = append(page.HiddenOrgs, HiddenOrg{ID: h.ID, Slug: h.Slug, Name: h.Name, City: h.City})
	}
	return page, nil
}

// Get — профиль по номеру глазами смотрящего (nil — аноним). Если приватность не пускает, ответ тот же, что у несуществующего.
func (s *Service) Get(ctx context.Context, id uuid.UUID, viewer *auth.User) (Page, error) {
	row, err := s.q.GetProfileByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("profiles: load profile: %w", err)
	}
	who, err := s.viewerOf(ctx, row.Profile.ID, row.Profile.UserID, viewer)
	if err != nil {
		return Page{}, err
	}
	vis, _ := privacy.Parse(row.Profile.Visibility)
	if !vis.CanView(who) {
		return Page{}, ErrNotFound
	}
	// Сотрудник организации открывает чужие профили с контактами не больше предела в сутки (D-191).
	if who.Staff && !who.Owner && viewer != nil {
		if err := s.spendKind(ctx, kindProfileView, s.cfg.views(), viewer.ID); err != nil {
			return Page{}, err
		}
	}
	return s.build(ctx, s.q, row.Profile, row.DisplayName, row.RegionName, row.DegreeSpecialtyName, who)
}

// viewerOf собирает сведения о смотрящем для приватности: владелец ли он, состоит ли в организации, скрыт ли профиль от
// одной из его организаций и открыто ли ей имя (D-199, D-200).
func (s *Service) viewerOf(ctx context.Context, profileID, owner uuid.UUID, viewer *auth.User) (privacy.Viewer, error) {
	if viewer == nil {
		return privacy.Viewer{}, nil
	}
	if viewer.ID == owner {
		return privacy.Viewer{Owner: true}, nil
	}
	orgIDs, err := s.q.ListUserOrgIDs(ctx, viewer.ID)
	if err != nil {
		return privacy.Viewer{}, fmt.Errorf("profiles: list organizations: %w", err)
	}
	if len(orgIDs) == 0 {
		return privacy.Viewer{}, nil
	}
	rel, err := s.q.ProfileOrgRelations(ctx, dbgen.ProfileOrgRelationsParams{ProfileID: profileID, UserID: owner, OrgIds: orgIDs})
	if err != nil {
		return privacy.Viewer{}, fmt.Errorf("profiles: check profile relations: %w", err)
	}
	return privacy.Viewer{Staff: true, HiddenFrom: rel.HiddenFrom, Revealed: rel.Revealed}, nil
}

// build собирает профиль для смотрящего: контакты и приватность попадают в ответ, только если им это положено.
func (s *Service) build(ctx context.Context, q *dbgen.Queries, p dbgen.Profile, name string, regionName, specialtyName *string, who privacy.Viewer) (Page, error) {
	specs, err := q.ListProfileSpecialties(ctx, p.ID)
	if err != nil {
		return Page{}, fmt.Errorf("profiles: load specialties: %w", err)
	}
	items, err := q.ListProfileItems(ctx, p.ID)
	if err != nil {
		return Page{}, fmt.Errorf("profiles: load items: %w", err)
	}
	vis, _ := privacy.Parse(p.Visibility)
	v := View{
		ID: p.ID, Name: name, OpenToOffers: p.OpenToOffers, Headline: p.Headline, City: p.City, About: p.About,
		AcademicTitle: p.AcademicTitle, AcademicTitleYear: intp(p.AcademicTitleYear),
		Degree:      DegreeInfo{Level: p.Degree, Year: intp(p.DegreeYear), Institution: p.DegreeInstitution, Dissertation: p.DissertationTitle},
		Identifiers: Identifiers{ORCID: p.Orcid, SPIN: p.Spin, ScopusID: p.ScopusID, WosID: p.WosID},
		HIndex:      HIndex{RSCI: intp(p.HRsci), Scopus: intp(p.HScopus), WoS: intp(p.HWos), Scholar: intp(p.HScholar)},
		Specialties: make([]Code, 0, len(specs)),
		Skills:      Skills{Research: nonNil(p.ResearchSkills), General: nonNil(p.GeneralSkills)},
		Sections:    emptySections(),
		UpdatedAt:   p.UpdatedAt,
	}
	if p.RegionCode != nil && regionName != nil {
		v.Region = &Code{Code: *p.RegionCode, Name: *regionName}
	}
	if p.DegreeSpecialtyCode != nil && specialtyName != nil {
		v.Degree.Specialty = &Code{Code: *p.DegreeSpecialtyCode, Name: *specialtyName}
	}
	for _, sp := range specs {
		v.Specialties = append(v.Specialties, Code{Code: sp.Code, Name: sp.Name})
	}
	for _, it := range items {
		item, err := itemOf(it)
		if err != nil {
			return Page{}, err
		}
		v.Sections.add(item)
	}
	if err := s.attachJournals(ctx, q, &v); err != nil {
		return Page{}, err
	}
	if who.Owner {
		v.Visibility = p.Visibility
	}
	canContacts := vis.CanSeeContacts(who)
	if canContacts {
		v.ContactEmail = p.ContactEmail
	}
	if vis.Detail(who) == privacy.Blind {
		v = blindView(v)
	}
	return Page{Profile: v, Viewer: ViewerInfo{IsOwner: who.Owner, CanSeeContacts: canContacts}}, nil
}

// blindView — безымянная карточка (D-199). Собирается списком разрешённого, а не вычёркиванием лишнего: новое поле
// профиля по умолчанию в безымянную карточку не попадает. Разрешено: степень (уровень и специальность), звание, регион,
// специальности, h-index, счётчики публикаций и статей в Q1–Q2, «открыт к предложениям», дата обновления.
func blindView(v View) View {
	pubs := len(v.Sections.Publications)
	return View{
		ID: v.ID, OpenToOffers: v.OpenToOffers, Region: v.Region,
		Degree:        DegreeInfo{Level: v.Degree.Level, Specialty: v.Degree.Specialty},
		AcademicTitle: v.AcademicTitle,
		HIndex:        v.HIndex, Specialties: v.Specialties,
		Skills: Skills{Research: []string{}, General: []string{}}, Sections: emptySections(),
		Quartiles: v.Quartiles, Anonymous: true, PublicationCount: &pubs, UpdatedAt: v.UpdatedAt,
	}
}

// RecentYears — сколько последних лет, считая текущий, входит в «статей в Q1–Q2 за 5 лет».
const RecentYears = 5

// moscow — «текущий год» для счётчика считается по Москве, как сроки подачи (D-057).
var moscow = time.FixedZone("MSK", 3*60*60)

// recentFrom — первый год «последних пяти лет».
func (s *Service) recentFrom() int { return s.now().In(moscow).Year() - (RecentYears - 1) }

// journalsOf находит журналы справочника по ISSN.
func journalsOf(ctx context.Context, q *dbgen.Queries, issns []string) (map[string]JournalRef, error) {
	out := map[string]JournalRef{}
	if len(issns) == 0 {
		return out, nil
	}
	rows, err := q.JournalsByISSN(ctx, issns)
	if err != nil {
		return nil, fmt.Errorf("profiles: load journals: %w", err)
	}
	for _, r := range rows {
		out[r.Issn] = JournalRef{Title: r.Title, ISSN: r.Issn, Quartile: intp(r.Quartile), Year: int(r.DataYear)}
	}
	return out, nil
}

// attachJournals подставляет публикациям журналы из справочника и считает «статей в Q1–Q2» (D-127): всего и с года
// recentFrom. Публикация без года в «последние пять лет» не попадает.
func (s *Service) attachJournals(ctx context.Context, q *dbgen.Queries, v *View) error {
	v.Quartiles = QuartileStats{RecentFrom: s.recentFrom()}
	pubs := v.Sections.Publications
	var issns []string
	for _, it := range pubs {
		if it.ISSN != "" {
			issns = append(issns, it.ISSN)
		}
	}
	found, err := journalsOf(ctx, q, issns)
	if err != nil {
		return err
	}
	for i := range pubs {
		j, ok := found[pubs[i].ISSN]
		if !ok {
			continue
		}
		pubs[i].Journal = &j
		if v.Quartiles.Year == nil || *v.Quartiles.Year < j.Year {
			year := j.Year
			v.Quartiles.Year = &year
		}
		if j.Quartile == nil || *j.Quartile > 2 {
			continue
		}
		v.Quartiles.Q12Total++
		if y := pubs[i].Year; y != nil && *y >= v.Quartiles.RecentFrom {
			v.Quartiles.Q12Recent++
		}
	}
	return nil
}

func intp(v *int16) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func emptySections() Sections {
	return Sections{
		Education: []Item{}, Experience: []Item{}, Publications: []Item{}, Grants: []Item{}, Patents: []Item{}, Teaching: []Item{},
	}
}

func (s *Sections) add(it Item) {
	switch it.Kind {
	case KindEducation:
		s.Education = append(s.Education, it)
	case KindExperience:
		s.Experience = append(s.Experience, it)
	case KindPublication:
		s.Publications = append(s.Publications, it)
	case KindGrant:
		s.Grants = append(s.Grants, it)
	case KindPatent:
		s.Patents = append(s.Patents, it)
	case KindTeaching:
		s.Teaching = append(s.Teaching, it)
	}
}

func itemOf(r dbgen.ListProfileItemsRow) (Item, error) {
	it := Item{ID: r.ID, Kind: r.Kind}
	if err := json.Unmarshal(r.Data, &it.ItemFields); err != nil {
		return Item{}, fmt.Errorf("profiles: decode item %s: %w", r.ID, err)
	}
	return it, nil
}

// ---- основные поля и приватность ----

// check проверяет основные поля и то, что регион и специальности есть в справочниках.
func (s *Service) check(ctx context.Context, q *dbgen.Queries, in CoreInput) (coreFields, error) {
	f, errs := validateCore(in, s.now())
	if f.regionCode != nil {
		ok, err := q.RegionExists(ctx, *f.regionCode)
		if err != nil {
			return coreFields{}, fmt.Errorf("profiles: check region: %w", err)
		}
		if !ok {
			errs["region_code"] = msgRegionUnknown
		}
	}
	if f.degreeSpecialty != nil {
		known, err := q.ExistingSpecialtyCodes(ctx, []string{*f.degreeSpecialty})
		if err != nil {
			return coreFields{}, fmt.Errorf("profiles: check degree specialty: %w", err)
		}
		if len(known) != 1 {
			errs["degree_specialty_code"] = msgSpecUnknown
		}
	}
	if len(f.specialties) > 0 && errs["specialties"] == "" {
		known, err := q.ExistingSpecialtyCodes(ctx, f.specialties)
		if err != nil {
			return coreFields{}, fmt.Errorf("profiles: check specialties: %w", err)
		}
		if len(known) != len(f.specialties) {
			errs["specialties"] = msgSpecsUnknown
		}
	}
	if len(errs) > 0 {
		return coreFields{}, &auth.ValidationError{Fields: errs}
	}
	return f, nil
}

// SaveCore сохраняет основные поля профиля целиком (приватность и записи разделов не трогает).
func (s *Service) SaveCore(ctx context.Context, user auth.User, in CoreInput) (Page, error) {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		id, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		f, err := s.check(ctx, q, in)
		if err != nil {
			return err
		}
		n, err := q.UpdateProfileCore(ctx, dbgen.UpdateProfileCoreParams{
			ID: id, Headline: f.headline, City: f.city, RegionCode: f.regionCode, About: f.about, Degree: f.degree,
			DegreeSpecialtyCode: f.degreeSpecialty, DegreeYear: f.degreeYear, DegreeInstitution: f.degreeInstitution,
			DissertationTitle: f.dissertation, AcademicTitle: f.academicTitle, AcademicTitleYear: f.academicTitleYear,
			Orcid: f.orcid, Spin: f.spin, ScopusID: f.scopusID, WosID: f.wosID,
			HRsci: f.hRsci, HScopus: f.hScopus, HWos: f.hWos, HScholar: f.hScholar,
			ContactEmail: f.contactEmail, ResearchSkills: f.researchSkills, GeneralSkills: f.generalSkills, Now: s.now(),
		})
		if err != nil {
			return fmt.Errorf("profiles: update profile: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := q.DeleteProfileSpecialties(ctx, id); err != nil {
			return fmt.Errorf("profiles: clear specialties: %w", err)
		}
		if len(f.specialties) == 0 {
			return nil
		}
		if err := q.AddProfileSpecialties(ctx, dbgen.AddProfileSpecialtiesParams{ProfileID: id, Codes: f.specialties}); err != nil {
			return fmt.Errorf("profiles: add specialties: %w", err)
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return s.Own(ctx, user)
}

// SetPrivacy меняет, кому виден профиль, и отметку «открыт к предложениям».
func (s *Service) SetPrivacy(ctx context.Context, user auth.User, visibility string, openToOffers bool) (Page, error) {
	vis, ok := privacy.Parse(visibility)
	if !ok {
		return Page{}, &auth.ValidationError{Fields: map[string]string{"visibility": msgPickFromList}}
	}
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		id, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		n, err := q.UpdateProfilePrivacy(ctx, dbgen.UpdateProfilePrivacyParams{ID: id, Visibility: string(vis), OpenToOffers: openToOffers, Now: s.now()})
		if err != nil {
			return fmt.Errorf("profiles: update privacy: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return s.Own(ctx, user)
}

// MaxHiddenOrgs — от скольких организаций человек может скрыть профиль.
const MaxHiddenOrgs = 30

// SetHiddenOrgs заменяет список организаций, от сотрудников которых скрыт профиль (D-200): они не видят его ни в каком
// режиме. Организации выбираются из существующих; повторы отбрасываются. Скрытый от организации человек, который сам
// откликается на её вакансию, откликается обычным образом: отклик его осознанный шаг (D-079).
func (s *Service) SetHiddenOrgs(ctx context.Context, user auth.User, orgIDs []uuid.UUID) (Page, error) {
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(orgIDs))
	for _, id := range orgIDs {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxHiddenOrgs {
		return Page{}, &auth.ValidationError{Fields: map[string]string{"org_ids": fmt.Sprintf("Не больше %d организаций", MaxHiddenOrgs)}}
	}
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		id, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			known, err := q.ExistingOrgIDs(ctx, ids)
			if err != nil {
				return fmt.Errorf("profiles: check organizations: %w", err)
			}
			if len(known) != len(ids) {
				return &auth.ValidationError{Fields: map[string]string{"org_ids": "Такой организации нет. Выберите организации из списка"}}
			}
		}
		if err := q.DeleteHiddenOrgs(ctx, id); err != nil {
			return fmt.Errorf("profiles: clear hidden organizations: %w", err)
		}
		if len(ids) > 0 {
			if err := q.AddHiddenOrgs(ctx, dbgen.AddHiddenOrgsParams{ProfileID: id, OrgIds: ids, Now: s.now()}); err != nil {
				return fmt.Errorf("profiles: add hidden organizations: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return s.Own(ctx, user)
}

// ---- записи разделов ----

// marshalItem записывает поля записи в JSON. У ItemFields только строки и числа, поэтому ошибки кодирования не бывает.
func marshalItem(f ItemFields) []byte {
	data, _ := json.Marshal(f)
	return data
}

// checkItem проверяет запись; для публикации ещё и что такого DOI в профиле нет (кроме записи except, которую как раз правят).
func (s *Service) checkItem(ctx context.Context, q *dbgen.Queries, profileID uuid.UUID, kind string, in ItemFields, except uuid.UUID) (itemFields, error) {
	f, errs := validateItem(kind, in, s.now())
	if f.fields.DOI != "" {
		dup, err := q.ProfileHasDOI(ctx, dbgen.ProfileHasDOIParams{ProfileID: profileID, Doi: f.fields.DOI, ExceptID: except})
		if err != nil {
			return itemFields{}, fmt.Errorf("profiles: check doi: %w", err)
		}
		if dup {
			errs["doi"] = msgDOIDuplicate
		}
	}
	if len(errs) > 0 {
		return itemFields{}, &auth.ValidationError{Fields: errs}
	}
	return f, nil
}

// AddItem добавляет запись в раздел своего профиля.
func (s *Service) AddItem(ctx context.Context, user auth.User, in ItemInput) (Item, error) {
	if _, known := maxItemsKind[in.Kind]; !known {
		return Item{}, &auth.ValidationError{Fields: map[string]string{"kind": msgKindInvalid}}
	}
	var out Item
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		profileID, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		count, err := q.CountProfileItems(ctx, dbgen.CountProfileItemsParams{ProfileID: profileID, Kind: in.Kind})
		if err != nil {
			return fmt.Errorf("profiles: count items: %w", err)
		}
		if count >= int64(maxItemsKind[in.Kind]) {
			return ErrTooMany
		}
		f, err := s.checkItem(ctx, q, profileID, in.Kind, in.ItemFields, uuid.Nil)
		if err != nil {
			return err
		}
		id, err := q.InsertProfileItem(ctx, dbgen.InsertProfileItemParams{ProfileID: profileID, Kind: in.Kind, SortYear: f.sortYear, Data: marshalItem(f.fields), Now: s.now()})
		if err != nil {
			return fmt.Errorf("profiles: insert item: %w", err)
		}
		if err := q.TouchProfile(ctx, dbgen.TouchProfileParams{ID: profileID, Now: s.now()}); err != nil {
			return fmt.Errorf("profiles: touch profile: %w", err)
		}
		out = Item{ID: id, Kind: in.Kind, ItemFields: f.fields}
		return nil
	})
	return out, err
}

// ownItem находит запись и проверяет, что она принадлежит человеку; чужая запись «не существует».
func (s *Service) ownItem(ctx context.Context, q *dbgen.Queries, user auth.User, id uuid.UUID) (dbgen.GetProfileItemRow, error) {
	row, err := q.GetProfileItem(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetProfileItemRow{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetProfileItemRow{}, fmt.Errorf("profiles: load item: %w", err)
	}
	if row.UserID != user.ID {
		return dbgen.GetProfileItemRow{}, ErrNotFound
	}
	return row, nil
}

// UpdateItem меняет запись в своём профиле. Вид записи менять нельзя.
func (s *Service) UpdateItem(ctx context.Context, user auth.User, id uuid.UUID, in ItemInput) (Item, error) {
	var out Item
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := s.ownItem(ctx, q, user, id)
		if err != nil {
			return err
		}
		if _, err := s.ensure(ctx, q, user.ID); err != nil {
			return err
		}
		if in.Kind != "" && in.Kind != row.Kind {
			return &auth.ValidationError{Fields: map[string]string{"kind": msgKindMismatch}}
		}
		f, err := s.checkItem(ctx, q, row.ProfileID, row.Kind, in.ItemFields, id)
		if err != nil {
			return err
		}
		n, err := q.UpdateProfileItem(ctx, dbgen.UpdateProfileItemParams{ID: id, SortYear: f.sortYear, Data: marshalItem(f.fields), Now: s.now()})
		if err != nil {
			return fmt.Errorf("profiles: update item: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := q.TouchProfile(ctx, dbgen.TouchProfileParams{ID: row.ProfileID, Now: s.now()}); err != nil {
			return fmt.Errorf("profiles: touch profile: %w", err)
		}
		out = Item{ID: id, Kind: row.Kind, ItemFields: f.fields}
		return nil
	})
	return out, err
}

// DeleteItem удаляет запись из своего профиля.
func (s *Service) DeleteItem(ctx context.Context, user auth.User, id uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := s.ownItem(ctx, q, user, id)
		if err != nil {
			return err
		}
		if _, err := s.ensure(ctx, q, user.ID); err != nil {
			return err
		}
		n, err := q.DeleteProfileItem(ctx, id)
		if err != nil {
			return fmt.Errorf("profiles: delete item: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := q.TouchProfile(ctx, dbgen.TouchProfileParams{ID: row.ProfileID, Now: s.now()}); err != nil {
			return fmt.Errorf("profiles: touch profile: %w", err)
		}
		return nil
	})
}

// ---- DOI ----

// spend учитывает поиск по DOI и отказывает, если лимит исчерпан. Проверка и запись идут под замком счётчика:
// параллельные запросы не проходят проверку с одним и тем же нулём (срез 29, CON-2).
func (s *Service) spend(ctx context.Context, user uuid.UUID) error {
	return s.spendKind(ctx, kindDOILookup, s.cfg.DOI, user)
}

// spendKind — общий счётчик события вида kind на человека под замком.
func (s *Service) spendKind(ctx context.Context, kind string, lim Limit, user uuid.UUID) error {
	now := s.now()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("profiles: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err := q.LockRateKey(ctx, kind+":"+user.String()); err != nil {
		return fmt.Errorf("profiles: lock rate key: %w", err)
	}
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kind, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("profiles: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kind, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("profiles: record rate event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("profiles: commit: %w", err)
	}
	return nil
}

// FoundWork — публикация, найденная по DOI, и её журнал в справочнике.
type FoundWork struct {
	crossref.Work
	// ISSN — какой ISSN поставить в публикацию: тот, что нашёлся в справочнике, иначе первый годный из Crossref.
	ISSN string `json:"issn"`
	// Journal — журнал справочника; nil, если ни один ISSN из Crossref в справочнике не нашёлся.
	Journal *JournalRef `json:"journal"`
}

// LookupDOI находит публикацию в Crossref и её журнал в справочнике. Если такой DOI уже есть в профиле, Crossref
// не спрашиваем.
func (s *Service) LookupDOI(ctx context.Context, user auth.User, raw string) (FoundWork, error) {
	work, err := s.lookupWork(ctx, user, raw)
	if err != nil {
		return FoundWork{}, err
	}
	out := FoundWork{Work: work}
	var issns []string
	for _, rawISSN := range work.ISSNs {
		if n, ok := issn.Normalize(rawISSN); ok {
			issns = append(issns, n)
		}
	}
	found, err := journalsOf(ctx, s.q, issns)
	if err != nil {
		return FoundWork{}, err
	}
	for _, issn := range issns {
		if j, ok := found[issn]; ok {
			out.ISSN, out.Journal = issn, &j
			return out, nil
		}
	}
	if len(issns) > 0 {
		out.ISSN = issns[0]
	}
	return out, nil
}

func (s *Service) lookupWork(ctx context.Context, user auth.User, raw string) (crossref.Work, error) {
	doi, ok := crossref.NormalizeDOI(raw)
	if !ok {
		return crossref.Work{}, &auth.ValidationError{Fields: map[string]string{"doi": msgDOIInvalid}}
	}
	profile, err := s.q.GetProfileByUser(ctx, user.ID)
	switch {
	case err == nil:
		dup, err := s.q.ProfileHasDOI(ctx, dbgen.ProfileHasDOIParams{ProfileID: profile.Profile.ID, Doi: doi, ExceptID: uuid.Nil})
		if err != nil {
			return crossref.Work{}, fmt.Errorf("profiles: check doi: %w", err)
		}
		if dup {
			return crossref.Work{}, &auth.ValidationError{Fields: map[string]string{"doi": msgDOIDuplicate}}
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return crossref.Work{}, fmt.Errorf("profiles: load profile: %w", err)
	}
	if err := s.spend(ctx, user.ID); err != nil {
		return crossref.Work{}, err
	}
	work, err := s.doi.Lookup(ctx, doi)
	switch {
	case errors.Is(err, crossref.ErrNotFound):
		return crossref.Work{}, ErrDOINotFound
	case err != nil:
		return crossref.Work{}, fmt.Errorf("%w: %w", ErrDOIUnavailable, err)
	}
	work.DOI = doi
	return work, nil
}

// ---- резюме ----

// CV собирает PDF с резюме. id == nil — собственный профиль, иначе чужой по правилам приватности (то, что смотрящему
// не положено, в файл не попадает, как и на страницу). Второе значение — подходящее имя файла.
func (s *Service) CV(ctx context.Context, user auth.User, id *uuid.UUID) ([]byte, string, error) {
	var (
		page Page
		err  error
	)
	if id == nil {
		page, err = s.Own(ctx, user)
	} else {
		page, err = s.Get(ctx, *id, &user)
	}
	if err != nil {
		return nil, "", err
	}
	pdf, err := renderCV(page, s.cfg.ProductName, s.now())
	if err != nil {
		return nil, "", err
	}
	return pdf, cvFileName(page.Profile.Name), nil
}

// cvFileName: «Елена Орлова — CV.pdf».
func cvFileName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`/\:*?"<>|`, r) {
			return -1
		}
		return r
	}, name))
	if name == "" {
		return "CV.pdf"
	}
	return name + " — CV.pdf"
}

// ---- профиль для отклика ----

// ErrIncomplete — профиль слишком пуст, чтобы откликаться: не заполнена должность («кто вы»).
var ErrIncomplete = errors.New("profiles: profile is too empty to apply with")

// ApplicationPackage — то, что уходит организации вместе с откликом: снимок профиля и PDF-резюме.
type ApplicationPackage struct {
	Profile View
	CV      []byte
	CVName  string
}

// ForApplication собирает снимок собственного профиля для отклика (D-072, D-081). Человек сам откликается, поэтому режим
// приватности здесь не спрашивается: организация, на вакансию которой пришёл отклик, видит именно то, что человек отправил,
// включая контакты. Служебное поле `visibility` в снимок не попадает. Контактная почта в снимке и резюме — та,
// которую человек указал в отклике, а не из профиля. Вызывающий (пакет applications) отвечает за то, чтобы снимок
// ушёл только организации этой вакансии. Приватность профиля при этом не меняется, и для других путей (страница, каталог)
// действуют прежние правила.
func (s *Service) ForApplication(ctx context.Context, user auth.User, contactEmail string) (ApplicationPackage, error) {
	page, err := s.Own(ctx, user)
	if err != nil {
		return ApplicationPackage{}, err
	}
	v := page.Profile
	if strings.TrimSpace(v.Headline) == "" {
		return ApplicationPackage{}, ErrIncomplete
	}
	v.Visibility = ""
	v.ContactEmail = contactEmail
	pdf, err := renderCV(Page{Profile: v, Viewer: ViewerInfo{CanSeeContacts: true}}, s.cfg.ProductName, s.now())
	if err != nil {
		return ApplicationPackage{}, err
	}
	return ApplicationPackage{Profile: v, CV: pdf, CVName: cvFileName(v.Name)}, nil
}

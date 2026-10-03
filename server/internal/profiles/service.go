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
const kindDOILookup = "doi_lookup"

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
}

// DefaultConfig — боевые настройки: 60 поисков по DOI в час с одного аккаунта.
func DefaultConfig(productName string) Config {
	return Config{ProductName: productName, DOI: Limit{Max: 60, Window: time.Hour}}
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
	return s.build(ctx, s.q, row.Profile, row.DisplayName, row.RegionName, row.DegreeSpecialtyName, privacy.Viewer{Owner: true})
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
	who, err := s.viewerOf(ctx, row.Profile.UserID, viewer)
	if err != nil {
		return Page{}, err
	}
	vis, _ := privacy.Parse(row.Profile.Visibility)
	if !vis.CanView(who) {
		return Page{}, ErrNotFound
	}
	return s.build(ctx, s.q, row.Profile, row.DisplayName, row.RegionName, row.DegreeSpecialtyName, who)
}

// viewerOf собирает сведения о смотрящем для приватности: владелец ли он и состоит ли в организации.
func (s *Service) viewerOf(ctx context.Context, owner uuid.UUID, viewer *auth.User) (privacy.Viewer, error) {
	if viewer == nil {
		return privacy.Viewer{}, nil
	}
	if viewer.ID == owner {
		return privacy.Viewer{Owner: true}, nil
	}
	staff, err := s.q.IsOrgStaff(ctx, viewer.ID)
	if err != nil {
		return privacy.Viewer{}, fmt.Errorf("profiles: check organization staff: %w", err)
	}
	return privacy.Viewer{Staff: staff}, nil
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
	if who.Owner {
		v.Visibility = p.Visibility
	}
	canContacts := vis.CanSeeContacts(who)
	if canContacts {
		v.ContactEmail = p.ContactEmail
	}
	return Page{Profile: v, Viewer: ViewerInfo{IsOwner: who.Owner, CanSeeContacts: canContacts}}, nil
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
			ContactEmail: f.contactEmail, Now: s.now(),
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

// spend учитывает поиск по DOI и отказывает, если лимит исчерпан.
func (s *Service) spend(ctx context.Context, user uuid.UUID) error {
	now := s.now()
	lim := s.cfg.DOI
	row, err := s.q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindDOILookup, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("profiles: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := s.q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindDOILookup, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("profiles: record rate event: %w", err)
	}
	return nil
}

// LookupDOI находит публикацию в Crossref. Если такой DOI уже есть в профиле, Crossref не спрашиваем.
func (s *Service) LookupDOI(ctx context.Context, user auth.User, raw string) (crossref.Work, error) {
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
		return crossref.Work{}, fmt.Errorf("%w: %v", ErrDOIUnavailable, err)
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

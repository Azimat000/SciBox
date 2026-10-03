package vacancies

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

// Ошибки, которые обработчики превращают в ответы API.
var (
	ErrNotFound      = errors.New("vacancies: not found")
	ErrForbidden     = errors.New("vacancies: forbidden")
	ErrBadTransition = errors.New("vacancies: this status change is not allowed")
	ErrNotDraft      = errors.New("vacancies: only a draft can be deleted")
	ErrChanged       = errors.New("vacancies: the vacancy was changed by someone else")
)

// kindVacancyCreate — вид счётчика частоты (таблица rate_events общая с аккаунтами и организациями).
const kindVacancyCreate = "vacancy_create"

// moscow — «сегодня» для срока подачи считается по московскому времени (в России нет перехода на летнее время).
var moscow = time.FixedZone("MSK", 3*60*60)

// Limit — не больше Max событий за окно Window.
type Limit struct {
	Max    int
	Window time.Duration
}

// Config — настройки сервиса.
type Config struct {
	Create Limit // создание вакансий одним человеком
}

// DefaultConfig — боевые настройки: не больше 100 новых вакансий в сутки с одного аккаунта (защита от мусора, D-003).
func DefaultConfig() Config {
	return Config{Create: Limit{Max: 100, Window: 24 * time.Hour}}
}

// DB — то, что сервису нужно от базы: запросы и транзакции (так же, как в пакетах auth и orgs).
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика вакансий.
type Service struct {
	db  DB
	q   *dbgen.Queries
	cfg Config
	now func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, cfg Config) *Service { return newService(pool, cfg) }

func newService(db DB, cfg Config) *Service {
	return &Service{db: db, q: dbgen.New(db), cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("vacancies: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("vacancies: commit: %w", err)
	}
	return nil
}

// today — сегодняшняя дата по Москве как полночь в UTC (так сравниваются даты срока подачи).
func (s *Service) today() time.Time {
	n := s.now().In(moscow)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// spend учитывает событие и отказывает, если лимит исчерпан.
func (s *Service) spend(ctx context.Context, q *dbgen.Queries, user uuid.UUID) error {
	now := s.now()
	lim := s.cfg.Create
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kindVacancyCreate, Key: user.String(), At: now.Add(-lim.Window)})
	if err != nil {
		return fmt.Errorf("vacancies: count rate events: %w", err)
	}
	if row.Events >= int64(lim.Max) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(lim.Window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kindVacancyCreate, Key: user.String(), At: now}); err != nil {
		return fmt.Errorf("vacancies: record rate event: %w", err)
	}
	return nil
}

// ---- права ----

func unitOf(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return access.NoUnit
	}
	return *id
}

// canManage: может ли человек вести вакансии этого подразделения (nil — вакансия на всю организацию). Не вошедший не может ничего.
func canManage(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, unit *uuid.UUID, user *auth.User) (bool, error) {
	if user == nil {
		return false, nil
	}
	actor, err := orgs.ActorOf(ctx, q, orgID, user.ID)
	if err != nil {
		return false, err
	}
	return actor.Can(access.ManageVacancies, unitOf(unit)), nil
}

// authorize берёт вакансию под блокировку и проверяет, что человек может её вести. Тем, кто вести её не может,
// черновик и архив «не существуют», а опубликованная и закрытая вакансии отвечают отказом (она публична, прятать нечего).
func (s *Service) authorize(ctx context.Context, q *dbgen.Queries, id uuid.UUID, user auth.User) (dbgen.Vacancy, access.Actor, error) {
	v, err := q.LockVacancy(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.Vacancy{}, access.Actor{}, ErrNotFound
	}
	if err != nil {
		return dbgen.Vacancy{}, access.Actor{}, fmt.Errorf("vacancies: load vacancy: %w", err)
	}
	actor, err := orgs.ActorOf(ctx, q, v.OrgID, user.ID)
	if err != nil {
		return dbgen.Vacancy{}, access.Actor{}, err
	}
	if !actor.Can(access.ManageVacancies, unitOf(v.UnitID)) {
		if PubliclyVisible(v.Status) {
			return dbgen.Vacancy{}, access.Actor{}, ErrForbidden
		}
		return dbgen.Vacancy{}, access.Actor{}, ErrNotFound
	}
	return v, actor, nil
}

// ---- проверка по справочникам ----

// check проверяет поля вакансии и то, что должность, регион и специальности есть в справочниках.
func (s *Service) check(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, in Input, mode checkMode) (fields, error) {
	var positionType string
	pos, err := q.GetPosition(ctx, in.PositionCode)
	switch {
	case err == nil:
		positionType = pos.PositionType
	case !errors.Is(err, pgx.ErrNoRows):
		return fields{}, fmt.Errorf("vacancies: load position: %w", err)
	}
	f, errs := validate(in, positionType, mode, s.today())
	if positionType == "" {
		errs["position_code"] = msgPositionUnknown
	}
	if in.UnitID != nil {
		if _, err := q.GetUnitInOrg(ctx, dbgen.GetUnitInOrgParams{ID: *in.UnitID, OrgID: orgID}); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return fields{}, fmt.Errorf("vacancies: load unit: %w", err)
			}
			errs["unit_id"] = msgUnitUnknown
		}
	}
	if f.regionCode != nil {
		ok, err := q.RegionExists(ctx, *f.regionCode)
		if err != nil {
			return fields{}, fmt.Errorf("vacancies: check region: %w", err)
		}
		if !ok {
			errs["region_code"] = msgRegionRequired
		}
	}
	if len(f.specialties) > 0 && errs["specialties"] == "" {
		known, err := q.ExistingSpecialtyCodes(ctx, f.specialties)
		if err != nil {
			return fields{}, fmt.Errorf("vacancies: check specialties: %w", err)
		}
		if len(known) != len(f.specialties) {
			errs["specialties"] = msgSpecsUnknown
		}
	}
	if len(errs) > 0 {
		return fields{}, &auth.ValidationError{Fields: errs}
	}
	return f, nil
}

// ---- создание и правка ----

// Create создаёт черновик вакансии в организации (кто ведёт вакансии этого подразделения).
func (s *Service) Create(ctx context.Context, user auth.User, orgSlug string, in Input) (Detail, error) {
	var id uuid.UUID
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		org, err := q.GetOrganizationBySlug(ctx, orgSlug)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("vacancies: load organization: %w", err)
		}
		can, err := canManage(ctx, q, org.ID, in.UnitID, &user)
		if err != nil {
			return err
		}
		if !can {
			return ErrForbidden
		}
		f, err := s.check(ctx, q, org.ID, in, modeDraft)
		if err != nil {
			return err
		}
		if err := s.spend(ctx, q, user.ID); err != nil {
			return err
		}
		id, err = q.CreateVacancy(ctx, dbgen.CreateVacancyParams{
			OrgID: org.ID, UnitID: f.unitID, CreatedBy: &user.ID, Title: f.title, PositionCode: f.positionCode,
			Summary: f.summary, Description: f.description, Requirements: f.requirements, Focus: f.focus,
			CareerLevel: f.careerLevel, WorkFormat: f.workFormat, RegionCode: f.regionCode, City: f.city, Housing: f.housing,
			RatePercent: f.rate, SalaryFrom: f.salaryFrom, SalaryTo: f.salaryTo, ContractType: f.contractType,
			ContractMonths: f.contractMonths, FundingSource: f.fundingSource, FundingNote: f.fundingNote,
			DegreeRequired: f.degree, TitleRequired: f.academicTitle, IsCompetition: f.isCompetition, Deadline: f.deadline,
			Now: s.now(),
		})
		if err != nil {
			return fmt.Errorf("vacancies: create vacancy: %w", err)
		}
		return s.setSpecialties(ctx, q, id, f.specialties)
	})
	if err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id, &user)
}

func (s *Service) setSpecialties(ctx context.Context, q *dbgen.Queries, id uuid.UUID, codes []string) error {
	if err := q.DeleteVacancySpecialties(ctx, id); err != nil {
		return fmt.Errorf("vacancies: clear specialties: %w", err)
	}
	if len(codes) == 0 {
		return nil
	}
	if err := q.AddVacancySpecialties(ctx, dbgen.AddVacancySpecialtiesParams{VacancyID: id, Codes: codes}); err != nil {
		return fmt.Errorf("vacancies: add specialties: %w", err)
	}
	return nil
}

// Update меняет вакансию. Черновик можно сохранять неполным; опубликованная, закрытая и архивная должны оставаться готовыми к показу.
// Перенести вакансию в другое подразделение может тот, кто ведёт вакансии и старого, и нового.
func (s *Service) Update(ctx context.Context, user auth.User, id uuid.UUID, in Input) (Detail, error) {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		v, actor, err := s.authorize(ctx, q, id, user)
		if err != nil {
			return err
		}
		if !actor.Can(access.ManageVacancies, unitOf(in.UnitID)) {
			return ErrForbidden
		}
		mode := modeStrict
		if v.Status == StatusDraft {
			mode = modeDraft
		}
		f, err := s.check(ctx, q, v.OrgID, in, mode)
		if err != nil {
			return err
		}
		n, err := q.UpdateVacancy(ctx, dbgen.UpdateVacancyParams{
			ID: id, UnitID: f.unitID, Title: f.title, PositionCode: f.positionCode, Summary: f.summary,
			Description: f.description, Requirements: f.requirements, Focus: f.focus, CareerLevel: f.careerLevel,
			WorkFormat: f.workFormat, RegionCode: f.regionCode, City: f.city, Housing: f.housing, RatePercent: f.rate,
			SalaryFrom: f.salaryFrom, SalaryTo: f.salaryTo, ContractType: f.contractType, ContractMonths: f.contractMonths,
			FundingSource: f.fundingSource, FundingNote: f.fundingNote, DegreeRequired: f.degree,
			TitleRequired: f.academicTitle, IsCompetition: f.isCompetition, Deadline: f.deadline, Now: s.now(),
		})
		if err != nil {
			return fmt.Errorf("vacancies: update vacancy: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return s.setSpecialties(ctx, q, id, f.specialties)
	})
	if err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id, &user)
}

// inputOf превращает сохранённую вакансию обратно в поля формы: так проверяем готовность к публикации.
func inputOf(v dbgen.Vacancy, specialties []string) Input {
	in := Input{
		Title: v.Title, PositionCode: v.PositionCode, UnitID: v.UnitID, Summary: v.Summary, Description: v.Description,
		Requirements: v.Requirements, Focus: v.Focus, CareerLevel: widen(v.CareerLevel), City: v.City, Housing: v.Housing,
		RatePercent: widen(v.RatePercent), SalaryFrom: widen32(v.SalaryFrom), SalaryTo: widen32(v.SalaryTo),
		ContractMonth: widen(v.ContractMonths), FundingNote: v.FundingNote, Degree: v.DegreeRequired,
		AcademicTitle: v.TitleRequired, IsCompetition: v.IsCompetition, Specialties: specialties,
		WorkFormat: deref(v.WorkFormat), RegionCode: deref(v.RegionCode), ContractType: deref(v.ContractType),
		FundingSource: deref(v.FundingSource),
	}
	if v.Deadline != nil {
		in.Deadline = v.Deadline.Format(dateLayout)
	}
	return in
}

// SetStatus переводит вакансию в другой статус. Публикация и повторное открытие проверяют, что вакансия заполнена и срок подачи не прошёл.
func (s *Service) SetStatus(ctx context.Context, user auth.User, id uuid.UUID, to string) (Detail, error) {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		v, _, err := s.authorize(ctx, q, id, user)
		if err != nil {
			return err
		}
		if !CanTransition(v.Status, to) {
			return ErrBadTransition
		}
		if to == StatusPublished {
			rows, err := q.ListVacancySpecialties(ctx, []uuid.UUID{id})
			if err != nil {
				return fmt.Errorf("vacancies: load specialties: %w", err)
			}
			codes := make([]string, 0, len(rows))
			for _, r := range rows {
				codes = append(codes, r.Code)
			}
			if _, err := s.check(ctx, q, v.OrgID, inputOf(v, codes), modePublish); err != nil {
				return err
			}
		}
		n, err := q.SetVacancyStatus(ctx, dbgen.SetVacancyStatusParams{ID: id, FromStatus: v.Status, ToStatus: to, Now: s.now()})
		if err != nil {
			return fmt.Errorf("vacancies: set status: %w", err)
		}
		if n == 0 {
			return ErrChanged
		}
		return nil
	})
	if err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id, &user)
}

// Delete удаляет черновик. Опубликованные вакансии не удаляются: их закрывают и убирают в архив.
func (s *Service) Delete(ctx context.Context, user auth.User, id uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		v, _, err := s.authorize(ctx, q, id, user)
		if err != nil {
			return err
		}
		if v.Status != StatusDraft {
			return ErrNotDraft
		}
		n, err := q.DeleteDraftVacancy(ctx, id)
		if err != nil {
			return fmt.Errorf("vacancies: delete draft: %w", err)
		}
		if n == 0 {
			return ErrChanged
		}
		return nil
	})
}

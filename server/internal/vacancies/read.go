package vacancies

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
)

const (
	dateLayout = "2006-01-02"
	// DefaultLimit и MaxLimit — сколько вакансий отдаёт один запрос списка.
	DefaultLimit = 50
	MaxLimit     = 100
)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func widen(n *int16) *int {
	if n == nil {
		return nil
	}
	v := int(*n)
	return &v
}

func widen32(n *int32) *int {
	if n == nil {
		return nil
	}
	v := int(*n)
	return &v
}

// pageOf приводит limit и offset из запроса к допустимым.
func pageOf(limit, offset int) (int32, int32) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	return int32(limit), int32(max(offset, 0))
}

func cardFrom(v dbgen.VacancyView, specialties []Ref) Card {
	c := Card{
		ID: v.ID, Status: v.Status, Title: v.Title, Summary: v.Summary,
		Position:     PositionRef{Code: v.PositionCode, Name: v.PositionName, Type: v.PositionType},
		Organization: OrgRef{Slug: v.OrgSlug, Name: v.OrgName, Kind: v.OrgKind, City: v.OrgCity},
		City:         v.City, WorkFormat: deref(v.WorkFormat), CareerLevel: widen(v.CareerLevel),
		RatePercent: widen(v.RatePercent), SalaryFrom: widen32(v.SalaryFrom), SalaryTo: widen32(v.SalaryTo),
		ContractType: deref(v.ContractType), ContractMonths: widen(v.ContractMonths), IsCompetition: v.IsCompetition,
		Specialties: specialties, PublishedAt: v.PublishedAt, UpdatedAt: v.UpdatedAt,
	}
	if specialties == nil {
		c.Specialties = []Ref{}
	}
	if v.UnitID != nil && v.UnitName != nil {
		c.Unit = &UnitRef{ID: *v.UnitID, Name: *v.UnitName}
	}
	if v.RegionCode != nil && v.RegionName != nil {
		c.Region = &Ref{Code: *v.RegionCode, Name: *v.RegionName}
	}
	if v.Deadline != nil {
		c.Deadline = v.Deadline.Format(dateLayout)
	}
	return c
}

// specialtiesOf читает специальности сразу для нескольких вакансий.
func specialtiesOf(ctx context.Context, q *dbgen.Queries, ids []uuid.UUID) (map[uuid.UUID][]Ref, error) {
	out := make(map[uuid.UUID][]Ref, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListVacancySpecialties(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("vacancies: load specialties: %w", err)
	}
	for _, r := range rows {
		out[r.VacancyID] = append(out[r.VacancyID], Ref{Code: r.Code, Name: r.Name})
	}
	return out, nil
}

func cardsFrom(ctx context.Context, q *dbgen.Queries, rows []dbgen.VacancyView) ([]Card, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	specs, err := specialtiesOf(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Card, len(rows))
	for i, r := range rows {
		out[i] = cardFrom(r, specs[r.ID])
	}
	return out, nil
}

// Get собирает страницу вакансии. Черновик и архив видят только те, кто ведёт вакансии этого подразделения; остальным их «нет».
func (s *Service) Get(ctx context.Context, id uuid.UUID, viewer *auth.User) (Detail, error) {
	v, err := s.q.GetVacancyView(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("vacancies: load vacancy: %w", err)
	}
	can, err := canManage(ctx, s.q, v.OrgID, v.UnitID, viewer)
	if err != nil {
		return Detail{}, err
	}
	if !can && !PubliclyVisible(v.Status) {
		return Detail{}, ErrNotFound
	}
	specs, err := specialtiesOf(ctx, s.q, []uuid.UUID{id})
	if err != nil {
		return Detail{}, err
	}
	d := Detail{
		Card: cardFrom(v, specs[id]), Description: v.Description, Requirements: v.Requirements, Focus: v.Focus,
		Housing: v.Housing, FundingSource: deref(v.FundingSource), FundingNote: v.FundingNote,
		Degree: v.DegreeRequired, AcademicTitle: v.TitleRequired, CreatedAt: v.CreatedAt,
		Viewer: Viewer{CanManage: can, Transitions: []string{}},
	}
	if can {
		d.Viewer.Transitions = NextStatuses(v.Status)
	}
	return d, nil
}

// scope — какие вакансии человек ведёт: все вакансии организаций (wholeOrgs) и вакансии отдельных подразделений (units).
type scope struct {
	wholeOrgs []uuid.UUID
	units     []uuid.UUID
}

// scopeOf собирает права человека по всем его организациям. Решает пакет access: здесь только спрашиваем его.
func scopeOf(ctx context.Context, q *dbgen.Queries, user auth.User) (scope, error) {
	sc := scope{wholeOrgs: []uuid.UUID{}, units: []uuid.UUID{}}
	mine, err := q.ListOrganizationsOfUser(ctx, user.ID)
	if err != nil {
		return scope{}, fmt.Errorf("vacancies: list my organizations: %w", err)
	}
	for _, o := range mine {
		actor, err := orgs.ActorOf(ctx, q, o.ID, user.ID)
		if err != nil {
			return scope{}, err
		}
		if actor.Can(access.ManageVacancies, access.NoUnit) {
			sc.wholeOrgs = append(sc.wholeOrgs, o.ID)
			continue
		}
		for _, id := range actor.HeadOf {
			if actor.Can(access.ManageVacancies, id) {
				sc.units = append(sc.units, id)
			}
		}
	}
	return sc, nil
}

// ListMine — «Мои вакансии»: всё, что человек может вести, по статусам. Пустой status — все статусы.
func (s *Service) ListMine(ctx context.Context, user auth.User, status string, limit, offset int) (MineList, error) {
	if status != "" && !isOneOf(status, Statuses) {
		return MineList{}, &auth.ValidationError{Fields: map[string]string{"status": "Такого статуса нет"}}
	}
	sc, err := scopeOf(ctx, s.q, user)
	if err != nil {
		return MineList{}, err
	}
	counts := map[string]int{}
	for _, st := range Statuses {
		counts[st] = 0
	}
	rows, err := s.q.CountMyVacanciesByStatus(ctx, dbgen.CountMyVacanciesByStatusParams{WholeOrgs: sc.wholeOrgs, Units: sc.units})
	if err != nil {
		return MineList{}, fmt.Errorf("vacancies: count mine: %w", err)
	}
	all := 0
	for _, r := range rows {
		counts[r.Status] = int(r.Total)
		all += int(r.Total)
	}
	total := all
	if status != "" {
		total = counts[status]
	}
	lim, off := pageOf(limit, offset)
	list, err := s.q.ListMyVacancies(ctx, dbgen.ListMyVacanciesParams{WholeOrgs: sc.wholeOrgs, Units: sc.units, Status: status, RowLimit: lim, RowOffset: off})
	if err != nil {
		return MineList{}, fmt.Errorf("vacancies: list mine: %w", err)
	}
	items, err := cardsFrom(ctx, s.q, list)
	if err != nil {
		return MineList{}, err
	}
	return MineList{Items: items, Total: total, Counts: counts}, nil
}

// Targets — где человек может создать вакансию: организации, где он ведёт вакансии всей организации, и подразделения, которыми он руководит.
func (s *Service) Targets(ctx context.Context, user auth.User) ([]Target, error) {
	mine, err := s.q.ListOrganizationsOfUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("vacancies: list my organizations: %w", err)
	}
	out := []Target{}
	for _, o := range mine {
		actor, err := orgs.ActorOf(ctx, s.q, o.ID, user.ID)
		if err != nil {
			return nil, err
		}
		t := Target{Organization: OrgRef{Slug: o.Slug, Name: o.Name, Kind: o.Kind, City: o.City}, Units: []UnitRef{}}
		t.WholeOrg = actor.Can(access.ManageVacancies, access.NoUnit)
		units, err := s.q.ListUnitNames(ctx, o.ID)
		if err != nil {
			return nil, fmt.Errorf("vacancies: list units: %w", err)
		}
		for _, u := range units {
			if actor.Can(access.ManageVacancies, u.ID) {
				t.Units = append(t.Units, UnitRef{ID: u.ID, Name: u.Name})
			}
		}
		if t.WholeOrg || len(t.Units) > 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

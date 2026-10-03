package orgs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// OrgRef — краткие сведения об организации на странице подразделения.
type OrgRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	City string `json:"city"`
}

// foreignKeyViolation — код ошибки PostgreSQL «нарушен внешний ключ».
const foreignKeyViolation = "23503"

// UnitView — страница подразделения.
type UnitView struct {
	Organization OrgRef  `json:"organization"`
	Unit         Unit    `json:"unit"`
	Viewer       *Viewer `json:"viewer"`
}

func (s *Service) loadUnit(ctx context.Context, q *dbgen.Queries, orgID, unitID uuid.UUID) (dbgen.GetUnitWithHeadRow, error) {
	row, err := q.GetUnitWithHead(ctx, dbgen.GetUnitWithHeadParams{ID: unitID, OrgID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetUnitWithHeadRow{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetUnitWithHeadRow{}, fmt.Errorf("orgs: load unit: %w", err)
	}
	return row, nil
}

func unitFromRow(r dbgen.GetUnitWithHeadRow, showHeadID bool) Unit {
	return unitFrom(dbgen.ListUnitsRow(r), showHeadID)
}

// GetUnit собирает публичную страницу подразделения.
func (s *Service) GetUnit(ctx context.Context, slug string, unitID uuid.UUID, viewer *auth.User) (UnitView, error) {
	org, err := s.loadOrg(ctx, s.q, slug)
	if err != nil {
		return UnitView{}, err
	}
	row, err := s.loadUnit(ctx, s.q, org.ID, unitID)
	if err != nil {
		return UnitView{}, err
	}
	v, err := s.viewerOf(ctx, s.q, org.ID, viewer, []dbgen.ListUnitsRow{dbgen.ListUnitsRow(row)})
	if err != nil {
		return UnitView{}, err
	}
	return UnitView{
		Organization: OrgRef{Slug: org.Slug, Name: org.Name, Kind: org.Kind, City: org.City},
		Unit:         unitFromRow(row, v != nil && v.Role != ""),
		Viewer:       v,
	}, nil
}

// CreateUnit добавляет подразделение (владелец).
func (s *Service) CreateUnit(ctx context.Context, user auth.User, slug string, in UnitInput) (Unit, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.ManageUnits, access.NoUnit)
	if err != nil {
		return Unit{}, err
	}
	f, err := validateUnit(in)
	if err != nil {
		return Unit{}, err
	}
	row, err := s.q.CreateUnit(ctx, dbgen.CreateUnitParams{OrgID: org.ID, Name: f.name, Kind: f.kind, Description: f.description, Topics: f.topics, CreatedAt: s.now()})
	if err != nil {
		return Unit{}, fmt.Errorf("orgs: create unit: %w", err)
	}
	return Unit{ID: row.ID, Name: row.Name, Kind: row.Kind, Description: row.Description, Topics: row.Topics}, nil
}

// UpdateUnit меняет название, вид, описание и темы подразделения (владелец или руководитель этого подразделения).
func (s *Service) UpdateUnit(ctx context.Context, user auth.User, slug string, unitID uuid.UUID, in UnitInput) (Unit, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.EditUnit, unitID)
	if err != nil {
		return Unit{}, err
	}
	f, err := validateUnit(in)
	if err != nil {
		return Unit{}, err
	}
	if _, err := s.q.UpdateUnit(ctx, dbgen.UpdateUnitParams{ID: unitID, OrgID: org.ID, Name: f.name, Kind: f.kind, Description: f.description, Topics: f.topics, UpdatedAt: s.now()}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Unit{}, ErrNotFound
		}
		return Unit{}, fmt.Errorf("orgs: update unit: %w", err)
	}
	row, err := s.loadUnit(ctx, s.q, org.ID, unitID)
	if err != nil {
		return Unit{}, err
	}
	return unitFromRow(row, true), nil
}

// SetUnitHead назначает руководителя подразделения (владелец). userID == nil снимает руководителя.
// Руководителем может быть только сотрудник этой организации.
func (s *Service) SetUnitHead(ctx context.Context, user auth.User, slug string, unitID uuid.UUID, userID *uuid.UUID) (Unit, error) {
	var out Unit
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		org, _, err := s.require(ctx, q, slug, user, access.ManageUnits, access.NoUnit)
		if err != nil {
			return err
		}
		if userID != nil {
			if _, err := q.GetMember(ctx, dbgen.GetMemberParams{OrgID: org.ID, UserID: *userID}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return &auth.ValidationError{Fields: map[string]string{"user_id": msgHeadNotMember}}
				}
				return fmt.Errorf("orgs: load future head: %w", err)
			}
		}
		n, err := q.SetUnitHead(ctx, dbgen.SetUnitHeadParams{ID: unitID, OrgID: org.ID, HeadUserID: userID, UpdatedAt: s.now()})
		if err != nil {
			return fmt.Errorf("orgs: set unit head: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		row, err := s.loadUnit(ctx, q, org.ID, unitID)
		if err != nil {
			return err
		}
		out = unitFromRow(row, true)
		return nil
	})
	return out, err
}

// DeleteUnit удаляет подразделение (владелец).
func (s *Service) DeleteUnit(ctx context.Context, user auth.User, slug string, unitID uuid.UUID) error {
	org, _, err := s.require(ctx, s.q, slug, user, access.ManageUnits, access.NoUnit)
	if err != nil {
		return err
	}
	n, err := s.q.DeleteUnit(ctx, dbgen.DeleteUnitParams{ID: unitID, OrgID: org.ID})
	if err != nil {
		// Внешний ключ вакансий не даёт удалить подразделение, пока в нём есть вакансии (D-051).
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == "vacancies_unit_id_fkey" {
			return ErrUnitHasVacancies
		}
		return fmt.Errorf("orgs: delete unit: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

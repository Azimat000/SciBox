package profiles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// MaxHiddenOrgs — от скольких организаций можно скрыть профиль (D-200).
const MaxHiddenOrgs = 50

// ErrOrgNotFound — организации с таким адресом нет.
var ErrOrgNotFound = errors.New("profiles: organization not found")

// ErrTooManyHidden — список «скрыть от организаций» полон.
var ErrTooManyHidden = errors.New("profiles: too many hidden organizations")

// HiddenOrgs — организации, от сотрудников которых человек скрыл свой профиль.
func (s *Service) HiddenOrgs(ctx context.Context, user auth.User) ([]HiddenOrg, error) {
	id, err := s.ensure(ctx, s.q, user.ID)
	if err != nil {
		return nil, err
	}
	return hiddenList(ctx, s.q, id)
}

func hiddenList(ctx context.Context, q *dbgen.Queries, profileID uuid.UUID) ([]HiddenOrg, error) {
	rows, err := q.ListHiddenOrgs(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("profiles: list hidden organizations: %w", err)
	}
	out := make([]HiddenOrg, 0, len(rows))
	for _, r := range rows {
		out = append(out, HiddenOrg{Slug: r.Slug, Name: r.Name, City: r.City, Hidden: r.CreatedAt})
	}
	return out, nil
}

// HideFrom скрывает профиль от сотрудников организации (по её адресу). Повторное скрытие ничего не меняет.
// Возвращает новый список.
func (s *Service) HideFrom(ctx context.Context, user auth.User, slug string) ([]HiddenOrg, error) {
	var out []HiddenOrg
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		profileID, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		orgID, err := q.OrganizationIDBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrgNotFound
		}
		if err != nil {
			return fmt.Errorf("profiles: find organization: %w", err)
		}
		count, err := q.CountHiddenOrgs(ctx, profileID)
		if err != nil {
			return fmt.Errorf("profiles: count hidden organizations: %w", err)
		}
		n, err := q.HideFromOrg(ctx, dbgen.HideFromOrgParams{ProfileID: profileID, OrgID: orgID, Now: s.now()})
		if err != nil {
			return fmt.Errorf("profiles: hide from organization: %w", err)
		}
		if n == 1 && count >= MaxHiddenOrgs {
			return ErrTooManyHidden
		}
		out, err = hiddenList(ctx, q, profileID)
		return err
	})
	return out, err
}

// UnhideFrom снимает скрытие. Организации, от которой профиль не скрыт, это не ошибка.
func (s *Service) UnhideFrom(ctx context.Context, user auth.User, slug string) ([]HiddenOrg, error) {
	var out []HiddenOrg
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		profileID, err := s.ensure(ctx, q, user.ID)
		if err != nil {
			return err
		}
		orgID, err := q.OrganizationIDBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrgNotFound
		}
		if err != nil {
			return fmt.Errorf("profiles: find organization: %w", err)
		}
		if _, err := q.UnhideFromOrg(ctx, dbgen.UnhideFromOrgParams{ProfileID: profileID, OrgID: orgID}); err != nil {
			return fmt.Errorf("profiles: unhide from organization: %w", err)
		}
		out, err = hiddenList(ctx, q, profileID)
		return err
	})
	return out, err
}

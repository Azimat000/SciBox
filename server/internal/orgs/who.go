package orgs

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/dbgen"
)

// UsersWhoCan возвращает сотрудников организации, которым разрешено действие perm в подразделении unit
// (access.NoUnit — на всю организацию). Нужно, чтобы сообщить о событии именно тем, кто вправе о нём знать
// (например, о новом отклике на вакансию). Права решает пакет access, здесь ролей не проверяется.
func UsersWhoCan(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID, perm access.Permission, unit uuid.UUID) ([]uuid.UUID, error) {
	ids, err := q.ListOrgMemberIDs(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("orgs: list members: %w", err)
	}
	var out []uuid.UUID
	for _, id := range ids {
		actor, err := ActorOf(ctx, q, orgID, id)
		if err != nil {
			return nil, err
		}
		if actor.Can(perm, unit) {
			out = append(out, id)
		}
	}
	return out, nil
}

// Scope — где человек вправе делать действие: организации целиком и отдельные подразделения (которыми он руководит).
// Из него строятся списки «всё, что я веду» (вакансии, отклики, приглашения).
type Scope struct {
	WholeOrgs []uuid.UUID
	Units     []uuid.UUID
}

// ScopeOf собирает права человека по всем его организациям. Решает пакет access: здесь только спрашиваем его.
// Списки всегда не nil, их можно отдавать в запрос как есть.
func ScopeOf(ctx context.Context, q *dbgen.Queries, userID uuid.UUID, perm access.Permission) (Scope, error) {
	sc := Scope{WholeOrgs: []uuid.UUID{}, Units: []uuid.UUID{}}
	mine, err := q.ListOrganizationsOfUser(ctx, userID)
	if err != nil {
		return Scope{}, fmt.Errorf("orgs: list organizations of user: %w", err)
	}
	for _, o := range mine {
		actor, err := ActorOf(ctx, q, o.ID, userID)
		if err != nil {
			return Scope{}, err
		}
		if actor.Can(perm, access.NoUnit) {
			sc.WholeOrgs = append(sc.WholeOrgs, o.ID)
			continue
		}
		for _, id := range actor.HeadOf {
			if actor.Can(perm, id) {
				sc.Units = append(sc.Units, id)
			}
		}
	}
	return sc, nil
}

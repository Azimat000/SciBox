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

package orgs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// Member — сотрудник организации (видит владелец).
type Member struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// PendingInvitation — приглашение, которое ещё не принято и не отозвано.
type PendingInvitation struct {
	ID        uuid.UUID  `json:"id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	UnitID    *uuid.UUID `json:"unit_id"`
	UnitName  *string    `json:"unit_name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
}

// MembersView — сотрудники и ожидающие приглашения организации.
type MembersView struct {
	Members     []Member            `json:"members"`
	Invitations []PendingInvitation `json:"invitations"`
}

// Members показывает сотрудников и приглашения (владелец: почты — личные данные).
func (s *Service) Members(ctx context.Context, user auth.User, slug string) (MembersView, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.ManageMembers, access.NoUnit)
	if err != nil {
		return MembersView{}, err
	}
	members, err := s.q.ListMembers(ctx, org.ID)
	if err != nil {
		return MembersView{}, fmt.Errorf("orgs: list members: %w", err)
	}
	invs, err := s.q.ListPendingInvitations(ctx, dbgen.ListPendingInvitationsParams{OrgID: org.ID, Now: s.now()})
	if err != nil {
		return MembersView{}, fmt.Errorf("orgs: list invitations: %w", err)
	}
	out := MembersView{Members: make([]Member, 0, len(members)), Invitations: make([]PendingInvitation, 0, len(invs))}
	for _, m := range members {
		out.Members = append(out.Members, Member{UserID: m.UserID, Name: m.DisplayName, Email: m.Email, Role: m.Role, JoinedAt: m.JoinedAt})
	}
	for _, i := range invs {
		out.Invitations = append(out.Invitations, PendingInvitation{ID: i.ID, Email: i.Email, Role: i.Role, UnitID: i.UnitID, UnitName: i.UnitName, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt})
	}
	return out, nil
}

// Audit показывает журнал действий организации (владелец): в нём действия и имена всех сотрудников.
// before — номер записи, раньше которой читать (0 — с самых новых).
func (s *Service) Audit(ctx context.Context, user auth.User, slug string, before int64, limit int) (audit.Page, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.ViewAudit, access.NoUnit)
	if err != nil {
		return audit.Page{}, err
	}
	return audit.List(ctx, s.q, org.ID, before, limit)
}

// lockedOwners блокирует владельцев организации и возвращает их число: два одновременных запроса не смогут оба
// убрать «последнего» владельца.
func lockedOwners(ctx context.Context, q *dbgen.Queries, orgID uuid.UUID) (int, error) {
	owners, err := q.LockOwners(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("orgs: lock owners: %w", err)
	}
	return len(owners), nil
}

func targetMember(ctx context.Context, q *dbgen.Queries, orgID, userID uuid.UUID) (dbgen.OrgMember, error) {
	m, err := q.GetMember(ctx, dbgen.GetMemberParams{OrgID: orgID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.OrgMember{}, ErrNotFound
	}
	if err != nil {
		return dbgen.OrgMember{}, fmt.Errorf("orgs: load member: %w", err)
	}
	return m, nil
}

// ChangeRole меняет роль сотрудника (владелец). Последнего владельца понизить нельзя.
// Сделать владельцем можно только вскоре после ввода пароля (p.Fresh, T-12): владелец получает всё.
func (s *Service) ChangeRole(ctx context.Context, p auth.Principal, slug string, target uuid.UUID, rawRole string) error {
	user := p.User
	role, ok := access.ParseRole(rawRole)
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		org, _, err := s.require(ctx, q, slug, user, access.ManageMembers, access.NoUnit)
		if err != nil {
			return err
		}
		if !ok {
			return &auth.ValidationError{Fields: map[string]string{"role": msgRoleInvalid}}
		}
		if role == access.RoleOwner && !p.Fresh {
			return auth.ErrReauthRequired
		}
		owners, err := lockedOwners(ctx, q, org.ID)
		if err != nil {
			return err
		}
		m, err := targetMember(ctx, q, org.ID, target)
		if err != nil {
			return err
		}
		if access.Role(m.Role) == access.RoleOwner && role != access.RoleOwner && owners <= 1 {
			return ErrLastOwner
		}
		if _, err := q.SetMemberRole(ctx, dbgen.SetMemberRoleParams{OrgID: org.ID, UserID: target, Role: string(role)}); err != nil {
			return fmt.Errorf("orgs: set role: %w", err)
		}
		if access.Role(m.Role) == access.RoleOwner && role != access.RoleOwner {
			if err := q.RevokeInvitationsBy(ctx, dbgen.RevokeInvitationsByParams{At: s.now(), OrgID: org.ID, InvitedBy: &target}); err != nil {
				return fmt.Errorf("orgs: revoke invitations of a demoted owner: %w", err)
			}
		}
		if m.Role == string(role) {
			return nil // роль та же: менять было нечего, в журнале этого нет
		}
		return audit.Record(ctx, q, audit.Event{
			Action: audit.MemberRoleChanged, OrgID: org.ID, Actor: user.ID, Subject: target,
			Details: map[string]string{"from": m.Role, "to": string(role)},
		})
	})
}

// RemoveMember убирает сотрудника из организации. Владелец убирает любого; любой сотрудник может уйти сам.
// Последнему владельцу уйти нельзя. Уходя, человек перестаёт руководить подразделениями.
func (s *Service) RemoveMember(ctx context.Context, user auth.User, slug string, target uuid.UUID) error {
	return s.inTx(ctx, func(q *dbgen.Queries) error {
		org, err := s.loadOrg(ctx, q, slug)
		if err != nil {
			return err
		}
		actor, err := s.actorOf(ctx, q, org.ID, user.ID)
		if err != nil {
			return err
		}
		leaving := target == user.ID
		if !actor.IsMember() || (!leaving && !actor.Can(access.ManageMembers, access.NoUnit)) {
			return ErrForbidden
		}
		owners, err := lockedOwners(ctx, q, org.ID)
		if err != nil {
			return err
		}
		m, err := targetMember(ctx, q, org.ID, target)
		if err != nil {
			return err
		}
		if access.Role(m.Role) == access.RoleOwner && owners <= 1 {
			return ErrLastOwner
		}
		if _, err := q.RemoveMember(ctx, dbgen.RemoveMemberParams{OrgID: org.ID, UserID: target}); err != nil {
			return fmt.Errorf("orgs: remove member: %w", err)
		}
		if err := q.ClearHeadOfUser(ctx, dbgen.ClearHeadOfUserParams{OrgID: org.ID, HeadUserID: &target, UpdatedAt: s.now()}); err != nil {
			return fmt.Errorf("orgs: clear unit head: %w", err)
		}
		if err := q.RevokeInvitationsBy(ctx, dbgen.RevokeInvitationsByParams{At: s.now(), OrgID: org.ID, InvitedBy: &target}); err != nil {
			return fmt.Errorf("orgs: revoke invitations of a removed member: %w", err)
		}
		action := audit.MemberRemoved
		if leaving {
			action = audit.MemberLeft
		}
		return audit.Record(ctx, q, audit.Event{
			Action: action, OrgID: org.ID, Actor: user.ID, Subject: target, Details: map[string]string{"role": m.Role},
		})
	})
}

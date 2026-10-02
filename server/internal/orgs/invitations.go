package orgs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// newToken возвращает случайный токен для ссылки и его хеш для базы (как у сессий в пакете auth).
func newToken() (raw string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// InviteInput — поля формы приглашения. UnitID указывают только для руководителя подразделения.
type InviteInput struct {
	Email  string
	Role   string
	UnitID *uuid.UUID
}

// Invite приглашает человека по почте (владелец). Новое приглашение на ту же почту отменяет прежние.
func (s *Service) Invite(ctx context.Context, user auth.User, slug string, in InviteInput) (PendingInvitation, error) {
	org, _, err := s.require(ctx, s.q, slug, user, access.ManageMembers, access.NoUnit)
	if err != nil {
		return PendingInvitation{}, err
	}
	fields := map[string]string{}
	email, msg := auth.NormalizeEmail(in.Email)
	if msg != "" {
		fields["email"] = msg
	}
	role, ok := access.ParseRole(in.Role)
	if !ok {
		fields["role"] = msgRoleInvalid
	}
	var unitName *string
	switch {
	case in.UnitID != nil && ok && role != access.RoleUnitHead:
		fields["unit_id"] = msgUnitForHeadsOn
	case in.UnitID != nil && ok:
		unit, err := s.q.GetUnitInOrg(ctx, dbgen.GetUnitInOrgParams{ID: *in.UnitID, OrgID: org.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			fields["unit_id"] = msgUnitUnknown
		} else if err != nil {
			return PendingInvitation{}, fmt.Errorf("orgs: load invitation unit: %w", err)
		} else {
			unitName = &unit.Name
		}
	}
	if msg == "" {
		_, err := s.q.GetMemberIDByEmail(ctx, dbgen.GetMemberIDByEmailParams{OrgID: org.ID, Email: email})
		if err == nil {
			fields["email"] = msgAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return PendingInvitation{}, fmt.Errorf("orgs: check member by email: %w", err)
		}
	}
	if len(fields) > 0 {
		return PendingInvitation{}, &auth.ValidationError{Fields: fields}
	}

	raw, hash := newToken()
	now := s.now()
	inv := PendingInvitation{Email: email, Role: string(role), UnitID: in.UnitID, UnitName: unitName, CreatedAt: now, ExpiresAt: now.Add(s.cfg.InviteTTL)}
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := s.spend(ctx, q, kindOrgInvite, user.ID, s.cfg.Invite); err != nil {
			return err
		}
		if err := q.RevokePendingInvitations(ctx, dbgen.RevokePendingInvitationsParams{At: now, OrgID: org.ID, Email: email}); err != nil {
			return fmt.Errorf("orgs: revoke earlier invitations: %w", err)
		}
		id, err := q.CreateInvitation(ctx, dbgen.CreateInvitationParams{
			OrgID: org.ID, Email: email, Role: string(role), UnitID: in.UnitID, TokenHash: hash, InvitedBy: &user.ID,
			CreatedAt: now, ExpiresAt: inv.ExpiresAt,
		})
		if err != nil {
			return fmt.Errorf("orgs: create invitation: %w", err)
		}
		inv.ID = id
		return nil
	})
	if err != nil {
		return PendingInvitation{}, err
	}
	s.sendAsync(s.inviteMail(user.Name, org.Name, role, unitName, email, raw))
	return inv, nil
}

// RevokeInvitation отзывает приглашение (владелец).
func (s *Service) RevokeInvitation(ctx context.Context, user auth.User, slug string, id uuid.UUID) error {
	org, _, err := s.require(ctx, s.q, slug, user, access.ManageMembers, access.NoUnit)
	if err != nil {
		return err
	}
	n, err := s.q.RevokeInvitation(ctx, dbgen.RevokeInvitationParams{At: s.now(), ID: id, OrgID: org.ID})
	if err != nil {
		return fmt.Errorf("orgs: revoke invitation: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// InvitationPreview — что человек видит на странице приглашения до того, как принять.
type InvitationPreview struct {
	Organization OrgRefShort `json:"organization"`
	Role         string      `json:"role"`
	UnitName     *string     `json:"unit_name"`
	Email        string      `json:"email"`
	// EmailMatches: приглашение отправлено на почту вошедшего человека. nil, если никто не вошёл.
	EmailMatches *bool `json:"email_matches"`
}

// OrgRefShort — название и адрес организации.
type OrgRefShort struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// LookupInvitation показывает приглашение по ссылке, не принимая его.
func (s *Service) LookupInvitation(ctx context.Context, rawToken string, viewer *auth.User) (InvitationPreview, error) {
	row, err := s.q.PeekInvitation(ctx, dbgen.PeekInvitationParams{TokenHash: hashToken(rawToken), Now: s.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationPreview{}, ErrInvalidInvitation
	}
	if err != nil {
		return InvitationPreview{}, fmt.Errorf("orgs: peek invitation: %w", err)
	}
	p := InvitationPreview{Organization: OrgRefShort{Slug: row.OrgSlug, Name: row.OrgName}, Role: row.Role, UnitName: row.UnitName, Email: row.Email}
	if viewer != nil {
		m := strings.EqualFold(viewer.Email, row.Email)
		p.EmailMatches = &m
	}
	return p, nil
}

// Joined — куда человек вступил.
type Joined struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// consumed — приглашение, только что «погашенное»: единая форма для приёма по ссылке и по номеру.
type consumed struct {
	orgID  uuid.UUID
	role   string
	unitID *uuid.UUID
	email  string
}

// AcceptInvitation принимает приглашение по ссылке из письма.
func (s *Service) AcceptInvitation(ctx context.Context, user auth.User, rawToken string) (Joined, error) {
	return s.accept(ctx, user, func(q *dbgen.Queries) (consumed, error) {
		r, err := q.ConsumeInvitation(ctx, dbgen.ConsumeInvitationParams{Now: s.now(), TokenHash: hashToken(rawToken)})
		return consumed{r.OrgID, r.Role, r.UnitID, r.Email}, err
	})
}

// AcceptInvitationByID принимает приглашение из списка «Мои приглашения» (там ссылки нет, но почта совпадает с аккаунтом).
func (s *Service) AcceptInvitationByID(ctx context.Context, user auth.User, id uuid.UUID) (Joined, error) {
	return s.accept(ctx, user, func(q *dbgen.Queries) (consumed, error) {
		r, err := q.ConsumeInvitationByID(ctx, dbgen.ConsumeInvitationByIDParams{Now: s.now(), ID: id})
		return consumed{r.OrgID, r.Role, r.UnitID, r.Email}, err
	})
}

// accept гасит приглашение и делает человека сотрудником в одной транзакции. Чужая почта или «уже сотрудник»
// откатывают всё, приглашение остаётся живым.
func (s *Service) accept(ctx context.Context, user auth.User, consume func(q *dbgen.Queries) (consumed, error)) (Joined, error) {
	var joined Joined
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		c, err := consume(q)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvitation
		}
		if err != nil {
			return fmt.Errorf("orgs: consume invitation: %w", err)
		}
		if !strings.EqualFold(c.email, user.Email) {
			return ErrWrongEmail
		}
		if _, err := q.GetMember(ctx, dbgen.GetMemberParams{OrgID: c.orgID, UserID: user.ID}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("orgs: check membership: %w", err)
		}
		now := s.now()
		if err := q.AddMember(ctx, dbgen.AddMemberParams{OrgID: c.orgID, UserID: user.ID, Role: c.role, JoinedAt: now}); err != nil {
			return fmt.Errorf("orgs: add member: %w", err)
		}
		if c.unitID != nil && access.Role(c.role) == access.RoleUnitHead {
			// Если у подразделения уже есть руководитель, человек просто становится сотрудником; владелец решит сам.
			if _, err := q.SetUnitHeadIfVacant(ctx, dbgen.SetUnitHeadIfVacantParams{ID: *c.unitID, OrgID: c.orgID, HeadUserID: &user.ID, UpdatedAt: now}); err != nil {
				return fmt.Errorf("orgs: assign unit head: %w", err)
			}
		}
		org, err := q.GetOrganizationByID(ctx, c.orgID)
		if err != nil {
			return fmt.Errorf("orgs: load organization: %w", err)
		}
		joined = Joined{Slug: org.Slug, Name: org.Name, Role: c.role}
		return nil
	})
	return joined, err
}

// MyInvitation — приглашение, ожидающее человека (по его почте).
type MyInvitation struct {
	ID           uuid.UUID   `json:"id"`
	Organization OrgRefShort `json:"organization"`
	Role         string      `json:"role"`
	UnitName     *string     `json:"unit_name"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

// MyInvitations — живые приглашения на почту человека.
func (s *Service) MyInvitations(ctx context.Context, user auth.User) ([]MyInvitation, error) {
	rows, err := s.q.ListInvitationsForEmail(ctx, dbgen.ListInvitationsForEmailParams{Email: user.Email, Now: s.now()})
	if err != nil {
		return nil, fmt.Errorf("orgs: list my invitations: %w", err)
	}
	out := make([]MyInvitation, 0, len(rows))
	for _, r := range rows {
		out = append(out, MyInvitation{ID: r.ID, Organization: OrgRefShort{Slug: r.OrgSlug, Name: r.OrgName}, Role: r.Role, UnitName: r.UnitName, ExpiresAt: r.ExpiresAt})
	}
	return out, nil
}

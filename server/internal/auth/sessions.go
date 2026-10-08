package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// SessionInfo — сессия в списке «Где вы вошли». Адрес (IP) не показываем: место по нему не определить,
// а сам адрес людям ничего не говорит.
type SessionInfo struct {
	ID         uuid.UUID
	Device     Device
	CreatedAt  time.Time
	LastSeenAt time.Time // с точностью до SessionRefresh: время обновляется не чаще раза в час
	Current    bool
}

// ListSessions возвращает живые сессии человека, последние сверху (не больше 50).
func (s *Service) ListSessions(ctx context.Context, p Principal) ([]SessionInfo, error) {
	rows, err := s.q.ListUserSessions(ctx, dbgen.ListUserSessionsParams{UserID: p.User.ID, Now: s.now()})
	if err != nil {
		return nil, fmt.Errorf("auth: list sessions: %w", err)
	}
	out := make([]SessionInfo, len(rows))
	for i, r := range rows {
		out[i] = SessionInfo{
			ID: r.ID, Device: ParseUserAgent(r.UserAgent), CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
			Current: r.ID == p.SessionID,
		}
	}
	return out, nil
}

// RevokeSession завершает одну сессию человека. Чужая или уже завершённая сессия — ErrNotFound:
// по ответу нельзя узнать, есть ли такая сессия у другого человека.
func (s *Service) RevokeSession(ctx context.Context, p Principal, id uuid.UUID) error {
	n, err := s.q.DeleteUserSession(ctx, dbgen.DeleteUserSessionParams{UserID: p.User.ID, ID: id})
	if err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Reauth проверяет пароль ещё раз и на ReauthTTL разрешает в этой сессии опасные действия (D-144).
// Неверные попытки считаются вместе со сменой пароля (PasswordUser): перебирать пароль через эту форму
// не быстрее, чем через смену пароля.
func (s *Service) Reauth(ctx context.Context, p Principal, password string) error {
	if err := s.checkPassword(ctx, p.User.ID, password, "password"); err != nil {
		return err
	}
	if err := s.q.MarkSessionReauth(ctx, dbgen.MarkSessionReauthParams{ID: p.SessionID, ReauthAt: new(s.now())}); err != nil {
		return fmt.Errorf("auth: mark reauth: %w", err)
	}
	return nil
}

// checkPassword сверяет пароль человека; неверный пароль — ошибка поля field и попытка в счёт лимита.
func (s *Service) checkPassword(ctx context.Context, userID uuid.UUID, password, field string) error {
	key := userID.String()
	if err := s.allow(ctx, kindPasswordUser, key, s.cfg.Limits.PasswordUser); err != nil {
		return err
	}
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth: load user: %w", err)
	}
	ok := false
	if passwordLen(password) > 0 && len(password) <= 4*MaxPasswordLen {
		if ok, err = s.hasher.Verify(ctx, password, user.PasswordHash); err != nil {
			return fmt.Errorf("auth: verify password: %w", err)
		}
	}
	if !ok {
		if err := s.record(ctx, kindPasswordUser, key); err != nil {
			return err
		}
		return newValidation(field, msgPasswordWrong)
	}
	return nil
}

// requireFresh — опасное действие разрешено только вскоре после ввода пароля.
func requireFresh(p Principal) error {
	if !p.Fresh {
		return ErrReauthRequired
	}
	return nil
}

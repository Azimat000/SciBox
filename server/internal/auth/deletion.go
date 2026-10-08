package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/mail"
)

// Удаление аккаунта (T-11, D-142, D-143).
//
// Просьба (свежий пароль) → срок отмены DeletionGrace: профиль сразу скрыт, все сессии завершены, уходит письмо
// с датой. В срок можно войти и отменить. По сроку фоновая задача (Cleanup) стирает данные (internal/erasure).
// Если человек единственный владелец организации с другими сотрудниками, просить нельзя (*erasure.BlockedError);
// если такая организация появилась за срок, удаление отменяется и уходит письмо, что сделать.

// DeletionPlan — что будет при удалении: организации, мешающие удалению, и организации, которые останутся без владельца.
func (s *Service) DeletionPlan(ctx context.Context, p Principal) (erasure.Plan, error) {
	return erasure.Check(ctx, s.q, p.User.ID)
}

// RequestDeletion назначает удаление аккаунта и возвращает, когда сотрутся данные.
func (s *Service) RequestDeletion(ctx context.Context, p Principal) (time.Time, error) {
	if err := requireFresh(p); err != nil {
		return time.Time{}, err
	}
	var (
		user dbgen.User
		due  time.Time
		now  = s.now()
	)
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		var err error
		if user, err = q.LockUserRow(ctx, p.User.ID); err != nil {
			return fmt.Errorf("auth: lock user: %w", err)
		}
		plan, err := erasure.Check(ctx, q, user.ID)
		if err != nil {
			return err
		}
		if plan.Blocked() {
			return &erasure.BlockedError{Orgs: plan.Blocking}
		}
		at, err := q.ScheduleUserDeletion(ctx, dbgen.ScheduleUserDeletionParams{ID: user.ID, Due: now.Add(s.cfg.DeletionGrace), Now: now})
		if err != nil {
			return fmt.Errorf("auth: schedule deletion: %w", err)
		}
		due = *at // ScheduleUserDeletion ставит срок, поэтому он не пустой
		if err := q.HideProfileForDeletion(ctx, dbgen.HideProfileForDeletionParams{UserID: user.ID, Now: now}); err != nil {
			return fmt.Errorf("auth: hide profile: %w", err)
		}
		if err := q.DeleteUserSessions(ctx, user.ID); err != nil {
			return fmt.Errorf("auth: revoke sessions: %w", err)
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountDeletionRequested, Actor: user.ID})
	})
	if err != nil {
		return time.Time{}, err
	}
	s.sendAsync(s.deletionScheduledMail(user, due))
	return due, nil
}

// CancelDeletion отменяет назначенное удаление. Если удаления не было, ничего не происходит.
func (s *Service) CancelDeletion(ctx context.Context, p Principal) error {
	var cancelled bool
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.CancelUserDeletion(ctx, dbgen.CancelUserDeletionParams{ID: p.User.ID, Now: s.now()})
		if err != nil {
			return fmt.Errorf("auth: cancel deletion: %w", err)
		}
		if cancelled = n > 0; !cancelled {
			return nil
		}
		if err := s.restoreProfile(ctx, q, p.User.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Event{Action: audit.AccountDeletionCanceled, Actor: p.User.ID})
	})
	if err != nil {
		return err
	}
	if cancelled {
		s.sendAsync(s.deletionCancelledMail(p.User))
	}
	return nil
}

// cancelDeletion отменяет удаление внутри чужой транзакции (откат почты).
func (s *Service) cancelDeletion(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) error {
	n, err := q.CancelUserDeletion(ctx, dbgen.CancelUserDeletionParams{ID: userID, Now: s.now()})
	if err != nil {
		return fmt.Errorf("auth: cancel deletion: %w", err)
	}
	if n == 0 {
		return nil
	}
	return s.restoreProfile(ctx, q, userID)
}

func (s *Service) restoreProfile(ctx context.Context, q *dbgen.Queries, userID uuid.UUID) error {
	if err := q.RestoreProfileAfterDeletion(ctx, dbgen.RestoreProfileAfterDeletionParams{UserID: userID, Now: s.now()}); err != nil {
		return fmt.Errorf("auth: restore profile: %w", err)
	}
	return nil
}

// ProcessDueDeletions стирает аккаунты, чей срок отмены прошёл, по одному в транзакции (не больше 100 за раз).
// Возвращает, сколько стёрто. Безопасно при нескольких копиях сервера: строки берутся с SKIP LOCKED.
func (s *Service) ProcessDueDeletions(ctx context.Context) (int, error) {
	erased := 0
	for range maxDeletionsPerRun {
		var (
			m      *mail.Message
			done   bool
			wasHit bool
			now    = s.now()
		)
		err := s.inTx(ctx, func(q *dbgen.Queries) error {
			ids, err := q.ClaimDueDeletions(ctx, dbgen.ClaimDueDeletionsParams{Now: now, MaxRows: 1})
			if err != nil {
				return fmt.Errorf("auth: claim deletions: %w", err)
			}
			if len(ids) == 0 {
				done = true
				return nil
			}
			user, err := q.GetUserByID(ctx, ids[0])
			if err != nil {
				return fmt.Errorf("auth: load user: %w", err)
			}
			err = erasure.Erase(ctx, q, user, now)
			if blocked, ok := errors.AsType[*erasure.BlockedError](err); ok {
				// За срок в организации, где он единственный владелец, появились сотрудники: стирать нельзя.
				if err := s.cancelDeletion(ctx, q, user.ID); err != nil {
					return err
				}
				built := s.deletionBlockedMail(user, blocked.Orgs)
				m = &built
				return audit.Record(ctx, q, audit.Event{Action: audit.AccountDeletionCanceled, Actor: user.ID, Details: map[string]string{"reason": "sole_owner"}})
			}
			if err != nil {
				return err
			}
			wasHit = true
			if err := audit.Record(ctx, q, audit.Event{Action: audit.AccountErased, Actor: user.ID}); err != nil {
				return err
			}
			built := s.accountDeletedMail(user)
			m = &built
			return nil
		})
		if err != nil {
			return erased, err
		}
		if done {
			break
		}
		if wasHit {
			erased++
		}
		if m != nil {
			s.sendAsync(*m)
		}
	}
	return erased, nil
}

// maxDeletionsPerRun — сколько аккаунтов стирается за один проход уборки (раз в час).
const maxDeletionsPerRun = 100

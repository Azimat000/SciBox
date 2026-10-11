package auth

import (
	"context"
	"fmt"
	"time"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/mail"
)

// Неактивные аккаунты (срез 29, D-192, T-38). Человек, в аккаунт которого не входили InactiveAfter (3 года), получает
// письмо за InactiveWarn (30 дней) до удаления; если за это время он не вошёл, аккаунт стирается так же, как по просьбе
// (internal/erasure). Любой вход или пользование аккаунтом начинает счёт заново (TouchUserActive). Единственный владелец
// организации с сотрудниками не стирается (как и при просьбе человека); его проверят снова через срок предупреждения.

const (
	// DefaultInactiveAfter — сколько аккаунт без входа считается заброшенным.
	DefaultInactiveAfter = 3 * 365 * 24 * time.Hour
	// DefaultInactiveWarn — за сколько до удаления уходит письмо.
	DefaultInactiveWarn = 30 * 24 * time.Hour
	// maxInactivePerRun — сколько аккаунтов уборка разбирает за проход каждого вида (раз в час).
	maxInactivePerRun = 100
)

// ProcessInactive предупреждает и ставит на стирание заброшенные аккаунты. Возвращает, сколько предупреждено и сколько
// назначено на стирание (стирает ProcessDueDeletions в той же уборке).
func (s *Service) ProcessInactive(ctx context.Context) (warned, scheduled int, err error) {
	now := s.now()
	after := positiveOr(s.cfg.InactiveAfter, DefaultInactiveAfter)
	warn := positiveOr(s.cfg.InactiveWarn, DefaultInactiveWarn)
	if warn >= after {
		warn = after / 2 // предупреждение не может быть длиннее самого срока
	}

	// Сначала стирание (по тем, кого предупредили давно), чтобы только что предупреждённый не попал сразу в оба шага.
	for range maxInactivePerRun {
		var (
			done bool
			hit  bool
		)
		err = s.inTx(ctx, func(q *dbgen.Queries) error {
			ids, err := q.ClaimInactiveToErase(ctx, dbgen.ClaimInactiveToEraseParams{
				WarnedBefore: now.Add(-warn), InactiveBefore: now.Add(-after), MaxRows: 1,
			})
			if err != nil {
				return fmt.Errorf("auth: claim inactive to erase: %w", err)
			}
			if len(ids) == 0 {
				done = true
				return nil
			}
			plan, err := erasure.Check(ctx, q, ids[0])
			if err != nil {
				return err
			}
			if plan.Blocked() {
				// Единственный владелец организации с сотрудниками: не стираем, проверим снова через срок предупреждения.
				return q.MarkInactiveWarned(ctx, dbgen.MarkInactiveWarnedParams{ID: ids[0], Now: now})
			}
			if _, err := q.ScheduleUserDeletion(ctx, dbgen.ScheduleUserDeletionParams{ID: ids[0], Due: now, Now: now}); err != nil {
				return fmt.Errorf("auth: schedule inactive deletion: %w", err)
			}
			hit = true
			return audit.Record(ctx, q, audit.Event{Action: audit.AccountDeletionRequested, Actor: ids[0], Details: map[string]string{"via": "inactive"}})
		})
		if err != nil {
			return warned, scheduled, err
		}
		if done {
			break
		}
		if hit {
			scheduled++
		}
	}

	for range maxInactivePerRun {
		var (
			m    *mail.Message
			done bool
		)
		err = s.inTx(ctx, func(q *dbgen.Queries) error {
			ids, err := q.ClaimInactiveToWarn(ctx, dbgen.ClaimInactiveToWarnParams{Before: now.Add(-(after - warn)), MaxRows: 1})
			if err != nil {
				return fmt.Errorf("auth: claim inactive to warn: %w", err)
			}
			if len(ids) == 0 {
				done = true
				return nil
			}
			user, err := q.GetUserByID(ctx, ids[0])
			if err != nil {
				return fmt.Errorf("auth: load user: %w", err)
			}
			if err := q.MarkInactiveWarned(ctx, dbgen.MarkInactiveWarnedParams{ID: user.ID, Now: now}); err != nil {
				return fmt.Errorf("auth: mark warned: %w", err)
			}
			built := s.inactiveWarningMail(user, now.Add(warn))
			m = &built
			return nil
		})
		if err != nil {
			return warned, scheduled, err
		}
		if done {
			break
		}
		if m != nil {
			s.sendAsync(*m)
			warned++
		}
	}
	return warned, scheduled, nil
}

func (s *Service) inactiveWarningMail(u dbgen.User, eraseAt time.Time) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Аккаунт на " + s.cfg.ProductName + " будет удалён " + moscowDate(eraseAt),
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"В ваш аккаунт на "+s.cfg.ProductName+" давно не входили. Мы не храним данные дольше, чем они нужны, поэтому "+moscowDate(eraseAt)+" аккаунт будет удалён вместе с профилем, избранным и поисками.",
			"",
			"Чтобы сохранить его, просто войдите: "+s.link("/login", ""),
			"Если аккаунт больше не нужен, ничего делать не надо.",
		),
	}
}

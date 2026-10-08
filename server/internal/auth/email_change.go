package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/mail"
)

// Смена почты (T-11, D-145).
//
//  1. Человек со свежим паролем просит новый адрес: на него уходит ссылка (24 часа). Если адрес занят другим
//     аккаунтом, туда уходит письмо «эта почта уже есть», а ответ тот же: чужие почты так не перебрать.
//  2. Ссылка срабатывает один раз и меняет почту. На старый адрес уходит письмо со ссылкой «это не я» (7 дней).
//  3. «Это не я» возвращает старую почту, завершает все сессии, отменяет удаление аккаунта, если его успели
//     заказать, и присылает на старый адрес ссылку для нового пароля.

// RequestEmailChange отправляет ссылку подтверждения на новый адрес.
func (s *Service) RequestEmailChange(ctx context.Context, p Principal, rawEmail string, meta Meta) error {
	if err := requireFresh(p); err != nil {
		return err
	}
	email, msg := NormalizeEmail(rawEmail)
	if msg != "" {
		return newValidation("email", msg)
	}
	if email == p.User.Email {
		return newValidation("email", msgEmailSame)
	}
	if err := s.allowMail(ctx, meta.IP); err != nil {
		return err
	}
	var m *mail.Message
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		user, err := q.GetUserByID(ctx, p.User.ID)
		if err != nil {
			return fmt.Errorf("auth: load user: %w", err)
		}
		taken, err := q.EmailTakenByOther(ctx, dbgen.EmailTakenByOtherParams{Email: email, ID: user.ID})
		if err != nil {
			return fmt.Errorf("auth: email taken: %w", err)
		}
		// Ссылка выдаётся и для занятого адреса: так у обоих случаев одна пауза между письмами,
		// а сработать такая ссылка не сможет (адрес проверяется ещё раз при подтверждении).
		raw, ok, err := s.issueEmailToken(ctx, q, user.ID, purposeChangeEmail, s.cfg.ChangeEmailTTL, email)
		if err != nil || !ok {
			return err
		}
		built := s.changeEmailMail(user, email, raw)
		if taken {
			built = s.emailTakenMail(email)
		}
		m = &built
		return nil
	})
	if err != nil {
		return err
	}
	if m != nil {
		s.sendAsync(*m)
	}
	return nil
}

// ConfirmEmailChange меняет почту по ссылке из письма. Входа не делает: ссылку могли открыть в другом браузере.
func (s *Service) ConfirmEmailChange(ctx context.Context, rawToken string) (User, error) {
	var (
		user    dbgen.User
		oldUser dbgen.User
		revert  string
		now     = s.now()
	)
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		tok, err := q.ConsumeEmailToken(ctx, dbgen.ConsumeEmailTokenParams{Now: now, TokenHash: hashToken(rawToken), Purpose: purposeChangeEmail})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("auth: consume token: %w", err)
		}
		if oldUser, err = q.LockUserRow(ctx, tok.UserID); err != nil {
			return fmt.Errorf("auth: lock user: %w", err)
		}
		if oldUser.DeletedAt != nil {
			return ErrInvalidToken
		}
		if user, err = changeEmail(ctx, q, tok.UserID, tok.NewEmail, now); err != nil {
			return err
		}
		// Ссылки сброса пароля ушли на старый адрес: после смены почты они не должны работать.
		if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: user.ID, Purpose: purposeReset}); err != nil {
			return fmt.Errorf("auth: supersede reset tokens: %w", err)
		}
		// Ссылки «это не я» прежних смен не отменяются: каждая возвращает свой адрес.
		revert, err = s.createToken(ctx, q, user.ID, purposeRevertEmail, s.cfg.RevertEmailTTL, oldUser.Email)
		return err
	})
	if err != nil {
		return User{}, err
	}
	s.sendAsync(s.emailChangedMail(oldUser, user.Email, revert))
	return userFrom(user), nil
}

// RevertEmailChange — «это не я» из письма на старый адрес.
func (s *Service) RevertEmailChange(ctx context.Context, rawToken string) error {
	var (
		user  dbgen.User
		reset string
		now   = s.now()
	)
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		tok, err := q.ConsumeEmailToken(ctx, dbgen.ConsumeEmailTokenParams{Now: now, TokenHash: hashToken(rawToken), Purpose: purposeRevertEmail})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return fmt.Errorf("auth: consume token: %w", err)
		}
		if user, err = q.LockUserRow(ctx, tok.UserID); err != nil {
			return fmt.Errorf("auth: lock user: %w", err)
		}
		if user.DeletedAt != nil {
			return ErrInvalidToken
		}
		if user.Email != tok.NewEmail {
			if user, err = changeEmail(ctx, q, user.ID, tok.NewEmail, now); err != nil {
				return err
			}
		}
		if err := q.DeleteUserSessions(ctx, user.ID); err != nil {
			return fmt.Errorf("auth: revoke sessions: %w", err)
		}
		for _, purpose := range []string{purposeChangeEmail, purposeReset} {
			if err := q.SupersedeAuthTokens(ctx, dbgen.SupersedeAuthTokensParams{At: now, UserID: user.ID, Purpose: purpose}); err != nil {
				return fmt.Errorf("auth: supersede tokens: %w", err)
			}
		}
		// Удаление аккаунта мог заказать тот, кто сменил почту.
		if err := s.cancelDeletion(ctx, q, user.ID); err != nil {
			return err
		}
		reset, err = s.createToken(ctx, q, user.ID, purposeReset, s.cfg.ResetTTL, "")
		return err
	})
	if err != nil {
		return err
	}
	s.sendAsync(s.emailRevertedMail(user, reset))
	return nil
}

// changeEmail ставит аккаунту новую почту; адрес, занятый другим аккаунтом, — ErrEmailTaken.
func changeEmail(ctx context.Context, q *dbgen.Queries, id uuid.UUID, email string, now time.Time) (dbgen.User, error) {
	user, err := q.ChangeUserEmail(ctx, dbgen.ChangeUserEmailParams{ID: id, Email: email, Now: now})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
		return dbgen.User{}, ErrEmailTaken
	}
	if err != nil {
		return dbgen.User{}, fmt.Errorf("auth: change email: %w", err)
	}
	return user, nil
}

// uniqueViolation — код ошибки PostgreSQL «такое значение уже есть».
const uniqueViolation = "23505"

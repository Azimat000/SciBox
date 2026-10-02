package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Здесь каждый обращение к базе в операции по очереди «ломается», и тест проверяет, что ошибка
// доходит до вызывающего (а не теряется и не превращается в «неверный пароль» или «ссылка устарела»).
// Это не подделка базы: все остальные запросы идут в настоящую PostgreSQL.

var errFault = errors.New("injected database failure")

type faultDB struct {
	DB
	calls  *atomic.Int32
	failAt int32
	// hook, если задан, вызывается перед каждым запросом с его текстом: так тест вклинивается
	// между двумя шагами операции и играет роль «параллельного» запроса.
	hook func(sql string)
}

func (f *faultDB) before(sql string) error {
	if f.hook != nil {
		f.hook(sql)
	}
	if f.calls.Add(1) == f.failAt {
		return errFault
	}
	return nil
}

func (f *faultDB) tick() error { return f.before("") }

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := f.before(sql); err != nil {
		return pgconn.CommandTag{}, err
	}
	return f.DB.Exec(ctx, sql, args...)
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := f.before(sql); err != nil {
		return nil, err
	}
	return f.DB.Query(ctx, sql, args...)
}

type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := f.before(sql); err != nil {
		return failedRow{err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if err := f.tick(); err != nil {
		return nil, err
	}
	tx, err := f.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &faultTx{Tx: tx, f: f}, nil
}

type faultTx struct {
	pgx.Tx
	f *faultDB
}

func (t *faultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := t.f.before(sql); err != nil {
		return pgconn.CommandTag{}, err
	}
	return t.Tx.Exec(ctx, sql, args...)
}

func (t *faultTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := t.f.before(sql); err != nil {
		return nil, err
	}
	return t.Tx.Query(ctx, sql, args...)
}

func (t *faultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := t.f.before(sql); err != nil {
		return failedRow{err}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *faultTx) Commit(ctx context.Context) error {
	if err := t.f.tick(); err != nil {
		return err
	}
	return t.Tx.Commit(ctx)
}

// runFaults снова и снова готовит свежее состояние и вызывает операцию, ломая n-е обращение к базе (n = 1, 2, …),
// пока не найдётся n, большее числа обращений операции. Каждый такой сбой обязан вернуться как errFault.
func runFaults(t *testing.T, prepare func(t *testing.T, e *env) func(s *Service) error) {
	t.Helper()
	for n := int32(1); n <= 60; n++ {
		e := newEnv(t)
		call := prepare(t, e)
		calls := &atomic.Int32{}
		s := newService(&faultDB{DB: sharedPool, calls: calls, failAt: n}, e.mailer, e.svc.cfg, slog.New(slog.DiscardHandler))
		s.now = e.clock.Now
		err := call(s)
		if calls.Load() < n {
			// Операция сделала меньше n обращений: сбой не сработал, значит, она завершилась как обычно.
			if errors.Is(err, errFault) {
				t.Fatalf("fault %d: an untriggered fault must not appear in the result", n)
			}
			if n == 1 {
				t.Fatal("the operation made no database calls")
			}
			return
		}
		if !errors.Is(err, errFault) {
			t.Fatalf("a database failure at call #%d was lost: err = %v", n, err)
		}
	}
	t.Fatal("operation made suspiciously many database calls")
}

func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	registerInput := func() RegisterInput {
		return RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}
	}
	cases := map[string]func(t *testing.T, e *env) func(s *Service) error{
		"register, new address": func(t *testing.T, e *env) func(*Service) error {
			in := registerInput()
			return func(s *Service) error { _, err := s.Register(bg, in, e.meta()); return err }
		},
		"register, unconfirmed address": func(t *testing.T, e *env) func(*Service) error {
			in := registerInput()
			e.register(in.Email)
			e.clock.Advance(2 * time.Minute)
			return func(s *Service) error { _, err := s.Register(bg, in, e.meta()); return err }
		},
		"register, confirmed address": func(t *testing.T, e *env) func(*Service) error {
			_, _, email := e.confirmedUser()
			in := registerInput()
			in.Email = email
			return func(s *Service) error { _, err := s.Register(bg, in, e.meta()); return err }
		},
		"resend confirmation": func(t *testing.T, e *env) func(*Service) error {
			email := uniqueEmail()
			e.register(email)
			e.clock.Advance(2 * time.Minute)
			return func(s *Service) error { return s.ResendConfirmation(bg, email, e.meta()) }
		},
		"request password reset": func(t *testing.T, e *env) func(*Service) error {
			_, _, email := e.confirmedUser()
			e.clock.Advance(2 * time.Minute)
			return func(s *Service) error { return s.RequestPasswordReset(bg, email, e.meta()) }
		},
		"request password reset, unknown address": func(t *testing.T, e *env) func(*Service) error {
			return func(s *Service) error { return s.RequestPasswordReset(bg, uniqueEmail(), e.meta()) }
		},
		"confirm email": func(t *testing.T, e *env) func(*Service) error {
			tok := e.register(uniqueEmail())
			return func(s *Service) error { _, _, err := s.ConfirmEmail(bg, tok, e.meta()); return err }
		},
		"login": func(t *testing.T, e *env) func(*Service) error {
			_, _, email := e.confirmedUser()
			return func(s *Service) error { _, _, err := s.Login(bg, email, goodPassword, e.meta()); return err }
		},
		"login, wrong password": func(t *testing.T, e *env) func(*Service) error {
			_, _, email := e.confirmedUser()
			return func(s *Service) error {
				_, _, err := s.Login(bg, email, "wrong password!!", e.meta())
				if errors.Is(err, ErrInvalidCredentials) {
					return nil // сбой не сработал
				}
				return err
			}
		},
		"login, unknown address": func(t *testing.T, e *env) func(*Service) error {
			return func(s *Service) error {
				_, _, err := s.Login(bg, uniqueEmail(), goodPassword, e.meta())
				if errors.Is(err, ErrInvalidCredentials) {
					return nil
				}
				return err
			}
		},
		"authenticate, refresh": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			e.clock.Advance(2 * time.Hour)
			return func(s *Service) error { _, err := s.Authenticate(bg, sess.Token); return err }
		},
		"authenticate, expired": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			e.clock.Advance(40 * 24 * time.Hour)
			return func(s *Service) error {
				_, err := s.Authenticate(bg, sess.Token)
				if errors.Is(err, ErrUnauthenticated) {
					return nil
				}
				return err
			}
		},
		"logout": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			return func(s *Service) error { return s.Logout(bg, sess.Token) }
		},
		"revoke other sessions": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			p, _ := e.svc.Authenticate(bg, sess.Token)
			return func(s *Service) error { return s.RevokeOtherSessions(bg, p) }
		},
		"reset password": func(t *testing.T, e *env) func(*Service) error {
			_, _, email := e.confirmedUser()
			e.clock.Advance(2 * time.Minute)
			_ = e.svc.RequestPasswordReset(bg, email, e.meta())
			tok := tokenIn(t, e.lastMailTo(email))
			return func(s *Service) error { return s.ResetPassword(bg, tok, "a good new passphrase") }
		},
		"change password": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			p, _ := e.svc.Authenticate(bg, sess.Token)
			return func(s *Service) error { return s.ChangePassword(bg, p, goodPassword, "a good new passphrase") }
		},
		"change password, wrong current": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			p, _ := e.svc.Authenticate(bg, sess.Token)
			return func(s *Service) error {
				err := s.ChangePassword(bg, p, "wrong password!!", "a good new passphrase")
				if _, ok := err.(*ValidationError); ok {
					return nil
				}
				return err
			}
		},
		"update name": func(t *testing.T, e *env) func(*Service) error {
			_, sess, _ := e.confirmedUser()
			p, _ := e.svc.Authenticate(bg, sess.Token)
			return func(s *Service) error { _, err := s.UpdateName(bg, p, "Анна"); return err }
		},
		"cleanup": func(t *testing.T, e *env) func(*Service) error {
			return func(s *Service) error { return s.Cleanup(bg) }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) { runFaults(t, prepare) })
	}
}

// Двое одновременно открыли одну ссылку сброса: оба увидели её живой, но «погасить» удалось только одному.
func TestResetPasswordLosesTheRaceForTheLink(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	tok := tokenIn(t, e.lastMailTo(email))

	calls := &atomic.Int32{}
	racer := &faultDB{DB: sharedPool, calls: calls}
	racer.hook = func(sql string) {
		if strings.Contains(sql, "UPDATE auth_tokens SET used_at") && strings.Contains(sql, "RETURNING user_id") && racer.hook != nil {
			racer.hook = nil // только один раз
			if err := e.svc.ResetPassword(bg, tok, "the winner's passphrase"); err != nil {
				t.Errorf("the other request must win: %v", err)
			}
		}
	}
	s := newService(racer, e.mailer, e.svc.cfg, slog.New(slog.DiscardHandler))
	s.now = e.clock.Now
	if err := s.ResetPassword(bg, tok, "the loser's passphrase"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
	if _, _, err := e.login(email, "the winner's passphrase"); err != nil {
		t.Fatalf("the winner's password must be the one that stuck: %v", err)
	}
}

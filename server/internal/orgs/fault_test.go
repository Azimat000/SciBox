package orgs

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

	"scibox/server/internal/auth"
)

// Здесь каждое обращение к базе в операции по очереди «ломается», и тест проверяет, что ошибка доходит до
// вызывающего (а не теряется и не превращается в «нет прав», «не найдено» или «приглашение устарело»).
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
	if err := f.before("BEGIN"); err != nil {
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
	if err := t.f.before("COMMIT"); err != nil {
		return err
	}
	return t.Tx.Commit(ctx)
}

// faulty возвращает сервис, у которого n-е обращение к базе ломается.
func (w *world) faulty(n int32) (*Service, *atomic.Int32) {
	calls := &atomic.Int32{}
	s := newService(&faultDB{DB: sharedPool, calls: calls, failAt: n}, w.mailer, w.svc.cfg, slog.New(slog.DiscardHandler))
	s.now = w.clock.Now
	return s, calls
}

// runFaults снова и снова готовит свежее состояние и вызывает операцию, ломая n-е обращение к базе (n = 1, 2, …),
// пока не найдётся n, большее числа обращений операции. Каждый такой сбой обязан вернуться как errFault.
func runFaults(t *testing.T, prepare func(t *testing.T, w *world) func(s *Service) error) {
	t.Helper()
	for n := int32(1); n <= 40; n++ {
		w := newWorld(t)
		call := prepare(t, w)
		s, calls := w.faulty(n)
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
	// base: владелец, организация и подразделение.
	type base struct {
		owner person
		org   Organization
		unit  Unit
	}
	mk := func(w *world) base {
		owner := w.user("Иван")
		org := w.org(owner)
		return base{owner, org, w.unit(owner, org.Slug)}
	}
	cases := map[string]func(t *testing.T, w *world) func(s *Service) error{
		"create organization": func(t *testing.T, w *world) func(*Service) error {
			owner := w.user("Иван")
			return func(s *Service) error { _, err := s.CreateOrganization(bg, owner.User, orgInput()); return err }
		},
		"create organization, slug taken": func(t *testing.T, w *world) func(*Service) error {
			owner := w.user("Иван")
			in := orgInput()
			if _, err := w.svc.CreateOrganization(bg, owner.User, in); err != nil {
				t.Fatal(err)
			}
			return func(s *Service) error { _, err := s.CreateOrganization(bg, owner.User, in); return err }
		},
		"update organization": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.UpdateOrganization(bg, b.owner.User, b.org.Slug, orgInput())
				return err
			}
		},
		"get organization, member": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			head := w.user("Руководитель")
			w.member(b.owner, b.org.Slug, head, "unit_head", &b.unit.ID)
			return func(s *Service) error { _, err := s.GetOrganization(bg, b.org.Slug, &head.User); return err }
		},
		"get organization, anonymous": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.GetOrganization(bg, b.org.Slug, nil); return err }
		},
		"get unit": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.GetUnit(bg, b.org.Slug, b.unit.ID, &b.owner.User); return err }
		},
		"list organizations": func(t *testing.T, w *world) func(*Service) error {
			return func(s *Service) error {
				_, err := s.ListOrganizations(bg, ListFilter{Query: "Институт"})
				return err
			}
		},
		"my organizations": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.MyOrganizations(bg, b.owner.User); return err }
		},
		"my invitations": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.MyInvitations(bg, b.owner.User); return err }
		},
		"create unit": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.CreateUnit(bg, b.owner.User, b.org.Slug, unitInput()); return err }
		},
		"update unit": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.UpdateUnit(bg, b.owner.User, b.org.Slug, b.unit.ID, unitInput())
				return err
			}
		},
		"update unit as head": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			head := w.user("Руководитель")
			w.member(b.owner, b.org.Slug, head, "unit_head", &b.unit.ID)
			return func(s *Service) error {
				_, err := s.UpdateUnit(bg, head.User, b.org.Slug, b.unit.ID, unitInput())
				return err
			}
		},
		"set unit head": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.SetUnitHead(bg, b.owner.User, b.org.Slug, b.unit.ID, &b.owner.ID)
				return err
			}
		},
		"set unit head, not a member": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			stranger := w.user("Чужой")
			return func(s *Service) error {
				_, err := s.SetUnitHead(bg, b.owner.User, b.org.Slug, b.unit.ID, &stranger.ID)
				var verr *auth.ValidationError
				if errors.As(err, &verr) {
					return nil // сбой не сработал
				}
				return err
			}
		},
		"clear unit head": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.SetUnitHead(bg, b.owner.User, b.org.Slug, b.unit.ID, nil)
				return err
			}
		},
		"delete unit": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { return s.DeleteUnit(bg, b.owner.User, b.org.Slug, b.unit.ID) }
		},
		"members": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.Members(bg, b.owner.User, b.org.Slug); return err }
		},
		"change role": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			other := w.user("Анна")
			w.member(b.owner, b.org.Slug, other, "hr", nil)
			return func(s *Service) error { return s.ChangeRole(bg, b.owner.User, b.org.Slug, other.ID, "owner") }
		},
		"remove member": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			other := w.user("Анна")
			w.member(b.owner, b.org.Slug, other, "unit_head", &b.unit.ID)
			return func(s *Service) error { return s.RemoveMember(bg, b.owner.User, b.org.Slug, other.ID) }
		},
		"invite": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.Invite(bg, b.owner.User, b.org.Slug, InviteInput{Email: uniqueEmail(), Role: "unit_head", UnitID: &b.unit.ID})
				return err
			}
		},
		"revoke invitation": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			inv, err := w.svc.Invite(bg, b.owner.User, b.org.Slug, InviteInput{Email: uniqueEmail(), Role: "hr"})
			if err != nil {
				t.Fatal(err)
			}
			return func(s *Service) error { return s.RevokeInvitation(bg, b.owner.User, b.org.Slug, inv.ID) }
		},
		"lookup invitation": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			guest := w.user("Гость")
			if _, err := w.svc.Invite(bg, b.owner.User, b.org.Slug, InviteInput{Email: guest.Email, Role: "hr"}); err != nil {
				t.Fatal(err)
			}
			tok := tokenIn(t, w.lastMailTo(guest.Email).Body)
			return func(s *Service) error { _, err := s.LookupInvitation(bg, tok, &guest.User); return err }
		},
		"accept invitation": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			guest := w.user("Гость")
			if _, err := w.svc.Invite(bg, b.owner.User, b.org.Slug, InviteInput{Email: guest.Email, Role: "unit_head", UnitID: &b.unit.ID}); err != nil {
				t.Fatal(err)
			}
			tok := tokenIn(t, w.lastMailTo(guest.Email).Body)
			return func(s *Service) error { _, err := s.AcceptInvitation(bg, guest.User, tok); return err }
		},
		"accept invitation by id": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			guest := w.user("Гость")
			inv, err := w.svc.Invite(bg, b.owner.User, b.org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
			if err != nil {
				t.Fatal(err)
			}
			return func(s *Service) error { _, err := s.AcceptInvitationByID(bg, guest.User, inv.ID); return err }
		},
		"cleanup": func(t *testing.T, w *world) func(*Service) error {
			return func(s *Service) error { return s.Cleanup(bg) }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) { runFaults(t, prepare) })
	}
}

// Два владельца одновременно уходят: проверка «последнего владельца» не должна пропустить обоих.
func TestTwoOwnersCannotBothLeave(t *testing.T) {
	w := newWorld(t)
	first := w.user("Первый")
	org := w.org(first)
	second := w.user("Второй")
	w.member(first, org.Slug, second, "owner", nil)

	// Пока первый владелец проверяется, второй успевает уйти: блокировка строк владельцев не даст этому случиться,
	// поэтому «параллельный» запрос второго просто подождёт. Проверяем итог: хоть один владелец остался.
	done := make(chan error, 2)
	go func() { done <- w.svc.RemoveMember(bg, first.User, org.Slug, first.ID) }()
	go func() { done <- w.svc.RemoveMember(bg, second.User, org.Slug, second.ID) }()
	var failures int
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			if !errors.Is(err, ErrLastOwner) {
				t.Errorf("unexpected error: %v", err)
			}
			failures++
		}
	}
	if failures != 1 {
		t.Errorf("exactly one of two simultaneous leaves must be refused, got %d refusals", failures)
	}
	if n := countRows(t, "SELECT count(*) FROM org_members WHERE org_id = $1 AND role = 'owner'", org.ID); n != 1 {
		t.Errorf("%d owners left, want exactly 1", n)
	}
}

// Одну ссылку приглашения открыли дважды одновременно: принять удаётся одному.
func TestInvitationLosesTheRaceForTheLink(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	guest := w.user("Гость")
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	tok := tokenIn(t, w.lastMailTo(guest.Email).Body)

	calls := &atomic.Int32{}
	racer := &faultDB{DB: sharedPool, calls: calls}
	racer.hook = func(sql string) {
		if strings.Contains(sql, "UPDATE org_invitations SET accepted_at") && racer.hook != nil {
			racer.hook = nil // только один раз
			if _, err := w.svc.AcceptInvitation(bg, guest.User, tok); err != nil {
				t.Errorf("the other request must win: %v", err)
			}
		}
	}
	s := newService(racer, w.mailer, w.svc.cfg, slog.New(slog.DiscardHandler))
	s.now = w.clock.Now
	if _, err := s.AcceptInvitation(bg, guest.User, tok); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("err = %v, want ErrInvalidInvitation", err)
	}
	if n := countRows(t, "SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2", org.ID, guest.ID); n != 1 {
		t.Errorf("the guest is in the organization %d times", n)
	}
}

func TestCleanup(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: uniqueEmail(), Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		return countRows(t, "SELECT count(*) FROM org_invitations WHERE org_id = $1", org.ID)
	}
	if err := w.svc.Cleanup(bg); err != nil || count() != 1 {
		t.Fatalf("a fresh invitation must stay: %d, %v", count(), err)
	}
	w.clock.Advance(7*24*time.Hour + 29*24*time.Hour)
	if err := w.svc.Cleanup(bg); err != nil || count() != 1 {
		t.Fatalf("expired less than 30 days ago must stay: %d, %v", count(), err)
	}
	w.clock.Advance(2 * 24 * time.Hour)
	if err := w.svc.Cleanup(bg); err != nil || count() != 0 {
		t.Fatalf("expired long ago must go: %d, %v", count(), err)
	}
}

func TestRunCleanupStopsOnCancel(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { w.svc.RunCleanup(ctx, 5*time.Millisecond); close(done) }()
	time.Sleep(30 * time.Millisecond) // несколько срабатываний
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunCleanup did not stop after cancel")
	}
}

func TestRunCleanupLogsFailures(t *testing.T) {
	w := newWorld(t)
	s, _ := w.faulty(1)
	logs := &syncBuffer{}
	s.logger = slog.New(slog.NewTextHandler(logs, nil))
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	go s.RunCleanup(ctx, 5*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "orgs cleanup") {
		if time.Now().After(deadline) {
			t.Fatal("a cleanup failure was not logged")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

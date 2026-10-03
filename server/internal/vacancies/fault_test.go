package vacancies

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	s := newService(&faultDB{DB: sharedPool, calls: calls, failAt: n}, w.svc.cfg)
	s.now = w.clock.Now
	return s, calls
}

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
	type base struct {
		tm  *team
		pub Detail
	}
	mk := func(w *world) base {
		tm := w.team()
		return base{tm, w.published(tm, goodInput())}
	}
	cases := map[string]func(t *testing.T, w *world) func(s *Service) error{
		"create": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			in := goodInput()
			in.UnitID = &tm.unitA.ID
			return func(s *Service) error { _, err := s.Create(bg, tm.owner.User, tm.slug, in); return err }
		},
		"create without specialties": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			return func(s *Service) error {
				_, err := s.Create(bg, tm.hr.User, tm.slug, Input{Title: "Черновик", PositionCode: "researcher"})
				return err
			}
		},
		"update": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			in := goodInput()
			in.UnitID = &b.tm.unitA.ID
			return func(s *Service) error { _, err := s.Update(bg, b.tm.owner.User, b.pub.ID, in); return err }
		},
		"update as unit head": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			in := goodInput()
			in.UnitID = &tm.unitA.ID
			d := w.create(tm, tm.owner, in)
			return func(s *Service) error { _, err := s.Update(bg, tm.headA.User, d.ID, in); return err }
		},
		"publish": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			d := w.create(tm, tm.owner, goodInput())
			return func(s *Service) error { _, err := s.SetStatus(bg, tm.owner.User, d.ID, StatusPublished); return err }
		},
		"close": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.SetStatus(bg, b.tm.owner.User, b.pub.ID, StatusClosed); return err }
		},
		"delete": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			d := w.create(tm, tm.owner, goodInput())
			return func(s *Service) error { return s.Delete(bg, tm.owner.User, d.ID) }
		},
		"get as owner": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.Get(bg, b.pub.ID, &b.tm.owner.User); return err }
		},
		"get as unit head": func(t *testing.T, w *world) func(*Service) error {
			tm := w.team()
			in := goodInput()
			in.UnitID = &tm.unitA.ID
			d := w.create(tm, tm.owner, in)
			return func(s *Service) error { _, err := s.Get(bg, d.ID, &tm.headA.User); return err }
		},
		"get anonymous": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.Get(bg, b.pub.ID, nil); return err }
		},
		"list published of an organization": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error {
				_, err := s.ListPublished(bg, ListFilter{OrgSlug: b.tm.slug, UnitID: &b.tm.unitA.ID})
				return err
			}
		},
		"list published": func(t *testing.T, w *world) func(*Service) error {
			mk(w)
			return func(s *Service) error { _, err := s.ListPublished(bg, ListFilter{}); return err }
		},
		"list mine": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.ListMine(bg, b.tm.headA.User, "", 0, 0); return err }
		},
		"list mine as owner": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.ListMine(bg, b.tm.owner.User, StatusPublished, 0, 0); return err }
		},
		"targets": func(t *testing.T, w *world) func(*Service) error {
			b := mk(w)
			return func(s *Service) error { _, err := s.Targets(bg, b.tm.headB.User); return err }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) { runFaults(t, prepare) })
	}
}

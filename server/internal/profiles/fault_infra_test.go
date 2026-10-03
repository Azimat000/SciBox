package profiles

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
	s := newService(&faultDB{DB: sharedPool, calls: calls, failAt: n}, w.doi, w.svc.cfg)
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

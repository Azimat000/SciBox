package testkit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/dbgen"
)

// DB — то, что сервисам нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ErrFault — «сбой базы», который подставляют тесты.
var ErrFault = errors.New("injected database failure")

// FaultDB пропускает все обращения к настоящей базе, кроме n-го: оно возвращает ErrFault.
// Это не подделка базы: остальные запросы идут в настоящую PostgreSQL.
type FaultDB struct {
	DB
	calls  *atomic.Int32
	failAt int32
	// Hook, если задан, вызывается перед каждым запросом с его текстом: так тест вклинивается
	// между двумя шагами операции и играет роль «параллельного» запроса.
	Hook func(sql string)
}

// Faulty оборачивает базу: n-е обращение ломается. Второе значение считает обращения.
func Faulty(base DB, n int32) (*FaultDB, *atomic.Int32) {
	calls := &atomic.Int32{}
	return &FaultDB{DB: base, calls: calls, failAt: n}, calls
}

func (f *FaultDB) before(sql string) error {
	if f.Hook != nil {
		f.Hook(sql)
	}
	if f.calls.Add(1) == f.failAt {
		return ErrFault
	}
	return nil
}

// Exec — см. pgx.
func (f *FaultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := f.before(sql); err != nil {
		return pgconn.CommandTag{}, err
	}
	return f.DB.Exec(ctx, sql, args...)
}

// Query — см. pgx.
func (f *FaultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := f.before(sql); err != nil {
		return nil, err
	}
	return f.DB.Query(ctx, sql, args...)
}

type failedRow struct{ err error }

func (r failedRow) Scan(...any) error { return r.err }

// QueryRow — см. pgx.
func (f *FaultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if err := f.before(sql); err != nil {
		return failedRow{err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

// Begin начинает транзакцию, все обращения которой тоже считаются.
func (f *FaultDB) Begin(ctx context.Context) (pgx.Tx, error) {
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
	f *FaultDB
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

// RunFaults по очереди ломает 1-е, 2-е, … обращение к базе в операции и проверяет, что ошибка доходит до
// вызывающего (а не теряется и не превращается в «нет прав», «не найдено» и т. п.). prepare вызывается заново для
// каждого n: готовит данные и возвращает операцию, которая принимает «ломающуюся» базу.
func RunFaults(t *testing.T, prepare func(t *testing.T) func(db DB) error) {
	t.Helper()
	for n := int32(1); n <= 80; n++ {
		call := prepare(t)
		db, calls := Faulty(Pool, n)
		err := call(db)
		if calls.Load() < n {
			// Операция сделала меньше n обращений: сбой не сработал, значит, она завершилась как обычно.
			if errors.Is(err, ErrFault) {
				t.Fatalf("fault %d: an untriggered fault must not appear in the result", n)
			}
			if n == 1 {
				t.Fatal("the operation made no database calls")
			}
			return
		}
		if !errors.Is(err, ErrFault) {
			t.Fatalf("a database failure at call #%d was lost: err = %v", n, err)
		}
	}
	t.Fatal("operation made suspiciously many database calls")
}

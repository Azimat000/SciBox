package auth

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/testdb"
)

// Одна временная база на весь пакет: тесты берут уникальные почты и адреса, поэтому не мешают друг другу.
var sharedPool *pgxpool.Pool

// mainTB — минимальная замена testing.TB для TestMain.
type mainTB struct{ cleanups []func() }

func (m *mainTB) Helper() {}
func (m *mainTB) Fatalf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	m.run()
	os.Exit(1)
}
func (m *mainTB) Cleanup(f func()) { m.cleanups = append(m.cleanups, f) }
func (m *mainTB) run() {
	for i := len(m.cleanups) - 1; i >= 0; i-- {
		m.cleanups[i]()
	}
	m.cleanups = nil
}

func TestMain(m *testing.M) {
	tb := &mainTB{}
	url := testdb.Create(tb, true)
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open pool:", err)
		tb.run()
		os.Exit(1)
	}
	sharedPool = pool
	code := m.Run()
	pool.Close()
	tb.run()
	os.Exit(code)
}

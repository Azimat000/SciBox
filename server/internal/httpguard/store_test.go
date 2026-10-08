package httpguard

import (
	"bytes"
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"scibox/server/internal/testdb"
)

func TestGuestKey(t *testing.T) {
	cases := []struct {
		addr netip.Addr
		want string
	}{
		{netip.MustParseAddr("203.0.113.7"), "ip:203.0.113.7"},
		{netip.MustParseAddr("2001:db8:1:2:3:4:5:6"), "ip6:2001:db8:1:2::/64"},
		{netip.MustParseAddr("2001:db8:1:2:ffff::1"), "ip6:2001:db8:1:2::/64"}, // та же сеть /64 — тот же ключ
		{netip.Addr{}, "ip:unknown"},
	}
	for _, tc := range cases {
		if got := guestKey(tc.addr); got != tc.want {
			t.Errorf("guestKey(%v) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()
	ctx := t.Context()
	w := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	hit := func(kind, key string, window time.Time) int {
		n, err := s.Hit(ctx, kind, key, window)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if hit("a", "k", w) != 1 || hit("a", "k", w) != 2 {
		t.Fatal("same window must count up")
	}
	// Один и тот же момент в другом часовом поясе — то же окно.
	if hit("a", "k", w.In(time.FixedZone("MSK", 3*3600))) != 3 {
		t.Fatal("time zone split the counter")
	}
	if hit("b", "k", w) != 1 || hit("a", "other", w) != 1 || hit("a", "k", w.Add(time.Minute)) != 1 {
		t.Fatal("kinds, keys and windows must be separate")
	}
	// Через KeepCounters старые окна удаляются, свежие остаются.
	later := w.Add(2*KeepCounters + time.Minute)
	hit("a", "fresh", later.Add(-time.Minute))
	hit("a", "k", later)
	if len(s.counts) != 2 {
		t.Fatalf("after sweep %d counters left: %v", len(s.counts), s.counts)
	}
}

// Счётчики в базе (D-141): общий счёт для всех копий сервера, одновременные запросы не теряются,
// старые окна удаляются.
func TestPostgresStore(t *testing.T) {
	pool := testdb.New(t)
	s := NewPostgresStore(pool)
	ctx := t.Context()
	w := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	if n, err := s.Hit(ctx, "a", "ip:1", w); err != nil || n != 1 {
		t.Fatalf("first hit = %d, %v", n, err)
	}
	// Вторая «копия сервера» со своим хранилищем продолжает тот же счёт.
	other := NewPostgresStore(pool)
	if n, err := other.Hit(ctx, "a", "ip:1", w); err != nil || n != 2 {
		t.Fatalf("hit from another copy = %d, %v", n, err)
	}
	if n, _ := s.Hit(ctx, "b", "ip:1", w); n != 1 {
		t.Fatalf("other kind = %d", n)
	}
	if n, _ := s.Hit(ctx, "a", "ip:1", w.Add(time.Minute)); n != 1 {
		t.Fatalf("next window = %d", n)
	}

	var wg sync.WaitGroup
	results := make([]int, 40)
	for i := range results {
		wg.Go(func() {
			n, err := s.Hit(ctx, "race", "user:x", w)
			if err != nil {
				t.Error(err)
			}
			results[i] = n
		})
	}
	wg.Wait()
	slices.Sort(results)
	for i, n := range results {
		if n != i+1 {
			t.Fatalf("concurrent hits = %v, want 1..40 each once", results)
		}
	}

	// Очистка: окна старше KeepCounters уходят.
	now := w.Add(KeepCounters + 30*time.Minute)
	if _, err := s.Hit(ctx, "a", "ip:fresh", now.Truncate(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(ctx, now); err != nil {
		t.Fatal(err)
	}
	var left []string
	rows, err := pool.Query(ctx, "SELECT kind || '/' || key FROM rate_counters ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		left = append(left, k)
	}
	if rows.Err() != nil || strings.Join(left, ",") != "a/ip:fresh" {
		t.Fatalf("after cleanup: %v (%v)", left, rows.Err())
	}

	// Цикл очистки: работает до отмены, сбой пишет в журнал.
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.RunCleanup(loopCtx, 10*time.Millisecond, logger)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	stop()
	<-done
	if logs.String() != "" {
		t.Fatalf("healthy cleanup logged: %s", logs.String())
	}

	broken := testdb.New(t)
	bs := NewPostgresStore(broken)
	if _, err := broken.Exec(ctx, "DROP TABLE rate_counters"); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.Hit(ctx, "a", "k", w); err == nil {
		t.Fatal("hit on a broken table must fail")
	}
	loopCtx, stop = context.WithCancel(ctx)
	done = make(chan struct{})
	go func() {
		bs.RunCleanup(loopCtx, 10*time.Millisecond, logger)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "rate counters cleanup") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	if !strings.Contains(logs.String(), "rate counters cleanup") {
		t.Fatalf("broken cleanup not logged: %q", logs.String())
	}
}

// lockedBuffer — журнал, в который пишет горутина очистки, пока тест его читает.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

package httpcache

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Паника сборки не оставляет запись «собирается» навсегда и не роняет фоновое обновление (срез 29, CON-8).
func TestKeyedLoadPanicBecomesAnErrorAndTheNextAskerRetries(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	k := NewKeyedWithClock(time.Minute, 10, func() time.Time { return now })
	_, err := k.Get(context.Background(), "k", 0, func(context.Context) (any, error) { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		doc, err := k.Get(context.Background(), "k", 0, func(context.Context) (any, error) { return map[string]int{"n": 1}, nil })
		if err != nil || len(doc.Body) == 0 {
			t.Errorf("the next asker: %v %q", err, doc.Body)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the next asker hangs on an entry that never finishes")
	}
}

func TestKeyedStaleRefreshPanicKeepsTheProcessAndTheOldAnswer(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	k := NewKeyedWithClock(time.Second, 10, func() time.Time { return now }).WithStale(time.Minute)
	first, err := k.Get(context.Background(), "k", 0, func(context.Context) (any, error) { return map[string]int{"n": 1}, nil })
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	refreshed := make(chan struct{})
	stale, err := k.Get(context.Background(), "k", 0, func(context.Context) (any, error) {
		defer close(refreshed)
		panic("boom in the background")
	})
	if err != nil || string(stale.Body) != string(first.Body) {
		t.Fatalf("stale answer: %v %q", err, stale.Body)
	}
	<-refreshed
	time.Sleep(50 * time.Millisecond) // процесс жив, если мы дошли досюда
}

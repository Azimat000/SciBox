package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newBreaker(threshold int) (*Breaker, *clock) {
	c := &clock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	return NewBreaker(BreakerConfig{Threshold: threshold, Cooldown: 30 * time.Second, MaxCooldown: 2 * time.Minute, Now: c.now}), c
}

// call делает одно обращение с заданным итогом и возвращает, пропустил ли его предохранитель.
func call(t *testing.T, b *Breaker, o Outcome) bool {
	t.Helper()
	done, err := b.Allow()
	if err != nil {
		if !errors.Is(err, ErrOpen) {
			t.Fatalf("Allow error = %v, want ErrOpen", err)
		}
		return false
	}
	done(o)
	return true
}

func TestNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   BreakerConfig
		want BreakerConfig
	}{
		{"empty", BreakerConfig{}, BreakerConfig{Threshold: 5, Cooldown: 30 * time.Second, MaxCooldown: 5 * time.Minute}},
		{"negative never means no pause", BreakerConfig{Threshold: -1, Cooldown: -time.Second, MaxCooldown: -time.Minute}, BreakerConfig{Threshold: 5, Cooldown: 30 * time.Second, MaxCooldown: 5 * time.Minute}},
		{"cooldown above the cap raises the cap", BreakerConfig{Threshold: 2, Cooldown: 10 * time.Minute, MaxCooldown: time.Minute}, BreakerConfig{Threshold: 2, Cooldown: 10 * time.Minute, MaxCooldown: 10 * time.Minute}},
		{"own values", BreakerConfig{Threshold: 3, Cooldown: time.Second, MaxCooldown: time.Hour}, BreakerConfig{Threshold: 3, Cooldown: time.Second, MaxCooldown: time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Normalized()
			if got.Now == nil {
				t.Fatal("Now is not set")
			}
			if got.Threshold != tt.want.Threshold || got.Cooldown != tt.want.Cooldown || got.MaxCooldown != tt.want.MaxCooldown {
				t.Errorf("Normalized() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBreakerOpensAfterAThresholdOfFailuresInARow(t *testing.T) {
	b, _ := newBreaker(3)
	for i := range 2 {
		if !call(t, b, Failure) {
			t.Fatalf("call %d was refused too early", i)
		}
	}
	if b.State() != Closed {
		t.Fatalf("state after 2 failures = %v, want closed", b.State())
	}
	// Успех обнуляет серию.
	call(t, b, Success)
	call(t, b, Failure)
	call(t, b, Failure)
	if b.State() != Closed {
		t.Fatalf("state = %v: a success must reset the series", b.State())
	}
	call(t, b, Failure)
	if b.State() != Open {
		t.Fatalf("state after the third failure in a row = %v, want open", b.State())
	}
	if call(t, b, Success) {
		t.Error("an open breaker let a call through")
	}
	if b.Trips() != 1 {
		t.Errorf("Trips() = %d, want 1", b.Trips())
	}
}

func TestNeutralOutcomesDoNotCount(t *testing.T) {
	b, _ := newBreaker(2)
	call(t, b, Failure)
	for range 10 {
		call(t, b, Neutral)
	}
	if b.State() != Closed {
		t.Fatalf("state = %v: neutral outcomes must not open the breaker", b.State())
	}
	call(t, b, Failure)
	if b.State() != Open {
		t.Fatalf("state = %v, want open: neutral must not reset the series either", b.State())
	}
}

func TestTripOpensAtOnce(t *testing.T) {
	b, _ := newBreaker(5)
	call(t, b, Trip)
	if b.State() != Open {
		t.Fatalf("state = %v, want open after a trip", b.State())
	}
}

func TestBreakerProbesAfterTheCooldownAndClosesOnSuccess(t *testing.T) {
	b, c := newBreaker(1)
	call(t, b, Failure)
	c.advance(29 * time.Second)
	if call(t, b, Success) {
		t.Fatal("a call passed before the cooldown ended")
	}
	if got := b.RetryAfter(); got != time.Second {
		t.Errorf("RetryAfter() = %v, want 1s", got)
	}
	c.advance(time.Second)
	done, err := b.Allow()
	if err != nil {
		t.Fatalf("probe was refused: %v", err)
	}
	if b.State() != HalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	// Пока проба идёт, остальные не пропускаются.
	if call(t, b, Success) {
		t.Error("a second call passed during the probe")
	}
	done(Success)
	if b.State() != Closed {
		t.Fatalf("state = %v, want closed after a successful probe", b.State())
	}
	if b.RetryAfter() != 0 {
		t.Errorf("RetryAfter() = %v, want 0 when closed", b.RetryAfter())
	}
	if !call(t, b, Success) {
		t.Error("closed breaker refused a call")
	}
}

func TestFailedProbeDoublesTheCooldownUpToTheCap(t *testing.T) {
	b, c := newBreaker(1)
	call(t, b, Failure) // пауза 30 с
	for _, wait := range []time.Duration{30 * time.Second, 60 * time.Second, 2 * time.Minute, 2 * time.Minute} {
		if got := b.RetryAfter(); got != wait {
			t.Fatalf("cooldown = %v, want %v", got, wait)
		}
		c.advance(wait)
		if !call(t, b, Failure) {
			t.Fatal("probe was refused")
		}
	}
	// После удачной пробы пауза возвращается к обычной.
	c.advance(2 * time.Minute)
	call(t, b, Success)
	call(t, b, Failure)
	if got := b.RetryAfter(); got != 30*time.Second {
		t.Errorf("cooldown after recovery = %v, want 30s", got)
	}
}

func TestNeutralProbeLetsTheNextCallTry(t *testing.T) {
	b, c := newBreaker(1)
	call(t, b, Failure)
	c.advance(30 * time.Second)
	call(t, b, Neutral) // проба ничего не показала
	if b.State() == Closed {
		t.Fatal("a neutral probe must not close the breaker")
	}
	if !call(t, b, Success) {
		t.Fatal("the next call was refused after a neutral probe")
	}
	if b.State() != Closed {
		t.Errorf("state = %v, want closed", b.State())
	}
}

func TestLateOutcomeOfAnEarlierStateIsIgnored(t *testing.T) {
	b, c := newBreaker(2)
	slow, err := b.Allow() // долгое обращение начато, пока всё было хорошо
	if err != nil {
		t.Fatal(err)
	}
	call(t, b, Failure)
	call(t, b, Failure) // открылся
	slow(Success)       // запоздалый успех не должен закрыть предохранитель
	if b.State() != Open {
		t.Fatalf("state = %v: a late success must not close an open breaker", b.State())
	}
	// Двойной вызов done игнорируется.
	c.advance(30 * time.Second)
	probe, _ := b.Allow()
	probe(Success)
	probe(Failure)
	if b.State() != Closed {
		t.Errorf("state = %v: a second done call must change nothing", b.State())
	}
}

func TestStateStrings(t *testing.T) {
	for s, want := range map[State]string{Closed: "closed", Open: "open", HalfOpen: "half-open", State(9): "unknown"} {
		if got := s.String(); got != want {
			t.Errorf("State(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestBreakerIsSafeUnderRace(t *testing.T) {
	b := NewBreaker(BreakerConfig{Threshold: 3, Cooldown: time.Millisecond})
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 200 {
				if done, err := b.Allow(); err == nil {
					done([]Outcome{Success, Failure, Neutral, Trip}[(i+j)%4])
				}
				_ = b.State()
				_ = b.RetryAfter()
			}
		})
	}
	wg.Wait()
}

func TestLimiterCapsConcurrencyAndRefusesAfterTheWait(t *testing.T) {
	l := NewLimiter(2, 20*time.Millisecond)
	if l.Capacity() != 2 {
		t.Fatalf("Capacity() = %d, want 2", l.Capacity())
	}
	r1, err := l.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := l.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.InFlight() != 2 {
		t.Errorf("InFlight() = %d, want 2", l.InFlight())
	}
	start := time.Now()
	if _, err := l.Acquire(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("third Acquire error = %v, want ErrBusy", err)
	}
	if waited := time.Since(start); waited < 15*time.Millisecond {
		t.Errorf("waited %v, want about 20ms", waited)
	}
	if l.Rejected() != 1 {
		t.Errorf("Rejected() = %d, want 1", l.Rejected())
	}
	r1()
	r3, err := l.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire after a release: %v", err)
	}
	r2()
	r3()
	if l.InFlight() != 0 {
		t.Errorf("InFlight() = %d, want 0", l.InFlight())
	}
}

func TestLimiterWaitsForARelease(t *testing.T) {
	l := NewLimiter(1, time.Second)
	r1, _ := l.Acquire(t.Context())
	got := make(chan error, 1)
	go func() {
		r, err := l.Acquire(t.Context())
		if err == nil {
			r()
		}
		got <- err
	}()
	time.Sleep(20 * time.Millisecond)
	r1()
	if err := <-got; err != nil {
		t.Fatalf("waiting Acquire error = %v, want nil", err)
	}
}

func TestLimiterWithoutWaitRefusesAtOnceAndMinimumIsOne(t *testing.T) {
	l := NewLimiter(0, -time.Second)
	if l.Capacity() != 1 {
		t.Fatalf("Capacity() = %d, want 1", l.Capacity())
	}
	r, err := l.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer r()
	if _, err := l.Acquire(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
}

func TestLimiterStopsOnCancel(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	r, _ := l.Acquire(t.Context())
	defer r()
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	if _, err := l.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	// Уже отменённый контекст не занимает место, даже если оно свободно.
	free := NewLimiter(1, 0)
	if _, err := free.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if free.InFlight() != 0 {
		t.Error("a cancelled Acquire took a slot")
	}
}

func TestLimiterNeverExceedsItsCapacity(t *testing.T) {
	l := NewLimiter(3, time.Second)
	var cur, peak atomic.Int64
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			r, err := l.Acquire(t.Context())
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			n := cur.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(time.Millisecond)
			cur.Add(-1)
			r()
		})
	}
	wg.Wait()
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency = %d, want <= 3", peak.Load())
	}
}

func TestGate(t *testing.T) {
	boom := errors.New("boom")
	t.Run("nil gate just calls", func(t *testing.T) {
		var g *Gate
		if err := g.Do(t.Context(), func(context.Context) error { return boom }, nil); !errors.Is(err, boom) {
			t.Fatalf("error = %v, want boom", err)
		}
	})
	t.Run("failures open the breaker and the call is not made", func(t *testing.T) {
		b, _ := newBreaker(2)
		g := &Gate{Breaker: b}
		for range 2 {
			_ = g.Do(t.Context(), func(context.Context) error { return boom }, nil)
		}
		called := false
		err := g.Do(t.Context(), func(context.Context) error { called = true; return nil }, nil)
		if !errors.Is(err, ErrOpen) || called {
			t.Fatalf("err = %v, called = %v: want ErrOpen and no call", err, called)
		}
		if !Rejected(err) {
			t.Error("Rejected(ErrOpen) = false")
		}
	})
	t.Run("caller cancel is neutral", func(t *testing.T) {
		b, _ := newBreaker(1)
		g := &Gate{Breaker: b}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_ = g.Do(ctx, func(ctx context.Context) error { return ctx.Err() }, nil)
		if b.State() != Closed {
			t.Fatalf("state = %v: the caller's cancel must not count as a service failure", b.State())
		}
	})
	t.Run("custom classifier", func(t *testing.T) {
		b, _ := newBreaker(1)
		g := &Gate{Breaker: b}
		notFound := errors.New("not found")
		classify := func(err error) Outcome {
			if errors.Is(err, notFound) {
				return Neutral
			}
			return Failure
		}
		if err := g.Do(t.Context(), func(context.Context) error { return notFound }, classify); !errors.Is(err, notFound) {
			t.Fatal(err)
		}
		if b.State() != Closed {
			t.Fatalf("state = %v: a neutral outcome must not open the breaker", b.State())
		}
	})
	t.Run("a refused place is not a service failure and frees the probe", func(t *testing.T) {
		b, c := newBreaker(1)
		l := NewLimiter(1, 0)
		g := &Gate{Breaker: b, Limiter: l}
		call(t, b, Failure)
		c.advance(30 * time.Second) // пора пробовать
		hold, _ := l.Acquire(t.Context())
		err := g.Do(t.Context(), func(context.Context) error { return nil }, nil)
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("error = %v, want ErrBusy", err)
		}
		hold()
		if err := g.Do(t.Context(), func(context.Context) error { return nil }, nil); err != nil {
			t.Fatalf("the probe slot was not freed: %v", err)
		}
		if b.State() != Closed {
			t.Errorf("state = %v, want closed", b.State())
		}
	})
	t.Run("limiter alone", func(t *testing.T) {
		g := &Gate{Limiter: NewLimiter(1, 0)}
		if err := g.Do(t.Context(), func(context.Context) error { return nil }, nil); err != nil {
			t.Fatal(err)
		}
	})
}

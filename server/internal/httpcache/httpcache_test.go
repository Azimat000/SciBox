package httpcache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestEncodeIsStable(t *testing.T) {
	a, err := Encode(map[string]int{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Encode(map[string]int{"a": 1, "b": 2})
	c, _ := Encode(map[string]int{"a": 1, "b": 3})
	if a.ETag != b.ETag || a.ETag == c.ETag {
		t.Fatalf("etags: %s %s %s", a.ETag, b.ETag, c.ETag)
	}
	if string(a.Body) != "{\"a\":1,\"b\":2}\n" || !strings.HasPrefix(a.ETag, `"`) || !strings.HasSuffix(a.ETag, `"`) {
		t.Fatalf("doc: %q %s", a.Body, a.ETag)
	}
	if _, err := Encode(make(chan int)); err == nil {
		t.Fatal("a value that cannot be encoded must be an error")
	}
}

func TestCacheLivesForTheTTLAndReloadsAfter(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	c := newWithClock(30*time.Second, clk.now)
	loads := 0
	load := func() (any, error) { loads++; return map[string]int{"n": loads}, nil }

	first, err := c.Get(load)
	if err != nil || loads != 1 {
		t.Fatalf("first: loads=%d err=%v", loads, err)
	}
	clk.t = clk.t.Add(29 * time.Second)
	again, _ := c.Get(load)
	if loads != 1 || again.ETag != first.ETag {
		t.Fatalf("within the ttl the answer must be reused: loads=%d", loads)
	}
	clk.t = clk.t.Add(time.Second) // ровно срок: уже старое
	fresh, _ := c.Get(load)
	if loads != 2 || fresh.ETag == first.ETag {
		t.Fatalf("after the ttl the answer must be rebuilt: loads=%d", loads)
	}
}

func TestCacheDoesNotKeepFailures(t *testing.T) {
	c := New(time.Minute)
	boom := errors.New("база недоступна")
	if _, err := c.Get(func() (any, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	doc, err := c.Get(func() (any, error) { return 1, nil })
	if err != nil || string(doc.Body) != "1\n" {
		t.Fatalf("after a failure the next request must try again: %v %q", err, doc.Body)
	}
	c2 := New(time.Minute)
	if _, err := c2.Get(func() (any, error) { return make(chan int), nil }); err == nil {
		t.Fatal("an unencodable value must be an error, not a cached empty answer")
	}
}

func TestNonPositiveTTLBecomesOneSecond(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour} {
		if got := New(ttl).ttl; got != time.Second {
			t.Fatalf("ttl %v became %v", ttl, got)
		}
	}
}

func TestOneLoadForManyVisitors(t *testing.T) {
	c := New(time.Minute)
	var loads atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := c.Get(func() (any, error) { loads.Add(1); time.Sleep(20 * time.Millisecond); return "x", nil }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatalf("the database was asked %d times", loads.Load())
	}
}

func TestServe(t *testing.T) {
	doc, _ := Encode(map[string]string{"k": "v"})
	tests := []struct {
		name, header string
		status       int
	}{
		{"без заголовка", "", http.StatusOK},
		{"тот же ETag", doc.ETag, http.StatusNotModified},
		{"слабый ETag", "W/" + doc.ETag, http.StatusNotModified},
		{"список", `"other", ` + doc.ETag, http.StatusNotModified},
		{"звёздочка", "*", http.StatusNotModified},
		{"другой ETag", `"other"`, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
			if tc.header != "" {
				r.Header.Set("If-None-Match", tc.header)
			}
			w := httptest.NewRecorder()
			w.Header().Set("Cache-Control", "no-store") // как делает роутер до обработчика
			Serve(w, r, doc, 90*time.Second)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if w.Header().Get("ETag") != doc.ETag || w.Header().Get("Cache-Control") != "public, max-age=90" {
				t.Fatalf("headers: %v", w.Header())
			}
			if tc.status == http.StatusOK && (w.Body.String() != string(doc.Body) || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json")) {
				t.Fatalf("body %q type %q", w.Body, w.Header().Get("Content-Type"))
			}
			if tc.status == http.StatusNotModified && w.Body.Len() != 0 {
				t.Fatalf("304 must have no body: %q", w.Body)
			}
		})
	}
}

// ---- Keyed (срез 27) ----

func keyedLoad(n *atomic.Int32, v any) func(context.Context) (any, error) {
	return func(context.Context) (any, error) {
		n.Add(1)
		return v, nil
	}
}

func TestKeyedAnswersFromMemoryUntilTheTTLEnds(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	k := newKeyed(15*time.Second, 10, clk.now)
	var loads atomic.Int32
	load := keyedLoad(&loads, map[string]int{"a": 1})

	first, err := k.Get(t.Context(), "q=1", 0, load)
	if err != nil || loads.Load() != 1 {
		t.Fatalf("first: loads=%d err=%v", loads.Load(), err)
	}
	clk.t = clk.t.Add(14 * time.Second)
	again, _ := k.Get(t.Context(), "q=1", 0, load)
	if loads.Load() != 1 || again.ETag != first.ETag {
		t.Fatalf("within the ttl the answer must be reused: loads=%d", loads.Load())
	}
	other, _ := k.Get(t.Context(), "q=2", 0, load)
	if loads.Load() != 2 || other.ETag != first.ETag {
		t.Fatalf("another key is loaded on its own: loads=%d", loads.Load())
	}
	clk.t = clk.t.Add(2 * time.Second)
	if _, err := k.Get(t.Context(), "q=1", 0, load); err != nil || loads.Load() != 3 {
		t.Fatalf("after the ttl the answer is rebuilt: loads=%d err=%v", loads.Load(), err)
	}
	if st := k.Stats(); st.Hits != 1 || st.Misses != 3 || st.Entries != 2 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestKeyedDoesNotRememberFailuresOrHugeAnswers(t *testing.T) {
	k := NewKeyed(time.Minute, 10)
	var loads atomic.Int32
	boom := errors.New("boom")
	if _, err := k.Get(t.Context(), "k", 0, func(context.Context) (any, error) { loads.Add(1); return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("error must reach the caller: %v", err)
	}
	if _, err := k.Get(t.Context(), "k", 0, keyedLoad(&loads, 1)); err != nil || loads.Load() != 2 {
		t.Fatalf("a failure is not remembered: loads=%d err=%v", loads.Load(), err)
	}
	if _, err := k.Get(t.Context(), "bad", 0, func(context.Context) (any, error) { return make(chan int), nil }); err == nil {
		t.Fatal("a value that cannot be encoded must be an error")
	}

	huge := strings.Repeat("x", 5000)
	for range 2 {
		doc, err := k.Get(t.Context(), "huge", 1000, keyedLoad(&loads, huge))
		if err != nil || len(doc.Body) < 5000 {
			t.Fatalf("a huge answer is still returned: %v", err)
		}
	}
	if loads.Load() != 4 {
		t.Fatalf("a huge answer is not remembered, loads=%d", loads.Load())
	}
	if n := k.Stats().Entries; n != 1 {
		t.Fatalf("only the small answer stays: %d entries", n)
	}
}

func TestKeyedOneLoadForManySimultaneousAskers(t *testing.T) {
	k := NewKeyed(time.Minute, 10)
	var loads atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) (any, error) {
		loads.Add(1)
		<-release
		return "answer", nil
	}
	var wg sync.WaitGroup
	etags := make([]string, 40)
	for i := range etags {
		wg.Go(func() {
			doc, err := k.Get(t.Context(), "same", 0, load)
			if err != nil {
				t.Error(err)
			}
			etags[i] = doc.ETag
		})
	}
	// Один собирает, остальные ждут его (или придут уже к готовому ответу: итог тот же).
	for deadline := time.Now().Add(5 * time.Second); loads.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if loads.Load() != 1 {
		t.Fatalf("loads = %d, want one for 40 askers", loads.Load())
	}
	for _, e := range etags {
		if e == "" || e != etags[0] {
			t.Fatalf("every asker gets the same answer: %q", etags)
		}
	}
	if st := k.Stats(); st.Misses != 1 || st.Hits+st.Shared != 39 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestKeyedWaitersShareTheFailureAndTheNextAskerRetries(t *testing.T) {
	k := NewKeyed(time.Minute, 10)
	release := make(chan struct{})
	boom := errors.New("boom")
	var loads atomic.Int32
	load := func(context.Context) (any, error) {
		loads.Add(1)
		<-release
		return nil, boom
	}
	errs := make(chan error, 5)
	for range 5 {
		go func() { _, err := k.Get(t.Context(), "k", 0, load); errs <- err }()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	for range 5 {
		if err := <-errs; !errors.Is(err, boom) {
			t.Fatalf("every waiter sees the failure: %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("loads = %d", loads.Load())
	}
	if _, err := k.Get(t.Context(), "k", 0, keyedLoad(&loads, 1)); err != nil {
		t.Fatalf("the next asker retries: %v", err)
	}
}

func TestKeyedAWaiterThatLeavesDoesNotSpoilTheLoad(t *testing.T) {
	k := NewKeyed(time.Minute, 10)
	release := make(chan struct{})
	started := make(chan struct{})
	var loads atomic.Int32
	load := func(ctx context.Context) (any, error) {
		loads.Add(1)
		close(started)
		<-release
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return "ok", nil
	}
	// Первый запрос начинает сбор и уходит (его контекст отменён), сбор при этом не должен быть оборван.
	first, cancelFirst := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)
	go func() { _, err := k.Get(first, "k", 0, load); firstDone <- err }()
	<-started
	waiterCtx, cancelWaiter := context.WithCancel(t.Context())
	waiterDone := make(chan error, 1)
	go func() { _, err := k.Get(waiterCtx, "k", 0, load); waiterDone <- err }()
	time.Sleep(50 * time.Millisecond)
	cancelWaiter()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("a leaving waiter gets its own cancellation: %v", err)
	}
	cancelFirst()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("the load must survive the cancellation of its asker: %v", err)
	}
	if doc, err := k.Get(t.Context(), "k", 0, load); err != nil || doc.Body == nil || loads.Load() != 1 {
		t.Fatalf("the answer is stored: loads=%d err=%v", loads.Load(), err)
	}
}

func TestKeyedStaysWithinTheLimit(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	k := newKeyed(10*time.Second, 3, clk.now)
	var loads atomic.Int32
	for i := range 3 {
		if _, err := k.Get(t.Context(), fmt.Sprint("k", i), 0, keyedLoad(&loads, i)); err != nil {
			t.Fatal(err)
		}
	}
	// Все три устарели: новая запись вытесняет устаревшие.
	clk.t = clk.t.Add(time.Minute)
	if _, err := k.Get(t.Context(), "fresh", 0, keyedLoad(&loads, 9)); err != nil {
		t.Fatal(err)
	}
	if n := k.Stats().Entries; n != 1 {
		t.Fatalf("expired entries leave first: %d entries", n)
	}
	// Все свежие: уходит любая готовая, предел держится.
	for i := range 6 {
		if _, err := k.Get(t.Context(), fmt.Sprint("n", i), 0, keyedLoad(&loads, i)); err != nil {
			t.Fatal(err)
		}
		if n := k.Stats().Entries; n > 3 {
			t.Fatalf("entries = %d, limit is 3", n)
		}
	}
	if NewKeyed(0, 0).ttl <= 0 || NewKeyed(0, 0).max < 1 {
		t.Fatal("zero settings are replaced with safe ones")
	}
}

func TestKeyedServesAStaleAnswerWhileRefreshing(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	var mu sync.Mutex
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clk.t }
	advance := func(d time.Duration) { mu.Lock(); clk.t = clk.t.Add(d); mu.Unlock() }
	k := newKeyed(30*time.Second, 5, now).WithStale(5 * time.Minute)

	var loads atomic.Int32
	gate := make(chan struct{}, 4)
	load := func(context.Context) (any, error) {
		n := loads.Add(1)
		if n > 1 {
			<-gate // обновление ждёт разрешения: просрочивший запрос не должен его дожидаться
		}
		return map[string]int32{"n": n}, nil
	}
	first, err := k.Get(t.Context(), "landing", 0, load)
	if err != nil || loads.Load() != 1 {
		t.Fatalf("first: %v loads=%d", err, loads.Load())
	}

	advance(31 * time.Second)
	// Просрочено: все получают прежний ответ сразу, обновление одно.
	for range 5 {
		doc, err := k.Get(t.Context(), "landing", 0, load)
		if err != nil || doc.ETag != first.ETag {
			t.Fatalf("a stale answer must be served at once: %v", err)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); loads.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if loads.Load() != 2 {
		t.Fatalf("exactly one background refresh, loads=%d", loads.Load())
	}
	gate <- struct{}{}
	var fresh Doc
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		fresh, _ = k.Get(t.Context(), "landing", 0, load)
		if fresh.ETag != first.ETag {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fresh.ETag == first.ETag {
		t.Fatal("the refreshed answer never appeared")
	}
	if st := k.Stats(); st.Stale < 5 || st.Misses != 1 {
		t.Fatalf("stats: %+v", st)
	}

	// Дальше запаса отдавать старое нельзя: ответ собирается в самом запросе.
	advance(10 * time.Minute)
	gate <- struct{}{}
	doc, err := k.Get(t.Context(), "landing", 0, load)
	if err != nil || doc.ETag == fresh.ETag || loads.Load() != 3 {
		t.Fatalf("beyond the stale window the answer is rebuilt in the request: %v loads=%d", err, loads.Load())
	}
}

func TestKeyedFailedRefreshKeepsTheOldAnswerAndRetries(t *testing.T) {
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	var mu sync.Mutex
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clk.t }
	k := newKeyed(10*time.Second, 5, now).WithStale(time.Minute)
	var loads, fail atomic.Int32
	load := func(context.Context) (any, error) {
		n := loads.Add(1)
		if fail.Load() == 1 {
			return nil, errors.New("база недоступна")
		}
		return n, nil
	}
	first, _ := k.Get(t.Context(), "k", 0, load)
	mu.Lock()
	clk.t = clk.t.Add(11 * time.Second)
	mu.Unlock()
	fail.Store(1)
	for range 3 {
		doc, err := k.Get(t.Context(), "k", 0, load)
		if err != nil || doc.ETag != first.ETag {
			t.Fatalf("the old answer survives a failing refresh: %v", err)
		}
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			k.mu.Lock()
			busy := k.items["k"].refreshing
			k.mu.Unlock()
			if !busy {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if loads.Load() < 2 {
		t.Fatalf("a failed refresh is tried again, loads=%d", loads.Load())
	}
	// Слишком большое обновление не запоминается: запись уходит, старый ответ больше не отдаётся.
	fail.Store(0)
	big := newKeyed(10*time.Second, 5, now).WithStale(time.Minute)
	small := func(context.Context) (any, error) { return "a", nil }
	huge := func(context.Context) (any, error) { return strings.Repeat("x", 3000), nil }
	if _, err := big.Get(t.Context(), "k", 1000, small); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	clk.t = clk.t.Add(11 * time.Second)
	mu.Unlock()
	if _, err := big.Get(t.Context(), "k", 1000, huge); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); big.Stats().Entries > 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if n := big.Stats().Entries; n != 0 {
		t.Fatalf("a huge refreshed answer must not stay: %d entries", n)
	}
}

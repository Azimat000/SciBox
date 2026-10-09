package httpcache

import (
	"errors"
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

package errreport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"scibox/server/internal/errreport"
)

// sink — «GlitchTip» для тестов: запоминает отчёты и заголовки.
type sink struct {
	mu     sync.Mutex
	bodies []map[string]any
	raw    []string
	auth   []string
	paths  []string
	status int
	block  chan struct{}
	srv    *httptest.Server
}

func newSink(t *testing.T) *sink {
	t.Helper()
	s := &sink{status: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.block != nil {
			<-s.block
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		s.mu.Lock()
		s.bodies = append(s.bodies, m)
		s.raw = append(s.raw, string(body))
		s.auth = append(s.auth, r.Header.Get("X-Sentry-Auth"))
		s.paths = append(s.paths, r.URL.Path)
		status := s.status
		s.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sink) dsn() string {
	return strings.Replace(s.srv.URL, "http://", "http://pubkey123@", 1) + "/7"
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *sink) waitFor(t *testing.T, n int) {
	t.Helper()
	for range 400 {
		if s.count() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wanted %d reports, got %d", n, s.count())
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newReporter(t *testing.T, s *sink, mod func(*errreport.Config)) (*errreport.Reporter, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	cfg := errreport.Config{DSN: s.dsn(), Release: "1.2.3", Environment: "production", ServerName: "worker", Now: clock.Now}
	if mod != nil {
		mod(&cfg)
	}
	r, err := errreport.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, clock
}

func TestDSN(t *testing.T) {
	for dsn, ok := range map[string]bool{
		"https://abc@glitchtip.example.ru/3":      true,
		"http://abc@glitchtip:8000/1":             true,
		"https://abc@example.ru:8443/sub/path/12": true,
		"":                                  false,
		"glitchtip.example.ru/3":            false,
		"https://example.ru/3":              false,
		"https://abc:secret@example.ru/3":   false,
		"https://abc@example.ru/":           false,
		"https://abc@example.ru/notanumber": false,
		"ftp://abc@example.ru/3":            false,
		"https://abc@/3":                    false,
		"https://abc@example.ru/3\x7f":      false,
	} {
		r, err := errreport.New(errreport.Config{DSN: dsn})
		if (err == nil) != ok {
			t.Errorf("%q: err = %v, want ok = %v", dsn, err, ok)
		}
		r.Close()
	}
}

func TestEventGoesToTheProjectWithThePublicKeyAndNothingElse(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	var out bytes.Buffer
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(&out, nil), r))

	logger.ErrorContext(context.Background(), "applications storage failed", "path", "/api/applications/42", "err", errors.New("list: boom"),
		"request_id", "abc123", "candidate_name", "Иван Петрович Сидоров", "subject", "Отклик от Ивана", "failures", 3)
	r.Close()

	if s.count() != 1 {
		t.Fatalf("got %d reports", s.count())
	}
	if s.paths[0] != "/api/7/store/" {
		t.Errorf("path = %q", s.paths[0])
	}
	if !strings.Contains(s.auth[0], "sentry_key=pubkey123") || !strings.Contains(s.auth[0], "sentry_version=7") || !strings.Contains(s.auth[0], "scibox/1.2.3") {
		t.Errorf("auth = %q", s.auth[0])
	}
	body := s.bodies[0]
	if body["message"] != "applications storage failed" || body["level"] != "error" || body["platform"] != "go" ||
		body["release"] != "1.2.3" || body["environment"] != "production" || body["server_name"] != "worker" {
		t.Errorf("body = %v", body)
	}
	if tags := body["tags"].(map[string]any); tags["request_id"] != "abc123" {
		t.Errorf("tags = %v", tags)
	}
	extra := body["extra"].(map[string]any)
	if extra["path"] != "/api/applications/42" || extra["err"] != "list: boom" || extra["failures"] != "3" {
		t.Errorf("extra = %v", extra)
	}
	omitted := fmt.Sprint(extra["omitted_keys"])
	if omitted != "[candidate_name subject]" {
		t.Errorf("omitted_keys = %s", omitted)
	}
	// Значения не названных ключей в отчёте нет нигде, ни в каком виде.
	for _, secret := range []string{"Иван", "Сидоров", "Петрович", "Отклик от"} {
		if strings.Contains(s.raw[0], secret) {
			t.Errorf("%q reached the error tracker: %s", secret, s.raw[0])
		}
	}
	// Место записи в журнал стоит в стеке отчёта.
	ex := body["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)
	frames := ex["stacktrace"].(map[string]any)["frames"].([]any)
	last := frames[len(frames)-1].(map[string]any)
	if !strings.Contains(last["function"].(string), "TestEventGoesToTheProjectWithThePublicKeyAndNothingElse") || last["in_app"] != true {
		t.Errorf("frame = %v", last)
	}
	if strings.Contains(last["filename"].(string), "/Users/") || strings.Count(last["filename"].(string), "/") > 2 {
		t.Errorf("filename is not shortened: %v", last["filename"])
	}
	if !strings.Contains(out.String(), "applications storage failed") {
		t.Error("the record did not reach the next handler")
	}
}

func TestOnlyErrorRecordsAreReported(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r)).With("service", "serve").WithGroup("g")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error one")
	r.Close()
	if s.count() != 1 || s.bodies[0]["message"] != "error one" {
		t.Fatalf("reports: %v", s.raw)
	}
	// Без отправителя записи просто идут дальше.
	var out bytes.Buffer
	slog.New(errreport.NewHandler(slog.NewTextHandler(&out, nil), nil)).Error("alone")
	if !strings.Contains(out.String(), "alone") {
		t.Error("record lost without a reporter")
	}
}

func TestDatabaseErrorsAreReducedToTheirCode(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	logger.Error("profile request failed", "err",
		fmt.Errorf("save profile: ERROR: invalid input syntax for type uuid: \"ivan.sidorov's secret\" (SQLSTATE 22P02)"))
	r.Close()
	if strings.Contains(s.raw[0], "secret") || !strings.Contains(s.raw[0], "database error (SQLSTATE 22P02)") || !strings.Contains(s.raw[0], "save profile") {
		t.Fatalf("report: %s", s.raw[0])
	}
}

func TestRouteComesFromTheRequestContext(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	rc := chi.NewRouteContext()
	rc.RoutePatterns = []string{"/api/vacancies/{id}"}
	ctx := context.WithValue(context.Background(), chi.RouteCtxKey, rc)
	logger.ErrorContext(ctx, "vacancies request failed", "err", "boom")
	r.Close()
	if tags := s.bodies[0]["tags"].(map[string]any); tags["route"] != "/api/vacancies/{id}" {
		t.Fatalf("tags = %v", tags)
	}
}

func TestPanicStackIsParsedAndShortened(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	func() {
		defer func() {
			p := recover()
			logger.Error("panic in handler", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
		}()
		var m map[string]int
		m["x"] = 1 //nolint:staticcheck // SA5000: паника записи в пустую карту — именно то, что нужно для стека
	}()
	r.Close()
	ex := s.bodies[0]["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)
	frames := ex["stacktrace"].(map[string]any)["frames"].([]any)
	if len(frames) < 2 {
		t.Fatalf("frames: %v", frames)
	}
	raw := s.raw[0]
	if strings.Contains(raw, "/Users/") || strings.Contains(raw, "runtime/debug.Stack") || strings.Contains(raw, "0x") {
		t.Errorf("stack leaked paths, helper frames or addresses: %s", raw)
	}
	// На вершине функция, которая упала, а не перехват и не среда выполнения.
	top := frames[len(frames)-1].(map[string]any)
	if !strings.HasSuffix(top["function"].(string), "TestPanicStackIsParsedAndShortened.func1") {
		t.Errorf("innermost frame = %v", top)
	}
	var inApp bool
	for _, f := range frames {
		if f.(map[string]any)["in_app"] == true {
			inApp = true
		}
	}
	if !inApp {
		t.Errorf("no frame of our code: %v", frames)
	}
}

func TestRepeatsAreFoldedAndCounted(t *testing.T) {
	s := newSink(t)
	r, clock := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	report := func() { logger.Error("same failure", "err", "x") }

	report()
	for range 5 {
		report()
	}
	logger.Error("another failure")
	clock.Advance(11 * time.Second)
	report()
	r.Close()

	if s.count() != 3 {
		t.Fatalf("got %d reports, want 3 (first, another, the repeat after the window)", s.count())
	}
	if st := r.Stats(); st.Suppressed != 5 || st.Sent != 3 {
		t.Errorf("stats = %+v", st)
	}
	extra := s.bodies[2]["extra"].(map[string]any)
	if extra["repeats_folded"] != float64(5) {
		t.Errorf("the folded repeats are not reported: %v", extra)
	}
}

func TestOverallLimitDropsInsteadOfFlooding(t *testing.T) {
	s := newSink(t)
	r, clock := newReporter(t, s, func(c *errreport.Config) { c.Burst, c.PerMinute = 5, 6 })
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	for i := range 50 {
		logger.Error(fmt.Sprintf("different failure %d", i))
	}
	clock.Advance(10 * time.Second) // шесть в минуту = один за десять секунд
	logger.Error("after a pause")
	r.Close()
	if s.count() != 6 {
		t.Errorf("got %d reports, want 5 from the burst and 1 after the refill", s.count())
	}
	if st := r.Stats(); st.Dropped != 45 {
		t.Errorf("dropped = %d, want 45", st.Dropped)
	}
}

func TestSlowTrackerNeverBlocksTheProgram(t *testing.T) {
	s := newSink(t)
	s.block = make(chan struct{})
	r, _ := newReporter(t, s, func(c *errreport.Config) { c.Queue = 3; c.Burst = 100 })
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			logger.Error(fmt.Sprintf("failure %d", i))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging blocked behind a hung error tracker")
	}
	if st := r.Stats(); st.Dropped < 40 {
		t.Errorf("dropped = %d, want most of 50", st.Dropped)
	}
	close(s.block)
}

func TestDeliveryFailuresAreCountedAndLoggedRarely(t *testing.T) {
	s := newSink(t)
	s.status = http.StatusInternalServerError
	r, clock := newReporter(t, s, func(c *errreport.Config) { c.Burst = 100 })
	var out bytes.Buffer
	r.SetLogger(slog.New(slog.NewTextHandler(&out, nil)))
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	for i := range 4 {
		logger.Error(fmt.Sprintf("failure %d", i))
	}
	s.waitFor(t, 4)
	clock.Advance(0)
	r.Close()
	st := r.Stats()
	if st.Failed != 4 || st.Sent != 0 {
		t.Errorf("stats = %+v", st)
	}
	if n := strings.Count(out.String(), "error reports are not delivered"); n != 1 {
		t.Errorf("failure logged %d times:\n%s", n, out.String())
	}
	if strings.Contains(out.String(), "pubkey123") {
		t.Errorf("the key reached the log: %s", out.String())
	}
}

func TestUnreachableTrackerIsCountedWithoutTheKeyOrPath(t *testing.T) {
	s := newSink(t)
	dsn := s.dsn()
	s.srv.Close()
	clock := &fakeClock{t: time.Now()}
	r, err := errreport.New(errreport.Config{DSN: dsn, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r.SetLogger(slog.New(slog.NewTextHandler(&out, nil)))
	slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r)).Error("failure")
	r.Close()
	if r.Stats().Failed != 1 || strings.Contains(out.String(), "pubkey123") || strings.Contains(out.String(), "/api/7/store") {
		t.Errorf("stats = %+v log = %s", r.Stats(), out.String())
	}
}

func TestSendTest(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	if err := r.SendTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.count() != 1 || !strings.Contains(s.raw[0], "test event") {
		t.Fatalf("reports: %v", s.raw)
	}
	s.mu.Lock()
	s.status = http.StatusForbidden
	s.mu.Unlock()
	if err := r.SendTest(context.Background()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	var none *errreport.Reporter
	if err := none.SendTest(context.Background()); err == nil {
		t.Fatal("a missing reporter must say so")
	}
}

func TestNilAndClosedReportersAreSafe(t *testing.T) {
	var none *errreport.Reporter
	none.Capture(errreport.Event{Message: "x"})
	none.SetLogger(slog.Default())
	none.Close()
	if (none.Stats() != errreport.Stats{}) {
		t.Error("stats of nil must be zero")
	}

	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	r.Close()
	r.Close()
	r.Capture(errreport.Event{Message: "after close"}) // не паникует на закрытой очереди
	if s.count() != 0 {
		t.Error("an event was sent after Close")
	}
}

func TestConcurrentCaptureAndClose(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, func(c *errreport.Config) { c.Burst = 1000; c.PerMinute = 1000 })
	var wg sync.WaitGroup
	var n atomic.Int64
	for g := range 8 {
		wg.Go(func() {
			for i := range 100 {
				r.Capture(errreport.Event{Message: fmt.Sprintf("g%d-%d", g, i), Func: "f"})
				n.Add(1)
			}
		})
	}
	time.Sleep(5 * time.Millisecond)
	r.Close() // посреди записи: ни паники, ни гонки
	wg.Wait()
	if n.Load() != 800 {
		t.Fatal("not all captures ran")
	}
}

func TestClipsHugeFields(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	r.Capture(errreport.Event{Message: strings.Repeat("я", 5000), Err: strings.Repeat("e", 5000),
		Extra: map[string]string{"path": strings.Repeat("p", 5000)}, Func: "f", File: `C:\Users\someone\project\internal\x\y.go`, Line: 3})
	r.Close()
	if len(s.raw[0]) > 8000 {
		t.Errorf("report is %d bytes", len(s.raw[0]))
	}
	if strings.Contains(s.raw[0], "someone") {
		t.Errorf("a Windows path was not shortened: %s", s.raw[0])
	}
}

func TestMemoryOfRepeatsIsBounded(t *testing.T) {
	s := newSink(t)
	r, clock := newReporter(t, s, func(c *errreport.Config) { c.Kinds = 3; c.Burst = 100 })
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	// Три разные ошибки подряд заполняют память; четвёртая очищает её (все ещё «свежие»), поэтому повтор первой
	// после этого снова уходит: память не растёт, а потерянное повторение стоит одного лишнего отчёта.
	for i := range 4 {
		logger.Error(fmt.Sprintf("kind %d", i))
	}
	logger.Error("kind 0")
	// Старые записи забываются без полной очистки.
	clock.Advance(time.Minute)
	logger.Error("kind 4")
	logger.Error("kind 5")
	logger.Error("kind 6")
	logger.Error("kind 7")
	r.Close()
	if s.count() != 9 {
		t.Fatalf("got %d reports, want 9", s.count())
	}
}

func TestStackParsing(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	var stack strings.Builder
	stack.WriteString("goroutine 7 [running]:\nruntime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n")
	stack.WriteString("scibox/server/internal/httpapi.recoverer.func1.1()\n\t/Users/secret-person/Documents/server/internal/httpapi/router.go:226 +0x7a\n")
	stack.WriteString("panic({0x1, 0x2})\n\t/usr/local/go/src/runtime/panic.go:860 +0x13a\n")
	stack.WriteString("runtime.panicmem(...)\n\t/usr/local/go/src/runtime/panic.go:262\n")
	stack.WriteString("runtime.sigpanic()\n\t/usr/local/go/src/runtime/signal_unix.go:925 +0x3d\n")
	stack.WriteString("scibox/server/internal/vacancies.(*Service).Get(0xc0001, {0x5, 0x6})\n\t/Users/secret-person/Documents/server/internal/vacancies/service.go:88 +0x1a\n")
	for i := range 80 {
		fmt.Fprintf(&stack, "net/http.handler%d(0x1)\n\t/usr/local/go/src/net/http/server.go:%d +0x1\n", i, 100+i)
	}
	stack.WriteString("created by net/http.(*Server).Serve in goroutine 1\n\t/usr/local/go/src/net/http/server.go:3500 +0x4e\n")
	r.Capture(errreport.Event{Message: "boom", Stack: stack.String()})
	r.Close()

	frames := s.bodies[0]["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	if len(frames) == 0 || len(frames) > 50 {
		t.Fatalf("%d frames, want 1…50", len(frames))
	}
	if strings.Contains(s.raw[0], "secret-person") || strings.Contains(s.raw[0], "0xc0001") {
		t.Errorf("path or arguments leaked: %s", s.raw[0])
	}
}

func TestStackWithTheFaultOnTop(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	stack := "goroutine 7 [running]:\n" +
		"runtime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n" +
		"panic({0x1, 0x2})\n\t/usr/local/go/src/runtime/panic.go:860 +0x13a\n" +
		"runtime.sigpanic()\n\t/usr/local/go/src/runtime/signal_unix.go:925 +0x3d\n" +
		"scibox/server/internal/vacancies.(*Service).Get(0xc0001, {0x5, 0x6})\n\t/src/server/internal/vacancies/service.go:88 +0x1a\n" +
		"net/http.HandlerFunc.ServeHTTP(0x1)\n\t/usr/local/go/src/net/http/server.go:2300 +0x1\n"
	r.Capture(errreport.Event{Message: "boom", Stack: stack})
	r.Close()
	frames := s.bodies[0]["exception"].(map[string]any)["values"].([]any)[0].(map[string]any)["stacktrace"].(map[string]any)["frames"].([]any)
	if len(frames) != 2 {
		t.Fatalf("frames = %v", frames)
	}
	top := frames[1].(map[string]any)
	if top["function"] != "scibox/server/internal/vacancies.(*Service).Get" || top["lineno"] != float64(88) || top["in_app"] != true {
		t.Errorf("top = %v", top)
	}
	if frames[0].(map[string]any)["in_app"] != false {
		t.Errorf("a standard library frame is marked as ours: %v", frames[0])
	}
}

func TestOddAttributesAreHandled(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	logger := slog.New(errreport.NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	logger.Error("odd attributes", "route", "/api/things/{id}", "error", "ERROR: bad value \"x\" (SQLSTATE 22P02)", "bad\nkey", "v")
	r.Close()
	body := s.bodies[0]
	if tags := body["tags"].(map[string]any); tags["route"] != "/api/things/{id}" {
		t.Errorf("tags = %v", tags)
	}
	extra := body["extra"].(map[string]any)
	if extra["error"] != "database error (SQLSTATE 22P02)" || fmt.Sprint(extra["omitted_keys"]) != "[bad_key]" {
		t.Errorf("extra = %v", extra)
	}
}

func TestSendTestWithABrokenContext(t *testing.T) {
	s := newSink(t)
	r, _ := newReporter(t, s, nil)
	//nolint:staticcheck // SA1012: пустой контекст — единственный способ проверить отказ сборки запроса
	if err := r.SendTest(nil); err == nil {
		t.Fatal("want an error for a nil context")
	}
}

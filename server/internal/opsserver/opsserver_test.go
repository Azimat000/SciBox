package opsserver_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"scibox/server/internal/opsserver"
)

const secretToken = "0123456789abcdef0123456789abcdef"

var metricsBody = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("scibox_up 1\n")) })

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newServer(t *testing.T, cfg opsserver.Config) (*opsserver.Server, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	cfg.Logger = slog.New(slog.NewTextHandler(logs, nil))
	s, err := opsserver.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, logs
}

func get(h http.Handler, method, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLivenessNeedsNoSecretAndShowsNothing(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Token: secretToken, Metrics: metricsBody})
	rec := get(s.Handler(), http.MethodGet, "/livez", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestSecretGuardsMetricsAndProfiles(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Token: secretToken, Metrics: metricsBody, Pprof: true})
	cases := []struct {
		name, path, auth string
		want             int
	}{
		{"metrics without secret", "/metrics", "", http.StatusUnauthorized},
		{"metrics wrong secret", "/metrics", "Bearer wrong", http.StatusUnauthorized},
		{"metrics other scheme", "/metrics", "Basic " + secretToken, http.StatusUnauthorized},
		{"metrics prefix of the secret", "/metrics", "Bearer " + secretToken[:len(secretToken)-1], http.StatusUnauthorized},
		{"metrics secret with a tail", "/metrics", "Bearer " + secretToken + "x", http.StatusUnauthorized},
		{"metrics right secret", "/metrics", "Bearer " + secretToken, http.StatusOK},
		{"pprof index without secret", "/debug/pprof/", "", http.StatusUnauthorized},
		{"pprof heap without secret", "/debug/pprof/heap", "", http.StatusUnauthorized},
		{"pprof cmdline without secret", "/debug/pprof/cmdline", "", http.StatusUnauthorized},
		{"pprof index right secret", "/debug/pprof/", "Bearer " + secretToken, http.StatusOK},
		{"pprof heap right secret", "/debug/pprof/heap", "Bearer " + secretToken, http.StatusOK},
		{"pprof goroutine right secret", "/debug/pprof/goroutine?debug=1", "Bearer " + secretToken, http.StatusOK},
		{"pprof cmdline right secret", "/debug/pprof/cmdline", "Bearer " + secretToken, http.StatusOK},
		{"pprof symbol right secret", "/debug/pprof/symbol", "Bearer " + secretToken, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := get(s.Handler(), http.MethodGet, c.path, c.auth)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
			if c.want == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" || strings.Contains(rec.Body.String(), "scibox_up") || strings.Contains(rec.Body.String(), "goroutine") {
					t.Fatalf("a refusal leaked data or lacks the challenge: %v %q", rec.Header(), rec.Body.String())
				}
			}
		})
	}
}

func TestWithoutASecretTheLoopbackPortIsOpen(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Metrics: metricsBody, Pprof: true})
	for _, path := range []string{"/metrics", "/debug/pprof/", "/debug/pprof/cmdline"} {
		if rec := get(s.Handler(), http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("%s: got %d", path, rec.Code)
		}
	}
}

func TestProfilesAreAbsentUnlessSwitchedOn(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Token: secretToken, Metrics: metricsBody})
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof/profile", "/debug/pprof/trace", "/debug/pprof/cmdline"} {
		if rec := get(s.Handler(), http.MethodGet, path, "Bearer "+secretToken); rec.Code != http.StatusNotFound {
			t.Errorf("%s with pprof off: got %d, want 404", path, rec.Code)
		}
	}
}

func TestOnlyDeclaredAddressesAndMethodsExist(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Metrics: metricsBody, Pprof: true})
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodPost, "/readyz", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/debug/pprof/", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", http.StatusNotFound},
		{http.MethodGet, "/api/health", http.StatusNotFound},
		{http.MethodGet, "/debug/vars", http.StatusNotFound},
		{http.MethodGet, "/debug/pprof", http.StatusTemporaryRedirect}, // без слэша ведёт к индексу
	} {
		if rec := get(s.Handler(), c.method, c.path, ""); rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	// Без Metrics адреса /metrics нет вовсе.
	bare, _ := newServer(t, opsserver.Config{})
	if rec := get(bare.Handler(), http.MethodGet, "/metrics", ""); rec.Code != http.StatusNotFound {
		t.Errorf("/metrics without a handler: got %d", rec.Code)
	}
}

func TestReadinessReportsChecksWithoutErrorText(t *testing.T) {
	var dbErr error
	var migErr error
	s, logs := newServer(t, opsserver.Config{Checks: []opsserver.Check{
		{Name: "database", Run: func(context.Context) error { return dbErr }},
		{Name: "migrations", Run: func(context.Context) error { return migErr }},
	}})

	rec := get(s.Handler(), http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "database: ok\nmigrations: ok\n" {
		t.Fatalf("healthy: %d %q", rec.Code, rec.Body.String())
	}

	dbErr = errors.New("connect postgres://scibox:hunter2@db.internal:5432: connection refused")
	for range 3 {
		rec = get(s.Handler(), http.MethodGet, "/readyz", "")
		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "database: fail\nmigrations: ok\n" {
			t.Fatalf("database down: %d %q", rec.Code, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "refused") {
		t.Fatalf("error text leaked into the response: %q", rec.Body.String())
	}
	// Три провала подряд дают одну строку в журнале, а не три: проверку опрашивают каждые несколько секунд.
	if n := strings.Count(logs.String(), "readiness check failed"); n != 1 {
		t.Fatalf("failure logged %d times:\n%s", n, logs.String())
	}

	dbErr = nil
	migErr = errors.New("2 migrations pending")
	rec = get(s.Handler(), http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "migrations: fail") {
		t.Fatalf("migrations pending: %d %q", rec.Code, rec.Body.String())
	}

	migErr = nil
	rec = get(s.Handler(), http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusOK || !strings.Contains(logs.String(), "readiness restored") {
		t.Fatalf("recovery: %d\n%s", rec.Code, logs.String())
	}
}

func TestDrainingIsNotReadyButStillAlive(t *testing.T) {
	s, logs := newServer(t, opsserver.Config{Checks: []opsserver.Check{{Name: "database", Run: func(context.Context) error { return nil }}}})
	if rec := get(s.Handler(), http.MethodGet, "/readyz", ""); rec.Code != http.StatusOK {
		t.Fatalf("before: %d", rec.Code)
	}
	s.SetDraining()
	rec := get(s.Handler(), http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "draining: yes") {
		t.Fatalf("draining: %d %q", rec.Code, rec.Body.String())
	}
	if rec = get(s.Handler(), http.MethodGet, "/livez", ""); rec.Code != http.StatusOK {
		t.Fatalf("a draining process is still alive: %d", rec.Code)
	}
	if !strings.Contains(logs.String(), "shutting down") {
		t.Fatalf("no log line about the shutdown:\n%s", logs.String())
	}
}

func TestSlowCheckFailsAtTheDeadline(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{CheckTimeout: 50 * time.Millisecond, Checks: []opsserver.Check{
		{Name: "database", Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
	}})
	start := time.Now()
	rec := get(s.Handler(), http.MethodGet, "/readyz", "")
	if rec.Code != http.StatusServiceUnavailable || time.Since(start) > 2*time.Second {
		t.Fatalf("got %d after %v", rec.Code, time.Since(start))
	}
}

func TestNewRejectsBrokenSetups(t *testing.T) {
	ok := func(context.Context) error { return nil }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, cfg := range map[string]opsserver.Config{
		"no logger":         {},
		"unnamed check":     {Logger: logger, Checks: []opsserver.Check{{Run: ok}}},
		"check without run": {Logger: logger, Checks: []opsserver.Check{{Name: "database"}}},
		"name with space":   {Logger: logger, Checks: []opsserver.Check{{Name: "data base", Run: ok}}},
		"name with colon":   {Logger: logger, Checks: []opsserver.Check{{Name: "db:1", Run: ok}}},
		"duplicate":         {Logger: logger, Checks: []opsserver.Check{{Name: "database", Run: ok}, {Name: "database", Run: ok}}},
	} {
		if _, err := opsserver.New(cfg); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Shutdown, обогнавший горутину с Serve, не даёт порту запуститься: иначе остановка процесса ждала бы его вечно.
func TestShutdownBeforeServeKeepsThePortClosed(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{})
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown before serve: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve after Shutdown returned %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = ln.Close()
		t.Fatal("Serve after Shutdown kept accepting connections")
	}
	if c, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String()); err == nil {
		_ = c.Close()
		t.Fatal("the listener is still open after Serve returned")
	}
}

func TestServeAndShutdownOnARealSocket(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{Token: secretToken, Metrics: metricsBody})
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String()+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+secretToken)
	var resp *http.Response
	for range 50 { // Serve стартует в горутине: подождать, пока она принимает
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "scibox_up 1\n" {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve after Shutdown returned %v", err)
	}
}

func TestServeReportsAListenerThatBreaks(t *testing.T) {
	s, _ := newServer(t, opsserver.Config{})
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close() // Accept на закрытом слушателе вернёт ошибку, не ErrServerClosed
	if err := s.Serve(ln); err == nil {
		t.Fatal("want the accept error")
	}
}

package reqid_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"scibox/server/internal/reqid"
)

var idRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestNewIDsAreRandomHex(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := reqid.New()
		if !idRe.MatchString(id) {
			t.Fatalf("id %q is not 32 hex characters", id)
		}
		if seen[id] {
			t.Fatalf("id %q repeated", id)
		}
		seen[id] = true
	}
}

func TestMiddlewareGivesEveryRequestItsOwnNumberAndIgnoresTheClients(t *testing.T) {
	var inHandler []string
	h := reqid.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		inHandler = append(inHandler, reqid.From(r.Context()))
	}))

	// Заголовок от посетителя не принимается: в нём может быть подделка строки журнала.
	forged := "abc\nlevel=ERROR msg=fake"
	var headers []string
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(reqid.Header, forged)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		headers = append(headers, rec.Header().Get(reqid.Header))
	}
	for i, id := range headers {
		if !idRe.MatchString(id) {
			t.Fatalf("response header %q is not a generated number", id)
		}
		if inHandler[i] != id {
			t.Fatalf("handler saw %q, response carries %q", inHandler[i], id)
		}
	}
	if headers[0] == headers[1] {
		t.Fatal("two requests share a number")
	}
}

func TestFromIsEmptyWithoutARequest(t *testing.T) {
	if got := reqid.From(context.Background()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestHandlerAddsTheNumberOnlyWhenTheContextHasOne(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(reqid.NewHandler(slog.NewTextHandler(&out, nil))).With("service", "serve").WithGroup("g")
	ctx := reqid.With(context.Background(), "0123456789abcdef0123456789abcdef")

	logger.InfoContext(ctx, "with request", "k", "v")
	logger.Info("without request")
	logger.InfoContext(ctx, "has its own", reqid.Key, "mine")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], "g.request_id=0123456789abcdef0123456789abcdef") || !strings.Contains(lines[0], "service=serve") {
		t.Errorf("request line: %s", lines[0])
	}
	if strings.Contains(lines[1], "request_id") {
		t.Errorf("a line outside a request got a number: %s", lines[1])
	}
	if strings.Count(lines[2], "request_id") != 1 || !strings.Contains(lines[2], "request_id=mine") {
		t.Errorf("a line with its own number was changed: %s", lines[2])
	}
}

func TestHandlerHonoursTheLevelOfTheNextHandler(t *testing.T) {
	var out bytes.Buffer
	h := reqid.NewHandler(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) || !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Enabled must follow the wrapped handler")
	}
}

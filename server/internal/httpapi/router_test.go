package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scibox/server/internal/health"
)

type fakeHealth struct {
	db  health.Database
	err error
}

func (f fakeHealth) CheckDatabase(ctx context.Context) (health.Database, error) {
	if _, ok := ctx.Deadline(); !ok {
		return health.Database{}, errors.New("health check must have a deadline")
	}
	return f.db, f.err
}

func newTestRouter(h HealthChecker, logs io.Writer) http.Handler {
	return NewRouter(Deps{
		ProductName: "SciBox",
		Version:     "test",
		Health:      h,
		Logger:      slog.New(slog.NewTextHandler(logs, nil)),
	})
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("%s %s: content-type %q", method, path, ct)
	}
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) ErrorDetail {
	t.Helper()
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	if body.Error.Message == "" {
		t.Fatal("error message is empty")
	}
	return body.Error
}

func TestHealthOK(t *testing.T) {
	var logs bytes.Buffer
	r := newTestRouter(fakeHealth{db: health.Database{SchemaVersion: 1, ServerVersion: "16.15"}}, &logs)
	rec := do(t, r, http.MethodGet, "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := HealthResponse{Status: "ok", Product: "SciBox", Version: "test",
		Database: health.Database{SchemaVersion: 1, ServerVersion: "16.15"}}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !strings.Contains(logs.String(), "path=/api/health") || !strings.Contains(logs.String(), "status=200") {
		t.Fatalf("request not logged: %s", logs.String())
	}
}

func TestErrorResponses(t *testing.T) {
	cases := []struct {
		name, method, path string
		health             HealthChecker
		status             int
		code               string
	}{
		{"database down", http.MethodGet, "/api/health", fakeHealth{err: errors.New("no db")}, http.StatusServiceUnavailable, CodeDatabaseUnavailable},
		{"unknown path", http.MethodGet, "/api/nope", fakeHealth{}, http.StatusNotFound, CodeNotFound},
		{"wrong method", http.MethodPost, "/api/health", fakeHealth{}, http.StatusMethodNotAllowed, CodeMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, newTestRouter(tc.health, io.Discard), tc.method, tc.path)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := decodeError(t, rec); got.Code != tc.code {
				t.Fatalf("code = %q, want %q", got.Code, tc.code)
			}
		})
	}
}

func TestRecovererReturnsJSON500(t *testing.T) {
	var logs bytes.Buffer
	h := recoverer(slog.New(slog.NewTextHandler(&logs, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))
	rec := do(t, h, http.MethodGet, "/x")
	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Code != CodeInternal {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "kaboom") {
		t.Fatal("panic not logged")
	}
}

func TestRecovererRepanicsAbort(t *testing.T) {
	h := recoverer(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want ErrAbortHandler", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

type failingWriter struct{ *httptest.ResponseRecorder }

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriteJSONEncodeFailureIsLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	WriteJSON(failingWriter{httptest.NewRecorder()}, http.StatusOK, map[string]string{"a": "b"})
	if !strings.Contains(logs.String(), "broken pipe") {
		t.Fatalf("encode error not logged: %q", logs.String())
	}
}

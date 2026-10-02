package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"scibox/server/internal/auth"
	"scibox/server/internal/health"
	"scibox/server/internal/mail"
	"scibox/server/internal/testdb"
)

func routerWithAuth(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.New(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	cfg.Hash = auth.TestHashParams
	svc := auth.NewService(pool, &mail.Memory{}, cfg, logger)
	return NewRouter(Deps{
		ProductName: "SciBox", Version: "test", Logger: logger,
		Health: fakeHealth{db: health.Database{SchemaVersion: 2, ServerVersion: "16"}},
		Auth:   auth.NewHandler(svc, logger),
	})
}

func TestAuthRoutesAreMountedUnderAPI(t *testing.T) {
	r := routerWithAuth(t)
	rec := do(t, r, http.MethodGet, "/api/auth/me")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":null`) {
		t.Fatalf("GET /api/auth/me: %d %s", rec.Code, rec.Body)
	}
	// Для запросов, меняющих данные, работает проверка источника.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", out.Code)
	}
	// Закрытый раздел без входа.
	if rec := do(t, r, http.MethodPatch, "/api/account"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH /api/account: %d", rec.Code)
	}
}

func TestAPIResponsesAreNeverCached(t *testing.T) {
	r := routerWithAuth(t)
	for _, path := range []string{"/api/health", "/api/auth/me", "/api/nope"} {
		rec := do(t, r, http.MethodGet, path)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: Cache-Control = %q", path, got)
		}
	}
}

func TestWithoutAuthTheAccountRoutesDoNotExist(t *testing.T) {
	rec := do(t, newTestRouter(fakeHealth{}, io.Discard), http.MethodGet, "/api/auth/me")
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Code != CodeNotFound {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

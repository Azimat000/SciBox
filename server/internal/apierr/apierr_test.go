package apierr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestWriteInternalDatabaseTimeout(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	r := httptest.NewRequest(http.MethodGet, "/api/things", nil)
	w := httptest.NewRecorder()
	cause := fmt.Errorf("things: %w", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout", Detail: "secret detail"})

	WriteInternal(w, r, logger, "things", cause)

	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("status = %d, Retry-After = %q", w.Code, w.Header().Get("Retry-After"))
	}
	if body := w.Body.String(); !strings.Contains(body, `"code":"database_busy"`) || strings.Contains(body, "secret") {
		t.Fatalf("body = %q", body)
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "path=/api/things") {
		t.Fatalf("log = %q", out)
	}
	// Отмена запроса самим человеком по-прежнему не ошибка, даже если база успела ответить «остановлено».
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	WriteInternal(w, r.WithContext(gone), logger, "things", cause)
	if w.Code != StatusClientClosed {
		t.Fatalf("a client that left got %d", w.Code)
	}
	// «Остановлено по просьбе клиента» (не срок) остаётся обычной ошибкой.
	w = httptest.NewRecorder()
	WriteInternal(w, r, logger, "things", &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("user-request cancel got %d", w.Code)
	}
}

func TestWriteInternal(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name       string
		ctx        context.Context //nolint:containedctx // строка таблицы тестов: контекст и есть проверяемый вход
		wantStatus int
		wantLog    string
		wantLevel  string
		wantBody   bool
	}{
		{"real failure", context.Background(), http.StatusInternalServerError, "things request failed", "ERROR", true},
		{"client went away", cancelled, StatusClientClosed, "things cancelled by client", "INFO", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			r := httptest.NewRequest(http.MethodGet, "/api/things", nil).WithContext(tc.ctx)
			w := httptest.NewRecorder()

			WriteInternal(w, r, logger, "things", errors.New("secret detail"))

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			out := logs.String()
			if !strings.Contains(out, tc.wantLog) || !strings.Contains(out, "level="+tc.wantLevel) || !strings.Contains(out, "path=/api/things") {
				t.Fatalf("log = %q", out)
			}
			body := w.Body.String()
			if strings.Contains(body, "secret detail") {
				t.Fatalf("body leaks the error: %q", body)
			}
			if tc.wantBody != strings.Contains(body, `"code":"internal"`) {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

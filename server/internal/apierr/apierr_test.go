package apierr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

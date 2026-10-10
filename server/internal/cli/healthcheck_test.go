package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func healthcheck(t *testing.T, args []string, env map[string]string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runHealthcheck(context.Background(), args, Env{
		Getenv: func(k string) string { return env[k] },
		Stdout: &out,
		Stderr: &errb,
	})
	return code, errb.String()
}

func TestHealthcheckURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		addr string
		want string
	}{
		{"server default", nil, "", "http://127.0.0.1:9100/readyz"},
		{"server named", []string{"serve"}, "", "http://127.0.0.1:9100/readyz"},
		{"worker default", []string{"worker"}, "", "http://127.0.0.1:9101/readyz"},
		{"all networks, v4", nil, "0.0.0.0:9100", "http://127.0.0.1:9100/readyz"},
		{"all networks, v6", nil, "[::]:9300", "http://127.0.0.1:9300/readyz"},
		{"only the port", nil, ":9100", "http://127.0.0.1:9100/readyz"},
		{"a named interface", nil, " 10.1.2.3:9400 ", "http://10.1.2.3:9400/readyz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := healthcheckURL(tc.args, func(string) string { return tc.addr })
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args []string
		addr string
	}{
		{"unknown process", []string{"database"}, ""},
		{"port switched off", nil, "off"},
		{"port switched off in capitals", nil, "OFF"},
		{"no port", nil, "localhost"},
		{"empty port", nil, "localhost:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := healthcheckURL(tc.args, func(string) string { return tc.addr }); err == nil {
				t.Fatalf("got %q, want an error", got)
			}
		})
	}
}

func TestHealthcheckAnswers(t *testing.T) {
	status := http.StatusOK
	var path string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(status)
	}))
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")

	if code, msg := healthcheck(t, nil, map[string]string{"SCIBOX_OPS_ADDR": addr}); code != 0 || msg != "" {
		t.Fatalf("ready process: code %d, %q", code, msg)
	}
	if path != "/readyz" {
		t.Fatalf("asked %q, want /readyz: /livez would not see a broken database", path)
	}
	status = http.StatusServiceUnavailable
	if code, msg := healthcheck(t, nil, map[string]string{"SCIBOX_OPS_ADDR": addr}); code != 1 || !strings.Contains(msg, "503") {
		t.Fatalf("draining process: code %d, %q", code, msg)
	}
}

func TestHealthcheckFailures(t *testing.T) {
	// Ничего не слушает: порт выбран и сразу освобождён.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := ln.Addr().String()
	_ = ln.Close()
	if code, msg := healthcheck(t, nil, map[string]string{"SCIBOX_OPS_ADDR": dead}); code != 1 || !strings.Contains(msg, "healthcheck:") {
		t.Fatalf("nothing listens: code %d, %q", code, msg)
	}
	if code, msg := healthcheck(t, []string{"database"}, nil); code != 2 || !strings.Contains(msg, "Использование") {
		t.Fatalf("unknown kind: code %d, %q", code, msg)
	}
	if code, msg := healthcheck(t, nil, map[string]string{"SCIBOX_OPS_ADDR": "off"}); code != 1 || !strings.Contains(msg, "выключен") {
		t.Fatalf("port off: code %d, %q", code, msg)
	}
}

func TestRunDispatchesHealthcheckWithoutLoadingTheConfig(t *testing.T) {
	// Настройки заведомо негодные: проверка контейнера от них не зависит.
	r := run(context.Background(), []string{"healthcheck"}, map[string]string{"SCIBOX_ENV": "nonsense", "SCIBOX_OPS_ADDR": "off"}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "внутренний порт выключен") {
		t.Fatalf("code %d, stderr %q", r.code, r.stderr)
	}
}

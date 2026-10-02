package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/testdb"
)

const productConfig = "../../../config/product.json"

type result struct {
	code           int
	stdout, stderr string
}

func run(ctx context.Context, args []string, env map[string]string, onListen func(net.Addr)) result {
	var out, errb bytes.Buffer
	if _, ok := env["SCIBOX_PRODUCT_CONFIG"]; !ok {
		env["SCIBOX_PRODUCT_CONFIG"] = productConfig
	}
	code := Run(ctx, args, Env{
		Getenv:   func(k string) string { return env[k] },
		Stdout:   &out,
		Stderr:   &errb,
		OnListen: onListen,
	})
	return result{code, out.String(), errb.String()}
}

func TestCommandsWithoutDatabase(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		env      map[string]string
		code     int
		inStdout string
		inStderr string
	}{
		{"version", []string{"version"}, map[string]string{}, 0, "dev", ""},
		{"help", []string{"help"}, map[string]string{}, 0, "Использование", ""},
		{"unknown", []string{"fly"}, map[string]string{}, 2, "", "неизвестная команда"},
		{"bad product config", []string{"seed"}, map[string]string{"SCIBOX_PRODUCT_CONFIG": "/nope.json"}, 1, "", "load config"},
		{"seed placeholder", []string{"seed"}, map[string]string{}, 0, "Демо-данных пока нет", ""},
		{"migrate without subcommand", []string{"migrate"}, map[string]string{}, 2, "", "Использование"},
		{"migrate bad url", []string{"migrate", "up"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "parse database url"},
		{"serve bad url", []string{"serve"}, map[string]string{"DATABASE_URL": "::bad"}, 1, "", "connect database"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := run(context.Background(), tc.args, tc.env, nil)
			if r.code != tc.code || !strings.Contains(r.stdout, tc.inStdout) || !strings.Contains(r.stderr, tc.inStderr) {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestMigrateUp(t *testing.T) {
	dbURL := testdb.Create(t, false)
	r := run(context.Background(), []string{"migrate", "up"}, map[string]string{"DATABASE_URL": dbURL}, nil)
	if r.code != 0 || !strings.Contains(r.stdout, "00001_extensions.sql") {
		t.Fatalf("got %+v", r)
	}
}

func TestServeListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	r := run(context.Background(), []string{"serve"}, map[string]string{
		"DATABASE_URL":     testdb.Create(t, true),
		"SCIBOX_HTTP_ADDR": ln.Addr().String(), // порт уже занят
	}, nil)
	if r.code != 1 || !strings.Contains(r.stderr, "listen") {
		t.Fatalf("got %+v", r)
	}
}

// Сервер по умолчанию (без аргументов) поднимается, отвечает на /api/health и
// аккуратно останавливается по отмене контекста.
func TestServeHealthAndShutdown(t *testing.T) {
	dbURL := testdb.Create(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type healthBody struct {
		Status   string `json:"status"`
		Product  string `json:"product"`
		Database struct {
			SchemaVersion int64 `json:"schema_version"`
		} `json:"database"`
	}
	var got healthBody
	var status int
	var reqErr error

	r := run(ctx, nil, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_HTTP_ADDR": "127.0.0.1:0"}, func(addr net.Addr) {
		defer cancel()
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s/api/health", addr))
		if err != nil {
			reqErr = err
			return
		}
		defer resp.Body.Close()
		status = resp.StatusCode
		reqErr = json.NewDecoder(resp.Body).Decode(&got)
	})
	if reqErr != nil {
		t.Fatal(reqErr)
	}
	if r.code != 0 || !strings.Contains(r.stderr, "server stopped") {
		t.Fatalf("got %+v", r)
	}
	if status != http.StatusOK || got.Status != "ok" || got.Product != "SciBox" || got.Database.SchemaVersion < 1 {
		t.Fatalf("health = %d %+v", status, got)
	}
}

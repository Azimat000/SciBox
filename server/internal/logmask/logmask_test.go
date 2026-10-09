package logmask_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"scibox/server/internal/logmask"
)

func TestMask(t *testing.T) {
	const token = "Zk3nQ9vW1xYbT7uLmA2cR8dEfGhJ4pSo5iVn0KwXqzE" // 43 знака, как у настоящего токена
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text stays", "request finished", "request finished"},
		{"email", "send to a.petrova@msu.ru failed", "send to ***@msu.ru failed"},
		{"email from smtp refusal", "550 5.1.1 <ivan+jobs@mail.example.org>: user unknown", "550 5.1.1 <***@mail.example.org>: user unknown"},
		{"cyrillic email", "почта иван@почта.рф не найдена", "почта ***@почта.рф не найдена"},
		{"two emails", "a@x.ru и b@y.ru", "***@x.ru и ***@y.ru"},
		{"at sign without domain stays", "deleted+12@deleted", "deleted+12@deleted"},
		{"ipv4 public", "from 203.0.113.77", "from 203.0.113.0/24"},
		{"ipv4 with port", "from 203.0.113.77:51234", "from 203.0.113.0/24:51234"},
		{"ipv4 loopback stays", "listen 127.0.0.1:8080", "listen 127.0.0.1:8080"},
		{"ipv4 private stays", "db 10.1.2.3 and 192.168.0.5", "db 10.1.2.3 and 192.168.0.5"},
		{"ipv4 unspecified stays", "listen 0.0.0.0:8080", "listen 0.0.0.0:8080"},
		{"ipv4 invalid octet stays", "version 999.1.1.1", "version 999.1.1.1"},
		{"ipv4 five parts stays", "build 1.2.3.4.5", "build 1.2.3.4.5"},
		{"version stays", "go1.26.9 postgres 16.15", "go1.26.9 postgres 16.15"},
		{"ipv6 public", "from 2001:db8:abcd:12:aaaa:bbbb:cccc:dddd", "from 2001:db8:abcd::/48"},
		{"ipv6 compressed", "from 2001:db8::7", "from 2001:db8::/48"},
		{"ipv6 in brackets", "from [2001:db8:1:2::9]:443", "from [2001:db8:1::/48]:443"},
		{"ipv6 loopback stays", "listen [::1]:8080", "listen [::1]:8080"},
		{"ipv4 mapped to ipv6", "from ::ffff:203.0.113.9", "from 203.0.113.0/24"},
		{"clock time stays", "at 12:30:45 today", "at 12:30:45 today"},
		{"timestamp stays", "2026-10-09T12:30:45+03:00", "2026-10-09T12:30:45+03:00"},
		{"session token", "session " + token + " expired", "session " + logmask.Hidden + " expired"},
		{"hex hash", "hash " + strings.Repeat("ab12", 16), "hash " + logmask.Hidden},
		{"uuid stays", "user 3f2504e0-4f89-11d3-9a0c-0305e82c3301", "user 3f2504e0-4f89-11d3-9a0c-0305e82c3301"},
		{"readable slug stays", "/api/organizations/moskovskii-gosudarstvennyi-universitet-imeni-lomonosova", "/api/organizations/moskovskii-gosudarstvennyi-universitet-imeni-lomonosova"},
		{"snake case stays", "saved_search_conditions_are_no_longer_valid", "saved_search_conditions_are_no_longer_valid"},
		{"mixed case with hyphens is a token", "x " + strings.Repeat("Ab1-", 10), "x " + logmask.Hidden},
		{"short token-like stays", "request abcDEF123", "request abcDEF123"},
		{"token pair", "confirm?token=abc123def&x=1", "confirm?token=" + logmask.Hidden + "&x=1"},
		{"password pair", "login failed password=hunter2, retry", "login failed password=" + logmask.Hidden + ", retry"},
		{"password json", `{"password": "hunter2"}`, `{"password": "` + logmask.Hidden + `"}`},
		{"prefixed password pair", "new_password=hunter2", "new_password=" + logmask.Hidden},
		{"cookie pair", "cookie: scibox_session=abc", "cookie: " + logmask.Hidden},
		{"masked ipv4 network is stable", "from 203.0.113.0/24", "from 203.0.113.0/24"},
		{"masked ipv6 network is stable", "from 2001:db8:abcd::/48", "from 2001:db8:abcd::/48"},
		{"wide ipv4 network stays", "route 198.51.0.0/16", "route 198.51.0.0/16"},
		{"narrow ipv4 network is widened", "route 198.51.100.7/32", "route 198.51.100.0/24"},
		{"bearer", "Authorization: Bearer abcdefgh12345678", "Authorization: " + logmask.Hidden},
		{"bearer alone", "got Bearer abcdefgh12345678 header", "got Bearer " + logmask.Hidden + " header"},
		{"dsn password", "connect postgres://scibox:s3cret@db:5432/scibox failed", "connect postgres://scibox:" + logmask.Hidden + "@db:5432/scibox failed"},
		{"url without password stays", "open https://example.org/path", "open https://example.org/path"},
		{"control characters", "line1\nline2\r\tend", "line1 line2  end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logmask.Mask(tt.in); got != tt.want {
				t.Errorf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// secret — то, чего не должно быть в записи ни при каком способе передачи.
//
// value — что подаётся в журнал, raw — что не должно в нём остаться. Голый пароль без ключа узнать нельзя
// (поэтому секреты кода всегда идут под ключом password=…), токен и почту можно узнать по виду.
type secret struct{ name, value, raw string }

var secrets = []secret{
	{"email", "ivan.sidorov@msu.ru", "ivan.sidorov@msu.ru"},
	{"ipv4", "198.51.100.23", "198.51.100.23"},
	{"ipv6", "2001:db8:aaaa:bbbb::1234", "2001:db8:aaaa:bbbb::1234"},
	{"token", "Zk3nQ9vW1xYbT7uLmA2cR8dEfGhJ4pSo5iVn0KwXqzE", "Zk3nQ9vW1xYbT7uLmA2cR8dEfGhJ4pSo5iVn0KwXqzE"},
	{"password pair", "password=hunter2hunter2", "hunter2hunter2"},
}

// carriers — способы подать значение в журнал.
var carriers = []struct {
	name string
	log  func(l *slog.Logger, level slog.Level, value string)
}{
	{"message", func(l *slog.Logger, level slog.Level, v string) { l.Log(context.Background(), level, "value "+v) }},
	{"string attribute", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", "note", v)
	}},
	{"error attribute", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", "err", fmt.Errorf("send: %w", errors.New("refused "+v)))
	}},
	{"any attribute", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", "value", struct{ Field string }{v})
	}},
	{"slice attribute", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", "values", []string{"x", v})
	}},
	{"group", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", slog.Group("request", slog.String("detail", v), slog.Group("inner", slog.String("deep", v))))
	}},
	{"with attrs", func(l *slog.Logger, level slog.Level, v string) {
		l.With("note", v).Log(context.Background(), level, "m")
	}},
	{"with group then attrs", func(l *slog.Logger, level slog.Level, v string) {
		l.WithGroup("g").With("note", v).Log(context.Background(), level, "m", "other", v)
	}},
	{"log valuer", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", "who", valuer{v})
	}},
	{"attribute key", func(l *slog.Logger, level slog.Level, v string) {
		l.Log(context.Background(), level, "m", v, "x")
	}},
}

type valuer struct{ v string }

func (p valuer) LogValue() slog.Value { return slog.StringValue("user " + p.v) }

func TestHandlerHidesEverySecretOnEveryLevelAndEveryCarrier(t *testing.T) {
	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	for _, level := range levels {
		for _, c := range carriers {
			for _, s := range secrets {
				t.Run(fmt.Sprintf("%s/%s/%s", level, c.name, s.name), func(t *testing.T) {
					var buf bytes.Buffer
					logger := slog.New(logmask.NewHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
					c.log(logger, level, s.value)
					out := buf.String()
					if out == "" {
						t.Fatal("nothing was logged")
					}
					if strings.Contains(out, s.raw) {
						t.Errorf("%s leaked through %s at %s: %s", s.name, c.name, level, out)
					}
					if s.name == "email" && strings.Contains(out, "ivan.sidorov") {
						t.Errorf("local part of the email leaked: %s", out)
					}
				})
			}
		}
	}
}

func TestHandlerHidesTheValueOfSensitiveKeys(t *testing.T) {
	keys := []string{"password", "Password", "new_password", "token", "session_token", "csrf-token", "cookie", "Set-Cookie", "authorization", "secret", "client_secret", "dsn", "api_key", "apikey", "credentials"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(logmask.NewHandler(slog.NewJSONHandler(&buf, nil)))
			logger.Error("m", key, "plain value without any pattern")
			logger.With(key, "another plain value").Error("m2")
			logger.Error("m3", slog.Group("g", slog.String(key, "grouped plain value")))
			out := buf.String()
			for _, leaked := range []string{"plain value without any pattern", "another plain value", "grouped plain value"} {
				if strings.Contains(out, leaked) {
					t.Errorf("value under key %q leaked: %s", key, out)
				}
			}
			if strings.Count(out, logmask.Hidden) != 3 {
				t.Errorf("want 3 hidden values, got: %s", out)
			}
		})
	}
}

func TestHandlerKeepsHarmlessRecordsReadable(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(logmask.NewHandler(slog.NewJSONHandler(&buf, nil)))
	addr := netip.MustParseAddr("127.0.0.1")
	logger.Info("request", "method", "GET", "path", "/api/vacancies/3f2504e0-4f89-11d3-9a0c-0305e82c3301", "status", 200, "ok", true, "ratio", 0.5,
		"listen", addr, "ip", net.ParseIP("10.0.0.7"), "request_id", "host/AbCdEfGhIj-000001")
	out := buf.String()
	for _, want := range []string{`"method":"GET"`, `"status":200`, `"ok":true`, `"ratio":0.5`, "3f2504e0-4f89-11d3-9a0c-0305e82c3301", "127.0.0.1", "10.0.0.7", "host/AbCdEfGhIj-000001"} {
		if !strings.Contains(out, want) {
			t.Errorf("record lost %q: %s", want, out)
		}
	}
}

func TestHandlerMasksAddressesGivenAsTypedValues(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(logmask.NewHandler(slog.NewJSONHandler(&buf, nil)))
	logger.Info("request", "ip", net.ParseIP("203.0.113.9"), "addr", netip.MustParseAddr("2001:db8:1:2::9"), "pair", &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 4000})
	out := buf.String()
	if strings.Contains(out, "203.0.113.9") || strings.Contains(out, "2001:db8:1:2::9") {
		t.Errorf("typed address leaked: %s", out)
	}
	if !strings.Contains(out, "203.0.113.0/24") || !strings.Contains(out, "2001:db8:1::/48") {
		t.Errorf("addresses were not reduced to a network: %s", out)
	}
}

func TestHandlerPassesLevelAndTimeThrough(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(logmask.NewHandler(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info must stay disabled when the next handler disables it")
	}
	if !logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("error must be enabled")
	}
	logger.Info("skipped")
	logger.Warn("kept")
	out := buf.String()
	if strings.Contains(out, "skipped") || !strings.Contains(out, `"level":"WARN"`) || !strings.Contains(out, `"time":`) {
		t.Errorf("unexpected output: %s", out)
	}
}

func BenchmarkMaskTypicalRequestLine(b *testing.B) {
	for b.Loop() {
		logmask.Mask("/api/vacancies/3f2504e0-4f89-11d3-9a0c-0305e82c3301")
	}
}

func BenchmarkMaskLongLineWithSecrets(b *testing.B) {
	line := "send to ivan@msu.ru from 203.0.113.9 token=abc " + strings.Repeat("word ", 20)
	for b.Loop() {
		logmask.Mask(line)
	}
}

// Журнал создаётся в одном месте (cli.newLogger) и проходит через маску. Любой другой способ писать в журнал в рабочем
// коде обходит её, поэтому тест ищет их по исходникам.
func TestNoLoggerBypassesTheMask(t *testing.T) {
	bypass := regexp.MustCompile(`slog\.New(Text|JSON)Handler|slog\.SetDefault|\blog\.(Print|Fatal|Panic|New|SetOutput)|fmt\.Fprint\w*\(os\.Std(err|out)|\bprintln\(`)
	allowed := map[string]bool{
		"cli/cli.go":         true, // newLogger и useAsDefault
		"testkit/testkit.go": true, // журнал тестов уходит в io.Discard
	}
	files := 0
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, "../"))
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || allowed[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if bypass.MatchString(code) {
				t.Errorf("%s:%d writes to the log around the mask: %s", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Fatalf("scanned only %d files, the walk is broken", files)
	}
}

package auth

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scibox/server/internal/mail"
)

const goodPassword = "correct horse battery"

var seq atomic.Int64

func uniqueEmail() string { return fmt.Sprintf("user%d@example.ru", seq.Add(1)) }

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

// env — сервис аккаунтов на общей тестовой базе с подставным почтовым ящиком и своими часами.
type env struct {
	t      *testing.T
	svc    *Service
	mailer *mail.Memory
	clock  *fakeClock
	logs   *syncBuffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg := DefaultConfig("SciBox", "http://localhost:5173")
	cfg.Hash = TestHashParams
	e := &env{
		t:      t,
		mailer: &mail.Memory{},
		clock:  &fakeClock{t: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)},
		logs:   &syncBuffer{},
	}
	e.svc = NewService(sharedPool, e.mailer, cfg, slog.New(slog.NewTextHandler(e.logs, nil)))
	e.svc.now = e.clock.Now
	return e
}

// meta возвращает данные запроса с новым адресом, чтобы ограничения частоты разных тестов не пересекались.
func (e *env) meta() Meta {
	n := seq.Add(1)
	return Meta{IP: fmt.Sprintf("10.%d.%d.%d", n>>16&255, n>>8&255, n&255), UserAgent: "test-agent"}
}

func (e *env) mails() []mail.Message {
	e.svc.Flush()
	return e.mailer.Sent()
}

func (e *env) mailsTo(email string) []mail.Message {
	var out []mail.Message
	for _, m := range e.mails() {
		if m.To == email {
			out = append(out, m)
		}
	}
	return out
}

// tokenIn достаёт токен из ссылки в тексте письма.
func tokenIn(t *testing.T, m mail.Message) string {
	t.Helper()
	i := strings.Index(m.Body, "token=")
	if i < 0 {
		t.Fatalf("no token in mail:\n%s", m.Body)
	}
	rest := m.Body[i+len("token="):]
	if j := strings.IndexAny(rest, " \n\r"); j >= 0 {
		rest = rest[:j]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (e *env) lastMailTo(email string) mail.Message {
	e.t.Helper()
	ms := e.mailsTo(email)
	if len(ms) == 0 {
		e.t.Fatalf("no mail to %s", email)
	}
	return ms[len(ms)-1]
}

// register регистрирует человека и возвращает токен подтверждения из письма.
func (e *env) register(email string) string {
	e.t.Helper()
	if _, err := e.svc.Register(context.Background(), RegisterInput{Name: "Иван Петров", Email: email, Password: goodPassword, Consent: true}, e.meta()); err != nil {
		e.t.Fatalf("register: %v", err)
	}
	return tokenIn(e.t, e.lastMailTo(email))
}

// confirmedUser регистрирует и подтверждает человека; возвращает его, сессию и почту.
func (e *env) confirmedUser() (User, Session, string) {
	e.t.Helper()
	email := uniqueEmail()
	tok := e.register(email)
	u, s, err := e.svc.ConfirmEmail(context.Background(), tok, e.meta())
	if err != nil {
		e.t.Fatalf("confirm: %v", err)
	}
	return u, s, email
}

func (e *env) login(email, password string) (User, Session, error) {
	return e.svc.Login(context.Background(), email, password, e.meta())
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := sharedPool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

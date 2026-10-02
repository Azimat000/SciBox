package orgs

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

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/mail"
)

var (
	bg  = context.Background()
	seq atomic.Int64
)

const goodPassword = "correct horse battery"

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

// person — человек с настоящим аккаунтом и сессией.
type person struct {
	auth.User
	Token string
}

// world — сервис организаций и сервис аккаунтов на одной общей тестовой базе, с подставной почтой и своими часами.
type world struct {
	t        *testing.T
	svc      *Service
	auth     *auth.Service
	mailer   *mail.Memory // письма организаций
	authMail *mail.Memory // письма аккаунтов
	clock    *fakeClock
	logs     *syncBuffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	acfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	acfg.Hash = auth.TestHashParams
	w := &world{
		t:        t,
		mailer:   &mail.Memory{},
		authMail: &mail.Memory{},
		clock:    &fakeClock{t: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)},
		logs:     logs,
	}
	w.auth = auth.NewService(sharedPool, w.authMail, acfg, logger)
	w.svc = NewService(sharedPool, w.mailer, DefaultConfig("SciBox", "http://localhost:5173"), logger)
	w.svc.now = w.clock.Now
	return w
}

func (w *world) meta() auth.Meta {
	n := seq.Add(1)
	return auth.Meta{IP: fmt.Sprintf("10.%d.%d.%d", n>>16&255, n>>8&255, n&255), UserAgent: "test-agent"}
}

func tokenIn(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in mail:\n%s", body)
	}
	rest := body[i+len("token="):]
	if j := strings.IndexAny(rest, " \n\r"); j >= 0 {
		rest = rest[:j]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// user заводит человека с подтверждённой почтой и входом.
func (w *world) user(name string) person {
	w.t.Helper()
	email := fmt.Sprintf("user%d@example.ru", seq.Add(1))
	if _, err := w.auth.Register(bg, auth.RegisterInput{Name: name, Email: email, Password: goodPassword, Consent: true}, w.meta()); err != nil {
		w.t.Fatalf("register: %v", err)
	}
	w.auth.Flush()
	var confirm string
	for _, m := range w.authMail.Sent() {
		if m.To == email {
			confirm = tokenIn(w.t, m.Body)
		}
	}
	u, s, err := w.auth.ConfirmEmail(bg, confirm, w.meta())
	if err != nil {
		w.t.Fatalf("confirm: %v", err)
	}
	return person{User: u, Token: s.Token}
}

var orgSeq atomic.Int64

// orgInput — правильные поля организации с названием, которое не повторяется между тестами.
func orgInput() OrgInput {
	return OrgInput{
		Name: fmt.Sprintf("Институт тестовых наук %d", orgSeq.Add(1)), Kind: KindInstitute, City: "Новосибирск",
		Website: "example.ru", Description: "Занимаемся тестами.",
	}
}

func unitInput() UnitInput {
	return UnitInput{Name: fmt.Sprintf("Лаборатория %d", orgSeq.Add(1)), Kind: UnitLaboratory, Description: "Описание", Topics: []string{"Тема один", "Тема два"}}
}

// org создаёт организацию; owner становится владельцем.
func (w *world) org(owner person) Organization {
	w.t.Helper()
	o, err := w.svc.CreateOrganization(bg, owner.User, orgInput())
	if err != nil {
		w.t.Fatalf("create organization: %v", err)
	}
	return o
}

func (w *world) unit(owner person, slug string) Unit {
	w.t.Helper()
	u, err := w.svc.CreateUnit(bg, owner.User, slug, unitInput())
	if err != nil {
		w.t.Fatalf("create unit: %v", err)
	}
	return u
}

// member приглашает человека с ролью и сразу принимает приглашение его именем.
func (w *world) member(owner person, slug string, who person, role string, unitID *uuid.UUID) {
	w.t.Helper()
	w.clock.Advance(0)
	if _, err := w.svc.Invite(bg, owner.User, slug, InviteInput{Email: who.Email, Role: role, UnitID: unitID}); err != nil {
		w.t.Fatalf("invite: %v", err)
	}
	m := w.lastMailTo(who.Email)
	if _, err := w.svc.AcceptInvitation(bg, who.User, tokenIn(w.t, m.Body)); err != nil {
		w.t.Fatalf("accept: %v", err)
	}
}

func (w *world) mailsTo(email string) []mail.Message {
	w.svc.Flush()
	var out []mail.Message
	for _, m := range w.mailer.Sent() {
		if m.To == email {
			out = append(out, m)
		}
	}
	return out
}

func (w *world) lastMailTo(email string) mail.Message {
	w.t.Helper()
	ms := w.mailsTo(email)
	if len(ms) == 0 {
		w.t.Fatalf("no mail to %s", email)
	}
	return ms[len(ms)-1]
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := sharedPool.QueryRow(bg, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func ptr[T any](v T) *T { return &v }

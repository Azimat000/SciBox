package vacancies

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/mail"
	"scibox/server/internal/orgs"
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

// person — человек с настоящим аккаунтом и сессией.
type person struct {
	auth.User
	Token string
}

// world — сервисы аккаунтов, организаций и вакансий на одной общей тестовой базе, со своими часами.
type world struct {
	t     *testing.T
	svc   *Service
	auth  *auth.Service
	orgs  *orgs.Service
	mail  *mail.Memory
	clock *fakeClock
	log   *slog.Logger
}

// Сегодня в тестах: 3 октября 2026, полдень по UTC (в Москве 15:00).
var testNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func newWorld(t *testing.T) *world {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	acfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	acfg.Hash = auth.TestHashParams
	w := &world{t: t, mail: &mail.Memory{}, clock: &fakeClock{t: testNow}, log: logger}
	w.auth = auth.NewService(sharedPool, w.mail, acfg, logger)
	w.orgs = orgs.NewService(sharedPool, w.mail, orgs.DefaultConfig("SciBox", "http://localhost:5173"), logger)
	w.svc = NewService(sharedPool, Config{Create: Limit{Max: 100000, Window: time.Hour}}) // лимит проверяет отдельный тест
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
	email := fmt.Sprintf("vac%d@example.ru", seq.Add(1))
	if _, err := w.auth.Register(bg, auth.RegisterInput{Name: name, Email: email, Password: goodPassword, Consent: true}, w.meta()); err != nil {
		w.t.Fatalf("register: %v", err)
	}
	w.auth.Flush()
	var confirm string
	for _, m := range w.mail.Sent() {
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

// team — организация со всеми ролями: владелец, кадровик, руководитель подразделения A, руководитель подразделения B, посторонний.
type team struct {
	w                            *world
	slug                         string
	orgID                        uuid.UUID
	unitA, unitB                 orgs.Unit
	owner, hr, headA, headB, out person
}

var nameSeq atomic.Int64

func (w *world) team() *team {
	w.t.Helper()
	tm := &team{w: w}
	tm.owner, tm.hr = w.user("Владелец"), w.user("Кадровик")
	tm.headA, tm.headB, tm.out = w.user("Руководитель А"), w.user("Руководитель Б"), w.user("Посторонний")
	org, err := w.orgs.CreateOrganization(bg, tm.owner.User, orgs.OrgInput{
		Name: fmt.Sprintf("Институт вакансий %d", nameSeq.Add(1)), Kind: orgs.KindInstitute, City: "Новосибирск",
	})
	if err != nil {
		w.t.Fatal(err)
	}
	tm.slug, tm.orgID = org.Slug, org.ID
	mkUnit := func() orgs.Unit {
		u, err := w.orgs.CreateUnit(bg, tm.owner.User, tm.slug, orgs.UnitInput{Name: fmt.Sprintf("Лаборатория %d", nameSeq.Add(1)), Kind: orgs.UnitLaboratory})
		if err != nil {
			w.t.Fatal(err)
		}
		return u
	}
	tm.unitA, tm.unitB = mkUnit(), mkUnit()
	q := dbgen.New(sharedPool)
	add := func(p person, role access.Role) {
		if err := q.AddMember(bg, dbgen.AddMemberParams{OrgID: tm.orgID, UserID: p.ID, Role: string(role), JoinedAt: testNow}); err != nil {
			w.t.Fatal(err)
		}
	}
	add(tm.hr, access.RoleHR)
	add(tm.headA, access.RoleUnitHead)
	add(tm.headB, access.RoleUnitHead)
	for _, h := range []struct {
		p person
		u orgs.Unit
	}{{tm.headA, tm.unitA}, {tm.headB, tm.unitB}} {
		if _, err := w.orgs.SetUnitHead(bg, tm.owner.User, tm.slug, h.u.ID, &h.p.ID); err != nil {
			w.t.Fatal(err)
		}
	}
	return tm
}

func ptr[T any](v T) *T { return &v }

// goodInput — вакансия научной должности, готовая к публикации. Правится в тестах по месту.
func goodInput() Input {
	return Input{
		Title: "Старший научный сотрудник в лабораторию катализа", PositionCode: "senior_researcher",
		Summary:     "Исследования активных центров катализаторов методами операндо-спектроскопии.",
		Description: "Работа в команде из десяти человек: эксперименты, анализ данных, публикации.",
		CareerLevel: ptr(3), WorkFormat: FormatOnsite, RegionCode: "54", City: "Новосибирск",
		RatePercent: ptr(100), ContractType: ContractFixed, ContractMonth: ptr(36), Specialties: []string{"1.4.4"},
		Deadline: "2026-12-01",
	}
}

func (w *world) create(tm *team, who person, in Input) Detail {
	w.t.Helper()
	d, err := w.svc.Create(bg, who.User, tm.slug, in)
	if err != nil {
		w.t.Fatalf("create: %v", err)
	}
	return d
}

// published создаёт и публикует хорошую вакансию владельцем.
func (w *world) published(tm *team, in Input) Detail {
	w.t.Helper()
	d := w.create(tm, tm.owner, in)
	d, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished)
	if err != nil {
		w.t.Fatalf("publish: %v", err)
	}
	return d
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := sharedPool.QueryRow(bg, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

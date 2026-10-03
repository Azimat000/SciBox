package profiles

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

	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
	"scibox/server/internal/mail"
	"scibox/server/internal/orgs"
)

var (
	bg  = context.Background()
	seq atomic.Int64
)

const goodPassword = "correct horse battery"

// Сегодня в тестах: 3 октября 2026, полдень по UTC.
var testNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

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

// fakeDOI — подставной Crossref: считает обращения и отвечает заданным.
type fakeDOI struct {
	mu    sync.Mutex
	calls []string
	work  crossref.Work
	err   error
}

func (f *fakeDOI) Lookup(_ context.Context, doi string) (crossref.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, doi)
	if f.err != nil {
		return crossref.Work{}, f.err
	}
	w := f.work
	w.DOI = doi
	return w, nil
}

func (f *fakeDOI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// person — человек с настоящим аккаунтом и сессией.
type person struct {
	auth.User
	Token string
}

// world — сервисы аккаунтов, организаций и профилей на одной общей тестовой базе, со своими часами и подставным Crossref.
type world struct {
	t     *testing.T
	svc   *Service
	auth  *auth.Service
	orgs  *orgs.Service
	mail  *mail.Memory
	clock *fakeClock
	doi   *fakeDOI
	log   *slog.Logger
}

func newWorld(t *testing.T) *world {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	acfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	acfg.Hash = auth.TestHashParams
	w := &world{t: t, mail: &mail.Memory{}, clock: &fakeClock{t: testNow}, doi: &fakeDOI{work: crossref.Work{Title: "Найденная статья", Authors: "Орлова Е. А.", Venue: "Журнал", Year: 2023, Type: "article"}}, log: logger}
	w.auth = auth.NewService(sharedPool, w.mail, acfg, logger)
	w.orgs = orgs.NewService(sharedPool, w.mail, orgs.DefaultConfig("SciBox", "http://localhost:5173"), logger)
	w.svc = NewService(sharedPool, w.doi, Config{ProductName: "SciBox", DOI: Limit{Max: 100000, Window: time.Hour}}) // лимит проверяет отдельный тест
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
	email := fmt.Sprintf("prof%d@example.ru", seq.Add(1))
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

var orgSeq atomic.Int64

// staff заводит сотрудника организации: человека, который владеет своей организацией.
func (w *world) staff(name string) person {
	w.t.Helper()
	p := w.user(name)
	if _, err := w.orgs.CreateOrganization(bg, p.User, orgs.OrgInput{
		Name: fmt.Sprintf("Институт профилей %d", orgSeq.Add(1)), Kind: orgs.KindInstitute, City: "Казань",
	}); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// viewers — все, кто может смотреть чужой профиль.
type viewers struct {
	owner, stranger, staff person
}

func (w *world) viewers() viewers {
	return viewers{owner: w.user("Владелец профиля"), stranger: w.user("Вошедший без организации"), staff: w.staff("Сотрудник организации")}
}

func ptr[T any](v T) *T { return &v }

// goodCore — основные поля с заполненным всем, что можно заполнить.
func goodCore() CoreInput {
	return CoreInput{
		Headline: "Старший научный сотрудник, лаборатория катализа", City: "Новосибирск", RegionCode: "54",
		About:  "Изучаю активные центры катализаторов.\nЛюблю эксперименты.",
		Degree: DegreeCandidate, DegreeSpecialty: "1.4.4", DegreeYear: ptr(2016), DegreeInstitution: "Институт катализа", Dissertation: "Активные центры оксидных катализаторов",
		AcademicTitle: TitleDocent, AcademicTitleYear: ptr(2021),
		ORCID: "0000-0002-1825-0097", SPIN: "1234-5678", ScopusID: "57190123456", WosID: "a-1234-2008",
		HRsci: ptr(12), HScopus: ptr(9), HWos: ptr(7), HScholar: ptr(15),
		ContactEmail: "Orlova@Example.ru", Specialties: []string{"1.4.4", "1.4.1"},
	}
}

func goodPublication() ItemInput {
	return ItemInput{Kind: KindPublication, ItemFields: ItemFields{
		Title: "Активные центры катализаторов", Authors: "Орлова Е. А., Белов И. С.", Venue: "Журнал физической химии",
		PubType: "article", Year: ptr(2023), Volume: "97", Issue: "4", Pages: "512–520", DOI: "10.1234/abc.2023",
	}}
}

func (w *world) addItem(p person, in ItemInput) Item {
	w.t.Helper()
	it, err := w.svc.AddItem(bg, p.User, in)
	if err != nil {
		w.t.Fatalf("add %s: %v", in.Kind, err)
	}
	return it
}

func (w *world) setVisibility(p person, vis string, open bool) {
	w.t.Helper()
	if _, err := w.svc.SetPrivacy(bg, p.User, vis, open); err != nil {
		w.t.Fatalf("privacy: %v", err)
	}
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := sharedPool.QueryRow(bg, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func orgInput() orgs.OrgInput {
	return orgs.OrgInput{Name: fmt.Sprintf("Институт гостей %d", orgSeq.Add(1)), Kind: orgs.KindInstitute, City: "Казань"}
}

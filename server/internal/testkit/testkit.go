// Package testkit — общие помощники для тестов разделов с базой: временная база на весь пакет, настоящие сервисы
// аккаунтов, организаций, вакансий и профилей, люди с подтверждённой почтой, организация со всеми ролями,
// опубликованная вакансия и «поломка» n-го обращения к базе. В боевом коде не используется.
package testkit

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/crossref"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/mail"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
	"scibox/server/internal/testdb"
	"scibox/server/internal/vacancies"
)

// Pool — общая временная база пакета; заполняется в RunMain.
var Pool *pgxpool.Pool

var (
	// BG — пустой контекст для тестов.
	BG  = context.Background()
	seq atomic.Int64
)

// Password — пароль всех тестовых людей.
const Password = "correct horse battery"

type mainTB struct{ cleanups []func() }

func (m *mainTB) Helper() {}
func (m *mainTB) Fatalf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	m.run()
	os.Exit(1)
}
func (m *mainTB) Cleanup(f func()) { m.cleanups = append(m.cleanups, f) }
func (m *mainTB) run() {
	for i := len(m.cleanups) - 1; i >= 0; i-- {
		m.cleanups[i]()
	}
	m.cleanups = nil
}

// RunMain заводит временную базу на весь пакет, запускает тесты и убирает за собой. Вызывать из TestMain:
// os.Exit(testkit.RunMain(m)).
func RunMain(m *testing.M) int {
	tb := &mainTB{}
	url := testdb.Create(tb, true)
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open pool:", err)
		tb.run()
		return 1
	}
	Pool = pool
	code := m.Run()
	pool.Close()
	tb.run()
	return code
}

// Seq — следующий уникальный номер (для почт и названий).
func Seq() int64 { return seq.Add(1) }

// Person — человек с настоящим аккаунтом и сессией.
type Person struct {
	auth.User
	Token string
}

// World — настоящие сервисы на общей тестовой базе.
type World struct {
	T    *testing.T
	Auth *auth.Service
	Orgs *orgs.Service
	Vac  *vacancies.Service
	Prof *profiles.Service
	Mail *mail.Memory
	Log  *slog.Logger
}

// noDOI — Crossref в тестах не нужен.
type noDOI struct{}

func (noDOI) Lookup(context.Context, string) (crossref.Work, error) {
	return crossref.Work{}, crossref.ErrNotFound
}

// NewWorld собирает сервисы.
func NewWorld(t *testing.T) *World {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	acfg := auth.DefaultConfig("SciBox", "http://localhost:5173")
	acfg.Hash = auth.TestHashParams
	w := &World{T: t, Mail: &mail.Memory{}, Log: logger}
	w.Auth = auth.NewService(Pool, w.Mail, acfg, logger)
	w.Orgs = orgs.NewService(Pool, w.Mail, orgs.DefaultConfig("SciBox", "http://localhost:5173"), logger)
	w.Vac = vacancies.NewService(Pool, vacancies.Config{Create: vacancies.Limit{Max: 100000, Window: time.Hour}})
	w.Prof = profiles.NewService(Pool, noDOI{}, profiles.Config{ProductName: "SciBox", DOI: profiles.Limit{Max: 100000, Window: time.Hour}})
	return w
}

// Meta — сведения о запросе для аккаунтов; у каждого вызова свой «адрес».
func Meta() auth.Meta {
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

// User заводит человека с подтверждённой почтой и входом.
func (w *World) User(name string) Person {
	w.T.Helper()
	email := fmt.Sprintf("kit%d@example.ru", seq.Add(1))
	if _, err := w.Auth.Register(BG, auth.RegisterInput{Name: name, Email: email, Password: Password, Consent: true}, Meta()); err != nil {
		w.T.Fatalf("register: %v", err)
	}
	w.Auth.Flush()
	var confirm string
	for _, m := range w.Mail.Sent() {
		if m.To == email {
			confirm = tokenIn(w.T, m.Body)
		}
	}
	u, s, err := w.Auth.ConfirmEmail(BG, confirm, Meta())
	if err != nil {
		w.T.Fatalf("confirm: %v", err)
	}
	return Person{User: u, Token: s.Token}
}

// Team — организация со всеми ролями: владелец, кадровик, руководители подразделений A и B, посторонний.
type Team struct {
	W                            *World
	Slug                         string
	OrgID                        uuid.UUID
	UnitA, UnitB                 orgs.Unit
	Owner, HR, HeadA, HeadB, Out Person
}

// Team заводит организацию с двумя подразделениями и людьми во всех ролях.
func (w *World) Team() *Team {
	w.T.Helper()
	tm := &Team{W: w}
	tm.Owner, tm.HR = w.User("Владелец"), w.User("Кадровик")
	tm.HeadA, tm.HeadB, tm.Out = w.User("Руководитель А"), w.User("Руководитель Б"), w.User("Посторонний")
	org, err := w.Orgs.CreateOrganization(BG, tm.Owner.User, orgs.OrgInput{
		Name: fmt.Sprintf("Институт набора %d", seq.Add(1)), Kind: orgs.KindInstitute, City: "Новосибирск",
	})
	if err != nil {
		w.T.Fatal(err)
	}
	tm.Slug, tm.OrgID = org.Slug, org.ID
	mkUnit := func() orgs.Unit {
		u, err := w.Orgs.CreateUnit(BG, tm.Owner.User, tm.Slug, orgs.UnitInput{Name: fmt.Sprintf("Лаборатория %d", seq.Add(1)), Kind: orgs.UnitLaboratory})
		if err != nil {
			w.T.Fatal(err)
		}
		return u
	}
	tm.UnitA, tm.UnitB = mkUnit(), mkUnit()
	q := dbgen.New(Pool)
	add := func(p Person, role access.Role) {
		if err := q.AddMember(BG, dbgen.AddMemberParams{OrgID: tm.OrgID, UserID: p.ID, Role: string(role), JoinedAt: time.Now().UTC()}); err != nil {
			w.T.Fatal(err)
		}
	}
	add(tm.HR, access.RoleHR)
	add(tm.HeadA, access.RoleUnitHead)
	add(tm.HeadB, access.RoleUnitHead)
	for _, h := range []struct {
		p Person
		u orgs.Unit
	}{{tm.HeadA, tm.UnitA}, {tm.HeadB, tm.UnitB}} {
		if _, err := w.Orgs.SetUnitHead(BG, tm.Owner.User, tm.Slug, h.u.ID, &h.p.ID); err != nil {
			w.T.Fatal(err)
		}
	}
	return tm
}

func ptr[T any](v T) *T { return &v }

// Deadline — срок подачи через n дней от сегодня (для настоящих часов сервиса вакансий).
func Deadline(days int) string { return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02") }

// VacancyInput — вакансия научной должности, готовая к публикации, в подразделении unit (nil — на всю организацию).
func VacancyInput(unit *uuid.UUID) vacancies.Input {
	return vacancies.Input{
		Title: "Старший научный сотрудник в лабораторию катализа", PositionCode: "senior_researcher", UnitID: unit,
		Summary:     "Исследования активных центров катализаторов методами операндо-спектроскопии.",
		Description: "Работа в команде из десяти человек: эксперименты, анализ данных, публикации.",
		CareerLevel: ptr(3), WorkFormat: vacancies.FormatOnsite, RegionCode: "54", City: "Новосибирск",
		RatePercent: ptr(100), ContractType: vacancies.ContractFixed, ContractMonth: ptr(36), Specialties: []string{"1.4.4"},
		Deadline: Deadline(90),
	}
}

// Published создаёт и публикует вакансию владельцем. unit nil — на всю организацию.
func (tm *Team) Published(unit *uuid.UUID) vacancies.Detail {
	tm.W.T.Helper()
	d, err := tm.W.Vac.Create(BG, tm.Owner.User, tm.Slug, VacancyInput(unit))
	if err != nil {
		tm.W.T.Fatalf("create vacancy: %v", err)
	}
	d, err = tm.W.Vac.SetStatus(BG, tm.Owner.User, d.ID, vacancies.StatusPublished)
	if err != nil {
		tm.W.T.Fatalf("publish vacancy: %v", err)
	}
	return d
}

// Status переводит вакансию в другой статус владельцем (закрыть, архив…).
func (tm *Team) Status(id uuid.UUID, to string) {
	tm.W.T.Helper()
	if _, err := tm.W.Vac.SetStatus(BG, tm.Owner.User, id, to); err != nil {
		tm.W.T.Fatalf("set status %s: %v", to, err)
	}
}

// FillProfile заполняет профиль настолько, чтобы с ним можно было откликаться.
func (w *World) FillProfile(p Person) {
	w.T.Helper()
	if _, err := w.Prof.SaveCore(BG, p.User, profiles.CoreInput{
		Headline: "Научный сотрудник, лаборатория катализа", City: "Новосибирск", RegionCode: "54",
		About: "Изучаю активные центры катализаторов.", Specialties: []string{"1.4.4"},
	}); err != nil {
		w.T.Fatalf("fill profile: %v", err)
	}
}

// Applicant — человек с заполненным профилем (скрытым: отклику это не мешает).
func (w *World) Applicant(name string) Person {
	w.T.Helper()
	p := w.User(name)
	w.FillProfile(p)
	return p
}

// Count выполняет запрос вида SELECT count(*) и возвращает число.
func Count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := Pool.QueryRow(BG, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

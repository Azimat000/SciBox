package chat

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

var bg = testkit.BG

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type world struct {
	*testkit.World
	svc   *Service
	clock *clock
	tm    *testkit.Team
	// vacancy — опубликованная вакансия подразделения A, other — подразделения B.
	vacancy, other vacancies.Detail
}

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	w := &world{World: k, clock: &clock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	w.svc = NewService(testkit.Pool)
	w.svc.now = w.clock.Now
	w.tm = k.Team()
	w.vacancy = w.tm.Published(&w.tm.UnitA.ID)
	w.other = w.tm.Published(&w.tm.UnitB.ID)
	return w
}

func (w *world) q() *dbgen.Queries { return dbgen.New(testkit.Pool) }

// application записывает отклик человека на вакансию напрямую в базе (сам отклик проверяет пакет applications).
func (w *world) application(p testkit.Person, vacancy uuid.UUID, status string) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	err := testkit.Pool.QueryRow(bg, `INSERT INTO applications (vacancy_id, user_id, cover_letter, contact_email, profile, status, created_at, updated_at, status_changed_at)
		VALUES ($1, $2, 'Письмо', 'a@example.ru', '{}', $3, now(), now(), now()) RETURNING id`, vacancy, p.ID, status).Scan(&id)
	if err != nil {
		w.T.Fatal(err)
	}
	return id
}

// setApplication меняет статус отклика, как это делает разбор откликов.
func (w *world) setApplication(id uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET status = $2 WHERE id = $1`, id, status); err != nil {
		w.T.Fatal(err)
	}
}

// offer записывает приглашение на вакансию напрямую в базе.
func (w *world) offer(p testkit.Person, vacancy uuid.UUID, status string) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	var answered *time.Time
	if status == "interested" || status == "declined" {
		now := time.Now().UTC()
		answered = &now
	}
	err := testkit.Pool.QueryRow(bg, `INSERT INTO vacancy_offers (vacancy_id, user_id, invited_by, status, message, answered_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'Приглашаем', $5, now(), now()) RETURNING id`, vacancy, p.ID, w.tm.Owner.ID, status, answered).Scan(&id)
	if err != nil {
		w.T.Fatal(err)
	}
	return id
}

func (w *world) setOffer(id uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancy_offers SET status = $2, answered_at = CASE WHEN $2 IN ('interested', 'declined') THEN now() END WHERE id = $1`, id, status); err != nil {
		w.T.Fatal(err)
	}
}

// open заводит переписку учёного с организацией по отклику или приглашению.
func (w *world) open(p testkit.Person, vacancy uuid.UUID, application, offer *uuid.UUID) uuid.UUID {
	w.T.Helper()
	id, err := Open(bg, w.q(), OpenInput{VacancyID: vacancy, UserID: p.ID, ApplicationID: application, OfferID: offer}, w.clock.Now())
	if err != nil {
		w.T.Fatal(err)
	}
	return id
}

// chatOf — переписка учёного по новому отклику в нужном статусе.
func (w *world) chatOf(p testkit.Person, vacancy uuid.UUID, status string) uuid.UUID {
	w.T.Helper()
	app := w.application(p, vacancy, status)
	return w.open(p, vacancy, &app, nil)
}

func (w *world) system(chat uuid.UUID, event string, data any) int {
	w.T.Helper()
	seq, err := AppendSystem(bg, w.q(), chat, event, data, w.clock.Now())
	if err != nil {
		w.T.Fatal(err)
	}
	return seq
}

func (w *world) send(who testkit.Person, chat uuid.UUID, text string) Message {
	w.T.Helper()
	m, _, err := w.svc.Send(bg, who.User, chat, SendInput{Text: text, ClientID: uuid.NewString()})
	if err != nil {
		w.T.Fatalf("send: %v", err)
	}
	return m
}

// sees — тексты ленты глазами человека.
func (w *world) texts(who testkit.Person, chat uuid.UUID) []string {
	w.T.Helper()
	page, err := w.svc.Messages(bg, who.User, chat, MessagesQuery{Limit: 100})
	if err != nil {
		w.T.Fatal(err)
	}
	out := make([]string, 0, len(page.Items))
	for _, m := range page.Items {
		out = append(out, m.Text)
	}
	return out
}

func str(s string) *string { return &s }

func uuidOf(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func people(w *world) []struct {
	name string
	who  testkit.Person
} {
	return []struct {
		name string
		who  testkit.Person
	}{{"владелец", w.tm.Owner}, {"кадровик", w.tm.HR}, {"руководитель А", w.tm.HeadA}, {"руководитель Б", w.tm.HeadB}, {"посторонний", w.tm.Out}}
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

package offers

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/notifications"
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

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type world struct {
	*testkit.World
	svc   *Service
	notes *notifications.Service
	clock *clock
	tm    *testkit.Team
	// vacancy — опубликованная вакансия подразделения A.
	vacancy vacancies.Detail
}

func testConfig() Config { return Config{Invite: Limit{Max: 100000, Window: time.Hour}} }

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	w := &world{World: k, clock: &clock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	w.notes = notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	w.svc = NewService(testkit.Pool, w.notes, testConfig())
	w.svc.now = w.clock.Now
	w.tm = k.Team()
	w.vacancy = w.tm.Published(&w.tm.UnitA.ID)
	return w
}

// scientist — человек с заполненным профилем в нужном режиме приватности; вторым значением номер его профиля.
func (w *world) scientist(name, visibility string) (testkit.Person, uuid.UUID) {
	w.T.Helper()
	p := w.Applicant(name)
	if _, err := w.Prof.SetPrivacy(bg, p.User, visibility, false); err != nil {
		w.T.Fatalf("privacy: %v", err)
	}
	own, err := w.Prof.Own(bg, p.User)
	if err != nil {
		w.T.Fatal(err)
	}
	return p, own.Profile.ID
}

func (w *world) invite(by testkit.Person, vacancy, profile uuid.UUID, message string) (Offer, error) {
	return w.svc.Invite(bg, by.User, InviteInput{VacancyID: vacancy, ProfileID: profile, Message: message})
}

func (w *world) mustInvite(by testkit.Person, vacancy, profile uuid.UUID, message string) Offer {
	w.T.Helper()
	o, err := w.invite(by, vacancy, profile, message)
	if err != nil {
		w.T.Fatalf("invite: %v", err)
	}
	return o
}

// apply записывает живой отклик человека на вакансию напрямую в базе (сам отклик проверяет пакет applications).
func (w *world) apply(p testkit.Person, vacancy uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO applications (vacancy_id, user_id, cover_letter, contact_email, profile, status, created_at, updated_at, status_changed_at)
		VALUES ($1, $2, 'Письмо', 'a@example.ru', '{}', $3, now(), now(), now())`, vacancy, p.ID, status); err != nil {
		w.T.Fatal(err)
	}
}

func notificationsOf(t *testing.T, who testkit.Person, kind string) int {
	t.Helper()
	return testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind = $2`, who.ID, kind)
}

func mailCount(t *testing.T, email string) int {
	t.Helper()
	return testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1`, email)
}

// snapshot — всё, что приглашение может изменить у человека и вакансии: строки, уведомления, письма, счётчик частоты.
type snapshot struct{ offers, notices, mails, rates int }

func (w *world) snap(vacancy uuid.UUID, scientist, actor testkit.Person) snapshot {
	w.T.Helper()
	return snapshot{
		offers:  testkit.Count(w.T, `SELECT count(*) FROM vacancy_offers WHERE vacancy_id = $1`, vacancy),
		notices: testkit.Count(w.T, `SELECT count(*) FROM notifications WHERE user_id = $1`, scientist.ID),
		mails:   mailCount(w.T, scientist.Email),
		rates:   testkit.Count(w.T, `SELECT count(*) FROM rate_events WHERE kind = 'offer' AND key = $1`, actor.ID.String()),
	}
}

func (w *world) status(id uuid.UUID) string {
	w.T.Helper()
	var s string
	if err := testkit.Pool.QueryRow(bg, `SELECT status FROM vacancy_offers WHERE id = $1`, id).Scan(&s); err != nil {
		w.T.Fatal(err)
	}
	return s
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

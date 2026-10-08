package references

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
)

var bg = testkit.BG

// testNow — «сейчас» в тестах: настоящее время при запуске; часы идут только когда тест их двигает.
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
	notes *notifications.Service
	clock *clock
	tm    *testkit.Team
	// vacancy — опубликованная вакансия подразделения A.
	vacancyID uuid.UUID
}

func testConfig() Config {
	cfg := DefaultConfig("SciBox", "http://localhost:5173")
	cfg.Requests = Limit{Max: 100000, Window: time.Hour} // лимит проверяет отдельный тест
	return cfg
}

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	w := &world{World: k, clock: &clock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	w.notes = notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	w.svc = NewService(testkit.Pool, w.notes, testConfig())
	w.svc.now = w.clock.Now
	w.tm = k.Team()
	w.vacancyID = w.tm.Published(&w.tm.UnitA.ID).ID
	return w
}

// application заводит отклик человека на вакансию подразделения A напрямую в базе (пакет applications здесь не нужен).
func (w *world) application(who testkit.Person) uuid.UUID {
	w.T.Helper()
	id, err := dbgen.New(testkit.Pool).InsertApplication(bg, dbgen.InsertApplicationParams{
		VacancyID: w.vacancyID, UserID: who.ID, CoverLetter: "Сопроводительное письмо", ContactEmail: who.Email, Profile: []byte(`{}`), Now: w.clock.Now(),
	})
	if err != nil {
		w.T.Fatalf("insert application: %v", err)
	}
	return id
}

func (w *world) setStatus(appID uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET status = $2 WHERE id = $1`, appID, status); err != nil {
		w.T.Fatal(err)
	}
}

var emailSeq = func() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("ref%d.%d@example.ru", n, time.Now().UnixNano()%100000)
	}
}()

func referee() RefereeInput {
	return RefereeInput{Name: "Профессор Иванов", Email: emailSeq(), Relation: "Научный руководитель"}
}

// pdfBytes — минимальный PDF для тестов.
func pdfBytes() []byte { return append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("p"), 200)...) }

type outboxMail struct{ To, Subject, Body string }

func mailsTo(t *testing.T, email string) []outboxMail {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT to_email, subject, body FROM outbox WHERE to_email = $1 ORDER BY id`, email)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxMail
	for rows.Next() {
		var m outboxMail
		if err := rows.Scan(&m.To, &m.Subject, &m.Body); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

// tokenFrom достаёт ссылку из последнего письма на этот адрес.
func tokenFrom(t *testing.T, email string) string {
	t.Helper()
	ms := mailsTo(t, email)
	if len(ms) == 0 {
		t.Fatalf("no mail to %s", email)
	}
	body := ms[len(ms)-1].Body
	i := strings.Index(body, "/recommend?token=")
	if i < 0 {
		t.Fatalf("no link in mail:\n%s", body)
	}
	rest := body[i+len("/recommend?token="):]
	if j := strings.IndexAny(rest, " \n\r"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func notificationsOf(t *testing.T, who testkit.Person, kind string) int {
	t.Helper()
	return testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind = $2`, who.ID, kind)
}

// add просит рекомендателя и возвращает просьбу вместе с токеном из письма.
func (w *world) add(who testkit.Person, appID uuid.UUID) (Request, string) {
	w.T.Helper()
	in := referee()
	req, err := w.svc.Add(bg, who.User, appID, in)
	if err != nil {
		w.T.Fatalf("add: %v", err)
	}
	return req, tokenFrom(w.T, in.Email)
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

package applications

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/files"
	"scibox/server/internal/notifications"
	"scibox/server/internal/references"
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
	refs  *references.Service
	notes *notifications.Service
	clock *clock
	tm    *testkit.Team
	// vacancy — опубликованная вакансия подразделения A.
	vacancy vacancies.Detail
}

func testConfig() Config { return Config{Apply: Limit{Max: 100000, Window: time.Hour}} }

func newWorld(t *testing.T) *world {
	t.Helper()
	k := testkit.NewWorld(t)
	w := &world{World: k, clock: &clock{t: time.Now().UTC()}}
	w.notes = notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	w.refs = references.NewService(testkit.Pool, w.notes, references.DefaultConfig("SciBox", "http://localhost:5173"))
	w.svc = NewService(testkit.Pool, k.Prof, w.refs, w.notes, testConfig())
	w.svc.now = w.clock.Now
	w.tm = k.Team()
	w.vacancy = w.tm.Published(&w.tm.UnitA.ID)
	return w
}

func pdfBytes() []byte { return append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("p"), 300)...) }

func emailSeq() string { return fmt.Sprintf("rec%d@example.ru", testkit.Seq()) }

// input — хороший отклик на вакансию подразделения A.
func (w *world) input(who testkit.Person) Input {
	return Input{VacancyID: w.vacancy.ID, ContactEmail: who.Email, CoverLetter: "Здравствуйте! Мой опыт работы с катализаторами подходит для вашей лаборатории."}
}

// apply отправляет отклик; mod правит поля.
func (w *world) apply(who testkit.Person, mod func(*Input), ups ...files.Upload) (Detail, error) {
	w.T.Helper()
	in := w.input(who)
	if mod != nil {
		mod(&in)
	}
	return w.svc.Apply(bg, who.User, in, ups)
}

// mustApply отправляет отклик и требует успеха.
func (w *world) mustApply(who testkit.Person, mod func(*Input), ups ...files.Upload) Detail {
	w.T.Helper()
	d, err := w.apply(who, mod, ups...)
	if err != nil {
		w.T.Fatalf("apply: %v", err)
	}
	return d
}

func (w *world) setStatus(id uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET status = $2 WHERE id = $1`, id, status); err != nil {
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

func refereeIn() references.RefereeInput {
	return references.RefereeInput{Name: "Профессор Иванов", Email: emailSeq(), Relation: "научный руководитель"}
}

func pdf(name string) files.Upload { return files.Upload{Name: name, Data: pdfBytes()} }

package matching

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/profiles"
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

func (c *clock) Advance(d time.Duration) { c.Set(c.Now().Add(d)) }

type world struct {
	*testkit.World
	svc   *Service
	notes *notifications.Service
	clock *clock
	tm    *testkit.Team
}

func testConfig() Config {
	return Config{MaxFavorites: 200, MaxSearches: 20, Settle: time.Minute, MaxListed: 10, Batch: 100}
}

func silent() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newWorld собирает сервисы на общей базе. Все вакансии, которые остались от прошлых тестов, убираются в архив: подбор и
// списки в этом пакете считают всё, что есть в базе, а тесты должны видеть только своё.
func newWorld(t *testing.T) *world {
	t.Helper()
	// Поиски прошлых тестов тоже убираем: фоновый проход берёт все поиски, до которых дошла очередь.
	for _, q := range []string{`UPDATE vacancies SET status = 'archived'`, `DELETE FROM saved_searches`} {
		if _, err := testkit.Pool.Exec(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	k := testkit.NewWorld(t)
	w := &world{World: k, clock: &clock{t: time.Now().UTC().Truncate(time.Microsecond)}}
	w.notes = notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
	w.svc = newService(testkit.Pool, k.Vac, w.notes, testConfig(), silent())
	w.svc.now = w.clock.Now
	w.tm = k.Team()
	return w
}

// vacancyOpts описывает вакансию, нужную тесту.
type vacancyOpts struct {
	title       string
	specialties []string
	level       *int
	degree      string
	region      string
	format      string
	deadline    string // пусто — срок через 90 дней
	noDeadline  bool
	unit        *uuid.UUID
}

// publish создаёт вакансию владельцем и публикует её.
func (w *world) publish(o vacancyOpts) vacancies.Detail {
	w.T.Helper()
	d := w.create(o)
	d, err := w.Vac.SetStatus(bg, w.tm.Owner.User, d.ID, vacancies.StatusPublished)
	if err != nil {
		w.T.Fatalf("publish: %v", err)
	}
	return d
}

func (w *world) create(o vacancyOpts) vacancies.Detail {
	w.T.Helper()
	in := testkit.VacancyInput(o.unit)
	if o.title != "" {
		in.Title = o.title
	}
	if o.specialties != nil {
		in.Specialties = o.specialties
	}
	if o.level != nil {
		in.CareerLevel = o.level
	}
	if o.degree != "" {
		in.Degree = o.degree
	}
	if o.region != "" {
		in.RegionCode = o.region
	}
	if o.format != "" {
		in.WorkFormat = o.format
		if o.format == vacancies.FormatRemote {
			in.RegionCode, in.City = "", ""
		}
	}
	if o.deadline != "" {
		in.Deadline = o.deadline
	}
	if o.noDeadline {
		in.Deadline = ""
	}
	d, err := w.Vac.Create(bg, w.tm.Owner.User, w.tm.Slug, in)
	if err != nil {
		w.T.Fatalf("create vacancy: %v", err)
	}
	return d
}

func (w *world) setStatus(id uuid.UUID, to string) {
	w.T.Helper()
	w.tm.Status(id, to)
}

// setDeadline пишет срок подачи напрямую в базу: так можно получить срок «вчера» и другие, которые сервис не пропустит.
func (w *world) setDeadline(id uuid.UUID, deadline *time.Time) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET deadline = $2 WHERE id = $1`, id, deadline); err != nil {
		w.T.Fatal(err)
	}
}

// publishedAt передвигает момент первой публикации: часы тестов уходят вперёд, а настоящее время нет.
func (w *world) publishedAt(id uuid.UUID, at time.Time) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET published_at = $2 WHERE id = $1`, id, at); err != nil {
		w.T.Fatal(err)
	}
}

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// seeker — человек с заполненным профилем.
func (w *world) seeker(name string, in profiles.CoreInput) testkit.Person {
	w.T.Helper()
	p := w.User(name)
	if in.Headline == "" {
		in.Headline = "Научный сотрудник"
	}
	if _, err := w.Prof.SaveCore(bg, p.User, in); err != nil {
		w.T.Fatalf("save profile: %v", err)
	}
	return p
}

// apply записывает живой отклик человека на вакансию напрямую в базе.
func (w *world) apply(p testkit.Person, vacancy uuid.UUID, status string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO applications (vacancy_id, user_id, cover_letter, contact_email, profile, status, created_at, updated_at, status_changed_at)
		VALUES ($1, $2, 'Письмо', 'a@example.ru', '{}', $3, now(), now(), now())`, vacancy, p.ID, status); err != nil {
		w.T.Fatal(err)
	}
}

func (w *world) addFavorite(p testkit.Person, ids ...uuid.UUID) {
	w.T.Helper()
	for _, id := range ids {
		if err := w.svc.AddFavorite(bg, p.User, id); err != nil {
			w.T.Fatalf("add favorite: %v", err)
		}
		w.clock.Advance(time.Second) // у каждой закладки свой момент: порядок в списке не зависит от случая
	}
}

// backdateFavorite передвигает момент добавления в избранное (так проверяется правило «не напоминать тем, кто добавил только что»).
func (w *world) backdateFavorite(p testkit.Person, vacancy uuid.UUID, at time.Time) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE favorites SET created_at = $3 WHERE user_id = $1 AND vacancy_id = $2`, p.ID, vacancy, at); err != nil {
		w.T.Fatal(err)
	}
}

func marker() string { return fmt.Sprintf("mk%d", testkit.Seq()) }

func notificationsOf(t *testing.T, who testkit.Person, kind string) int {
	t.Helper()
	return testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind = $2`, who.ID, kind)
}

func mailCount(t *testing.T, email string) int {
	t.Helper()
	return testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1`, email)
}

func ids(items []FavoriteItem) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, it := range items {
		out[i] = it.Vacancy.ID
	}
	return out
}

func sameIDs(t *testing.T, name string, got []uuid.UUID, want ...uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d вакансий, ожидали %d", name, len(got), len(want))
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: на месте %d вакансия %s, ожидали %s", name, i, got[i], want[i])
		}
	}
}

func intp(n int) *int { return &n }

func contextCancelled() (context.Context, context.CancelFunc) { return context.WithCancel(bg) }

func dbgenOn(db testkit.DB) *dbgen.Queries { return dbgen.New(db) }

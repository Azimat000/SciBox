package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/config"
	"scibox/server/internal/jobs"
	"scibox/server/internal/mail"
)

// recorder — общий «почтовый сервис» всех копий: запоминает, сколько писем ушло на каждый адрес.
type recorder struct {
	mu    sync.Mutex
	to    map[string]int
	total atomic.Int64
	delay time.Duration
}

func (r *recorder) Send(_ context.Context, m mail.Message) error {
	time.Sleep(r.delay) // письма уходят не мгновенно: копии успевают мешать друг другу
	r.mu.Lock()
	if r.to == nil {
		r.to = map[string]int{}
	}
	r.to[m.To]++
	r.mu.Unlock()
	r.total.Add(1)
	return nil
}

func (r *recorder) count(addr string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.to[addr]
}

// anyCount — сколько адресов получили письмо не один раз: «ни одного двойного письма».
func (r *recorder) repeated() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for a, n := range r.to {
		if n != 1 {
			out = append(out, fmt.Sprintf("%s ×%d", a, n))
		}
	}
	return out
}

// instance — одна копия сервера или воркера: свои сервисы и свой пул соединений, как у отдельного процесса.
type instance struct {
	svc     *services
	pool    *pgxpool.Pool
	handler http.Handler
}

func newInstance(t *testing.T, w *perimeterWorld, now func() time.Time, mailer mail.Sender) *instance {
	t.Helper()
	cfg, err := config.Load(func(k string) string { return w.env[k] })
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), w.env["DATABASE_URL"])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := newServices(cfg, pool, w.vault, slog.New(slog.NewTextHandler(io.Discard, nil)), now, mailer)
	return &instance{svc: svc, pool: pool, handler: svc.router(okHealth{}, nil)}
}

// startWorker запускает все задачи копии; stop мягко останавливает и ждёт, пока они вернутся.
func startWorker(t *testing.T, in *instance, holder string) (stop func()) {
	t.Helper()
	runner := jobs.New(in.pool, slog.New(slog.NewTextHandler(io.Discard, nil)), jobs.Config{Poll: 20 * time.Millisecond, Grace: 10 * time.Second, Holder: holder})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := runner.Run(ctx, in.svc.jobs()...); err != nil {
			t.Errorf("worker %s: %v", holder, err)
		}
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Errorf("worker %s did not stop", holder)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	return n
}

// TestSeveralCopiesNeverSendTwice — тест среза 25 (T-23): две копии сервера и две копии воркера на одной базе, тысяча
// событий (прямые письма, рассылки по поискам, напоминания о сроках, запросы к обеим копиям сервера); одну копию
// воркера по дороге останавливают. Ни одного двойного письма и уведомления, ни одного потерянного.
func TestSeveralCopiesNeverSendTwice(t *testing.T) {
	const (
		direct    = 500 // письма, поставленные прямо в очередь
		searches  = 200 // рассылки по сохранённым поискам
		reminders = 200 // напоминания о сроке
		requests  = 100 // «забыл пароль» через обе копии сервера
	)
	w := newPerimeterWorld(t)
	root := w.pool

	// Часы сервисов: сегодня 12:00 по Москве. Напоминания не уходят раньше девяти утра, и тест не должен зависеть от того,
	// когда его запустили.
	msk := time.FixedZone("MSK", 3*3600)
	real := time.Now().In(msk)
	clock := time.Date(real.Year(), real.Month(), real.Day(), 12, 0, 0, 0, msk).UTC()
	now := func() time.Time { return clock }

	// Люди: по одному аккаунту на событие (без пароля: входить им не нужно).
	total := searches + reminders + requests
	exec(t, root, `INSERT INTO users (email, display_name, password_hash, email_confirmed_at, privacy_consent_at, privacy_policy_version, created_at, updated_at)
		SELECT 'copies-user-' || g || '@example.test', 'Копия ' || g, '!', $1, $1, '2026-10-draft', $1, $1 FROM generate_series(0, $2::int - 1) g`, clock, total)
	// Рассылки: поиск по региону одной из опубликованных вакансий, вакансии «появились» час назад, граница проверки сутки назад.
	exec(t, root, `UPDATE vacancies SET published_at = $1::timestamptz - interval '1 hour' WHERE status = 'published'`, clock)
	exec(t, root, `INSERT INTO saved_searches (user_id, name, query, frequency, checked_at, next_run_at, created_at, updated_at)
		SELECT u.id, 'Поиск', 'region=' || (SELECT region_code FROM vacancies WHERE status = 'published' AND region_code IS NOT NULL LIMIT 1),
		       'instant', $1::timestamptz - interval '1 day', $1::timestamptz - interval '1 minute', $1, $1
		FROM users u WHERE u.email LIKE 'copies-user-%' AND substring(u.email from 'copies-user-(\d+)@')::int < $2`, clock, searches)
	// Напоминания: у одной вакансии срок через три дня; все эти люди добавили её в избранное давно.
	exec(t, root, `UPDATE vacancies SET deadline = ($1::timestamptz AT TIME ZONE 'Europe/Moscow')::date + 3
		WHERE id = (SELECT id FROM vacancies WHERE status = 'published' ORDER BY id LIMIT 1)`, clock)
	exec(t, root, `INSERT INTO favorites (user_id, vacancy_id, created_at)
		SELECT u.id, (SELECT id FROM vacancies WHERE status = 'published' ORDER BY id LIMIT 1), $1::timestamptz - interval '30 days'
		FROM users u WHERE u.email LIKE 'copies-user-%'
		  AND substring(u.email from 'copies-user-(\d+)@')::int >= $2 AND substring(u.email from 'copies-user-(\d+)@')::int < $3`, clock, searches, searches+reminders)

	mailer := &recorder{delay: 300 * time.Microsecond}
	serverA := newInstance(t, w, now, mailer)
	serverB := newInstance(t, w, now, mailer)
	workerA := newInstance(t, w, now, mailer)
	workerB := newInstance(t, w, now, mailer)

	// Прямые письма.
	var expect []string
	for i := range direct {
		addr := fmt.Sprintf("copies-direct-%d@example.test", i)
		expect = append(expect, addr)
		if err := serverA.svc.queue.Send(t.Context(), mail.Message{To: addr, Subject: "Прямое письмо", Body: "Текст"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range total {
		expect = append(expect, fmt.Sprintf("copies-user-%d@example.test", i))
	}

	stopA := startWorker(t, workerA, "worker-a")
	startWorker(t, workerB, "worker-b")

	// «Забыл пароль» через обе копии сервера одновременно: каждый запрос — одно письмо.
	var wg sync.WaitGroup
	reqErrs := make(chan string, requests)
	for i := range requests {
		wg.Go(func() {
			h := serverA.handler
			if i%2 == 1 {
				h = serverB.handler
			}
			email := fmt.Sprintf("copies-user-%d@example.test", searches+reminders+i)
			rec := send(h, req{
				method: http.MethodPost, path: "/api/auth/forgot-password", contentType: "application/json",
				body: strings.NewReader(fmt.Sprintf(`{"email":%q}`, email)), remote: fmt.Sprintf("198.51.100.%d:5000", i+1),
			})
			if rec.Code != http.StatusAccepted {
				reqErrs <- fmt.Sprintf("%s: %d %s", email, rec.Code, rec.Body.String())
			}
		})
	}

	// Одну копию воркера останавливают, пока очередь ещё полна: взятые, но не отправленные письма возвращаются.
	deadline := time.Now().Add(90 * time.Second)
	for mailer.total.Load() < 150 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	stopA()
	wg.Wait()
	close(reqErrs)
	for e := range reqErrs {
		t.Errorf("request failed: %s", e)
	}

	// Остальное доделывает вторая копия.
	want := int64(direct + total)
	for mailer.total.Load() < want && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := mailer.total.Load(); got != want {
		t.Fatalf("delivered %d letters, want %d", got, want)
	}
	time.Sleep(500 * time.Millisecond) // опоздавшие дубли успели бы появиться
	if got := mailer.total.Load(); got != want {
		t.Fatalf("after a pause %d letters were delivered, want %d: something was sent twice", got, want)
	}
	for _, addr := range expect {
		if n := mailer.count(addr); n != 1 {
			t.Fatalf("%s got %d letters, want exactly 1", addr, n)
		}
	}
	if rep := mailer.repeated(); len(rep) > 0 {
		t.Fatalf("addresses with a repeated or missing letter: %v", rep)
	}

	// Уведомления: ровно одно у каждого, кому оно положено (рассылка по поиску, напоминание); у «забыл пароль» их нет.
	if got := countRows(t, root, `SELECT count(*) FROM notifications n JOIN users u ON u.id = n.user_id WHERE u.email LIKE 'copies-user-%'`); got != searches+reminders {
		t.Errorf("notifications = %d, want %d", got, searches+reminders)
	}
	if got := countRows(t, root, `SELECT count(*) FROM (SELECT n.user_id FROM notifications n JOIN users u ON u.id = n.user_id
		WHERE u.email LIKE 'copies-user-%' GROUP BY n.user_id, n.kind HAVING count(*) > 1) d`); got != 0 {
		t.Errorf("%d people got the same notification twice", got)
	}
	if got := countRows(t, root, `SELECT count(*) FROM deadline_reminders r JOIN users u ON u.id = r.user_id WHERE u.email LIKE 'copies-user-%'`); got != reminders {
		t.Errorf("reminders = %d, want %d", got, reminders)
	}
	// Очередь: все письма отправлены, ни одно не отброшено и не осталось в ожидании.
	if got := countRows(t, root, `SELECT count(*) FROM outbox WHERE (to_email LIKE 'copies-%') AND sent_at IS NULL`); got != 0 {
		t.Errorf("%d letters of this test are not marked as sent", got)
	}
	if got := countRows(t, root, `SELECT count(*) FROM outbox WHERE failed_at IS NOT NULL`); got != 0 {
		t.Errorf("%d letters were given up", got)
	}
}

// Даже если расписание обойти (две копии зовут рассылку и очередь напрямую, много раз и одновременно), строки поиска,
// напоминания и письма берутся не больше одного раза: защита лежит в самих запросах, а не только в расписании.
func TestDirectCallsFromSeveralCopiesNeverDuplicate(t *testing.T) {
	const people = 120
	w := newPerimeterWorld(t)
	root := w.pool
	msk := time.FixedZone("MSK", 3*3600)
	real := time.Now().In(msk)
	clock := time.Date(real.Year(), real.Month(), real.Day(), 12, 0, 0, 0, msk).UTC()
	now := func() time.Time { return clock }

	exec(t, root, `INSERT INTO users (email, display_name, password_hash, email_confirmed_at, privacy_consent_at, privacy_policy_version, created_at, updated_at)
		SELECT 'direct-user-' || g || '@example.test', 'Прямой ' || g, '!', $1, $1, '2026-10-draft', $1, $1 FROM generate_series(0, $2::int - 1) g`, clock, 2*people)
	exec(t, root, `UPDATE vacancies SET published_at = $1::timestamptz - interval '1 hour' WHERE status = 'published'`, clock)
	exec(t, root, `INSERT INTO saved_searches (user_id, name, query, frequency, checked_at, next_run_at, created_at, updated_at)
		SELECT u.id, 'Поиск', 'region=' || (SELECT region_code FROM vacancies WHERE status = 'published' AND region_code IS NOT NULL LIMIT 1),
		       'instant', $1::timestamptz - interval '1 day', $1::timestamptz - interval '1 minute', $1, $1
		FROM users u WHERE u.email LIKE 'direct-user-%' AND substring(u.email from 'direct-user-(\d+)@')::int < $2`, clock, people)
	exec(t, root, `UPDATE vacancies SET deadline = ($1::timestamptz AT TIME ZONE 'Europe/Moscow')::date + 3
		WHERE id = (SELECT id FROM vacancies WHERE status = 'published' ORDER BY id LIMIT 1)`, clock)
	exec(t, root, `INSERT INTO favorites (user_id, vacancy_id, created_at)
		SELECT u.id, (SELECT id FROM vacancies WHERE status = 'published' ORDER BY id LIMIT 1), $1::timestamptz - interval '30 days'
		FROM users u WHERE u.email LIKE 'direct-user-%' AND substring(u.email from 'direct-user-(\d+)@')::int >= $2`, clock, people)

	mailer := &recorder{delay: 100 * time.Microsecond}
	copies := []*instance{newInstance(t, w, now, mailer), newInstance(t, w, now, mailer), newInstance(t, w, now, mailer)}

	var wg sync.WaitGroup
	for _, in := range copies {
		for range 2 {
			wg.Go(func() {
				if _, err := in.svc.matching.SendDigests(t.Context()); err != nil {
					t.Errorf("digests: %v", err)
				}
			})
			wg.Go(func() {
				if _, err := in.svc.matching.SendReminders(t.Context()); err != nil {
					t.Errorf("reminders: %v", err)
				}
			})
		}
	}
	wg.Wait()
	if got := countRows(t, root, `SELECT count(*) FROM notifications n JOIN users u ON u.id = n.user_id WHERE u.email LIKE 'direct-user-%'`); got != 2*people {
		t.Fatalf("notifications = %d, want %d", got, 2*people)
	}
	if got := countRows(t, root, `SELECT count(*) FROM (SELECT n.user_id FROM notifications n JOIN users u ON u.id = n.user_id
		WHERE u.email LIKE 'direct-user-%' GROUP BY n.user_id, n.kind HAVING count(*) > 1) d`); got != 0 {
		t.Fatalf("%d people got the same notification twice", got)
	}

	// Письма берут одновременно все копии в несколько потоков: каждое уходит один раз.
	for range 3 {
		for _, in := range copies {
			wg.Go(func() {
				if _, err := in.svc.outbox.Drain(t.Context()); err != nil {
					t.Errorf("drain: %v", err)
				}
			})
		}
	}
	wg.Wait()
	if _, err := copies[0].svc.outbox.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := range 2 * people {
		addr := fmt.Sprintf("direct-user-%d@example.test", i)
		if n := mailer.count(addr); n != 1 {
			t.Fatalf("%s got %d letters, want exactly 1", addr, n)
		}
	}
}

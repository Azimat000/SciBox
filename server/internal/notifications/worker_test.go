package notifications

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/jobs"
	"scibox/server/internal/mail"
	"scibox/server/internal/num"
	"scibox/server/internal/resilience"
	"scibox/server/internal/testkit"
)

// flaky — почтовый отправитель, который отвечает заданной ошибкой и запоминает письма.
type flaky struct {
	mu   sync.Mutex
	err  func(m mail.Message) error
	sent []mail.Message
	// calls считает все обращения, в том числе неудачные.
	calls int
	// tries считает обращения по адресам.
	tries map[string]int
}

func (f *flaky) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.tries == nil {
		f.tries = map[string]int{}
	}
	f.tries[m.To]++
	if f.err != nil {
		if err := f.err(m); err != nil {
			return err
		}
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *flaky) attempts(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tries[addr]
}

func (f *flaky) to(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.sent {
		if m.To == addr {
			n++
		}
	}
	return n
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// worker забирает только письма на свои адреса: тесты идут на общей базе и не мешают друг другу.
// Для этого отправитель по умолчанию не нужен, нужна лишь привязка к адресам: письма других тестов остаются нетронутыми.
func newWorker(f *flaky, clock *time.Time) *Worker {
	w := NewWorker(testkit.Pool, f, quiet)
	if clock != nil {
		w.now = func() time.Time { return *clock }
	}
	return w
}

func queueMails(t *testing.T, prefix string, n int) []string {
	t.Helper()
	q := NewQueue(testkit.Pool)
	var addrs []string
	for i := range n {
		a := fmt.Sprintf("%s%d-%d@example.ru", prefix, testkit.Seq(), i)
		if err := q.Send(bg, mail.Message{To: a, Subject: "Тема", Body: "Текст"}); err != nil {
			t.Fatal(err)
		}
		addrs = append(addrs, a)
	}
	return addrs
}

func rowOf(t *testing.T, addr string) (attempts int, sent, failed bool, lastErr string, next time.Time) {
	t.Helper()
	var s, f *time.Time
	err := testkit.Pool.QueryRow(bg, `SELECT attempts, sent_at, failed_at, last_error, next_attempt_at FROM outbox WHERE to_email = $1`, addr).Scan(&attempts, &s, &f, &lastErr, &next)
	if err != nil {
		t.Fatal(err)
	}
	return attempts, s != nil, f != nil, lastErr, next
}

// textOf — тема и тело письма, как они лежат в очереди.
func textOf(t *testing.T, addr string) (subject, body string) {
	t.Helper()
	if err := testkit.Pool.QueryRow(bg, `SELECT subject, body FROM outbox WHERE to_email = $1`, addr).Scan(&subject, &body); err != nil {
		t.Fatal(err)
	}
	return subject, body
}

// drain гоняет отправитель, пока в очереди остаётся что-то готовое; письма других тестов тоже уходят, но только в f.
func drain(t *testing.T, w *Worker) Stats {
	t.Helper()
	var total Stats
	for range 50 {
		st, err := w.RunOnce(bg)
		if err != nil {
			t.Fatal(err)
		}
		total.Sent += st.Sent
		total.Retry += st.Retry
		total.Failed += st.Failed
		if st.Sent+st.Retry+st.Failed == 0 {
			return total
		}
	}
	t.Fatal("outbox never drained")
	return total
}

func TestWorkerSendsEachMailOnce(t *testing.T) {
	addrs := queueMails(t, "once", 3)
	f := &flaky{}
	w := newWorker(f, nil)
	drain(t, w)
	drain(t, w) // второй проход ничего не должен отправить повторно
	for _, a := range addrs {
		if n := f.to(a); n != 1 {
			t.Errorf("%s: sent %d times", a, n)
		}
		if att, sent, failed, _, _ := rowOf(t, a); att != 1 || !sent || failed {
			t.Errorf("%s: attempts=%d sent=%v failed=%v", a, att, sent, failed)
		}
	}
	for _, m := range f.sent {
		if m.To == addrs[0] && (m.Subject != "Тема" || m.Body != "Текст") {
			t.Errorf("message = %+v", m)
		}
	}
	// T-19: в отправленном письме могла быть ссылка с живым токеном и чужие имена; в очереди остаётся только запись о факте.
	for _, a := range addrs {
		if subject, body := textOf(t, a); subject != "" || body != "" {
			t.Errorf("%s: the text of a sent mail stays in the queue: %q %q", a, subject, body)
		}
	}
}

func TestWorkerRetriesWithBackoffAndGivesUp(t *testing.T) {
	addr := queueMails(t, "retry", 1)[0]
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return errors.New("smtp: connection refused")
		}
		return nil
	}}
	clock := time.Now().UTC().Truncate(time.Microsecond)
	w := newWorker(f, &clock)

	for attempt := 1; attempt <= w.MaxAttempts; attempt++ {
		st := drain(t, w)
		att, sent, failed, lastErr, next := rowOf(t, addr)
		if att != attempt || sent {
			t.Fatalf("attempt %d: attempts=%d sent=%v", attempt, att, sent)
		}
		if attempt < w.MaxAttempts {
			if failed || st.Retry < 1 || !strings.Contains(lastErr, "connection refused") {
				t.Fatalf("attempt %d: failed=%v stats=%+v lastErr=%q", attempt, failed, st, lastErr)
			}
			// Пока письмо ждёт повтора, его текст нужен.
			if subject, body := textOf(t, addr); subject != "Тема" || body != "Текст" {
				t.Fatalf("attempt %d: a mail waiting for a retry lost its text: %q %q", attempt, subject, body)
			}
			want := clock.Add(DefaultBackoff(attempt))
			if next.Sub(want).Abs() > time.Second {
				t.Fatalf("attempt %d: next attempt at %v, want about %v", attempt, next, want)
			}
			// До срока письмо не трогают.
			tries := f.attempts(addr)
			drain(t, w)
			if f.attempts(addr) != tries {
				t.Fatalf("attempt %d: the mail was retried before its time", attempt)
			}
			clock = next.Add(time.Second)
		} else if !failed || st.Failed < 1 {
			t.Fatalf("last attempt: failed=%v stats=%+v", failed, st)
		}
	}
	if subject, body := textOf(t, addr); subject != "" || body != "" {
		t.Errorf("an undeliverable mail keeps its text: %q %q", subject, body)
	}
	// Окончательно недоставленное письмо больше не берут.
	clock = clock.Add(100 * 24 * time.Hour)
	drain(t, w)
	if att, _, failed, _, _ := rowOf(t, addr); att != w.MaxAttempts || !failed || f.attempts(addr) != w.MaxAttempts {
		t.Errorf("attempts=%d failed=%v tries=%d", att, failed, f.attempts(addr))
	}
	if f.to(addr) != 0 {
		t.Error("an undeliverable mail was reported as sent")
	}
}

func TestWorkerRetryThenSuccess(t *testing.T) {
	addr := queueMails(t, "flaky", 1)[0]
	fail := true
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr && fail {
			return errors.New("temporary")
		}
		return nil
	}}
	clock := time.Now().UTC().Truncate(time.Microsecond)
	w := newWorker(f, &clock)
	drain(t, w)
	if _, sent, _, _, _ := rowOf(t, addr); sent {
		t.Fatal("sent despite the failure")
	}
	fail = false
	clock = clock.Add(2 * time.Minute)
	drain(t, w)
	att, sent, failed, lastErr, _ := rowOf(t, addr)
	if !sent || failed || att != 2 || lastErr != "" || f.to(addr) != 1 {
		t.Errorf("attempts=%d sent=%v failed=%v lastErr=%q delivered=%d", att, sent, failed, lastErr, f.to(addr))
	}
}

func TestWorkerLeaseStopsAnotherSenderFromTakingTheSameMail(t *testing.T) {
	addr := queueMails(t, "lease", 1)[0]
	started := make(chan struct{})
	release := make(chan struct{})
	slow := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			close(started)
			<-release
		}
		return nil
	}}
	first := newWorker(slow, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		drain(t, first)
	}()
	<-started
	// Пока первый отправитель держит письмо, второй его не берёт.
	other := &flaky{}
	drain(t, newWorker(other, nil))
	if other.to(addr) != 0 {
		t.Error("the mail was taken by a second sender while the first one held it")
	}
	close(release)
	<-done
	if slow.to(addr) != 1 || other.to(addr) != 0 {
		t.Errorf("delivered: first=%d second=%d", slow.to(addr), other.to(addr))
	}
}

func TestTwoSendersShareTheQueueWithoutDuplicates(t *testing.T) {
	addrs := queueMails(t, "pair", 30)
	f1, f2 := &flaky{}, &flaky{}
	w1, w2 := newWorker(f1, nil), newWorker(f2, nil)
	w1.Batch, w2.Batch = 4, 4
	var wg sync.WaitGroup
	for _, w := range []*Worker{w1, w2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			drain(t, w)
		}()
	}
	wg.Wait()
	for _, a := range addrs {
		if n := f1.to(a) + f2.to(a); n != 1 {
			t.Errorf("%s delivered %d times", a, n)
		}
	}
}

func TestExpiredLeaseReturnsMailToQueue(t *testing.T) {
	// Отправитель взял письмо и «упал»: аренда истекает, и письмо уходит со следующим проходом.
	addr := queueMails(t, "crash", 1)[0]
	clock := time.Now().UTC().Truncate(time.Microsecond)
	crashed := newWorker(&flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return errors.New("process died")
		}
		return nil
	}}, &clock)
	// Берём письмо в аренду, не доходя до отметки: имитируем падение сразу после захвата.
	if _, err := crashed.q.ClaimOutbox(bg, claimParams(clock, crashed)); err != nil {
		t.Fatal(err)
	}
	healthy := &flaky{}
	w := newWorker(healthy, &clock)
	drain(t, w)
	if healthy.to(addr) != 0 {
		t.Fatal("mail on lease was sent early")
	}
	clock = clock.Add(w.Lease + time.Minute)
	drain(t, w)
	if healthy.to(addr) != 1 {
		t.Errorf("delivered %d times after the lease expired", healthy.to(addr))
	}
}

func TestLongErrorsAreShortened(t *testing.T) {
	addr := queueMails(t, "long", 1)[0]
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return errors.New(strings.Repeat("ошибка ", 200))
		}
		return nil
	}}
	drain(t, newWorker(f, nil))
	_, _, _, lastErr, _ := rowOf(t, addr)
	if n := len([]rune(lastErr)); n != maxErrorRunes {
		t.Errorf("last_error has %d runes", n)
	}
	if shorten("коротко") != "коротко" {
		t.Error("short text changed")
	}
}

func TestDefaultBackoff(t *testing.T) {
	cases := map[int]time.Duration{-1: time.Minute, 0: time.Minute, 1: time.Minute, 2: 5 * time.Minute, 3: 15 * time.Minute, 4: time.Hour, 5: 3 * time.Hour, 6: 6 * time.Hour, 7: 12 * time.Hour, 8: 12 * time.Hour, 50: 12 * time.Hour}
	for attempt, want := range cases {
		if got := DefaultBackoff(attempt); got != want {
			t.Errorf("DefaultBackoff(%d) = %v, want %v", attempt, got, want)
		}
	}
}

func TestCleanupRemovesOnlyOldFinishedMail(t *testing.T) {
	addrs := queueMails(t, "clean", 4)
	// 0 — отправлено давно, 1 — отправлено вчера, 2 — давно провалено, 3 — ждёт отправки.
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := testkit.Pool.Exec(bg, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE outbox SET sent_at = $2 WHERE to_email = $1`, addrs[0], old)
	exec(`UPDATE outbox SET sent_at = now() - interval '1 day' WHERE to_email = $1`, addrs[1])
	exec(`UPDATE outbox SET failed_at = $2 WHERE to_email = $1`, addrs[2], old)
	if err := newWorker(&flaky{}, nil).Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	var left []int
	for _, a := range addrs {
		left = append(left, testkit.Count(t, `SELECT count(*) FROM outbox WHERE to_email = $1`, a))
	}
	if fmt.Sprint(left) != "[0 1 0 1]" {
		t.Errorf("rows left: %v", left)
	}
}

func TestDrainTakesEveryBatchWithoutPauses(t *testing.T) {
	// Очередь больше пачки: Drain разбирает её целиком, не дожидаясь следующего прохода по таймеру.
	addrs := queueMails(t, "big", 9)
	f := &flaky{}
	w := newWorker(f, nil)
	w.Batch = 2
	st, err := w.Drain(bg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sent < 9 {
		t.Errorf("Drain sent %d, want at least 9", st.Sent)
	}
	for _, a := range addrs {
		if f.to(a) != 1 {
			t.Errorf("%s delivered %d times", a, f.to(a))
		}
	}
}

func TestDrainStopsOnDatabaseFailure(t *testing.T) {
	queueMails(t, "drainfault", 1)
	db, _ := testkit.Faulty(testkit.Pool, 1)
	w := NewWorker(db, &flaky{}, quiet)
	if _, err := w.Drain(bg); !errors.Is(err, testkit.ErrFault) {
		t.Fatalf("Drain error = %v, want the injected failure", err)
	}
}

// ---- вид сбоя решает судьбу письма (срез 25, D-168) ----

func kindErr(kind mail.Kind, text string) error {
	return &mail.Error{Kind: kind, Err: errors.New(text)}
}

func TestServiceFailureDoesNotSpendTheMailsAttempts(t *testing.T) {
	addr := queueMails(t, "svcdown", 1)[0]
	down := true
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr && down {
			return kindErr(mail.KindService, "connect refused")
		}
		return nil
	}}
	clock := time.Now().UTC().Truncate(time.Microsecond)
	w := newWorker(f, &clock)
	// Месяц сбоев почтовой службы: у письма по-прежнему нет потраченных попыток, и оно не отброшено.
	for range 40 {
		drain(t, w)
		clock = clock.Add(w.Backoff(1) + time.Second)
	}
	att, sent, failed, lastErr, _ := rowOf(t, addr)
	if att != 0 || sent || failed || lastErr != "connect refused" {
		t.Fatalf("row after the outage: attempts=%d sent=%v failed=%v err=%q; want an untouched waiting mail", att, sent, failed, lastErr)
	}
	if f.attempts(addr) < 40 {
		t.Errorf("service was asked %d times, want it asked on every pass", f.attempts(addr))
	}
	down = false
	drain(t, w)
	if _, sent, _, _, _ := rowOf(t, addr); !sent {
		t.Error("the mail was not sent once the service came back")
	}
}

func TestPermanentRefusalGivesUpAtOnce(t *testing.T) {
	addr := queueMails(t, "nosuchuser", 1)[0]
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return kindErr(mail.KindPermanent, "550 no such user")
		}
		return nil
	}}
	drain(t, newWorker(f, nil))
	att, _, failed, lastErr, _ := rowOf(t, addr)
	if att != 1 || !failed || lastErr != "550 no such user" {
		t.Fatalf("attempts=%d failed=%v err=%q; want given up after the first refusal", att, failed, lastErr)
	}
	if f.attempts(addr) != 1 {
		t.Errorf("refused address was tried %d times", f.attempts(addr))
	}
	if subject, body := textOf(t, addr); subject != "" || body != "" {
		t.Error("the text of an undelivered mail must be erased")
	}
}

func TestMailThatIsTooOldIsGivenUp(t *testing.T) {
	addr := queueMails(t, "stale", 1)[0]
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return kindErr(mail.KindService, "service is down")
		}
		return nil
	}}
	clock := time.Now().UTC().Truncate(time.Microsecond)
	w := newWorker(f, &clock)
	clock = clock.Add(w.MaxAge + time.Hour)
	drain(t, w)
	if _, _, failed, _, _ := rowOf(t, addr); !failed {
		t.Error("a mail older than MaxAge must be given up when it fails, however many attempts are left")
	}
}

// Остановка посреди пачки: отправляемое письмо дописывается, остальные возвращаются в очередь без траты попытки.
// Порядок писем внутри пачки не определён (база отдаёт строки как найдёт), поэтому «первое» — то, что пришло первым.
func TestShutdownFinishesTheMailInFlightAndReleasesTheRest(t *testing.T) {
	drain(t, newWorker(&flaky{}, nil)) // очередь пуста: пачка из шести писем состоит только из наших
	addrs := queueMails(t, "shutdown", 6)
	ctx, stop := context.WithCancel(bg)
	defer stop()
	var started sync.Once
	f := &flaky{err: func(m mail.Message) error {
		started.Do(stop) // сигнал остановки приходит, пока первое письмо отправляется
		return nil
	}}
	w := newWorker(f, nil)
	w.Batch = 6
	var st Stats
	var runErr error
	runner := jobs.New(testkit.Pool, quiet, jobs.Config{Grace: 5 * time.Second})
	err := runner.Run(ctx, jobs.Job{Name: fmt.Sprintf("outbox-shutdown-%d", testkit.Seq()), Every: time.Hour, Run: func(ctx context.Context) error {
		st, runErr = w.RunOnce(ctx)
		return runErr
	}})
	if err != nil || runErr != nil {
		t.Fatalf("Run: %v, RunOnce: %v", err, runErr)
	}
	if st.Sent != 1 || st.Released != 5 {
		t.Fatalf("stats = %+v: want the mail in flight sent and the other five released", st)
	}
	delivered := 0
	for _, a := range addrs {
		delivered += f.to(a)
		if f.to(a) == 1 {
			continue
		}
		att, sent, failed, _, next := rowOf(t, a)
		if att != 0 || sent || failed || next.After(time.Now().Add(time.Minute)) {
			t.Errorf("%s: attempts=%d sent=%v failed=%v next=%v; want a mail back in the queue with its attempt refunded", a, att, sent, failed, next)
		}
	}
	if delivered != 1 {
		t.Errorf("%d mails left during the shutdown, want exactly the one in flight", delivered)
	}
	// Другая копия забирает возвращённые письма сразу, без ожидания конца аренды, и никто не получает письмо дважды.
	other := &flaky{}
	drain(t, newWorker(other, nil))
	for _, a := range addrs {
		if n := f.to(a) + other.to(a); n != 1 {
			t.Errorf("%s delivered %d times, want exactly once", a, n)
		}
	}
}

func TestDrainingBeforeTheStartTakesNothing(t *testing.T) {
	addr := queueMails(t, "alreadystopping", 1)[0]
	ctx, cancel := context.WithCancel(bg)
	cancel()
	f := &flaky{}
	st, err := newWorker(f, nil).RunOnce(ctx)
	if err != nil || st != (Stats{}) {
		t.Fatalf("RunOnce under a stop signal = %+v, %v; want nothing", st, err)
	}
	if att, _, _, _, _ := rowOf(t, addr); att != 0 {
		t.Error("a stopping sender must not even claim mail")
	}
}

func TestMailIsNotStartedIfTheLeaseWouldEndBeforeItIsSent(t *testing.T) {
	drain(t, newWorker(&flaky{}, nil))
	addrs := queueMails(t, "shortlease", 3)
	clock := time.Now().UTC().Truncate(time.Microsecond)
	f := &flaky{}
	w := newWorker(f, &clock)
	w.Batch, w.Lease, w.SendTimeout = 3, 30*time.Second, 15*time.Second
	// Первое письмо «идёт» 20 секунд: до конца аренды остаётся меньше срока на отправку следующего.
	var first sync.Once
	f.err = func(mail.Message) error {
		first.Do(func() { clock = clock.Add(20 * time.Second) })
		return nil
	}
	st, err := w.RunOnce(bg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sent != 1 || st.Released != 2 {
		t.Fatalf("stats = %+v: want one mail sent and two released because the lease was about to end", st)
	}
	tried := 0
	for _, a := range addrs {
		tried += f.attempts(a)
	}
	if tried != 1 {
		t.Errorf("the service was asked %d times, want 1: the others must not be started", tried)
	}
}

func TestBreakerPausesTheQueueAndResumesAfterTheProbe(t *testing.T) {
	drain(t, newWorker(&flaky{}, nil))
	addrs := queueMails(t, "breaker", 5)
	clock := time.Now().UTC().Truncate(time.Microsecond)
	down := true
	f := &flaky{err: func(m mail.Message) error {
		if down {
			return kindErr(mail.KindService, "smtp is down")
		}
		return nil
	}}
	w := newWorker(f, &clock)
	w.Batch = 5
	w.Breaker = resilience.NewBreaker(resilience.BreakerConfig{Threshold: 2, Cooldown: time.Minute, Now: func() time.Time { return clock }})

	st, err := w.RunOnce(bg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Deferred != 2 || st.Released == 0 {
		t.Fatalf("stats = %+v: want two failures that open the breaker and the rest released unsent", st)
	}
	calls := f.calls
	// Пока предохранитель открыт, отправитель даже не берёт письма.
	st, err = w.RunOnce(bg)
	if err != nil || !st.Paused || f.calls != calls {
		t.Fatalf("second pass = %+v, %v, calls %d→%d: want a paused pass without calls", st, err, calls, f.calls)
	}
	for _, a := range addrs {
		if att, _, failed, _, _ := rowOf(t, a); att != 0 || failed {
			t.Errorf("%s: attempts=%d failed=%v; the outage must not spend attempts", a, att, failed)
		}
	}
	// Пауза кончилась, служба поднялась: проба проходит, очередь разбирается.
	down = false
	clock = clock.Add(2 * time.Minute)
	if _, err := w.Drain(bg); err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		if f.to(a) != 1 {
			t.Errorf("%s delivered %d times after the service came back", a, f.to(a))
		}
	}
}

// Отправку оборвала отмена контекста (остановка не дождалась): письмо возвращается, предохранитель этого не считает сбоем.
func TestAbortedSendIsNeitherAFailureNorALostMail(t *testing.T) {
	drain(t, newWorker(&flaky{}, nil))
	addr := queueMails(t, "aborted", 1)[0]
	ctx, cancel := context.WithCancel(bg)
	f := &flaky{err: func(m mail.Message) error {
		cancel()
		return kindErr(mail.KindService, "context canceled")
	}}
	w := newWorker(f, nil)
	w.Breaker = resilience.NewBreaker(resilience.BreakerConfig{Threshold: 1})
	st, err := w.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Released != 1 || st.Deferred != 0 || st.Failed != 0 {
		t.Errorf("stats = %+v, want the aborted mail released", st)
	}
	if w.Breaker.State() != resilience.Closed {
		t.Errorf("breaker is %v: an aborted send says nothing about the service", w.Breaker.State())
	}
	if att, sent, failed, _, _ := rowOf(t, addr); att != 0 || sent || failed {
		t.Errorf("row: attempts=%d sent=%v failed=%v; want the mail waiting with its attempt refunded", att, sent, failed)
	}
}

func TestMailRefusalsDoNotOpenTheBreaker(t *testing.T) {
	queueMails(t, "alive", 4)
	f := &flaky{err: func(mail.Message) error { return kindErr(mail.KindPermanent, "550 no such user") }}
	w := newWorker(f, nil)
	w.Breaker = resilience.NewBreaker(resilience.BreakerConfig{Threshold: 2})
	drain(t, w)
	if w.Breaker.State() != resilience.Closed {
		t.Errorf("breaker is %v: an answering service that refuses letters is alive", w.Breaker.State())
	}
}

func TestQueueStats(t *testing.T) {
	// Состояние читается по всей очереди: общая база теста не пуста, поэтому сравниваем приращения.
	w := newWorker(&flaky{}, nil)
	drain(t, w)
	before, err := w.QueueStats(bg)
	if err != nil {
		t.Fatal(err)
	}
	waiting := queueMails(t, "stats", 3)
	after, err := w.QueueStats(bg)
	if err != nil {
		t.Fatal(err)
	}
	if after.Waiting-before.Waiting != 3 || after.Retrying != before.Retrying {
		t.Errorf("stats %+v → %+v: want three more waiting mails", before, after)
	}
	if after.OldestWaiting < 0 || after.OldestWaiting > time.Hour {
		t.Errorf("OldestWaiting = %v, want a small positive age", after.OldestWaiting)
	}
	// Одно письмо отказали навсегда: оно в «недоставлено», а не в «ждёт».
	w2 := newWorker(&flaky{err: func(m mail.Message) error {
		if m.To == waiting[0] {
			return kindErr(mail.KindPermanent, "550")
		}
		return errors.New("later")
	}}, nil)
	w2.Batch = 1
	if _, err := w2.RunOnce(bg); err != nil {
		t.Fatal(err)
	}
	empty := newWorker(&flaky{}, nil)
	drain(t, empty)
	end, err := empty.QueueStats(bg)
	if err != nil {
		t.Fatal(err)
	}
	if end.Waiting != 0 || end.OldestWaiting != 0 || end.Undelivered < 1 {
		t.Errorf("stats after draining = %+v: want an empty queue and at least one undelivered mail", end)
	}
}

func TestStatsHandled(t *testing.T) {
	if got := (Stats{Sent: 1, Retry: 2, Failed: 3, Deferred: 4, Released: 5}).Handled(); got != 10 {
		t.Errorf("Handled() = %d, want 10 (released mails were not handled)", got)
	}
}

func claimParams(now time.Time, w *Worker) dbgen.ClaimOutboxParams {
	return dbgen.ClaimOutboxParams{LeaseUntil: now.Add(w.Lease), Now: now, Batch: num.Int32(w.Batch)}
}

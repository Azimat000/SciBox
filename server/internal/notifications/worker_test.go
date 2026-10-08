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
	"scibox/server/internal/mail"
	"scibox/server/internal/num"
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
}

func TestWorkerRetriesWithBackoffAndGivesUp(t *testing.T) {
	addr := queueMails(t, "retry", 1)[0]
	f := &flaky{err: func(m mail.Message) error {
		if m.To == addr {
			return errors.New("smtp: connection refused")
		}
		return nil
	}}
	clock := time.Now().UTC()
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
	clock := time.Now().UTC()
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
	clock := time.Now().UTC()
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

func TestRunLoopSendsAndStops(t *testing.T) {
	addr := queueMails(t, "loop", 1)[0]
	f := &flaky{}
	w := newWorker(f, nil)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx, 20*time.Millisecond)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for f.to(addr) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// Письмо, поставленное уже во время работы, тоже уходит.
	late := queueMails(t, "late", 1)[0]
	for f.to(late) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	if f.to(addr) != 1 || f.to(late) != 1 {
		t.Errorf("delivered: first=%d late=%d", f.to(addr), f.to(late))
	}
}

func TestRunKeepsGoingAfterBatchIsFull(t *testing.T) {
	// Очередь больше пачки: Run не ждёт паузы между пачками.
	addrs := queueMails(t, "big", 9)
	f := &flaky{}
	w := newWorker(f, nil)
	w.Batch = 2
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx, time.Hour) // пауза в час: за неё без «сразу следующей пачки» ничего бы не ушло
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		all := true
		for _, a := range addrs {
			all = all && f.to(a) == 1
		}
		if all {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	for _, a := range addrs {
		if f.to(a) != 1 {
			t.Errorf("%s delivered %d times", a, f.to(a))
		}
	}
}

func claimParams(now time.Time, w *Worker) dbgen.ClaimOutboxParams {
	return dbgen.ClaimOutboxParams{LeaseUntil: now.Add(w.Lease), Now: now, Batch: num.Int32(w.Batch)}
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(bg, d)
}

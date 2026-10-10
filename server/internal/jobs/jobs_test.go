package jobs_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/jobs"
	"scibox/server/internal/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.RunMain(m)) }

// clock — часы теста: время стоит, пока тест его не сдвинет. Начинается с целой микросекунды: PostgreSQL хранит
// микросекунды, а Go даёт наносекунды.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Now().UTC().Truncate(time.Microsecond)}
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

// logBuf — журнал теста в памяти.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newLog() (*slog.Logger, *logBuf) {
	b := &logBuf{}
	return slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})), b
}

func name(prefix string) string { return fmt.Sprintf("%s-%d", prefix, testkit.Seq()) }

func newRunner(db jobs.DB, c *clock, holder string) (*jobs.Runner, *logBuf) {
	log, buf := newLog()
	cfg := jobs.Config{Poll: 5 * time.Millisecond, Grace: 300 * time.Millisecond, MaxBackoff: time.Minute, Holder: holder}
	if c != nil {
		cfg.Now = c.Now
	}
	return jobs.New(db, log, cfg), buf
}

// start запускает исполнитель в фоне; stop останавливает и ждёт возврата.
func start(t *testing.T, r *jobs.Runner, list ...jobs.Job) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, list...) }()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("Run returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("Run did not return after cancel")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// maybeRow читает строку расписания; ok == false, пока копия её не создала.
func maybeRow(t *testing.T, jobName string) (r dbgen.JobRun, ok bool) {
	t.Helper()
	rows, err := dbgen.New(testkit.Pool).ListJobRuns(testkit.BG)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == jobName {
			return r, true
		}
	}
	return dbgen.JobRun{}, false
}

func row(t *testing.T, jobName string) dbgen.JobRun {
	t.Helper()
	r, ok := maybeRow(t, jobName)
	if !ok {
		t.Fatalf("no schedule row for %q", jobName)
	}
	return r
}

// waitRow ждёт, пока строка расписания станет такой, как нужно.
func waitRow(t *testing.T, what, jobName string, cond func(dbgen.JobRun) bool) {
	t.Helper()
	waitFor(t, what, func() bool {
		r, ok := maybeRow(t, jobName)
		return ok && cond(r)
	})
}

// counter считает проходы задачи и самое большое число одновременных.
type counter struct {
	runs, cur, peak atomic.Int64
}

func (c *counter) enter() {
	n := c.cur.Add(1)
	for p := c.peak.Load(); n > p && !c.peak.CompareAndSwap(p, n); p = c.peak.Load() {
	}
	c.runs.Add(1)
}
func (c *counter) leave() { c.cur.Add(-1) }

func TestClusterJobRunsOncePerIntervalAcrossCopies(t *testing.T) {
	clk := newClock()
	jobName := name("once-per-interval")
	var c counter
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error {
		c.enter()
		defer c.leave()
		return nil
	}}
	a, _ := newRunner(testkit.Pool, clk, "copy-a")
	b, _ := newRunner(testkit.Pool, clk, "copy-b")
	start(t, a, job)
	start(t, b, job)

	waitFor(t, "the first run", func() bool { return c.runs.Load() == 1 })
	time.Sleep(100 * time.Millisecond) // обе копии много раз спрашивали расписание
	if got := c.runs.Load(); got != 1 {
		t.Fatalf("runs = %d, want 1: the copies must share the schedule", got)
	}

	clk.Advance(59 * time.Minute)
	time.Sleep(50 * time.Millisecond)
	if got := c.runs.Load(); got != 1 {
		t.Fatalf("runs = %d after 59 minutes, want 1", got)
	}
	clk.Advance(2 * time.Minute)
	waitFor(t, "the second run", func() bool { return c.runs.Load() == 2 })
	time.Sleep(100 * time.Millisecond)
	if got := c.runs.Load(); got != 2 {
		t.Fatalf("runs = %d, want 2: one run per interval for the whole cluster", got)
	}
	if got := row(t, jobName); got.Failures != 0 || got.Runs != 2 || got.Holder != "" {
		t.Errorf("schedule row = %+v, want 2 runs, no failures, no holder", got)
	}
}

func TestClusterJobIsNeverRunByTwoCopiesAtOnce(t *testing.T) {
	jobName := name("exclusive")
	var c counter
	job := jobs.Job{Name: jobName, Every: time.Millisecond, Cluster: true, Run: func(context.Context) error {
		c.enter()
		defer c.leave()
		time.Sleep(20 * time.Millisecond) // дольше паузы между проходами: любая копия успела бы вклиниться
		return nil
	}}
	var stops []func()
	for i := range 4 {
		r, _ := newRunner(testkit.Pool, nil, fmt.Sprintf("copy-%d", i))
		stops = append(stops, start(t, r, job))
	}
	waitFor(t, "several runs", func() bool { return c.runs.Load() >= 5 })
	for _, s := range stops {
		s()
	}
	if got := c.peak.Load(); got != 1 {
		t.Fatalf("peak concurrent runs = %d, want 1", got)
	}
}

func TestFailureBacksOffForAllCopiesAndRecovers(t *testing.T) {
	clk := newClock()
	jobName := name("flaky")
	var fail atomic.Bool
	fail.Store(true)
	var runs atomic.Int64
	job := jobs.Job{Name: jobName, Every: 5 * time.Minute, Cluster: true, Run: func(context.Context) error {
		runs.Add(1)
		if fail.Load() {
			return errors.New("crossref is down")
		}
		return nil
	}}
	a, logA := newRunner(testkit.Pool, clk, "copy-a")
	b, logB := newRunner(testkit.Pool, clk, "copy-b")
	start(t, a, job)
	start(t, b, job)

	waitRow(t, "the first failure", jobName, func(r dbgen.JobRun) bool { return r.Failures == 1 })
	first := row(t, jobName)
	if first.LastError != "crossref is down" {
		t.Errorf("last_error = %q", first.LastError)
	}
	// Пауза после первого сбоя короткая (30 секунд), а не пять минут, и общая для обеих копий.
	if got, want := first.NextRunAt.Sub(clk.Now()), jobs.Backoff(1, 5*time.Minute, time.Minute); got != want || got != 30*time.Second {
		t.Errorf("pause after the first failure = %v (want %v = 30s)", got, want)
	}
	time.Sleep(60 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("runs = %d, want 1: nobody may retry before the pause ends", got)
	}

	clk.Advance(31 * time.Second)
	waitRow(t, "the second failure", jobName, func(r dbgen.JobRun) bool { return r.Failures == 2 })
	if got, want := row(t, jobName).NextRunAt.Sub(clk.Now()), time.Minute; got != want {
		t.Errorf("pause after the second failure = %v, want %v (doubles)", got, want)
	}
	if runs.Load() != 2 {
		t.Fatalf("runs = %d, want 2", runs.Load())
	}

	fail.Store(false)
	clk.Advance(61 * time.Second)
	waitRow(t, "recovery", jobName, func(r dbgen.JobRun) bool { return r.Failures == 0 && r.LastOkAt != nil })
	if got := row(t, jobName).NextRunAt.Sub(clk.Now()); got != 5*time.Minute {
		t.Errorf("pause after success = %v, want the normal 5m", got)
	}
	waitFor(t, "the log line about recovery", func() bool {
		return strings.Contains(logA.String(), "job recovered") || strings.Contains(logB.String(), "job recovered")
	})
	var st jobs.Status
	for _, s := range append(a.Snapshot(), b.Snapshot()...) {
		if s.Name == jobName && s.Runs > 0 {
			st = s
		}
	}
	if st.Name == "" || !st.Cluster {
		t.Errorf("snapshot has no entry for the job: %+v", st)
	}
}

func TestCrashedCopyDoesNotHoldTheJobForever(t *testing.T) {
	clk := newClock()
	jobName := name("crashed")
	// Копия «упала» посреди прохода: строка занята, аренда кончится через 10 минут.
	q := dbgen.New(testkit.Pool)
	if _, err := q.ClaimJob(testkit.BG, dbgen.ClaimJobParams{Name: jobName, Now: clk.Now(), LeasedUntil: clk.Now().Add(10 * time.Minute), Holder: "dead-copy"}); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int64
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error { runs.Add(1); return nil }}
	r, _ := newRunner(testkit.Pool, clk, "alive")
	start(t, r, job)

	time.Sleep(60 * time.Millisecond)
	if runs.Load() != 0 {
		t.Fatal("the job was taken while the dead copy's lease was still valid")
	}
	clk.Advance(11 * time.Minute)
	waitFor(t, "takeover after the lease", func() bool { return runs.Load() == 1 })
	if got := row(t, jobName); got.Runs != 2 {
		t.Errorf("runs = %d, want 2 (dead copy + takeover)", got.Runs)
	}
}

func TestShutdownLetsTheJobFinishItsPieceAndRecordsSuccess(t *testing.T) {
	clk := newClock()
	jobName := name("graceful")
	entered := make(chan struct{})
	var drained atomic.Bool
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(ctx context.Context) error {
		close(entered)
		for !jobs.Draining(ctx) {
			time.Sleep(time.Millisecond)
		}
		drained.Store(true)
		time.Sleep(30 * time.Millisecond) // дописывает текущую порцию, контекст при этом не отменён
		return ctx.Err()
	}}
	r, _ := newRunner(testkit.Pool, clk, "copy")
	stop := start(t, r, job)
	<-entered
	stop()
	if !drained.Load() {
		t.Fatal("the job never saw the shutdown signal")
	}
	got := row(t, jobName)
	if got.Failures != 0 || got.LastOkAt == nil || got.Holder != "" {
		t.Errorf("row after a graceful stop = %+v, want success and no holder", got)
	}
}

func TestInterruptedJobIsReleasedWithoutCountingAsAFailure(t *testing.T) {
	clk := newClock()
	jobName := name("interrupted")
	entered := make(chan struct{})
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(ctx context.Context) error {
		close(entered)
		<-ctx.Done() // не обращает внимания на Draining: ждёт принудительной отмены после Grace
		return ctx.Err()
	}}
	r, log := newRunner(testkit.Pool, clk, "copy")
	stop := start(t, r, job)
	<-entered
	began := time.Now()
	stop()
	if waited := time.Since(began); waited < 250*time.Millisecond {
		t.Errorf("the job was cut after %v, want it to get the grace period (300ms)", waited)
	}
	got := row(t, jobName)
	if got.Failures != 0 || got.Holder != "" || got.LeasedUntil != nil {
		t.Errorf("row = %+v: an interrupted job must be released, not failed", got)
	}
	if !got.NextRunAt.Equal(clk.Now()) {
		t.Errorf("next_run_at = %v, want now (another copy takes it at once)", got.NextRunAt)
	}
	if !strings.Contains(log.String(), "interrupted by shutdown") {
		t.Errorf("log does not mention the interruption:\n%s", log)
	}
	// Другая копия берёт возвращённую задачу сразу.
	var runs atomic.Int64
	other, _ := newRunner(testkit.Pool, clk, "other")
	start(t, other, jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error { runs.Add(1); return nil }})
	waitFor(t, "takeover of the released job", func() bool { return runs.Load() == 1 })
}

func TestJobThatOverrunsItsLeaseFails(t *testing.T) {
	clk := newClock()
	jobName := name("overrun")
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Lease: 40 * time.Millisecond, Run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	r, _ := newRunner(testkit.Pool, clk, "copy")
	start(t, r, job)
	waitRow(t, "failure by lease timeout", jobName, func(r dbgen.JobRun) bool { return r.Failures == 1 })
	if got := row(t, jobName).LastError; !strings.Contains(got, "deadline") {
		t.Errorf("last_error = %q, want a deadline error", got)
	}
}

func TestPanicIsRecoveredAndRecorded(t *testing.T) {
	clk := newClock()
	jobName := name("panics")
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error { panic("boom") }}
	r, log := newRunner(testkit.Pool, clk, "copy")
	start(t, r, job)
	waitRow(t, "the panic to be recorded", jobName, func(r dbgen.JobRun) bool { return r.Failures == 1 })
	if got := row(t, jobName).LastError; !strings.Contains(got, "panicked: boom") {
		t.Errorf("last_error = %q", got)
	}
	if !strings.Contains(log.String(), "job panicked") {
		t.Errorf("log has no panic record:\n%s", log)
	}
}

func TestLocalJobsRunInEveryCopyAndBackOffAfterFailures(t *testing.T) {
	var okRuns, badRuns atomic.Int64
	good := jobs.Job{Name: name("local-ok"), Every: 5 * time.Millisecond, Run: func(context.Context) error { okRuns.Add(1); return nil }}
	bad := jobs.Job{Name: name("local-bad"), Every: 5 * time.Millisecond, Run: func(context.Context) error {
		badRuns.Add(1)
		return errors.New("smtp is down")
	}}
	r, log := newRunner(testkit.Pool, nil, "copy")
	stop := start(t, r, good, bad)
	waitFor(t, "several runs of the good job", func() bool { return okRuns.Load() >= 5 })
	time.Sleep(150 * time.Millisecond)
	stop()
	if okRuns.Load() < 5 {
		t.Errorf("the good job ran %d times", okRuns.Load())
	}
	// Без нарастающей паузы плохая задача сделала бы за это время десятки проходов.
	if got := badRuns.Load(); got < 2 || got > 12 {
		t.Errorf("the failing job ran %d times, want a few with growing pauses", got)
	}
	statuses := r.Snapshot()
	if len(statuses) != 2 || statuses[0].Name > statuses[1].Name {
		t.Fatalf("Snapshot() = %+v, want two entries sorted by name", statuses)
	}
	for _, s := range statuses {
		if s.Name == bad.Name && (s.Failures < 2 || s.LastError != "smtp is down" || s.Running) {
			t.Errorf("failing job status = %+v", s)
		}
		if s.Name == good.Name && (s.Failures != 0 || s.LastOK.IsZero() || s.Cluster) {
			t.Errorf("good job status = %+v", s)
		}
	}
	if !strings.Contains(log.String(), "job keeps failing") {
		t.Errorf("a series of failures must be logged as an error:\n%s", log)
	}
	// Локальные задачи не ведут строку в расписании кластера.
	rows, _ := dbgen.New(testkit.Pool).ListJobRuns(testkit.BG)
	for _, r := range rows {
		if r.Name == good.Name || r.Name == bad.Name {
			t.Errorf("local job %q has a cluster schedule row", r.Name)
		}
	}
}

func TestScheduleDatabaseFailuresAreLoggedAndRetried(t *testing.T) {
	for n := int32(1); n <= 3; n++ {
		db, calls := testkit.Faulty(testkit.Pool, n)
		jobName := name(fmt.Sprintf("fault-%d", n))
		var runs atomic.Int64
		job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error { runs.Add(1); return nil }}
		r, log := newRunner(db, nil, "copy")
		stop := start(t, r, job)
		// Сбой при занятии задачи (1) или при записи итога (2): копия не падает и доводит дело до конца.
		waitFor(t, "the job to run at least once and settle", func() bool { return runs.Load() >= 1 && calls.Load() >= n+1 })
		stop()
		if n == 1 && !strings.Contains(log.String(), "job schedule is unavailable") {
			t.Errorf("fault %d: schedule failure is not logged:\n%s", n, log)
		}
		if n == 2 && !strings.Contains(log.String(), "job result was not recorded") {
			t.Errorf("fault %d: failure to record the result is not logged:\n%s", n, log)
		}
	}
}

func TestLostLeaseIsReported(t *testing.T) {
	clk := newClock()
	jobName := name("lost-lease")
	entered, resume := make(chan struct{}), make(chan struct{})
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Lease: time.Minute, Run: func(context.Context) error {
		close(entered)
		<-resume
		return nil
	}}
	r, log := newRunner(testkit.Pool, clk, "slow")
	start(t, r, job)
	<-entered
	// Пока копия работала, строку забрал другой (аренда по часам теста кончилась).
	clk.Advance(2 * time.Minute)
	if _, err := dbgen.New(testkit.Pool).ClaimJob(testkit.BG, dbgen.ClaimJobParams{Name: jobName, Now: clk.Now().Add(time.Hour), LeasedUntil: clk.Now().Add(2 * time.Hour), Holder: "thief"}); err != nil {
		t.Fatal(err)
	}
	close(resume)
	waitFor(t, "the lost lease to be logged", func() bool { return strings.Contains(log.String(), "lease was lost") })
	if got := row(t, jobName); got.Holder != "thief" {
		t.Errorf("holder = %q: the slow copy must not overwrite the new holder's row", got.Holder)
	}
}

func TestRunRejectsBrokenJobDescriptions(t *testing.T) {
	ok := func(context.Context) error { return nil }
	tests := []struct {
		name string
		jobs []jobs.Job
		want string
	}{
		{"no name", []jobs.Job{{Every: time.Second, Run: ok}}, "no name"},
		{"blank name", []jobs.Job{{Name: "  ", Every: time.Second, Run: ok}}, "no name"},
		{"duplicate", []jobs.Job{{Name: "a", Every: time.Second, Run: ok}, {Name: "a", Every: time.Second, Run: ok}}, "duplicate"},
		{"no interval", []jobs.Job{{Name: "a", Run: ok}}, "no interval"},
		{"negative interval", []jobs.Job{{Name: "a", Every: -time.Second, Run: ok}}, "no interval"},
		{"no run", []jobs.Job{{Name: "a", Every: time.Second}}, "no Run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newRunner(testkit.Pool, nil, "copy")
			err := r.Run(t.Context(), tt.jobs...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		every    time.Duration
		max      time.Duration
		want     time.Duration
	}{
		{"no failures keeps the interval", 0, 5 * time.Minute, 15 * time.Minute, 5 * time.Minute},
		{"first failure of an hourly job is retried in 30s", 1, time.Hour, 15 * time.Minute, 30 * time.Second},
		{"doubles", 3, time.Hour, 15 * time.Minute, 2 * time.Minute},
		{"capped by the maximum", 20, time.Hour, 15 * time.Minute, 15 * time.Minute},
		{"short loop starts at its own interval", 1, 2 * time.Second, 15 * time.Minute, 2 * time.Second},
		{"short loop is capped at a minute", 20, 2 * time.Second, 15 * time.Minute, time.Minute},
		{"five-minute job never waits longer than five minutes", 20, 5 * time.Minute, 15 * time.Minute, 5 * time.Minute},
		{"a tiny maximum is raised to a minute", 20, time.Hour, time.Second, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jobs.Backoff(tt.failures, tt.every, tt.max); got != tt.want {
				t.Errorf("Backoff(%d, %v, %v) = %v, want %v", tt.failures, tt.every, tt.max, got, tt.want)
			}
		})
	}
}

func TestConfigNormalizedNeverMeansNoLimit(t *testing.T) {
	got := jobs.Config{Poll: -1, Grace: -time.Second, MaxBackoff: -time.Hour}.Normalized()
	if got.Poll <= 0 || got.Grace <= 0 || got.MaxBackoff <= 0 || got.Holder == "" || got.Now == nil {
		t.Errorf("Normalized() = %+v: every field must get a usable value", got)
	}
	r := jobs.New(testkit.Pool, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), jobs.Config{})
	if r.Holder() == "" {
		t.Error("Holder() is empty")
	}
}

func TestDrainingIsFalseOutsideTheRunner(t *testing.T) {
	if jobs.Draining(context.Background()) {
		t.Error("Draining() = true for a plain context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !jobs.Draining(ctx) {
		t.Error("Draining() = false for a cancelled context")
	}
}

func TestLongErrorsAreShortenedInTheSchedule(t *testing.T) {
	jobName := name("long-error")
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error {
		return errors.New(strings.Repeat("я", 900))
	}}
	r, _ := newRunner(testkit.Pool, newClock(), "copy")
	start(t, r, job)
	waitRow(t, "the failure", jobName, func(r dbgen.JobRun) bool { return r.Failures == 1 })
	if got := len([]rune(row(t, jobName).LastError)); got != 500 {
		t.Errorf("last_error has %d runes, want 500", got)
	}
}

// Текст ошибки хранится в базе и показывается в снимке: почты и адреса в нём скрыты так же, как в журнале.
func TestErrorTextInTheScheduleIsMasked(t *testing.T) {
	jobName := name("masked")
	job := jobs.Job{Name: jobName, Every: time.Hour, Cluster: true, Run: func(context.Context) error {
		return errors.New("send to ivan.petrov@example.ru from 203.0.113.77 failed")
	}}
	r, _ := newRunner(testkit.Pool, newClock(), "copy")
	start(t, r, job)
	waitRow(t, "the failure", jobName, func(r dbgen.JobRun) bool { return r.Failures == 1 })
	got := row(t, jobName).LastError
	if strings.Contains(got, "ivan.petrov") || strings.Contains(got, "203.0.113.77") {
		t.Errorf("last_error leaks personal data: %q", got)
	}
	for _, s := range r.Snapshot() {
		if s.Name == jobName && (strings.Contains(s.LastError, "ivan.petrov") || strings.Contains(s.LastError, "203.0.113.77")) {
			t.Errorf("Snapshot leaks personal data: %q", s.LastError)
		}
	}
}

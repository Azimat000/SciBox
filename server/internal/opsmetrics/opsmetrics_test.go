package opsmetrics_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"scibox/server/internal/errreport"
	"scibox/server/internal/jobs"
	"scibox/server/internal/notifications"
	"scibox/server/internal/opsmetrics"
	"scibox/server/internal/resilience"
	"scibox/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var now = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

type values map[string]float64

// gather собирает реестр в карту «имя{метки}» → значение.
func gather(t *testing.T, reg *prometheus.Registry) values {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := values{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := f.GetName()
			var labels []string
			for _, l := range m.GetLabel() {
				labels = append(labels, l.GetName()+"="+l.GetValue())
			}
			if len(labels) > 0 {
				key += "{" + strings.Join(labels, ",") + "}"
			}
			switch {
			case m.Gauge != nil:
				out[key] = m.GetGauge().GetValue()
			case m.Counter != nil:
				out[key] = m.GetCounter().GetValue()
			}
		}
	}
	return out
}

func register(t *testing.T, src opsmetrics.Sources) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	if src.Now == nil {
		src.Now = func() time.Time { return now }
	}
	if err := opsmetrics.Register(reg, src); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestEverySourceIsExported(t *testing.T) {
	pool := testdb.New(t)
	breaker := resilience.NewBreaker(resilience.BreakerConfig{Threshold: 1, Cooldown: time.Minute, MaxCooldown: time.Minute})
	done, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	done(resilience.Failure) // порог 1: предохранитель открыт
	limiter := resilience.NewLimiter(2, time.Millisecond)
	release, _ := limiter.Acquire(t.Context())
	defer release()

	var seenMail atomic.Int32
	reg := register(t, opsmetrics.Sources{
		Pool: pool,
		Mail: func(context.Context) (notifications.QueueStats, error) {
			seenMail.Add(1)
			return notifications.QueueStats{Waiting: 7, Retrying: 2, Undelivered: 1, OldestWaiting: 90 * time.Second}, nil
		},
		Files: func(context.Context) (opsmetrics.FileQueues, error) {
			return opsmetrics.FileQueues{Pending: 3, OldestPending: now.Add(-10 * time.Minute), Deletions: 4, Legacy: 5}, nil
		},
		Database: func(context.Context) (opsmetrics.DatabaseStats, error) {
			return opsmetrics.DatabaseStats{SizeBytes: 1 << 20, Connections: 12, MaxConnections: 100}, nil
		},
		Cluster: func(context.Context) ([]opsmetrics.ClusterJob, error) {
			return []opsmetrics.ClusterJob{
				{Name: "retention", LastOK: now.Add(-30 * time.Minute), Failures: 0},
				{Name: "matching.digests", LastOK: now.Add(-time.Hour), Failures: 3, Running: true},
				{Name: "not-in-definitions", Failures: 9},
			}, nil
		},
		JobDefs: []jobs.Job{
			{Name: "retention", Every: time.Hour, Cluster: true},
			{Name: "matching.digests", Every: 5 * time.Minute, Cluster: true},
			{Name: "files.sweep", Every: 6 * time.Hour, Cluster: true}, // ещё ни разу не занята: строки в таблице нет
			{Name: "outbox.send", Every: 2 * time.Second},
		},
		Breakers: map[string]*resilience.Breaker{"mail": breaker},
		Limiters: map[string]*resilience.Limiter{"crossref": limiter},
	})
	got := gather(t, reg)

	want := values{
		"scibox_mail_queue_messages{state=waiting}":                  7,
		"scibox_mail_queue_messages{state=retrying}":                 2,
		"scibox_mail_queue_messages{state=undelivered}":              1,
		"scibox_mail_queue_oldest_waiting_seconds":                   90,
		"scibox_files_pending_scan":                                  3,
		"scibox_files_oldest_pending_scan_seconds":                   600,
		"scibox_files_blob_deletions_queued":                         4,
		"scibox_files_legacy_in_database":                            5,
		"scibox_db_size_bytes":                                       1 << 20,
		"scibox_db_server_connections":                               12,
		"scibox_db_server_max_connections":                           100,
		"scibox_job_interval_seconds{task=retention}":                 3600,
		"scibox_job_interval_seconds{task=outbox.send}":               2,
		"scibox_job_last_success_timestamp_seconds{task=retention}":   float64(now.Add(-30 * time.Minute).Unix()),
		"scibox_job_last_success_timestamp_seconds{task=files.sweep}": 0,
		"scibox_job_consecutive_failures{task=matching.digests}":      3,
		"scibox_job_running{task=matching.digests}":                   1,
		"scibox_job_running{task=retention}":                          0,
		"scibox_breaker_state{name=mail}":                            float64(resilience.Open),
		"scibox_breaker_trips_total{name=mail}":                      1,
		"scibox_limiter_in_flight{name=crossref}":                    1,
		"scibox_limiter_capacity{name=crossref}":                     2,
		"scibox_source_up{source=mail_queue}":                        1,
		"scibox_source_up{source=file_queues}":                       1,
		"scibox_source_up{source=database}":                          1,
		"scibox_source_up{source=job_schedule}":                      1,
		"scibox_db_pool_connections{state=idle}":                     -1, // только наличие
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if v >= 0 && g != v {
			t.Errorf("%s = %v, want %v", k, g, v)
		}
	}
	if _, bad := got["scibox_job_last_success_timestamp_seconds{task=outbox.send}"]; bad {
		t.Error("a per-copy job got a cluster schedule metric")
	}
	if _, bad := got["scibox_job_last_success_timestamp_seconds{task=not-in-definitions}"]; bad {
		t.Error("a job missing from the definitions was exported")
	}
	for _, name := range []string{"scibox_db_pool_max_connections", "scibox_db_pool_acquires_total", "scibox_breaker_retry_after_seconds{name=mail}"} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if got["scibox_breaker_retry_after_seconds{name=mail}"] <= 0 {
		t.Error("an open breaker reports no pause")
	}
}

func TestFailingSourceKeepsTheLastValueAndSaysSo(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	clock := now
	var mu sync.Mutex
	reg := register(t, opsmetrics.Sources{
		Mail: func(context.Context) (notifications.QueueStats, error) {
			calls.Add(1)
			if fail.Load() {
				return notifications.QueueStats{}, errors.New("database is down")
			}
			return notifications.QueueStats{Waiting: 11}, nil
		},
		TTL: 5 * time.Second,
		Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return clock },
	})
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	got := gather(t, reg)
	if got["scibox_mail_queue_messages{state=waiting}"] != 11 || got["scibox_source_up{source=mail_queue}"] != 1 {
		t.Fatalf("first: %v", got)
	}
	// Опросы внутри срока годности не ходят в базу.
	gather(t, reg)
	gather(t, reg)
	if calls.Load() != 1 {
		t.Fatalf("source asked %d times within the TTL", calls.Load())
	}

	fail.Store(true)
	advance(6 * time.Second)
	got = gather(t, reg)
	if got["scibox_mail_queue_messages{state=waiting}"] != 11 || got["scibox_source_up{source=mail_queue}"] != 0 {
		t.Fatalf("after failure the last value must stay and source_up must be 0: %v", got)
	}

	fail.Store(false)
	advance(6 * time.Second)
	if got = gather(t, reg); got["scibox_source_up{source=mail_queue}"] != 1 {
		t.Fatalf("recovery: %v", got)
	}
}

func TestSourceThatNeverAnswersFailsAtTheDeadlineAndNeverBlocksScrapes(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	reg := register(t, opsmetrics.Sources{
		Database: func(ctx context.Context) (opsmetrics.DatabaseStats, error) {
			select {
			case <-ctx.Done():
				return opsmetrics.DatabaseStats{}, ctx.Err()
			case <-release:
				return opsmetrics.DatabaseStats{}, nil
			}
		},
		Timeout: 50 * time.Millisecond,
	})
	start := time.Now()
	got := gather(t, reg)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("scrape took %v", time.Since(start))
	}
	if got["scibox_source_up{source=database}"] != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestMissingSourcesProduceNoMetrics(t *testing.T) {
	got := gather(t, register(t, opsmetrics.Sources{}))
	if len(got) != 0 {
		t.Fatalf("an empty set of sources exported %v", got)
	}
}

func TestRunnerAndReporterMetrics(t *testing.T) {
	pool := testdb.New(t)
	runner := jobs.New(pool, quiet, jobs.Config{})
	rep, err := errreport.New(errreport.Config{DSN: "http://key@127.0.0.1:1/1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Close()
	got := gather(t, register(t, opsmetrics.Sources{Runner: runner, Reporter: rep}))
	for _, k := range []string{
		"scibox_jobs_clock_skewed", "scibox_error_reports_total{result=sent}", "scibox_error_reports_total{result=failed}",
		"scibox_error_reports_total{result=dropped}", "scibox_error_reports_total{result=suppressed}",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %s in %v", k, got)
		}
	}
	if got["scibox_jobs_clock_skewed"] != 0 {
		t.Error("clock skew reported without a check")
	}
}

func TestRunnerStateIsExportedWhileJobsRun(t *testing.T) {
	pool := testdb.New(t)
	runner := jobs.New(pool, quiet, jobs.Config{Poll: 5 * time.Millisecond, Grace: 200 * time.Millisecond})
	var fail atomic.Bool
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.Run(ctx,
			jobs.Job{Name: "local.ok", Every: 5 * time.Millisecond, Run: func(context.Context) error { return nil }},
			jobs.Job{Name: "local.bad", Every: 5 * time.Millisecond, Run: func(context.Context) error { fail.Store(true); return errors.New("down") }},
		)
	}()
	defer func() { cancel(); <-done }()
	reg := register(t, opsmetrics.Sources{Runner: runner, Now: time.Now})
	var got values
	for range 200 {
		got = gather(t, reg)
		if got["scibox_job_local_last_success_timestamp_seconds{task=local.ok}"] > 0 && got["scibox_job_local_consecutive_failures{task=local.bad}"] > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got["scibox_job_local_last_success_timestamp_seconds{task=local.ok}"] <= 0 || got["scibox_job_local_consecutive_failures{task=local.bad}"] < 1 {
		t.Fatalf("runner state not exported: %v", got)
	}
	if got["scibox_job_local_last_success_timestamp_seconds{task=local.bad}"] != 0 {
		t.Errorf("a job that never succeeded shows a success time: %v", got)
	}
}

func TestSkewedClockIsExported(t *testing.T) {
	pool := testdb.New(t)
	ahead := func() time.Time { return time.Now().Add(6 * time.Hour) }
	runner := jobs.New(pool, quiet, jobs.Config{Poll: 5 * time.Millisecond, Grace: 100 * time.Millisecond, Now: ahead, SkewRecheck: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runner.Run(ctx, jobs.Job{Name: "skewed.job", Every: time.Hour, Run: func(context.Context) error { return nil }})
	}()
	defer func() { cancel(); <-done }()
	reg := register(t, opsmetrics.Sources{Runner: runner})
	for range 200 {
		if gather(t, reg)["scibox_jobs_clock_skewed"] == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a clock 6 hours ahead is not reported")
}

func TestDefaultsApplyWithoutSettings(t *testing.T) {
	reg := prometheus.NewRegistry()
	if err := opsmetrics.Register(reg, opsmetrics.Sources{
		Files: func(context.Context) (opsmetrics.FileQueues, error) {
			return opsmetrics.FileQueues{Pending: 1, OldestPending: time.Now().Add(-time.Minute)}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	got := gather(t, reg)
	if age := got["scibox_files_oldest_pending_scan_seconds"]; age < 59 || age > 120 {
		t.Fatalf("age = %v", age)
	}
}

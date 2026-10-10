// Package opsmetrics — метрики состояния системы (срез 26): пул соединений, очередь писем, очередь файлов, фоновые
// задачи, предохранители внешних сервисов, размер базы. В отличие от `internal/metrics` (счётчики событий) эти значения
// не копятся, а читаются в момент опроса Prometheus: из памяти процесса или из базы.
//
// Правила сборщика:
//   - опрос Prometheus не должен нагружать базу: ответ базы кешируется на несколько секунд, а запрос идёт под своим
//     сроком (по умолчанию 3 секунды) и не чаще одного за раз;
//   - сбой источника не роняет опрос: метрики источника берутся из последнего удачного ответа, а
//     `scibox_source_up{source}` показывает 0 (тревога «метрика устарела» строится на нём);
//   - метки конечны: имя задачи, предохранителя и очереди из кода, не из данных.
package opsmetrics

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"scibox/server/internal/errreport"
	"scibox/server/internal/jobs"
	"scibox/server/internal/notifications"
	"scibox/server/internal/resilience"
)

// Значения по умолчанию.
const (
	DefaultTTL     = 5 * time.Second
	DefaultTimeout = 3 * time.Second
)

// FileQueues — очереди файловой работы (T-44).
type FileQueues struct {
	// Pending — файлы, ждущие проверки на вирусы; OldestPending — с какого момента ждёт самый старый (нулевое время —
	// никто не ждёт).
	Pending       int64
	OldestPending time.Time
	// Deletions — объекты, ждущие удаления из хранилища; Legacy — старые файлы, ещё лежащие в базе.
	Deletions, Legacy int64
}

// DatabaseStats — база глазами сервера.
type DatabaseStats struct {
	SizeBytes, Connections, MaxConnections int64
}

// ClusterJob — строка общего расписания (таблица job_runs).
type ClusterJob struct {
	Name     string
	LastOK   time.Time // нулевое время — ни разу не завершалась успешно
	Failures int
	Running  bool
}

// Sources — откуда брать значения. Любой источник может быть nil: его метрик не будет.
type Sources struct {
	// Pool — пул соединений процесса.
	Pool *pgxpool.Pool
	// Mail — состояние очереди писем.
	Mail func(ctx context.Context) (notifications.QueueStats, error)
	// Files — очереди файловой работы.
	Files func(ctx context.Context) (FileQueues, error)
	// Database — размер базы и соединения.
	Database func(ctx context.Context) (DatabaseStats, error)
	// Cluster — общее расписание задач из базы.
	Cluster func(ctx context.Context) ([]ClusterJob, error)
	// JobDefs — описание задач процесса: сроки для тревоги «задача не выполнялась».
	JobDefs []jobs.Job
	// Runner — исполнитель этого процесса (у `serve` без фоновых задач его нет).
	Runner *jobs.Runner
	// Breakers и Limiters — предохранители и ограничители внешних сервисов по имени.
	Breakers map[string]*resilience.Breaker
	Limiters map[string]*resilience.Limiter
	// Reporter — отправитель отчётов об ошибках.
	Reporter *errreport.Reporter

	// TTL — как долго годится ответ базы; Timeout — срок на один запрос к базе. Нулевые — по умолчанию.
	TTL, Timeout time.Duration
	// Now — часы; nil — time.Now.
	Now func() time.Time
}

// Register регистрирует сборщик в реестре.
func Register(reg prometheus.Registerer, src Sources) error {
	if src.TTL <= 0 {
		src.TTL = DefaultTTL
	}
	if src.Timeout <= 0 {
		src.Timeout = DefaultTimeout
	}
	if src.Now == nil {
		src.Now = time.Now
	}
	c := &collector{src: src}
	c.mail = newCached(src, src.Mail)
	c.files = newCached(src, src.Files)
	c.database = newCached(src, src.Database)
	c.cluster = newCached(src, src.Cluster)
	return reg.Register(c)
}

// cached — ответ источника с коротким сроком годности. Один запрос за раз: параллельные опросы ждут тот же ответ.
type cached[T any] struct {
	src   Sources
	fetch func(ctx context.Context) (T, error)

	mu    sync.Mutex
	value T
	at    time.Time
	ok    bool
	asked bool
}

func newCached[T any](src Sources, fetch func(ctx context.Context) (T, error)) *cached[T] {
	if fetch == nil {
		return nil
	}
	return &cached[T]{src: src, fetch: fetch}
}

// get возвращает значение и признак «источник отвечает». До первого удачного ответа значение нулевое, ok == false.
func (c *cached[T]) get(ctx context.Context) (value T, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asked && c.src.Now().Sub(c.at) < c.src.TTL {
		return c.value, c.ok
	}
	ctx, cancel := context.WithTimeout(ctx, c.src.Timeout)
	defer cancel()
	v, err := c.fetch(ctx)
	c.at = c.src.Now()
	c.asked = true
	c.ok = err == nil
	if err == nil {
		c.value = v
	}
	return c.value, c.ok
}

type collector struct {
	src      Sources
	mail     *cached[notifications.QueueStats]
	files    *cached[FileQueues]
	database *cached[DatabaseStats]
	cluster  *cached[[]ClusterJob]
}

var (
	descPoolConns      = desc("db_pool_connections", "Соединения пула этого процесса по состоянию.", "state")
	descPoolMax        = desc("db_pool_max_connections", "Предел соединений пула этого процесса.")
	descPoolAcquires   = desc("db_pool_acquires_total", "Сколько раз брали соединение из пула.")
	descPoolEmpty      = desc("db_pool_empty_acquires_total", "Сколько раз соединение пришлось ждать: пул был занят целиком.")
	descPoolCanceled   = desc("db_pool_canceled_acquires_total", "Сколько раз ожидание соединения оборвано отменой или сроком.")
	descPoolWait       = desc("db_pool_acquire_wait_seconds_total", "Суммарное время ожидания соединения из пула.")
	descDBSize         = desc("db_size_bytes", "Размер базы данных.")
	descDBConns        = desc("db_server_connections", "Соединения со всей базой от всех процессов.")
	descDBMaxConns     = desc("db_server_max_connections", "Предел соединений базы (max_connections).")
	descMailQueue      = desc("mail_queue_messages", "Письма в очереди по состоянию: waiting (ждут, включая повторяемые), retrying (из них пробовали), undelivered (окончательно не доставлены, хранятся 30 дней).", "state")
	descMailOldest     = desc("mail_queue_oldest_waiting_seconds", "Сколько ждёт самое старое письмо из ждущих; 0 — очередь пуста.")
	descFilesPending   = desc("files_pending_scan", "Файлы, ждущие проверки на вирусы.")
	descFilesOldest    = desc("files_oldest_pending_scan_seconds", "Сколько ждёт проверки самый старый файл; 0 — никто не ждёт.")
	descFilesDeletions = desc("files_blob_deletions_queued", "Объекты, ждущие удаления из хранилища файлов.")
	descFilesLegacy    = desc("files_legacy_in_database", "Старые файлы, которые ещё лежат в базе и ждут переноса в хранилище.")
	descJobInterval    = desc("job_interval_seconds", "Срок задачи: как часто она должна выполняться.", "job")
	descJobLastOK      = desc("job_last_success_timestamp_seconds", "Когда задача кластера последний раз завершилась успешно (по общему расписанию); 0 — ни разу.", "job")
	descJobFailures    = desc("job_consecutive_failures", "Сколько проходов задачи кластера подряд закончились ошибкой.", "job")
	descJobRunning     = desc("job_running", "Задача кластера выполняется сейчас (в любой копии).", "job")
	descLocalLastOK    = desc("job_local_last_success_timestamp_seconds", "Когда задача последний раз завершилась успешно в этом процессе; 0 — ни разу.", "job")
	descLocalFailures  = desc("job_local_consecutive_failures", "Сколько проходов задачи подряд закончились ошибкой в этом процессе.", "job")
	descClockSkewed    = desc("jobs_clock_skewed", "1 — часы процесса разошлись с часами базы, фоновые задачи этого процесса остановлены.")
	descBreakerState   = desc("breaker_state", "Предохранитель внешнего сервиса: 0 закрыт (всё идёт), 1 открыт (обращений нет), 2 пробует.", "name")
	descBreakerTrips   = desc("breaker_trips_total", "Сколько раз предохранитель открывался.", "name")
	descBreakerRetry   = desc("breaker_retry_after_seconds", "Через сколько секунд открытый предохранитель пропустит пробное обращение.", "name")
	descLimiterBusy    = desc("limiter_in_flight", "Обращения к внешнему сервису, идущие сейчас.", "name")
	descLimiterCap     = desc("limiter_capacity", "Сколько обращений к внешнему сервису одновременно разрешено.", "name")
	descLimiterReject  = desc("limiter_rejected_total", "Сколько обращений отклонено ограничителем (не хватило места за срок).", "name")
	descReports        = desc("error_reports_total", "Отчёты об ошибках по исходу: sent, failed, dropped, suppressed.", "result")
	descSourceUp       = desc("source_up", "1 — источник метрик ответил в последний раз, 0 — нет (значения взяты из прошлого ответа).", "source")
)

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc("scibox_"+name, help, labels, nil)
}

// Describe ничего не сообщает: набор метрик зависит от источников (сборщик «непроверяемый»).
func (c *collector) Describe(chan<- *prometheus.Desc) {}

// Collect читает источники и отдаёт значения.
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
	}
	counter := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, labels...)
	}
	up := func(source string, ok bool) {
		v := 0.0
		if ok {
			v = 1
		}
		gauge(descSourceUp, v, source)
	}

	if p := c.src.Pool; p != nil {
		s := p.Stat()
		gauge(descPoolConns, float64(s.AcquiredConns()), "acquired")
		gauge(descPoolConns, float64(s.IdleConns()), "idle")
		gauge(descPoolConns, float64(s.ConstructingConns()), "constructing")
		gauge(descPoolMax, float64(s.MaxConns()))
		counter(descPoolAcquires, float64(s.AcquireCount()))
		counter(descPoolEmpty, float64(s.EmptyAcquireCount()))
		counter(descPoolCanceled, float64(s.CanceledAcquireCount()))
		counter(descPoolWait, s.AcquireDuration().Seconds())
	}
	if c.database != nil {
		v, ok := c.database.get(ctx)
		up("database", ok)
		gauge(descDBSize, float64(v.SizeBytes))
		gauge(descDBConns, float64(v.Connections))
		gauge(descDBMaxConns, float64(v.MaxConnections))
	}
	if c.mail != nil {
		v, ok := c.mail.get(ctx)
		up("mail_queue", ok)
		gauge(descMailQueue, float64(v.Waiting), "waiting")
		gauge(descMailQueue, float64(v.Retrying), "retrying")
		gauge(descMailQueue, float64(v.Undelivered), "undelivered")
		gauge(descMailOldest, v.OldestWaiting.Seconds())
	}
	if c.files != nil {
		v, ok := c.files.get(ctx)
		up("file_queues", ok)
		gauge(descFilesPending, float64(v.Pending))
		age := 0.0
		if v.Pending > 0 && !v.OldestPending.IsZero() {
			age = max(c.src.Now().Sub(v.OldestPending).Seconds(), 0)
		}
		gauge(descFilesOldest, age)
		gauge(descFilesDeletions, float64(v.Deletions))
		gauge(descFilesLegacy, float64(v.Legacy))
	}
	for _, j := range c.src.JobDefs {
		gauge(descJobInterval, j.Every.Seconds(), j.Name)
	}
	if c.cluster != nil {
		rows, ok := c.cluster.get(ctx)
		up("job_schedule", ok)
		byName := map[string]ClusterJob{}
		for _, r := range rows {
			byName[r.Name] = r
		}
		for _, j := range c.src.JobDefs {
			if !j.Cluster {
				continue
			}
			r := byName[j.Name] // задачи, ещё не занятой ни разу, в таблице нет: нули
			last := 0.0
			if !r.LastOK.IsZero() {
				last = float64(r.LastOK.Unix())
			}
			gauge(descJobLastOK, last, j.Name)
			gauge(descJobFailures, float64(r.Failures), j.Name)
			running := 0.0
			if r.Running {
				running = 1
			}
			gauge(descJobRunning, running, j.Name)
		}
	}
	if rn := c.src.Runner; rn != nil {
		for _, s := range rn.Snapshot() {
			last := 0.0
			if !s.LastOK.IsZero() {
				last = float64(s.LastOK.Unix())
			}
			gauge(descLocalLastOK, last, s.Name)
			gauge(descLocalFailures, float64(s.Failures), s.Name)
		}
		skewed := 0.0
		if rn.ClockSkewed() {
			skewed = 1
		}
		gauge(descClockSkewed, skewed)
	}
	for name, b := range c.src.Breakers {
		gauge(descBreakerState, float64(b.State()), name)
		counter(descBreakerTrips, float64(b.Trips()), name)
		gauge(descBreakerRetry, b.RetryAfter().Seconds(), name)
	}
	for name, l := range c.src.Limiters {
		gauge(descLimiterBusy, float64(l.InFlight()), name)
		gauge(descLimiterCap, float64(l.Capacity()), name)
		counter(descLimiterReject, float64(l.Rejected()), name)
	}
	if rep := c.src.Reporter; rep != nil {
		s := rep.Stats()
		counter(descReports, float64(s.Sent), "sent")
		counter(descReports, float64(s.Failed), "failed")
		counter(descReports, float64(s.Dropped), "dropped")
		counter(descReports, float64(s.Suppressed), "suppressed")
	}
}

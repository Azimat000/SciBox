package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"scibox/server/internal/config"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/errreport"
	"scibox/server/internal/httpcache"
	"scibox/server/internal/jobs"
	"scibox/server/internal/metrics"
	"scibox/server/internal/opsmetrics"
	"scibox/server/internal/opsserver"
)

// Внутренний порт по умолчанию: у сервера и у воркера разные, чтобы оба помещались на одной машине (make dev).
const (
	defaultServeOpsAddr  = "127.0.0.1:9100"
	defaultWorkerOpsAddr = "127.0.0.1:9101"
)

// observability — то, что нужно процессу, чтобы за ним можно было наблюдать: метрики и отправка ошибок. У команд,
// не являющихся долгими процессами (migrate, seed), метрик нет.
type observability struct {
	service  string
	metrics  *metrics.Metrics
	reporter *errreport.Reporter
}

// newObservability создаёт метрики и отправителя ошибок по настройкам. Отправитель появляется только при заданном
// SCIBOX_ERROR_DSN; адрес проверен при чтении настроек, поэтому ошибка здесь невозможна.
func newObservability(cfg config.Config, service string) observability {
	o := observability{service: service}
	switch service {
	case "serve", "worker":
		o.metrics = metrics.New(service, Version)
	}
	if cfg.ErrorDSN != "" && (service == "serve" || service == "worker" || service == "error-test") {
		o.reporter, _ = errreport.New(errreport.Config{
			DSN: cfg.ErrorDSN, Release: Version, Environment: cfg.Env, ServerName: service,
		})
	}
	return o
}

// runErrorTest — `scibox error-test`: отправляет пробный отчёт и говорит, дошёл ли он.
func runErrorTest(ctx context.Context, obs observability, env Env) int {
	if obs.reporter == nil {
		fmt.Fprintln(env.Stderr, "SCIBOX_ERROR_DSN не задан: отправлять отчёты некуда")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := obs.reporter.SendTest(ctx); err != nil {
		fmt.Fprintln(env.Stderr, "Пробный отчёт не дошёл:", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "Пробный отчёт принят: найдите его в GlitchTip в проекте, к которому относится ключ.")
	return 0
}

// opsAddr выбирает адрес внутреннего порта: настройка, иначе адрес по умолчанию процесса; пустая строка — порт выключен.
func opsAddr(cfg config.Config, def string) string {
	switch cfg.Ops.Addr {
	case "":
		return def
	case config.OpsOff:
		return ""
	}
	return cfg.Ops.Addr
}

// schemaChecker — проверка, что в базе накатаны все миграции (goose.Provider).
type schemaChecker interface {
	HasPending(ctx context.Context) (bool, error)
}

// opsParams — что внутреннему порту нужно знать о процессе.
type opsParams struct {
	defaultAddr string
	pool        *pgxpool.Pool
	schema      schemaChecker
	// runner — исполнитель задач процесса; nil, если фоновых задач в процессе нет.
	runner *jobs.Runner
	// onListen вызывается, когда порт начал принимать соединения (для тестов).
	onListen func(net.Addr)
}

// opsHandle — запущенный внутренний порт; нулевой указатель — порт выключен.
type opsHandle struct {
	srv  *opsserver.Server
	done chan error
}

// startOps открывает внутренний порт, если он не выключен. Занятый адрес — ошибка запуска: процесс без метрик и без
// проверок готовности в бою хуже процесса, который не стартовал и об этом сказал.
func startOps(ctx context.Context, cfg config.Config, logger *slog.Logger, obs observability, p opsParams) (*opsHandle, error) {
	addr := opsAddr(cfg, p.defaultAddr)
	if addr == "" {
		logger.Info("internal port is off (SCIBOX_OPS_ADDR=off): no metrics, no readiness checks")
		return nil, nil
	}
	checks := []opsserver.Check{
		{Name: "database", Run: func(ctx context.Context) error { return p.pool.Ping(ctx) }},
		{Name: "migrations", Run: func(ctx context.Context) error {
			pending, err := p.schema.HasPending(ctx)
			if err != nil {
				return err
			}
			if pending {
				return errors.New("migrations are not applied")
			}
			return nil
		}},
	}
	if p.runner != nil {
		checks = append(checks, opsserver.Check{Name: "clock", Run: func(context.Context) error {
			if p.runner.ClockSkewed() {
				return errors.New("the clock differs from the database clock")
			}
			return nil
		}})
	}
	srv, err := opsserver.New(opsserver.Config{
		Token:   cfg.Ops.Token,
		Pprof:   cfg.Ops.Pprof,
		Metrics: promhttp.HandlerFor(obs.metrics.Registry, promhttp.HandlerOpts{ErrorLog: promLogger{logger}}),
		Checks:  checks,
		Logger:  logger,
	})
	if err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on the internal port %s (SCIBOX_OPS_ADDR): %w", addr, err)
	}
	h := &opsHandle{srv: srv, done: make(chan error, 1)}
	go func() { h.done <- srv.Serve(ln) }()
	logger.Info("internal port started", "addr", ln.Addr().String(), "pprof", cfg.Ops.Pprof, "protected", cfg.Ops.Token != "")
	if p.onListen != nil {
		p.onListen(ln.Addr())
	}
	return h, nil
}

// drain говорит оркестратору «не присылай новых запросов»: /readyz начинает отвечать 503.
func (h *opsHandle) drain() {
	if h != nil {
		h.srv.SetDraining()
	}
}

// stop закрывает порт и возвращает ошибку его работы, если она была.
func (h *opsHandle) stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	err := h.srv.Shutdown(ctx)
	return errors.Join(err, <-h.done)
}

// promLogger направляет ошибки Prometheus-обработчика в наш журнал (через маску).
type promLogger struct{ l *slog.Logger }

func (p promLogger) Println(v ...any) { p.l.Error("metrics handler failed", "err", fmt.Sprint(v...)) }

// registerSources подключает к реестру метрики состояния: очереди, задачи, предохранители, пул.
func (s *services) registerSources(obs observability, runner *jobs.Runner, defs []jobs.Job) error {
	q := dbgen.New(s.pool)
	caches := map[string]func() httpcache.Stats{}
	if s.searchCache != nil {
		caches["vacancy_search"] = s.searchCache.Stats
		caches["scientist_catalog"] = s.catalogCache.Stats
	}
	return opsmetrics.Register(obs.metrics.Registry, opsmetrics.Sources{
		Pool:   s.pool,
		Caches: caches,
		Mail:   s.outbox.QueueStats,
		Files: func(ctx context.Context) (opsmetrics.FileQueues, error) {
			row, err := q.FilesQueueStats(ctx)
			if err != nil {
				return opsmetrics.FileQueues{}, err
			}
			out := opsmetrics.FileQueues{Pending: row.Pending, Deletions: row.Deletions, Legacy: row.Legacy}
			if row.Pending > 0 {
				out.OldestPending = row.OldestPending
			}
			return out, nil
		},
		Database: func(ctx context.Context) (opsmetrics.DatabaseStats, error) {
			row, err := q.DatabaseStats(ctx)
			if err != nil {
				return opsmetrics.DatabaseStats{}, err
			}
			out := opsmetrics.DatabaseStats{SizeBytes: row.SizeBytes, MaxConnections: row.MaxConnections}
			// Системное представление sqlc не знает: обычный запрос без параметров.
			err = s.pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").Scan(&out.Connections)
			return out, err
		},
		Cluster: func(ctx context.Context) ([]opsmetrics.ClusterJob, error) {
			rows, err := q.ListJobRuns(ctx)
			if err != nil {
				return nil, err
			}
			now := s.now()
			out := make([]opsmetrics.ClusterJob, 0, len(rows))
			for _, r := range rows {
				cj := opsmetrics.ClusterJob{Name: r.Name, Failures: int(r.Failures)}
				if r.LastOkAt != nil {
					cj.LastOK = *r.LastOkAt
				}
				cj.Running = r.Holder != "" && r.LeasedUntil != nil && r.LeasedUntil.After(now)
				out = append(out, cj)
			}
			return out, nil
		},
		JobDefs:  defs,
		Runner:   runner,
		Breakers: s.breakers,
		Limiters: s.limiters,
		Reporter: obs.reporter,
		Now:      s.now,
	})
}

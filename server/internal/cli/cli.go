// Package cli разбирает команды бинарника сервера: serve, migrate, seed, version.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"scibox/server/internal/antivirus"
	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/blobstore"
	"scibox/server/internal/config"
	"scibox/server/internal/crossref"
	"scibox/server/internal/dataexport"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/dbpool"
	"scibox/server/internal/errreport"
	"scibox/server/internal/files"
	"scibox/server/internal/filework"
	"scibox/server/internal/health"
	"scibox/server/internal/httpapi"
	"scibox/server/internal/httpcache"
	"scibox/server/internal/httpguard"
	"scibox/server/internal/jobs"
	"scibox/server/internal/journals"
	"scibox/server/internal/landing"
	"scibox/server/internal/logmask"
	"scibox/server/internal/mail"
	"scibox/server/internal/matching"
	"scibox/server/internal/metrics"
	"scibox/server/internal/migrate"
	"scibox/server/internal/notifications"
	"scibox/server/internal/offers"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
	"scibox/server/internal/refdata"
	"scibox/server/internal/references"
	"scibox/server/internal/reqid"
	"scibox/server/internal/resilience"
	"scibox/server/internal/vacancies"
	"scibox/server/seed"
)

// Version задаётся при сборке через -ldflags; при локальном запуске "dev".
var Version = "dev"

const usage = `Использование: scibox <команда>

Команды:
  serve                      запустить HTTP-сервер (по умолчанию); с SCIBOX_BACKGROUND=off без фоновых задач
  worker                     запустить только фоновые задачи (письма, подбор, напоминания, чистка, файлы)
  error-test                 отправить пробный отчёт в GlitchTip (проверка SCIBOX_ERROR_DSN)
  migrate up|down|reset|status   миграции базы данных (down и reset в боевом режиме только с SCIBOX_ALLOW_DESTRUCTIVE=on)
  release                    шаг выкладки: накатить миграции и загрузить справочник журналов (оба шага повторяемы)
  seed                       загрузить демо-данные (в боевом режиме только с SCIBOX_ALLOW_DESTRUCTIVE=on)
  journals load [--force] [файл]  загрузить справочник журналов SCImago (без файла — встроенный в сервер)
  healthcheck [serve|worker] спросить внутренний порт процесса, готов ли он (для проверки контейнера)
  version                    показать версию
`

// Env — внешний мир программы, подменяемый в тестах.
type Env struct {
	Getenv func(string) string
	Stdout io.Writer
	Stderr io.Writer
	// OnListen вызывается, когда сервер начал принимать соединения (для тестов).
	OnListen func(addr net.Addr)
	// OnOpsListen вызывается, когда внутренний порт начал принимать соединения (для тестов).
	OnOpsListen func(addr net.Addr)
}

// Run выполняет команду и возвращает код выхода процесса.
func Run(ctx context.Context, args []string, env Env) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	// До разбора настроек журнал простой: уровень Info, читаемый текст. Настоящий журнал (формат, уровень, сбор ошибок)
	// строится по настройкам сразу после их чтения.
	logger := newLogger(env.Stderr, logOptions{})
	restoreDefault := useAsDefault(logger)
	defer func() { restoreDefault() }()

	switch cmd {
	case "version":
		fmt.Fprintln(env.Stdout, Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	case "healthcheck":
		return runHealthcheck(ctx, args, env)
	case "serve", "worker", "migrate", "release", "seed", "journals", "error-test":
	default:
		fmt.Fprintf(env.Stderr, "неизвестная команда %q\n\n%s", cmd, usage)
		return 2
	}

	cfg, err := config.Load(env.Getenv)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}
	// Журнал по настройкам: каждая строка несёт имя процесса, версию и режим, чтобы строки нескольких копий и воркера,
	// собранные в одном месте, различались.
	obs := newObservability(cfg, cmd)
	defer obs.reporter.Close()
	restoreDefault()
	logger = newLogger(env.Stderr, logOptions{json: cfg.Log.JSON, level: cfg.Log.Level, reporter: obs.reporter, metrics: obs.metrics}).
		With("service", cmd, "version", Version, "env", cfg.Env)
	restoreDefault = useAsDefault(logger)

	switch cmd {
	case "migrate":
		if len(args) != 1 {
			fmt.Fprint(env.Stderr, usage)
			return 2
		}
		if (args[0] == "down" || args[0] == "reset") && refuseDestructive(cfg, logger, "migrate "+args[0]) {
			return 1
		}
		if err := migrate.Command(ctx, cfg.DatabaseURL, args[0], env.Stdout); err != nil {
			logger.Error("migrate", "err", err)
			return 1
		}
		return 0
	case "release":
		// Миграции и справочник журналов одним шагом: так их запускает выкладка (deploy/deploy.sh). Образ без
		// оболочки не может выполнить две команды подряд.
		if len(args) != 0 {
			fmt.Fprint(env.Stderr, usage)
			return 2
		}
		if err := migrate.Command(ctx, cfg.DatabaseURL, "up", env.Stdout); err != nil {
			logger.Error("release: migrate", "err", err)
			return 1
		}
		return runJournals(ctx, cfg.DatabaseURL, []string{"load"}, env, logger)
	case "journals":
		return runJournals(ctx, cfg.DatabaseURL, args, env, logger)
	case "seed":
		if refuseDestructive(cfg, logger, "seed") {
			return 1
		}
		vault, err := openVault(ctx, cfg, logger)
		if err != nil {
			logger.Error("seed", "err", err)
			return 1
		}
		if err := runSeed(ctx, cfg.DatabaseURL, vault, env.Stdout); err != nil {
			logger.Error("seed", "err", err)
			return 1
		}
		return 0
	case "error-test":
		return runErrorTest(ctx, obs, env)
	case "worker":
		if err := runWorker(ctx, cfg, logger, obs, env.OnOpsListen); err != nil {
			logger.Error("worker", "err", err)
			return 1
		}
		return 0
	default:
		if err := serve(ctx, cfg, logger, obs, env.OnListen, env.OnOpsListen); err != nil {
			logger.Error("serve", "err", err)
			return 1
		}
		return 0
	}
}

// refuseDestructive не даёт боевому серверу выполнить команду, которая стирает или подменяет данные (срез 28):
// без явного SCIBOX_ALLOW_DESTRUCTIVE=on она отказывает. Возвращает true, если команду выполнять нельзя.
func refuseDestructive(cfg config.Config, logger *slog.Logger, command string) bool {
	if cfg.Env != config.EnvProduction || cfg.AllowDestructive {
		return false
	}
	logger.Error("refused in production: "+command+" erases or replaces data; set SCIBOX_ALLOW_DESTRUCTIVE=on for this one command if you really mean it",
		"command", command)
	return true
}

// logOptions — как собрать журнал. Нулевое значение: читаемый текст, уровень Info, без метрик и сбора ошибок.
type logOptions struct {
	// json — по объекту JSON на запись (в бою), иначе читаемый текст.
	json  bool
	level slog.Level
	// reporter получает записи уровня Error (сбор ошибок, срез 26); nil — никому.
	reporter *errreport.Reporter
	// metrics считает записи по уровню; nil — не считает.
	metrics *metrics.Metrics
}

// newLogger — единственное место, где создаётся журнал сервера. Порядок слоёв важен:
//  1. logmask чистит сообщение и значения (T-18): почты, адреса людей и токены не попадают в запись, где бы их ни
//     написали по ошибке, и не уходят ни в журнал, ни в сбор ошибок;
//  2. reqid дописывает номер запроса к записям, написанным с контекстом запроса;
//  3. счётчик записей по уровню (метрики);
//  4. errreport отправляет уже очищенные записи уровня Error в GlitchTip и передаёт их дальше;
//  5. обработчик вывода (JSON в бою, текст локально).
func newLogger(w io.Writer, o logOptions) *slog.Logger {
	hopts := &slog.HandlerOptions{Level: o.level}
	var out slog.Handler = slog.NewTextHandler(w, hopts)
	if o.json {
		out = slog.NewJSONHandler(w, hopts)
	}
	if o.reporter != nil {
		// О своих сбоях отправитель пишет в журнал без отправки ошибок: иначе сбой отправки породил бы ещё одну ошибку.
		o.reporter.SetLogger(slog.New(logmask.NewHandler(out)))
		out = errreport.NewHandler(out, o.reporter)
	}
	if o.metrics != nil {
		out = o.metrics.NewLogHandler(out)
	}
	return slog.New(logmask.NewHandler(reqid.NewHandler(out)))
}

// useAsDefault делает logger журналом по умолчанию: его используют slog.Info и пр. без логгера и стандартный log
// (в том числе сам net/http, когда пишет об оборванных соединениях и адресе клиента). Возвращает функцию, которая
// всё возвращает назад: тесты вызывают Run много раз.
func useAsDefault(logger *slog.Logger) func() {
	prev, writer, flags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(logger)
	return func() {
		slog.SetDefault(prev)
		log.SetOutput(writer)
		log.SetFlags(flags)
	}
}

// runJournals: `journals load [--force] [файл]`. Тот же файл второй раз не загружается (без --force).
func runJournals(ctx context.Context, databaseURL string, args []string, env Env, logger *slog.Logger) int {
	if len(args) == 0 || args[0] != "load" {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	src, force := journals.Embedded(), false
	for _, a := range args[1:] {
		switch {
		case a == "--force":
			force = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprint(env.Stderr, usage)
			return 2
		default:
			data, err := os.ReadFile(a) //nolint:gosec // G304: файл указывает администратор в командной строке
			if err != nil {
				logger.Error("journals", "err", err)
				return 1
			}
			info, _ := os.Stat(a) // файл только что прочитан
			src = journals.Source{Data: data, Downloaded: info.ModTime().UTC()}
		}
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		logger.Error("journals", "err", fmt.Errorf("connect database: %w", err))
		return 1
	}
	defer pool.Close()
	res, err := journals.Load(ctx, pool, src, force)
	if err != nil {
		logger.Error("journals", "err", err)
		return 1
	}
	if res.Unchanged {
		fmt.Fprintf(env.Stdout, "Справочник журналов уже загружен (%s).\n", res.Edition)
		return 0
	}
	fmt.Fprintf(env.Stdout, "Справочник журналов загружен (%s): журналов %d, ISSN %d, пропущено без ISSN %d.\n", res.Edition, res.Journals, res.ISSNs, res.Skipped)
	return 0
}

func runSeed(ctx context.Context, databaseURL string, vault *files.Vault, out io.Writer) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	res, err := seed.Run(ctx, pool, vault, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Демо-данные загружены: новых людей %d, организаций %d, вакансий %d, профилей соискателей %d, откликов %d, приглашений %d, избранных вакансий %d, сохранённых поисков %d.\n", res.People, res.Organizations, res.Vacancies, res.Profiles, res.Applications, res.Offers, res.Favorites, res.Searches)
	fmt.Fprintf(out, "Вход для проверки: %s (пароль записан в server/seed/seed.go).\n", seed.Logins()[0])
	return nil
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, obs observability, onListen, onOpsListen func(net.Addr)) error {
	// Сайт проверяется до базы и фоновых задач: несобранный сайт — ошибка запуска, а не пустые страницы.
	var web http.Handler
	if cfg.WebDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err != nil {
			return fmt.Errorf("site not built in %s (run npm run build in web/): %w", cfg.WebDir, err)
		}
		web = httpapi.WebHandler(os.DirFS(cfg.WebDir))
	}

	// Пул сервера со сроками для базы и журналом медленных запросов (T-22). Команды администратора (migrate, seed,
	// journals) открывают обычные соединения без этих сроков: загрузка справочника или миграция может идти долго.
	cfg.DB.Observer = obs.metrics // у serve метрики есть всегда
	pool, err := dbpool.New(ctx, cfg.DatabaseURL, cfg.DB, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Warn("close database handle", "err", err)
		}
	}()
	migrations, err := migrate.NewProvider(sqlDB)
	if err != nil {
		return err
	}

	vault, err := openVault(ctx, cfg, logger)
	if err != nil {
		return err
	}
	svc := newServices(cfg, pool, vault, logger, time.Now, nil)
	svc.metrics = obs.metrics
	handler := svc.router(health.Checker{Queries: dbgen.New(pool), Migrations: migrations}, web)

	// Фоновые задачи (если их не вынесли в `worker`) останавливаются отменой jobCtx; сервер дожидается их до закрытия
	// базы и выхода. Остановка мягкая: задачи видят сигнал и дописывают текущую порцию (jobs.Draining).
	var background sync.WaitGroup
	defer background.Wait()
	jobCtx, stopJobs := context.WithCancel(ctx)
	defer stopJobs()
	for _, flush := range svc.flushers() {
		defer flush()
	}
	list := svc.jobs()
	var runner *jobs.Runner
	if cfg.Background {
		if err := jobs.Validate(list); err != nil {
			return err
		}
		runner = jobs.New(pool, logger, jobs.Config{Grace: cfg.ShutdownTimeout, Observer: obs.metrics})
		background.Go(func() { _ = runner.Run(jobCtx, list...) }) // описание проверено выше: Run не вернёт ошибку
	} else {
		logger.Info("background jobs are off in this process: run `worker` for them")
	}
	if err := svc.registerSources(obs, runner, list); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}
	ops, err := startOps(ctx, cfg, logger, obs, opsParams{defaultAddr: defaultServeOpsAddr, pool: pool, schema: migrations, runner: runner, onListen: onOpsListen})
	if err != nil {
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ops.stop(stopCtx); err != nil {
			logger.Warn("internal port stopped with an error", "err", err)
		}
	}()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	srv := httpguard.NewServer(handler, httpguard.DefaultTimeouts())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info("server started", "addr", ln.Addr().String(), "product", cfg.Product.Name,
		"trusted_proxies", !cfg.TrustedProxies.Empty(), "background", cfg.Background)
	if onListen != nil {
		onListen(ln.Addr())
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	// Оркестратору сначала сообщается «не готов» (/readyz): пока сервер дописывает запросы, новых ему не шлют. Затем
	// сервер перестаёт принимать новые соединения и дожидается текущих запросов; фоновые задачи в это время тоже
	// дописывают свою порцию (их остановка началась с той же отмены ctx).
	ops.drain()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("server stopped")
	return nil
}

// runWorker — отдельный процесс фоновых задач: те же задачи, что у `serve` с включённым SCIBOX_BACKGROUND, но без
// HTTP. Копий воркера может быть несколько: общее расписание лежит в базе (internal/jobs).
func runWorker(ctx context.Context, cfg config.Config, logger *slog.Logger, obs observability, onOpsListen func(net.Addr)) error {
	// Задачи идут по одной на каждый вид и почти не держат соединений: воркеру хватает половины обычного пула, остальное
	// остаётся копиям сервера (в базе всего 100 соединений на все процессы).
	cfg.DB.MaxConns = min(cfg.DB.MaxConns, workerMaxConns)
	cfg.DB.Observer = obs.metrics // у worker метрики есть всегда
	pool, err := dbpool.New(ctx, cfg.DatabaseURL, cfg.DB, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Warn("close database handle", "err", err)
		}
	}()
	migrations, err := migrate.NewProvider(sqlDB)
	if err != nil {
		return err
	}
	vault, err := openVault(ctx, cfg, logger)
	if err != nil {
		return err
	}
	svc := newServices(cfg, pool, vault, logger, time.Now, nil)
	svc.metrics = obs.metrics
	for _, flush := range svc.flushers() {
		defer flush()
	}
	list := svc.jobs()
	if err := jobs.Validate(list); err != nil {
		return err
	}
	runner := jobs.New(pool, logger, jobs.Config{Grace: cfg.ShutdownTimeout, Observer: obs.metrics})
	if err := svc.registerSources(obs, runner, list); err != nil {
		return fmt.Errorf("register metrics: %w", err)
	}
	ops, err := startOps(ctx, cfg, logger, obs, opsParams{defaultAddr: defaultWorkerOpsAddr, pool: pool, schema: migrations, runner: runner, onListen: onOpsListen})
	if err != nil {
		return err
	}
	// Порт закрывается после остановки задач: пока они дописывают, /readyz отвечает «не готов», /livez «жив».
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := ops.stop(stopCtx); err != nil {
			logger.Warn("internal port stopped with an error", "err", err)
		}
	}()
	context.AfterFunc(ctx, ops.drain)
	logger.Info("worker started", "product", cfg.Product.Name, "jobs", len(list), "holder", runner.Holder())
	if err := runner.Run(ctx, list...); err != nil {
		return err
	}
	logger.Info("worker stopped")
	return nil
}

// app — собранный сервер: обработчик всех адресов и фоновые задачи, которые запускает serve.
type app struct {
	handler http.Handler
	// jobs — фоновые задачи.
	jobs []jobs.Job
	// flush дожидается писем, которые сервисы отправляют в фоне, перед выходом.
	flush []func()
}

// newApp собирает сервисы и роутер так же, как в работе. Отдельно от serve, чтобы тесты проверяли
// настоящую сборку (например, что каждый адрес ограничивает тело запроса). now — часы ограничения частоты.
func newApp(cfg config.Config, pool *pgxpool.Pool, vault *files.Vault, logger *slog.Logger, checker httpapi.HealthChecker, web http.Handler, now func() time.Time) app {
	svc := newServices(cfg, pool, vault, logger, now, nil)
	return app{handler: svc.router(checker, web), jobs: svc.jobs(), flush: svc.flushers()}
}

// services — все сервисы сервера. Из них собираются и роутер (serve), и список фоновых задач (serve и worker): так
// оба процесса работают с одними и теми же настройками и правилами.
type services struct {
	cfg    config.Config
	pool   *pgxpool.Pool
	logger *slog.Logger
	now    func() time.Time

	accounts      *auth.Service
	organizations *orgs.Service
	notes         *notifications.Service
	outbox        *notifications.Worker
	profiles      *profiles.Service
	references    *references.Service
	applications  *applications.Service
	vacancies     *vacancies.Service
	matching      *matching.Service
	counters      *httpguard.PostgresStore
	fileWork      *filework.Service
	vault         *files.Vault
	authHandler   *auth.Handler
	queue         *notifications.Queue

	// metrics считает запросы роутера; nil — без метрик (тесты). Задаётся после сборки (serve, worker).
	metrics *metrics.Metrics
	// searchCache и catalogCache — короткие кеши ответов поиска вакансий и каталога учёных для гостей (срез 27);
	// nil — кеш выключен (SCIBOX_SEARCH_CACHE_TTL=off).
	searchCache, catalogCache *httpcache.Keyed
	// breakers и limiters — предохранители и ограничители внешних сервисов по имени (для метрик).
	breakers map[string]*resilience.Breaker
	limiters map[string]*resilience.Limiter
}

// searchCacheEntries — сколько ответов поиска копия держит в памяти (страница из двадцати карточек — десятки килобайт, ответ больше 128 КБ не запоминается).
const searchCacheEntries = 500

// workerMaxConns — предел пула воркера (SCIBOX_DB_MAX_CONNS ниже этого предела действует как есть).
const workerMaxConns = 10

// Предохранитель внешних сервисов (T-24): после стольких сбоев подряд обращений нет, пауза от 30 секунд до 5 минут.
const (
	breakerThreshold   = 5
	breakerCooldown    = 30 * time.Second
	breakerMaxCooldown = 5 * time.Minute
	// crossrefMaxWait — сколько запрос ждёт свободное место для обращения к Crossref: человек ждёт ответа в форме.
	crossrefMaxWait = 2 * time.Second
)

// newServices собирает сервисы. mailer — отправитель писем из очереди (nil — настоящий SMTP по настройкам); подменяется
// в тестах.
func newServices(cfg config.Config, pool *pgxpool.Pool, vault *files.Vault, logger *slog.Logger, now func() time.Time, mailer mail.Sender) *services {
	if mailer == nil {
		mailer = mail.SMTP{Addr: cfg.SMTPAddr, From: cfg.MailFrom, TLS: cfg.SMTPTLS, Username: cfg.SMTPUser, Password: cfg.SMTPPassword}
	}
	// Письма сначала попадают в очередь в базе и уходят оттуда в фоне: они переживают падение сервера и повторяются при сбоях.
	queue := notifications.NewQueue(pool)
	accounts := auth.NewService(pool, queue, auth.DefaultConfig(cfg.Product.Name, cfg.PublicURL), logger)
	organizations := orgs.NewService(pool, queue, orgs.DefaultConfig(cfg.Product.Name, cfg.PublicURL), logger)
	outbox := notifications.NewWorker(pool, mailer, logger)
	outbox.Breaker = resilience.NewBreaker(resilience.BreakerConfig{Threshold: breakerThreshold, Cooldown: breakerCooldown, MaxCooldown: breakerMaxCooldown})

	notes := notifications.NewService(pool, notifications.Config{ProductName: cfg.Product.Name, PublicURL: cfg.PublicURL})
	cross := crossref.NewClient(cfg.CrossrefURL, cfg.CrossrefMailto)
	cross.Gate = &resilience.Gate{
		Limiter: resilience.NewLimiter(cfg.CrossrefMaxConcurrent, crossrefMaxWait),
		Breaker: resilience.NewBreaker(resilience.BreakerConfig{Threshold: breakerThreshold, Cooldown: breakerCooldown, MaxCooldown: breakerMaxCooldown}),
	}
	profileSvc := profiles.NewService(pool, cross, profiles.DefaultConfig(cfg.Product.Name))
	refSvc := references.NewService(pool, notes, vault, references.DefaultConfig(cfg.Product.Name, cfg.PublicURL))
	vacancySvc := vacancies.NewService(pool, vacancies.DefaultConfig())
	matchingCfg := matching.DefaultConfig()
	matchingCfg.Now = now
	var searchCache, catalogCache *httpcache.Keyed
	if cfg.SearchCacheTTL > 0 {
		searchCache = httpcache.NewKeyed(cfg.SearchCacheTTL, searchCacheEntries)
		catalogCache = httpcache.NewKeyed(cfg.SearchCacheTTL, searchCacheEntries)
	}
	return &services{
		searchCache: searchCache, catalogCache: catalogCache,
		cfg: cfg, pool: pool, logger: logger, now: now, vault: vault,
		accounts: accounts, organizations: organizations, notes: notes, outbox: outbox, queue: queue,
		profiles: profileSvc, references: refSvc, vacancies: vacancySvc,
		applications: applications.NewService(pool, profileSvc, refSvc, notes, vault, applications.DefaultConfig()),
		matching:     matching.NewService(pool, vacancySvc, notes, matchingCfg, logger),
		// Счётчики частоты в базе: лимит общий для всех копий сервера (D-141).
		counters:    httpguard.NewPostgresStore(pool),
		fileWork:    filework.NewService(pool, vault, notes, filework.DefaultConfig(), logger),
		authHandler: auth.NewHandler(accounts, logger),
		breakers:    map[string]*resilience.Breaker{"mail": outbox.Breaker, "crossref": cross.Gate.Breaker},
		limiters:    map[string]*resilience.Limiter{"crossref": cross.Gate.Limiter},
	}
}

// router собирает обработчики всех адресов.
func (s *services) router(checker httpapi.HealthChecker, web http.Handler) http.Handler {
	cfg, pool, logger, requireUser := s.cfg, s.pool, s.logger, s.authHandler.RequireUser
	return httpapi.NewRouter(httpapi.Deps{
		ProductName:   cfg.Product.Name,
		Version:       Version,
		Health:        checker,
		Logger:        logger,
		Auth:          s.authHandler,
		Orgs:          orgs.NewHandler(s.organizations, logger, requireUser),
		Vacancies:     vacancies.NewHandler(s.vacancies, logger, requireUser).WithSearchCache(s.searchCache),
		Profiles:      profiles.NewHandler(s.profiles, logger, requireUser).WithCatalogCache(s.catalogCache),
		Applications:  applications.NewHandler(s.applications, logger, requireUser),
		References:    references.NewHandler(s.references, logger, requireUser),
		Notifications: notifications.NewHandler(s.notes, logger, requireUser),
		Offers:        offers.NewHandler(offers.NewService(pool, s.notes, offers.DefaultConfig()), logger, requireUser),
		Matching:      matching.NewHandler(s.matching, logger, requireUser),
		Reference:     refdata.NewHandler(refdata.NewService(pool), logger),
		Landing:       landing.NewHandler(landing.NewService(pool), logger),
		Journals:      journals.NewHandler(journals.NewService(pool), logger, requireUser),
		Export:        dataexport.NewHandler(dataexport.NewService(pool, s.vault, dataexport.DefaultConfig(cfg.Product.Name)), logger, requireUser),
		Web:           web,
		Perimeter:     httpapi.Perimeter{Proxies: cfg.TrustedProxies, HTTPS: cfg.HTTPS(), Store: s.counters, Now: s.now},
		Metrics:       s.metrics,
	})
}

// flushers — что дождаться перед выходом: письма, которые сервисы отправляют в очередь в фоне.
func (s *services) flushers() []func() {
	return []func(){s.accounts.Flush, s.organizations.Flush}
}

// openVault подключает хранилище файлов и антивирус. Хранилище, которое не отвечает при запуске, — ошибка запуска:
// без него нельзя ни принять, ни выдать файл. Антивирус может подняться позже: до тех пор файлы ждут проверки.
func openVault(ctx context.Context, cfg config.Config, logger *slog.Logger) (*files.Vault, error) {
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	store, err := blobstore.NewS3(connectCtx, blobstore.S3Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey, SecretKey: cfg.S3.SecretKey,
		Region: cfg.S3.Region, TLS: cfg.S3.TLS, PathStyle: cfg.S3.PathStyle, CreateBucket: cfg.S3.CreateBucket,
	})
	if err != nil {
		return nil, fmt.Errorf("file storage %s: %w", cfg.S3.Endpoint, err)
	}
	var scanner antivirus.Scanner
	if strings.EqualFold(cfg.ClamAVAddr, config.ClamAVOff) {
		logger.Warn("antivirus is OFF (SCIBOX_CLAMAV_ADDR=off): uploaded files are not checked; development only")
		scanner = antivirus.Skip{}
	} else {
		clamd := antivirus.NewClamd(cfg.ClamAVAddr)
		pingCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		if err := clamd.Ping(pingCtx); err != nil {
			logger.Warn("antivirus does not answer yet: new files wait for the check", "addr", cfg.ClamAVAddr, "err", err)
		}
		scanner = clamd
	}
	return files.NewVault(store, scanner, func() time.Time { return time.Now().UTC() }), nil
}

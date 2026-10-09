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

	"scibox/server/internal/applications"
	"scibox/server/internal/auth"
	"scibox/server/internal/config"
	"scibox/server/internal/crossref"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/health"
	"scibox/server/internal/httpapi"
	"scibox/server/internal/httpguard"
	"scibox/server/internal/journals"
	"scibox/server/internal/landing"
	"scibox/server/internal/logmask"
	"scibox/server/internal/mail"
	"scibox/server/internal/matching"
	"scibox/server/internal/migrate"
	"scibox/server/internal/notifications"
	"scibox/server/internal/offers"
	"scibox/server/internal/orgs"
	"scibox/server/internal/profiles"
	"scibox/server/internal/refdata"
	"scibox/server/internal/references"
	"scibox/server/internal/vacancies"
	"scibox/server/seed"
)

// outboxInterval — как часто отправитель смотрит в очередь писем.
const outboxInterval = 2 * time.Second

// matchingInterval — как часто фоновый цикл смотрит, не пора ли слать письма о новых вакансиях и напоминания о сроках.
const matchingInterval = 5 * time.Minute

// Version задаётся при сборке через -ldflags; при локальном запуске "dev".
var Version = "dev"

const usage = `Использование: scibox <команда>

Команды:
  serve                      запустить HTTP-сервер (по умолчанию)
  migrate up|down|reset|status   миграции базы данных
  seed                       загрузить демо-данные
  journals load [--force] [файл]  загрузить справочник журналов SCImago (без файла — встроенный в сервер)
  version                    показать версию
`

// Env — внешний мир программы, подменяемый в тестах.
type Env struct {
	Getenv func(string) string
	Stdout io.Writer
	Stderr io.Writer
	// OnListen вызывается, когда сервер начал принимать соединения (для тестов).
	OnListen func(addr net.Addr)
}

// Run выполняет команду и возвращает код выхода процесса.
func Run(ctx context.Context, args []string, env Env) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	logger := newLogger(env.Stderr)
	defer useAsDefault(logger)()

	switch cmd {
	case "version":
		fmt.Fprintln(env.Stdout, Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	case "serve", "migrate", "seed", "journals":
	default:
		fmt.Fprintf(env.Stderr, "неизвестная команда %q\n\n%s", cmd, usage)
		return 2
	}

	cfg, err := config.Load(env.Getenv)
	if err != nil {
		logger.Error("load config", "err", err)
		return 1
	}

	switch cmd {
	case "migrate":
		if len(args) != 1 {
			fmt.Fprint(env.Stderr, usage)
			return 2
		}
		if err := migrate.Command(ctx, cfg.DatabaseURL, args[0], env.Stdout); err != nil {
			logger.Error("migrate", "err", err)
			return 1
		}
		return 0
	case "journals":
		return runJournals(ctx, cfg.DatabaseURL, args, env, logger)
	case "seed":
		if err := runSeed(ctx, cfg.DatabaseURL, env.Stdout); err != nil {
			logger.Error("seed", "err", err)
			return 1
		}
		return 0
	default:
		if err := serve(ctx, cfg, logger, env.OnListen); err != nil {
			logger.Error("serve", "err", err)
			return 1
		}
		return 0
	}
}

// newLogger — единственное место, где создаётся журнал сервера: всё, что в него пишется, проходит через logmask (T-18),
// поэтому почты, адреса людей и токены не попадают в записи, где бы их ни написали по ошибке.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(logmask.NewHandler(slog.NewTextHandler(w, nil)))
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

func runSeed(ctx context.Context, databaseURL string, out io.Writer) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	res, err := seed.Run(ctx, pool, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Демо-данные загружены: новых людей %d, организаций %d, вакансий %d, профилей соискателей %d, откликов %d, приглашений %d, избранных вакансий %d, сохранённых поисков %d.\n", res.People, res.Organizations, res.Vacancies, res.Profiles, res.Applications, res.Offers, res.Favorites, res.Searches)
	fmt.Fprintf(out, "Вход для проверки: %s (пароль записан в server/seed/seed.go).\n", seed.Logins()[0])
	return nil
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, onListen func(net.Addr)) error {
	// Сайт проверяется до базы и фоновых задач: несобранный сайт — ошибка запуска, а не пустые страницы.
	var web http.Handler
	if cfg.WebDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err != nil {
			return fmt.Errorf("site not built in %s (run npm run build in web/): %w", cfg.WebDir, err)
		}
		web = httpapi.WebHandler(os.DirFS(cfg.WebDir))
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
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

	a := newApp(cfg, pool, logger, health.Checker{Queries: dbgen.New(pool), Migrations: migrations}, web, time.Now)
	// Фоновые задачи останавливаются отменой cleanupCtx; сервер дожидается их до закрытия базы и выхода.
	var background sync.WaitGroup
	defer background.Wait()
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	for _, flush := range a.flush {
		defer flush()
	}
	for _, run := range a.background {
		background.Go(func() { run(cleanupCtx) })
	}
	handler := a.handler

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	srv := httpguard.NewServer(handler, httpguard.DefaultTimeouts())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info("server started", "addr", ln.Addr().String(), "product", cfg.Product.Name, "env", cfg.Env,
		"trusted_proxies", !cfg.TrustedProxies.Empty())
	if onListen != nil {
		onListen(ln.Addr())
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

// app — собранный сервер: обработчик всех адресов и фоновые задачи, которые запускает serve.
type app struct {
	handler http.Handler
	// background — фоновые циклы; каждый работает, пока не отменят ctx.
	background []func(ctx context.Context)
	// flush дожидается писем, которые сервисы отправляют в фоне, перед выходом.
	flush []func()
}

// newApp собирает сервисы и роутер так же, как в работе. Отдельно от serve, чтобы тесты проверяли
// настоящую сборку (например, что каждый адрес ограничивает тело запроса). now — часы ограничения частоты.
func newApp(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger, checker httpapi.HealthChecker, web http.Handler, now func() time.Time) app {
	// Письма сначала попадают в очередь в базе и уходят оттуда в фоне: они переживают падение сервера и повторяются при сбоях.
	smtp := mail.SMTP{Addr: cfg.SMTPAddr, From: cfg.MailFrom, TLS: cfg.SMTPTLS, Username: cfg.SMTPUser, Password: cfg.SMTPPassword}
	queue := notifications.NewQueue(pool)
	accounts := auth.NewService(pool, queue, auth.DefaultConfig(cfg.Product.Name, cfg.PublicURL), logger)
	organizations := orgs.NewService(pool, queue, orgs.DefaultConfig(cfg.Product.Name, cfg.PublicURL), logger)
	worker := notifications.NewWorker(pool, smtp, logger)
	authHandler := auth.NewHandler(accounts, logger)

	notes := notifications.NewService(pool, notifications.Config{ProductName: cfg.Product.Name, PublicURL: cfg.PublicURL})
	profileSvc := profiles.NewService(pool, crossref.NewClient(cfg.CrossrefURL, cfg.CrossrefMailto), profiles.DefaultConfig(cfg.Product.Name))
	refSvc := references.NewService(pool, notes, references.DefaultConfig(cfg.Product.Name, cfg.PublicURL))
	appSvc := applications.NewService(pool, profileSvc, refSvc, notes, applications.DefaultConfig())
	vacancySvc := vacancies.NewService(pool, vacancies.DefaultConfig())
	matchingSvc := matching.NewService(pool, vacancySvc, notes, matching.DefaultConfig(), logger)
	// Счётчики частоты в базе: лимит общий для всех копий сервера (D-141).
	counters := httpguard.NewPostgresStore(pool)

	handler := httpapi.NewRouter(httpapi.Deps{
		ProductName:   cfg.Product.Name,
		Version:       Version,
		Health:        checker,
		Logger:        logger,
		Auth:          authHandler,
		Orgs:          orgs.NewHandler(organizations, logger, authHandler.RequireUser),
		Vacancies:     vacancies.NewHandler(vacancySvc, logger, authHandler.RequireUser),
		Profiles:      profiles.NewHandler(profileSvc, logger, authHandler.RequireUser),
		Applications:  applications.NewHandler(appSvc, logger, authHandler.RequireUser),
		References:    references.NewHandler(refSvc, logger, authHandler.RequireUser),
		Notifications: notifications.NewHandler(notes, logger, authHandler.RequireUser),
		Offers:        offers.NewHandler(offers.NewService(pool, notes, offers.DefaultConfig()), logger, authHandler.RequireUser),
		Matching:      matching.NewHandler(matchingSvc, logger, authHandler.RequireUser),
		Reference:     refdata.NewHandler(refdata.NewService(pool), logger),
		Landing:       landing.NewHandler(landing.NewService(pool), logger),
		Journals:      journals.NewHandler(journals.NewService(pool), logger, authHandler.RequireUser),
		Web:           web,
		Perimeter:     httpapi.Perimeter{Proxies: cfg.TrustedProxies, HTTPS: cfg.HTTPS(), Store: counters, Now: now},
	})
	return app{
		handler: handler,
		background: []func(ctx context.Context){
			func(ctx context.Context) { accounts.RunCleanup(ctx, time.Hour) },
			func(ctx context.Context) { organizations.RunCleanup(ctx, time.Hour) },
			func(ctx context.Context) { worker.Run(ctx, outboxInterval) },
			func(ctx context.Context) { matchingSvc.Run(ctx, matchingInterval) },
			func(ctx context.Context) { counters.RunCleanup(ctx, time.Hour, logger) },
		},
		flush: []func(){accounts.Flush, organizations.Flush},
	}
}

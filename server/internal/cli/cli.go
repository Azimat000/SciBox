// Package cli разбирает команды бинарника сервера: serve, migrate, seed, version.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"scibox/server/internal/auth"
	"scibox/server/internal/config"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/health"
	"scibox/server/internal/httpapi"
	"scibox/server/internal/mail"
	"scibox/server/internal/migrate"
)

// Version задаётся при сборке через -ldflags; при локальном запуске "dev".
var Version = "dev"

const usage = `Использование: scibox <команда>

Команды:
  serve                      запустить HTTP-сервер (по умолчанию)
  migrate up|down|reset|status   миграции базы данных
  seed                       загрузить демо-данные
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
	logger := slog.New(slog.NewTextHandler(env.Stderr, nil))

	switch cmd {
	case "version":
		fmt.Fprintln(env.Stdout, Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	case "serve", "migrate", "seed":
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
	case "seed":
		// Демо-данные появятся вместе с разделами (срезы 4–6).
		fmt.Fprintln(env.Stdout, "Демо-данных пока нет: они появятся вместе с организациями и вакансиями.")
		return 0
	default:
		if err := serve(ctx, cfg, logger, env.OnListen); err != nil {
			logger.Error("serve", "err", err)
			return 1
		}
		return 0
	}
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, onListen func(net.Addr)) error {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	migrations, err := migrate.NewProvider(sqlDB)
	if err != nil {
		return err
	}

	mailer := mail.SMTP{Addr: cfg.SMTPAddr, From: cfg.MailFrom}
	accounts := auth.NewService(pool, mailer, auth.DefaultConfig(cfg.Product.Name, cfg.PublicURL), logger)
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	defer stopCleanup()
	go accounts.RunCleanup(cleanupCtx, time.Hour)
	defer accounts.Flush()

	handler := httpapi.NewRouter(httpapi.Deps{
		ProductName: cfg.Product.Name,
		Version:     Version,
		Health:      health.Checker{Queries: dbgen.New(pool), Migrations: migrations},
		Logger:      logger,
		Auth:        auth.NewHandler(accounts, logger),
	})

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	logger.Info("server started", "addr", ln.Addr().String(), "product", cfg.Product.Name)
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

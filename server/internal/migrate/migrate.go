// Package migrate накатывает и откатывает миграции goose, встроенные в бинарник (D-030).
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	dbfiles "scibox/server/db"
)

// NewProvider создаёт провайдер goose поверх открытого соединения.
func NewProvider(db *sql.DB) (*goose.Provider, error) {
	// fs.Sub ошибается только на недопустимом пути, а "migrations" — константа.
	sub, _ := fs.Sub(dbfiles.Migrations, "migrations")
	return newProvider(db, sub)
}

func newProvider(db *sql.DB, files fs.FS) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, db, files)
	if err != nil {
		return nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, nil
}

// Command выполняет команду миграций по адресу базы: up, down, reset или status.
// Итоги пишутся в out.
func Command(ctx context.Context, databaseURL, command string, out io.Writer) (err error) {
	switch command {
	case "up", "down", "reset", "status":
	default:
		return fmt.Errorf("unknown migrate command %q (want up, down, reset or status)", command)
	}
	connCfg, err := connConfig(databaseURL)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { err = errors.Join(err, db.Close()) }()
	p, err := NewProvider(db)
	if err != nil {
		return err
	}
	return run(ctx, p, command, out)
}

// LockTimeout — сколько миграция ждёт блокировки таблицы. Выкладка идёт, пока сайт работает: изменение таблицы встаёт
// в очередь за долгим запросом, а все следующие запросы к этой таблице встают за ним, и сайт «замирает». Лучше,
// чтобы миграция сдалась через 10 секунд и выкладку повторили, чем остановила сайт (срез 28). Срок ожидания блокировки,
// а не срок самой миграции: долгая сборка индекса или заполнение не прерываются.
const LockTimeout = "10s"

// connConfig разбирает адрес базы и добавляет к соединению срок ожидания блокировок. Значение, которое администратор
// сам указал в адресе (`?lock_timeout=…`), остаётся его.
func connConfig(databaseURL string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if _, set := cfg.RuntimeParams["lock_timeout"]; !set {
		cfg.RuntimeParams["lock_timeout"] = LockTimeout
	}
	return cfg, nil
}

func run(ctx context.Context, p *goose.Provider, command string, out io.Writer) error {
	switch command {
	case "up":
		results, err := p.Up(ctx)
		printResults(out, results)
		return err
	case "down":
		result, err := p.Down(ctx)
		if result != nil {
			printResults(out, []*goose.MigrationResult{result})
		}
		return err
	case "reset":
		results, err := p.DownTo(ctx, 0)
		printResults(out, results)
		return err
	default: // status
		statuses, err := p.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			fmt.Fprintf(out, "%-8s %s\n", s.State, s.Source.Path)
		}
		return nil
	}
}

func printResults(out io.Writer, results []*goose.MigrationResult) {
	if len(results) == 0 {
		fmt.Fprintln(out, "no migrations to apply")
		return
	}
	for _, r := range results {
		fmt.Fprintln(out, r.String())
	}
}

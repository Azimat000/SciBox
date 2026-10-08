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
	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	db := stdlib.OpenDB(*connCfg)
	defer func() { err = errors.Join(err, db.Close()) }()
	p, err := NewProvider(db)
	if err != nil {
		return err
	}
	return run(ctx, p, command, out)
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

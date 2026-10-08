// Package testdb создаёт для тестов временную базу в PostgreSQL из Docker (D-031).
//
// Каждый вызов Create заводит отдельную базу scibox_t_<случайно>, при желании
// накатывает миграции и удаляет базу после теста. Подделок базы нет.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/migrate"
)

// DefaultAdminURL — служебная база контейнера из docker-compose.yml.
const DefaultAdminURL = "postgres://scibox:scibox@localhost:5433/postgres?sslmode=disable" //nolint:gosec // G101: служебная база тестового контейнера, пароль известен всем

// TB — часть testing.TB, которой пользуется пакет (удобно подменять в своих тестах).
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// AdminURL возвращает адрес служебной базы: TEST_DATABASE_URL или значение по умолчанию.
func AdminURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return DefaultAdminURL
}

// Create заводит пустую временную базу и возвращает её адрес.
// Если migrated, накатывает все миграции.
func Create(t TB, migrated bool) string {
	t.Helper()
	dbURL, err := create(context.Background(), AdminURL(), migrated, t.Cleanup)
	if err != nil {
		t.Fatalf("testdb: %v (запущен ли Docker? make db-up)", err)
	}
	return dbURL
}

// New заводит временную базу с миграциями и открывает к ней пул соединений.
func New(t TB) *pgxpool.Pool {
	t.Helper()
	dbURL := Create(t, true)
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("testdb: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func create(ctx context.Context, adminURL string, migrated bool, cleanup func(func())) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	name, err := randomName()
	if err != nil {
		return "", err
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return "", fmt.Errorf("connect admin database: %w", err)
	}
	defer func() { _ = admin.Close(context.Background()) }() // служебное соединение тестов: ошибка закрытия ни на что не влияет
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", fmt.Errorf("create database: %w", err)
	}
	cleanup(func() { _ = drop(adminURL, name) })

	dbURL, err := withDatabase(adminURL, name)
	if err != nil {
		return "", err
	}
	if migrated {
		if err := migrate.Command(ctx, dbURL, "up", io.Discard); err != nil {
			return "", fmt.Errorf("migrate: %w", err)
		}
	}
	return dbURL, nil
}

func drop(adminURL, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close(context.Background()) }() // служебное соединение тестов: ошибка закрытия ни на что не влияет
	_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	return err
}

func withDatabase(adminURL, name string) (string, error) {
	u, err := url.Parse(adminURL)
	if err != nil {
		return "", fmt.Errorf("parse admin url: %w", err)
	}
	u.Path = "/" + name
	return u.String(), nil
}

// randSource подменяется в тестах, чтобы проверить ошибку генератора.
var randSource io.Reader = rand.Reader

func randomName() (string, error) {
	b := make([]byte, 6)
	if _, err := io.ReadFull(randSource, b); err != nil {
		return "", fmt.Errorf("random name: %w", err)
	}
	return "scibox_t_" + hex.EncodeToString(b), nil
}

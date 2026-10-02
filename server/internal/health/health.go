// Package health проверяет, что база данных доступна и схема накатана.
package health

import (
	"context"
	"fmt"
)

// Database — состояние базы для ответа /api/health.
type Database struct {
	SchemaVersion int64  `json:"schema_version"`
	ServerVersion string `json:"server_version"`
}

// Querier — узкий срез сгенерированных sqlc запросов, нужный проверке.
type Querier interface {
	ServerVersion(ctx context.Context) (string, error)
}

// Versioner сообщает номер последней применённой миграции.
type Versioner interface {
	GetDBVersion(ctx context.Context) (int64, error)
}

// Checker проверяет базу.
type Checker struct {
	Queries    Querier
	Migrations Versioner
}

// CheckDatabase выполняет запрос к базе и читает версию схемы.
func (c Checker) CheckDatabase(ctx context.Context) (Database, error) {
	serverVersion, err := c.Queries.ServerVersion(ctx)
	if err != nil {
		return Database{}, fmt.Errorf("query database: %w", err)
	}
	schema, err := c.Migrations.GetDBVersion(ctx)
	if err != nil {
		return Database{}, fmt.Errorf("read schema version: %w", err)
	}
	return Database{SchemaVersion: schema, ServerVersion: serverVersion}, nil
}

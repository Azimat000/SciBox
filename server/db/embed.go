// Package db встраивает SQL-миграции в бинарник сервера.
package db

import "embed"

// Migrations содержит файлы goose из папки migrations.
//
//go:embed migrations/*.sql
var Migrations embed.FS

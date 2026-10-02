// Package config собирает настройки сервера из переменных окружения
// и общего файла продукта config/product.json (D-029).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Значения по умолчанию рассчитаны на локальный запуск из папки server/.
const (
	DefaultHTTPAddr      = "127.0.0.1:8080"
	DefaultDatabaseURL   = "postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable"
	DefaultProductConfig = "../config/product.json"
)

// Product описывает продукт; файл общий для сервера и сайта.
type Product struct {
	Name string `json:"name"`
}

// Config содержит все настройки сервера.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	Product     Product
}

// Load читает настройки. getenv обычно os.Getenv; в тестах подставляется своя функция.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:    envOr(getenv, "SCIBOX_HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL: envOr(getenv, "DATABASE_URL", DefaultDatabaseURL),
	}
	product, err := LoadProduct(envOr(getenv, "SCIBOX_PRODUCT_CONFIG", DefaultProductConfig))
	if err != nil {
		return Config{}, err
	}
	cfg.Product = product
	return cfg, nil
}

// LoadProduct читает и проверяет файл продукта.
func LoadProduct(path string) (Product, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Product{}, fmt.Errorf("read product config %q: %w", path, err)
	}
	var p Product
	if err := json.Unmarshal(raw, &p); err != nil {
		return Product{}, fmt.Errorf("parse product config %q: %w", path, err)
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Product{}, errors.New("product config: name is empty")
	}
	return p, nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Package config собирает настройки сервера из переменных окружения
// и общего файла продукта config/product.json (D-029).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
)

// Значения по умолчанию рассчитаны на локальный запуск из папки server/.
const (
	DefaultHTTPAddr      = "127.0.0.1:8080"
	DefaultDatabaseURL   = "postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable"
	DefaultProductConfig = "../config/product.json"
	DefaultSMTPAddr      = "localhost:1025" // Mailpit из docker-compose.yml
	DefaultPublicURL     = "http://localhost:5173"
	DefaultCrossrefURL   = "https://api.crossref.org"
	defaultMailAddress   = "no-reply@scibox.local"
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
	// SMTPAddr — почтовый сервер (локально Mailpit), MailFrom — отправитель писем целиком.
	SMTPAddr string
	MailFrom string
	// SMTPTLS — шифрование: off (локально), starttls (порт 587) или tls (порт 465).
	// SMTPUser и SMTPPassword — вход на почтовый сервер; пусто — без входа.
	SMTPTLS      string
	SMTPUser     string
	SMTPPassword string
	// PublicURL — адрес, по которому человек открывает сайт: из него строятся ссылки в письмах.
	PublicURL string
	// CrossrefURL — адрес API Crossref (поиск публикаций по DOI); CrossrefMailto — почта для «вежливого пула» Crossref.
	CrossrefURL    string
	CrossrefMailto string
	// WebDir — папка собранного сайта (web/dist). Если задана, сервер сам отдаёт страницы сайта
	// по всем адресам вне /api (D-131); пусто — только API, а сайт отдаёт Vite.
	WebDir string
}

// Load читает настройки. getenv обычно os.Getenv; в тестах подставляется своя функция.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:    envOr(getenv, "SCIBOX_HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL: envOr(getenv, "DATABASE_URL", DefaultDatabaseURL),
		SMTPAddr:    envOr(getenv, "SCIBOX_SMTP_ADDR", DefaultSMTPAddr),
		SMTPTLS:     strings.ToLower(envOr(getenv, "SCIBOX_SMTP_TLS", "off")),
		SMTPUser:    envOr(getenv, "SCIBOX_SMTP_USER", ""),
		// Пароль не обрезается: пробелы по краям могут быть его частью.
		SMTPPassword: getenv("SCIBOX_SMTP_PASSWORD"),
		PublicURL:    strings.TrimRight(envOr(getenv, "SCIBOX_PUBLIC_URL", DefaultPublicURL), "/"),

		CrossrefURL:    strings.TrimRight(envOr(getenv, "SCIBOX_CROSSREF_URL", DefaultCrossrefURL), "/"),
		CrossrefMailto: envOr(getenv, "SCIBOX_CROSSREF_MAILTO", ""),
		WebDir:         envOr(getenv, "SCIBOX_WEB_DIR", ""),
	}
	switch cfg.SMTPTLS {
	case "off", "starttls", "tls":
	default:
		return Config{}, fmt.Errorf("SCIBOX_SMTP_TLS: want off, starttls or tls, got %q", cfg.SMTPTLS)
	}
	if cfg.SMTPUser != "" && cfg.SMTPTLS == "off" {
		return Config{}, errors.New("SCIBOX_SMTP_USER is set but SCIBOX_SMTP_TLS is off: the password would travel unencrypted")
	}
	product, err := LoadProduct(envOr(getenv, "SCIBOX_PRODUCT_CONFIG", DefaultProductConfig))
	if err != nil {
		return Config{}, err
	}
	cfg.Product = product
	cfg.MailFrom = envOr(getenv, "SCIBOX_MAIL_FROM", (&mail.Address{Name: product.Name, Address: defaultMailAddress}).String())
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

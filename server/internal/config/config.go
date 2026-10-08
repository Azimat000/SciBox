// Package config собирает настройки сервера из переменных окружения
// и общего файла продукта config/product.json (D-029).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/httpguard"
)

// Значения по умолчанию рассчитаны на локальный запуск из папки server/.
const (
	DefaultHTTPAddr      = "127.0.0.1:8080"
	DefaultDatabaseURL   = "postgres://scibox:scibox@localhost:5433/scibox?sslmode=disable" //nolint:gosec // G101: пароль локальной базы из docker-compose.yml; в боевом режиме он запрещён (checkProduction, T-08)
	DefaultProductConfig = "../config/product.json"
	DefaultSMTPAddr      = "localhost:1025" // Mailpit из docker-compose.yml
	DefaultPublicURL     = "http://localhost:5173"
	DefaultCrossrefURL   = "https://api.crossref.org"
	defaultMailAddress   = "no-reply@scibox.local"
	// defaultDatabasePassword — пароль из docker-compose.yml и DefaultDatabaseURL.
	defaultDatabasePassword = "scibox"
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
	// Env — development (по умолчанию) или production: в боевом режиме опасные настройки не дают запуститься (T-08).
	Env string
	// TrustedProxies — прокси, которым верим X-Forwarded-For (SCIBOX_TRUSTED_PROXIES, T-06); пусто — никому.
	TrustedProxies httpguard.Proxies
}

// Режимы SCIBOX_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// HTTPS сообщает, что сайт открывается по https: тогда cookie Secure и заголовок HSTS.
func (c Config) HTTPS() bool {
	return strings.HasPrefix(strings.ToLower(c.PublicURL), "https://")
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
		Env:            strings.ToLower(envOr(getenv, "SCIBOX_ENV", EnvDevelopment)),
	}
	if cfg.Env != EnvDevelopment && cfg.Env != EnvProduction {
		return Config{}, fmt.Errorf("SCIBOX_ENV: want development or production, got %q", cfg.Env)
	}
	proxies, err := httpguard.ParseProxies(getenv("SCIBOX_TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, fmt.Errorf("SCIBOX_TRUSTED_PROXIES: %w", err)
	}
	cfg.TrustedProxies = proxies
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
	if cfg.Env == EnvProduction {
		if err := checkProduction(cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// checkProduction не даёт запустить боевой сервер с настройками для локальной работы (T-08).
// Перечисляет все проблемы сразу и для каждой пишет, что исправить.
func checkProduction(cfg Config) error {
	var problems []error
	if u, err := url.Parse(cfg.PublicURL); err != nil || u.Scheme != "https" || u.Host == "" {
		problems = append(problems, fmt.Errorf("SCIBOX_PUBLIC_URL must be the https:// address of the site, got %q: "+
			"over http passwords and the session cookie travel unencrypted", cfg.PublicURL))
	}
	problems = append(problems, checkDatabase(cfg.DatabaseURL)...)
	if cfg.SMTPTLS == "off" {
		problems = append(problems, errors.New("SCIBOX_SMTP_TLS is off: set starttls (port 587) or tls (port 465) "+
			"so that mail with sign-in links is encrypted on the way to the mail service"))
	}
	if from, err := mail.ParseAddress(cfg.MailFrom); err != nil || strings.HasSuffix(strings.ToLower(from.Address), ".local") {
		problems = append(problems, fmt.Errorf("SCIBOX_MAIL_FROM must be a real address on the site's domain, got %q: "+
			"mail services reject letters from .local addresses", cfg.MailFrom))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("SCIBOX_ENV=production refuses unsafe settings:\n%w", errors.Join(problems...))
}

// checkDatabase: пароль из docker-compose.yml и база в сети без шифрования в бою запрещены.
func checkDatabase(dsn string) []error {
	pg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		// Сам адрес не показываем: в нём пароль.
		return []error{errors.New("DATABASE_URL cannot be parsed: check its format")}
	}
	var problems []error
	if pg.Password == defaultDatabasePassword {
		problems = append(problems, errors.New("DATABASE_URL uses the development password from docker-compose.yml: "+
			"create a database user with a long random password"))
	}
	if !localHost(pg.Host) && !alwaysTLS(pg) {
		problems = append(problems, fmt.Errorf("DATABASE_URL connects to %s without encryption: "+
			"add sslmode=verify-full (or at least sslmode=require)", pg.Host))
	}
	return problems
}

// alwaysTLS — к базе нельзя подключиться без шифрования: sslmode require, verify-ca или verify-full.
// У disable, allow и prefer есть попытка без TLS.
func alwaysTLS(pg *pgconn.Config) bool {
	if pg.TLSConfig == nil {
		return false
	}
	for _, fb := range pg.Fallbacks {
		if fb.TLSConfig == nil {
			return false
		}
	}
	return true
}

// localHost — база на этой же машине: сокет Unix (путь) или адрес петли.
func localHost(host string) bool {
	if strings.HasPrefix(host, "/") || strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// LoadProduct читает и проверяет файл продукта.
func LoadProduct(path string) (Product, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: путь из настройки сервера (SCIBOX_PRODUCT_CONFIG), а не от посетителя
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

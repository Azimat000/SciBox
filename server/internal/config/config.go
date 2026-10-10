// Package config собирает настройки сервера из переменных окружения
// и общего файла продукта config/product.json (D-029).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/dbpool"
	"scibox/server/internal/errreport"
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
	// Хранилище файлов и антивирус из docker-compose.yml (срез 23, D-157).
	DefaultS3Endpoint  = "localhost:8333"
	DefaultS3Bucket    = "scibox-files"
	DefaultS3Region    = "us-east-1"
	DefaultS3AccessKey = "scibox"
	DefaultS3SecretKey = "scibox-dev-secret" //nolint:gosec // G101: ключ локального хранилища из docker-compose.yml; в боевом режиме он запрещён (checkProduction)
	DefaultClamAVAddr  = "localhost:3310"
	// ClamAVOff отключает проверку на вирусы: только для разработки без контейнера, в боевом режиме запрещено.
	ClamAVOff          = "off"
	defaultMailAddress = "no-reply@scibox.local"
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
	// S3 — хранилище файлов откликов (SCIBOX_S3_*, срез 23).
	S3 S3
	// ClamAVAddr — адрес clamd «хост:порт» (SCIBOX_CLAMAV_ADDR) или ClamAVOff.
	ClamAVAddr string
	// DB — пул соединений с базой и сроки для неё (SCIBOX_DB_*, срез 24, T-22).
	DB dbpool.Config
	// Background — выполняет ли `serve` фоновые задачи (SCIBOX_BACKGROUND, срез 25). По умолчанию да: сервер без
	// воркера работает целиком. Для нескольких копий и отдельного процесса `worker` ставится off.
	Background bool
	// ShutdownTimeout — сколько после сигнала остановки сервер ждёт текущие запросы, а воркер дописывает текущие задачи
	// (SCIBOX_SHUTDOWN_TIMEOUT, срез 25). Контейнеру нужно оставить не меньше этого срока до принудительной остановки.
	ShutdownTimeout time.Duration
	// CrossrefMaxConcurrent — сколько запросов к Crossref одна копия ведёт одновременно (SCIBOX_CROSSREF_MAX_CONCURRENT,
	// срез 25, T-24). Публичный пул Crossref допускает 1 одновременный запрос на адрес, «вежливый» 3; по умолчанию 1.
	CrossrefMaxConcurrent int
	// SearchCacheTTL — сколько копия сервера помнит ответ поиска вакансий и каталога учёных (для гостя) на один и тот же
	// запрос (SCIBOX_SEARCH_CACHE_TTL, срез 27, D-179). 0 — кеш выключен.
	SearchCacheTTL time.Duration
	// Log — формат и уровень журнала (SCIBOX_LOG_FORMAT, SCIBOX_LOG_LEVEL, срез 26).
	Log Log
	// Ops — внутренний порт: проверки живости, метрики, pprof (SCIBOX_OPS_*, срез 26, T-25).
	Ops Ops
	// ErrorDSN — адрес приёма ошибок GlitchTip (SCIBOX_ERROR_DSN, срез 26); пусто — ошибки никуда не отправляются.
	ErrorDSN string
}

// Log — настройки журнала сервера.
type Log struct {
	// JSON — строка журнала это один объект JSON (в бою, чтобы системы сбора логов разбирали поля); иначе читаемый текст.
	JSON  bool
	Level slog.Level
}

// Ops — внутренний порт сервера и воркера. Он не для людей и не для интернета: проверки для оркестратора, метрики для
// Prometheus и pprof для разработчика. Сайт и API на нём не отдаются, а его адреса не отдаются на публичном порту.
type Ops struct {
	// Addr — адрес «хост:порт»; пусто — взять адрес процесса по умолчанию (у сервера свой, у воркера свой), OpsOff —
	// порт выключен.
	Addr string
	// Token — общий секрет: если задан, каждый запрос к внутреннему порту должен нести `Authorization: Bearer <секрет>`.
	// В бою обязателен, когда порт слушает не петлю и не частную сеть.
	Token string
	// Pprof — отдавать ли /debug/pprof/. В разработке включён, в бою выключен: профиль раскрывает внутренности процесса
	// и сам стоит процессора.
	Pprof bool
}

// OpsOff выключает внутренний порт.
const OpsOff = "off"

// minOpsToken — наименьшая длина секрета внутреннего порта.
const minOpsToken = 24

// Значения срока остановки и числа обращений к Crossref по умолчанию и пределы.
const (
	DefaultShutdownTimeout       = 20 * time.Second
	DefaultSearchCacheTTL        = 15 * time.Second
	maxSearchCacheTTL            = 5 * time.Minute
	DefaultCrossrefMaxConcurrent = 1
	maxCrossrefMaxConcurrent     = 10
)

// S3 — настройки S3-совместимого хранилища.
type S3 struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	// TLS — ходить по https (SCIBOX_S3_TLS=on). В боевом режиме обязательно.
	TLS bool
	// PathStyle — адреса вида endpoint/bucket/key (SCIBOX_S3_PATH_STYLE). Локальному хранилищу нужно; у облаков обычно нет.
	PathStyle bool
	// CreateBucket — создать бакет при старте, если его нет (SCIBOX_S3_CREATE_BUCKET). Только для разработки.
	CreateBucket bool
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
	dev := cfg.Env == EnvDevelopment
	cfg.ClamAVAddr = envOr(getenv, "SCIBOX_CLAMAV_ADDR", DefaultClamAVAddr)
	if cfg.S3, err = loadS3(getenv, dev); err != nil {
		return Config{}, err
	}
	if cfg.DB, err = loadDB(getenv); err != nil {
		return Config{}, err
	}
	if cfg.Background, err = envBool(getenv, "SCIBOX_BACKGROUND", true); err != nil {
		return Config{}, err
	}
	cfg.ShutdownTimeout = DefaultShutdownTimeout
	if v := strings.TrimSpace(getenv("SCIBOX_SHUTDOWN_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("SCIBOX_SHUTDOWN_TIMEOUT: want a positive duration like 20s, got %q", v)
		}
		cfg.ShutdownTimeout = d
	}
	cfg.CrossrefMaxConcurrent = DefaultCrossrefMaxConcurrent
	if v := strings.TrimSpace(getenv("SCIBOX_CROSSREF_MAX_CONCURRENT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxCrossrefMaxConcurrent {
			return Config{}, fmt.Errorf("SCIBOX_CROSSREF_MAX_CONCURRENT: want a number from 1 to %d, got %q", maxCrossrefMaxConcurrent, v)
		}
		cfg.CrossrefMaxConcurrent = n
	}
	cfg.SearchCacheTTL = DefaultSearchCacheTTL
	if v := strings.ToLower(strings.TrimSpace(getenv("SCIBOX_SEARCH_CACHE_TTL"))); v != "" {
		if v == "off" || v == "0" {
			cfg.SearchCacheTTL = 0
		} else {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 || d > maxSearchCacheTTL {
				return Config{}, fmt.Errorf("SCIBOX_SEARCH_CACHE_TTL: want off or a duration from 1s to %v like 15s, got %q", maxSearchCacheTTL, v)
			}
			cfg.SearchCacheTTL = d
		}
	}
	if cfg.Log, err = loadLog(getenv, cfg.Env == EnvProduction); err != nil {
		return Config{}, err
	}
	if cfg.Ops, err = loadOps(getenv, dev); err != nil {
		return Config{}, err
	}
	cfg.ErrorDSN = strings.TrimSpace(getenv("SCIBOX_ERROR_DSN"))
	if cfg.ErrorDSN != "" {
		if err := errreport.ValidateDSN(cfg.ErrorDSN); err != nil {
			return Config{}, fmt.Errorf("SCIBOX_ERROR_DSN: %w (the address is not printed: it carries a key)", err)
		}
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
	if cfg.Env == EnvProduction {
		if err := checkProduction(cfg); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// loadDB читает настройки пула. Нулевое или отрицательное значение — ошибка, а не «без срока»: администратор,
// написавший 0, ждёт отключения срока, а получил бы обычный.
func loadDB(getenv func(string) string) (dbpool.Config, error) {
	var c dbpool.Config
	if v := strings.TrimSpace(getenv("SCIBOX_DB_MAX_CONNS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			return c, fmt.Errorf("SCIBOX_DB_MAX_CONNS: want a number from 1 to 1000, got %q", v)
		}
		c.MaxConns = int32(n) //nolint:gosec // G115: число проверено выше (1…1000)
	}
	for _, d := range []struct {
		key string
		dst *time.Duration
	}{
		{"SCIBOX_DB_STATEMENT_TIMEOUT", &c.StatementTimeout},
		{"SCIBOX_DB_IDLE_TX_TIMEOUT", &c.IdleInTxTimeout},
		{"SCIBOX_DB_SLOW_QUERY", &c.SlowQuery},
	} {
		v := strings.TrimSpace(getenv(d.key))
		if v == "" {
			continue
		}
		dur, err := time.ParseDuration(v)
		if err != nil || dur <= 0 {
			return c, fmt.Errorf("%s: want a positive duration like 10s, got %q", d.key, v)
		}
		*d.dst = dur
	}
	return c.Normalized(), nil
}

func loadS3(getenv func(string) string, dev bool) (S3, error) {
	s := S3{
		Endpoint:  envOr(getenv, "SCIBOX_S3_ENDPOINT", DefaultS3Endpoint),
		Bucket:    envOr(getenv, "SCIBOX_S3_BUCKET", DefaultS3Bucket),
		AccessKey: envOr(getenv, "SCIBOX_S3_ACCESS_KEY", DefaultS3AccessKey),
		SecretKey: envOr(getenv, "SCIBOX_S3_SECRET_KEY", DefaultS3SecretKey),
		Region:    envOr(getenv, "SCIBOX_S3_REGION", DefaultS3Region),
	}
	var err error
	// Для локальной работы по умолчанию путь вместо поддомена и создание бакета; в бою всё наоборот.
	if s.TLS, err = envBool(getenv, "SCIBOX_S3_TLS", false); err != nil {
		return S3{}, err
	}
	if s.PathStyle, err = envBool(getenv, "SCIBOX_S3_PATH_STYLE", dev); err != nil {
		return S3{}, err
	}
	if s.CreateBucket, err = envBool(getenv, "SCIBOX_S3_CREATE_BUCKET", dev); err != nil {
		return S3{}, err
	}
	if strings.Contains(s.Endpoint, "://") || strings.Contains(s.Endpoint, "/") {
		return S3{}, fmt.Errorf("SCIBOX_S3_ENDPOINT: want host or host:port without scheme and path, got %q (https is switched by SCIBOX_S3_TLS=on)", s.Endpoint)
	}
	return s, nil
}

// envBool читает on/off, true/false, 1/0; пусто — значение по умолчанию. Опечатка не молчит: «fasle» — ошибка.
func envBool(getenv func(string) string, key string, def bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(getenv(key)))
	switch raw {
	case "":
		return def, nil
	case "1", "true", "on", "yes":
		return true, nil
	case "0", "false", "off", "no":
		return false, nil
	}
	return false, fmt.Errorf("%s: want on or off, got %q", key, raw)
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
	problems = append(problems, checkFileStorage(cfg)...)
	problems = append(problems, checkObservability(cfg)...)
	if from, err := mail.ParseAddress(cfg.MailFrom); err != nil || strings.HasSuffix(strings.ToLower(from.Address), ".local") {
		problems = append(problems, fmt.Errorf("SCIBOX_MAIL_FROM must be a real address on the site's domain, got %q: "+
			"mail services reject letters from .local addresses", cfg.MailFrom))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("SCIBOX_ENV=production refuses unsafe settings:\n%w", errors.Join(problems...))
}

// checkFileStorage: в бою файлы идут в хранилище по шифрованному каналу с настоящими ключами, а антивирус включён.
// Ключ в сообщения не попадает.
func checkFileStorage(cfg Config) []error {
	var problems []error
	if !cfg.S3.TLS {
		problems = append(problems, errors.New("SCIBOX_S3_TLS is off: set SCIBOX_S3_TLS=on, "+
			"otherwise CVs and recommendation letters travel to the file storage unencrypted"))
	}
	if cfg.S3.AccessKey == DefaultS3AccessKey || cfg.S3.SecretKey == DefaultS3SecretKey {
		problems = append(problems, errors.New("SCIBOX_S3_ACCESS_KEY or SCIBOX_S3_SECRET_KEY is the development key from docker-compose.yml: "+
			"use the keys of a service account of your storage provider"))
	}
	if cfg.ClamAVAddr == "" || strings.EqualFold(cfg.ClamAVAddr, ClamAVOff) {
		problems = append(problems, errors.New("SCIBOX_CLAMAV_ADDR is off: files must be checked for viruses, "+
			"set the address of clamd (host:port)"))
	}
	return problems
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

// loadLog читает формат и уровень журнала. По умолчанию в бою JSON (по строке на запись, поля разбирает система
// сбора), локально читаемый текст.
func loadLog(getenv func(string) string, production bool) (Log, error) {
	l := Log{JSON: production, Level: slog.LevelInfo}
	switch raw := strings.ToLower(strings.TrimSpace(getenv("SCIBOX_LOG_FORMAT"))); raw {
	case "":
	case "json":
		l.JSON = true
	case "text":
		l.JSON = false
	default:
		return Log{}, fmt.Errorf("SCIBOX_LOG_FORMAT: want json or text, got %q", raw)
	}
	switch raw := strings.ToLower(strings.TrimSpace(getenv("SCIBOX_LOG_LEVEL"))); raw {
	case "":
	case "debug":
		l.Level = slog.LevelDebug
	case "info":
		l.Level = slog.LevelInfo
	case "warn", "warning":
		l.Level = slog.LevelWarn
	case "error":
		l.Level = slog.LevelError
	default:
		return Log{}, fmt.Errorf("SCIBOX_LOG_LEVEL: want debug, info, warn or error, got %q", raw)
	}
	return l, nil
}

// loadOps читает настройки внутреннего порта. Опечатка в адресе не молчит.
func loadOps(getenv func(string) string, dev bool) (Ops, error) {
	o := Ops{Addr: strings.TrimSpace(getenv("SCIBOX_OPS_ADDR")), Token: strings.TrimSpace(getenv("SCIBOX_OPS_TOKEN"))}
	if o.Addr != "" && !strings.EqualFold(o.Addr, OpsOff) {
		if _, port, err := net.SplitHostPort(o.Addr); err != nil || port == "" {
			return Ops{}, fmt.Errorf("SCIBOX_OPS_ADDR: want host:port like 127.0.0.1:9100 or off, got %q", o.Addr)
		}
	}
	if strings.EqualFold(o.Addr, OpsOff) {
		o.Addr = OpsOff
	}
	if o.Token != "" && len(o.Token) < minOpsToken {
		return Ops{}, fmt.Errorf("SCIBOX_OPS_TOKEN: at least %d characters are required (use `openssl rand -hex 32`)", minOpsToken)
	}
	var err error
	if o.Pprof, err = envBool(getenv, "SCIBOX_PPROF", dev); err != nil {
		return Ops{}, err
	}
	return o, nil
}

// checkObservability: внутренний порт в бою не слушает чужие сети без секрета, а ошибки уходят только по шифрованному
// каналу (или внутри закрытой сети самого сервера).
func checkObservability(cfg Config) []error {
	var problems []error
	if addr := cfg.Ops.Addr; addr != "" && addr != OpsOff && cfg.Ops.Token == "" {
		host, _, _ := net.SplitHostPort(addr)
		if !internalHost(host) {
			problems = append(problems, fmt.Errorf("SCIBOX_OPS_ADDR %q listens on a network that may be reachable from outside: "+
				"bind it to 127.0.0.1 or a private address, or set SCIBOX_OPS_TOKEN (metrics and profiles must not be open)", addr))
		}
	}
	if cfg.Ops.Pprof && cfg.Ops.Token == "" && cfg.Ops.Addr != OpsOff {
		problems = append(problems, errors.New("SCIBOX_PPROF is on without SCIBOX_OPS_TOKEN: profiles show the inside of the process, "+
			"set a token or switch SCIBOX_PPROF off"))
	}
	if cfg.ErrorDSN != "" {
		// Формат проверен при чтении настроек (errreport.ValidateDSN).
		if u, err := url.Parse(cfg.ErrorDSN); err == nil && u.Scheme != "https" && !internalHost(u.Hostname()) {
			problems = append(problems, errors.New("SCIBOX_ERROR_DSN goes over http to another network: use https, "+
				"error reports must not travel unencrypted"))
		}
	}
	return problems
}

// internalHost — адрес, который не виден из интернета: петля, частная сеть, link-local или имя без точки (сервис в сети
// docker compose). Пустой хост и 0.0.0.0 (все сети) внутренними не считаются.
func internalHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return host != "" && !strings.Contains(host, ".")
	}
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
}

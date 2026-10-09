package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "product.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	p := writeFile(t, `{"name":"SciBox"}`)
	cfg, err := Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": p}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr || cfg.DatabaseURL != DefaultDatabaseURL {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.Product.Name != "SciBox" {
		t.Fatalf("product name = %q", cfg.Product.Name)
	}
	if cfg.SMTPAddr != DefaultSMTPAddr || cfg.PublicURL != DefaultPublicURL {
		t.Fatalf("mail defaults not applied: %+v", cfg)
	}
	if cfg.CrossrefURL != DefaultCrossrefURL || cfg.CrossrefMailto != "" {
		t.Fatalf("crossref defaults not applied: %+v", cfg)
	}
	if cfg.MailFrom != "\"SciBox\" <no-reply@scibox.local>" && cfg.MailFrom != "SciBox <no-reply@scibox.local>" {
		t.Fatalf("mail from = %q", cfg.MailFrom)
	}
}

func TestLoadOverrides(t *testing.T) {
	p := writeFile(t, `{"name":"  НаукаРабота  "}`)
	cfg, err := Load(envMap(map[string]string{
		"SCIBOX_PRODUCT_CONFIG":  p,
		"SCIBOX_HTTP_ADDR":       " :9999 ",
		"DATABASE_URL":           "postgres://x@y/z",
		"SCIBOX_SMTP_ADDR":       "mail.internal:25",
		"SCIBOX_PUBLIC_URL":      "https://scibox.example/",
		"SCIBOX_MAIL_FROM":       "Команда <team@scibox.example>",
		"SCIBOX_CROSSREF_URL":    "http://crossref.test/",
		"SCIBOX_CROSSREF_MAILTO": " ops@scibox.example ",
		"SCIBOX_WEB_DIR":         " ../web/dist ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.DatabaseURL != "postgres://x@y/z" || cfg.Product.Name != "НаукаРабота" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.WebDir != "../web/dist" {
		t.Fatalf("web dir = %q", cfg.WebDir)
	}
	if cfg.CrossrefURL != "http://crossref.test" || cfg.CrossrefMailto != "ops@scibox.example" {
		t.Fatalf("crossref overrides not applied: %+v", cfg)
	}
	if cfg.SMTPAddr != "mail.internal:25" || cfg.PublicURL != "https://scibox.example" || cfg.MailFrom != "Команда <team@scibox.example>" {
		t.Fatalf("mail overrides not applied: %+v", cfg)
	}
}

func TestLoadSMTPSecurity(t *testing.T) {
	p := writeFile(t, `{"name":"SciBox"}`)
	cfg, err := Load(envMap(map[string]string{
		"SCIBOX_PRODUCT_CONFIG": p,
		"SCIBOX_SMTP_TLS":       " TLS ",
		"SCIBOX_SMTP_USER":      " no-reply@scibox.example ",
		"SCIBOX_SMTP_PASSWORD":  " pass word ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTPTLS != "tls" || cfg.SMTPUser != "no-reply@scibox.example" || cfg.SMTPPassword != " pass word " {
		t.Fatalf("smtp security not applied: %+v", cfg)
	}

	defaults, err := Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": p}))
	if err != nil {
		t.Fatal(err)
	}
	if defaults.SMTPTLS != "off" || defaults.SMTPUser != "" || defaults.SMTPPassword != "" {
		t.Fatalf("smtp security defaults: %+v", defaults)
	}

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unknown mode", map[string]string{"SCIBOX_SMTP_TLS": "ssl"}, "SCIBOX_SMTP_TLS"},
		{"login without encryption", map[string]string{"SCIBOX_SMTP_USER": "a@b.ru"}, "unencrypted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.env["SCIBOX_PRODUCT_CONFIG"] = p
			if _, err := Load(envMap(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadProductErrors(t *testing.T) {
	cases := []struct {
		name, path, want string
	}{
		{"missing file", filepath.Join(t.TempDir(), "nope.json"), "read product config"},
		{"bad json", writeFile(t, `{name`), "parse product config"},
		{"empty name", writeFile(t, `{"name":"   "}`), "name is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": tc.path}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// Общий файл продукта из репозитория должен читаться (D-029).
func TestRepositoryProductConfig(t *testing.T) {
	p, err := LoadProduct("../../" + DefaultProductConfig)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name == "" {
		t.Fatal("empty product name")
	}
}

// Боевой режим (T-08): с безопасными настройками сервер стартует, а каждая опасная настройка
// по отдельности не даёт ему запуститься и называет, что исправить.
func TestProductionRefusesUnsafeSettings(t *testing.T) {
	for _, k := range []string{"PGHOST", "PGPORT", "PGPASSWORD", "PGSSLMODE", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(k, "")
	}
	p := writeFile(t, `{"name":"SciBox"}`)
	safe := func() map[string]string {
		return map[string]string{
			"SCIBOX_PRODUCT_CONFIG": p,
			"SCIBOX_ENV":            " Production ",
			"SCIBOX_PUBLIC_URL":     "https://scibox.example",
			"DATABASE_URL":          "postgres://app:Long-Random-Secret@db.internal:5432/scibox?sslmode=verify-full",
			"SCIBOX_SMTP_TLS":       "starttls",
			"SCIBOX_MAIL_FROM":      "SciBox <no-reply@scibox.example>",
			"SCIBOX_S3_ENDPOINT":    "storage.yandexcloud.net",
			"SCIBOX_S3_TLS":         "on",
			"SCIBOX_S3_ACCESS_KEY":  "YCAJE-real-access-key",
			"SCIBOX_S3_SECRET_KEY":  "Real-Secret-Key-Of-The-Service-Account",
			"SCIBOX_CLAMAV_ADDR":    "clamav:3310",
		}
	}
	cfg, err := Load(envMap(safe()))
	if err != nil {
		t.Fatalf("safe production settings refused: %v", err)
	}
	if cfg.Env != EnvProduction || !cfg.HTTPS() {
		t.Fatalf("env = %q, https = %v", cfg.Env, cfg.HTTPS())
	}

	// Те же безопасные настройки, но база на этой машине: шифрование не требуется.
	for _, db := range []string{
		"postgres://app:Long-Random-Secret@localhost:5432/scibox?sslmode=disable",
		"postgres://app:Long-Random-Secret@127.0.0.1/scibox?sslmode=disable",
		"postgres://app:Long-Random-Secret@[::1]/scibox?sslmode=disable",
		"host=/var/run/postgresql user=app password=Long-Random-Secret dbname=scibox sslmode=disable",
		"postgres://app:Long-Random-Secret@db.internal/scibox?sslmode=require",
		"postgres://app:Long-Random-Secret@db.internal/scibox?sslmode=verify-ca",
	} {
		env := safe()
		env["DATABASE_URL"] = db
		if _, err := Load(envMap(env)); err != nil {
			t.Errorf("%s refused: %v", db, err)
		}
	}

	cases := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{"http site", "SCIBOX_PUBLIC_URL", "http://scibox.example", "SCIBOX_PUBLIC_URL must be the https://"},
		{"default site", "SCIBOX_PUBLIC_URL", "", "SCIBOX_PUBLIC_URL must be the https://"},
		{"site without host", "SCIBOX_PUBLIC_URL", "https://", "SCIBOX_PUBLIC_URL must be the https://"},
		{"broken site address", "SCIBOX_PUBLIC_URL", "https://%zz", "SCIBOX_PUBLIC_URL must be the https://"},
		{"default database", "DATABASE_URL", "", "development password"},
		{"development password", "DATABASE_URL", "postgres://scibox:scibox@db.internal/scibox?sslmode=verify-full", "development password"},
		{"remote database without tls", "DATABASE_URL", "postgres://app:Long-Random-Secret@db.internal/scibox?sslmode=disable", "connects to db.internal without encryption"},
		{"remote database with optional tls", "DATABASE_URL", "postgres://app:Long-Random-Secret@db.internal/scibox?sslmode=prefer", "without encryption"},
		{"remote database with tls allowed", "DATABASE_URL", "postgres://app:Long-Random-Secret@10.0.0.5/scibox?sslmode=allow", "without encryption"},
		{"remote database, sslmode not set", "DATABASE_URL", "postgres://app:Long-Random-Secret@db.internal/scibox", "without encryption"},
		{"broken database address", "DATABASE_URL", "postgres://app:x@db.internal:notaport/scibox", "DATABASE_URL cannot be parsed"},
		{"mail without encryption", "SCIBOX_SMTP_TLS", "off", "SCIBOX_SMTP_TLS is off"},
		{"default sender", "SCIBOX_MAIL_FROM", "", "SCIBOX_MAIL_FROM must be a real address"},
		{"broken sender", "SCIBOX_MAIL_FROM", "not an address", "SCIBOX_MAIL_FROM must be a real address"},
		{"storage without tls", "SCIBOX_S3_TLS", "off", "SCIBOX_S3_TLS is off"},
		{"storage tls not set", "SCIBOX_S3_TLS", "", "SCIBOX_S3_TLS is off"},
		{"development access key", "SCIBOX_S3_ACCESS_KEY", "scibox", "development key"},
		{"development secret key", "SCIBOX_S3_SECRET_KEY", "scibox-dev-secret", "development key"},
		{"keys not set", "SCIBOX_S3_SECRET_KEY", "", "development key"},
		{"antivirus switched off", "SCIBOX_CLAMAV_ADDR", "off", "SCIBOX_CLAMAV_ADDR is off"},
		{"antivirus switched off in capitals", "SCIBOX_CLAMAV_ADDR", "OFF", "SCIBOX_CLAMAV_ADDR is off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := safe()
			env[tc.key] = tc.val
			_, err := Load(envMap(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "SCIBOX_ENV=production") {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			for _, secret := range []string{"Long-Random-Secret", "Real-Secret-Key-Of-The-Service-Account", "scibox-dev-secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("secret %q leaked into the error: %v", secret, err)
				}
			}
		})
	}

	// Все проблемы видны сразу, а не по одной за запуск.
	_, err = Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": p, "SCIBOX_ENV": "production"}))
	for _, want := range []string{"SCIBOX_PUBLIC_URL", "development password", "SCIBOX_SMTP_TLS", "SCIBOX_MAIL_FROM", "SCIBOX_S3_TLS", "development key"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("all-defaults production: err = %v, want containing %q", err, want)
		}
	}

	// В режиме разработки те же значения по умолчанию разрешены.
	if _, err := Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": p})); err != nil {
		t.Fatalf("development defaults refused: %v", err)
	}
}

func TestLoadEnvAndProxies(t *testing.T) {
	p := writeFile(t, `{"name":"SciBox"}`)
	cfg, err := Load(envMap(map[string]string{"SCIBOX_PRODUCT_CONFIG": p, "SCIBOX_TRUSTED_PROXIES": "127.0.0.1, 10.0.0.0/8"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != EnvDevelopment || cfg.TrustedProxies.Empty() || cfg.HTTPS() {
		t.Fatalf("env = %q, proxies empty = %v, https = %v", cfg.Env, cfg.TrustedProxies.Empty(), cfg.HTTPS())
	}
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unknown env", map[string]string{"SCIBOX_ENV": "prod"}, "SCIBOX_ENV"},
		{"bad proxy", map[string]string{"SCIBOX_TRUSTED_PROXIES": "10.0.0.300"}, "SCIBOX_TRUSTED_PROXIES"},
		{"proxy trusts everyone", map[string]string{"SCIBOX_TRUSTED_PROXIES": "0.0.0.0/0"}, "trusts every address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.env["SCIBOX_PRODUCT_CONFIG"] = p
			if _, err := Load(envMap(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadFileStorage(t *testing.T) {
	p := writeFile(t, `{"name":"SciBox"}`)
	base := func(extra map[string]string) map[string]string {
		env := map[string]string{"SCIBOX_PRODUCT_CONFIG": p}
		maps.Copy(env, extra)
		return env
	}
	t.Run("development defaults match docker-compose", func(t *testing.T) {
		cfg, err := Load(envMap(base(nil)))
		if err != nil {
			t.Fatal(err)
		}
		want := S3{Endpoint: "localhost:8333", Bucket: "scibox-files", AccessKey: "scibox", SecretKey: "scibox-dev-secret", Region: "us-east-1", PathStyle: true, CreateBucket: true}
		if cfg.S3 != want || cfg.ClamAVAddr != "localhost:3310" {
			t.Fatalf("got %+v, clamav %q", cfg.S3, cfg.ClamAVAddr)
		}
	})
	t.Run("overrides", func(t *testing.T) {
		cfg, err := Load(envMap(base(map[string]string{
			"SCIBOX_S3_ENDPOINT": "s3.selcdn.ru", "SCIBOX_S3_BUCKET": "cvs", "SCIBOX_S3_REGION": "ru-1", "SCIBOX_S3_TLS": "true",
			"SCIBOX_S3_PATH_STYLE": "off", "SCIBOX_S3_CREATE_BUCKET": "0", "SCIBOX_CLAMAV_ADDR": "off",
		})))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.S3.Endpoint != "s3.selcdn.ru" || cfg.S3.Bucket != "cvs" || cfg.S3.Region != "ru-1" || !cfg.S3.TLS || cfg.S3.PathStyle || cfg.S3.CreateBucket || cfg.ClamAVAddr != "off" {
			t.Fatalf("got %+v, clamav %q", cfg.S3, cfg.ClamAVAddr)
		}
	})
	t.Run("production does not create buckets or use path style by default", func(t *testing.T) {
		env := base(map[string]string{"SCIBOX_ENV": "production"})
		_, err := Load(envMap(env)) // другие проблемы боя здесь не важны: смотрим на разбор
		if err == nil {
			t.Fatal("production defaults must be refused")
		}
		cfg, err := loadS3(envMap(env), false)
		if err != nil || cfg.PathStyle || cfg.CreateBucket {
			t.Fatalf("got %+v, %v", cfg, err)
		}
	})
	for _, tc := range []struct{ name, key, val, want string }{
		{"endpoint with scheme", "SCIBOX_S3_ENDPOINT", "https://storage.yandexcloud.net", "without scheme and path"},
		{"endpoint with path", "SCIBOX_S3_ENDPOINT", "storage.example/bucket", "without scheme and path"},
		{"typo in switch", "SCIBOX_S3_TLS", "fasle", "want on or off"},
		{"typo in path style", "SCIBOX_S3_PATH_STYLE", "maybe", "want on or off"},
		{"typo in create bucket", "SCIBOX_S3_CREATE_BUCKET", "2", "want on or off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(envMap(base(map[string]string{tc.key: tc.val})))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

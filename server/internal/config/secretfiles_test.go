package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secretFile пишет файл с секретом и возвращает путь; mode задаёт права.
func secretFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile подчиняется umask: права задаём явно.
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSecretsComeFromFiles(t *testing.T) {
	product := writeFile(t, `{"name":"SciBox"}`)
	env := map[string]string{
		"SCIBOX_PRODUCT_CONFIG":      product,
		"DATABASE_URL_FILE":          secretFile(t, "postgres://app:Pa ss@db.internal/scibox?sslmode=require\n", 0o600),
		"SCIBOX_S3_ACCESS_KEY_FILE":  secretFile(t, "access-key", 0o600),
		"SCIBOX_S3_SECRET_KEY_FILE":  secretFile(t, "secret key with spaces \r\n", 0o600),
		"SCIBOX_SMTP_PASSWORD_FILE":  secretFile(t, " leading and trailing ", 0o600),
		"SCIBOX_OPS_TOKEN_FILE":      secretFile(t, strings.Repeat("t", 40), 0o600),
		"SCIBOX_ERROR_DSN_FILE":      secretFile(t, "https://key@glitchtip.example/1\n", 0o600),
		"SCIBOX_SMTP_TLS":            "starttls",
		"SCIBOX_SMTP_USER":           "mailer",
		"SCIBOX_S3_ENDPOINT":         "storage.example",
		"SCIBOX_UNRELATED_SETTING":   "stays",
		"SCIBOX_UNRELATED_SETTING_2": "",
	}
	cfg, err := Load(envMap(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://app:Pa ss@db.internal/scibox?sslmode=require" {
		t.Errorf("database url = %q: one final newline goes, the rest stays", cfg.DatabaseURL)
	}
	if cfg.S3.AccessKey != "access-key" || cfg.S3.SecretKey != "secret key with spaces" {
		t.Errorf("s3 keys = %q, %q: CRLF goes, the setting itself trims the edges", cfg.S3.AccessKey, cfg.S3.SecretKey)
	}
	if cfg.SMTPPassword != " leading and trailing " {
		t.Errorf("smtp password = %q: spaces may be part of a password", cfg.SMTPPassword)
	}
	if cfg.Ops.Token != strings.Repeat("t", 40) || cfg.ErrorDSN != "https://key@glitchtip.example/1" {
		t.Errorf("ops token or error dsn not taken from the files: %+v %q", cfg.Ops, cfg.ErrorDSN)
	}
}

func TestSecretFilesDoNotChangeOtherSettings(t *testing.T) {
	get, err := withSecretFiles(envMap(map[string]string{
		"SCIBOX_PUBLIC_URL":         "https://scibox.example",
		"SCIBOX_S3_SECRET_KEY_FILE": secretFile(t, "k", 0o600),
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	if get("SCIBOX_PUBLIC_URL") != "https://scibox.example" || get("SCIBOX_S3_SECRET_KEY") != "k" || get("SCIBOX_ABSENT") != "" {
		t.Fatal("the wrapper must pass other settings through and add only the file values")
	}
	// Без `_FILE` секрет берётся из окружения, как раньше.
	get, err = withSecretFiles(envMap(map[string]string{"DATABASE_URL": "postgres://plain"}), true)
	if err != nil || get("DATABASE_URL") != "postgres://plain" {
		t.Fatalf("plain value lost: %q, %v", get("DATABASE_URL"), err)
	}
}

func TestSecretFileProblemsStopTheStartWithoutLeakingTheValue(t *testing.T) {
	dir := t.TempDir()
	big := secretFile(t, strings.Repeat("x", maxSecretFile+1), 0o600)
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"both set", map[string]string{"SCIBOX_OPS_TOKEN": "inline", "SCIBOX_OPS_TOKEN_FILE": secretFile(t, "from-file", 0o600)}, "SCIBOX_OPS_TOKEN and SCIBOX_OPS_TOKEN_FILE are both set"},
		{"no such file", map[string]string{"DATABASE_URL_FILE": filepath.Join(dir, "absent")}, "DATABASE_URL_FILE: open"},
		{"a directory", map[string]string{"SCIBOX_S3_SECRET_KEY_FILE": dir}, "not a regular file"},
		{"empty", map[string]string{"SCIBOX_S3_ACCESS_KEY_FILE": secretFile(t, "", 0o600)}, "is empty"},
		{"only a newline", map[string]string{"SCIBOX_S3_ACCESS_KEY_FILE": secretFile(t, "\n", 0o600)}, "is empty"},
		{"only spaces", map[string]string{"SCIBOX_S3_ACCESS_KEY_FILE": secretFile(t, "   \n", 0o600)}, "is empty"},
		{"too large", map[string]string{"SCIBOX_ERROR_DSN_FILE": big}, "is larger than"},
		{"readable by the group in production", map[string]string{"SCIBOX_ENV": "production", "SCIBOX_SMTP_PASSWORD_FILE": secretFile(t, "TOP-SECRET-VALUE", 0o640)}, "chmod 600"},
		{"readable by everyone in production", map[string]string{"SCIBOX_ENV": "Production", "SCIBOX_SMTP_PASSWORD_FILE": secretFile(t, "TOP-SECRET-VALUE", 0o644)}, "chmod 600"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"SCIBOX_PRODUCT_CONFIG": writeFile(t, `{"name":"SciBox"}`)}
			for k, v := range tc.env {
				env[k] = v
			}
			_, err := Load(envMap(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			for _, secret := range []string{"TOP-SECRET-VALUE", "from-file", "inline"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("the value %q leaked into the error: %v", secret, err)
				}
			}
		})
	}
}

func TestSecretFilePermissionsAreOnlyEnforcedInProduction(t *testing.T) {
	env := map[string]string{
		"SCIBOX_PRODUCT_CONFIG":     writeFile(t, `{"name":"SciBox"}`),
		"SCIBOX_SMTP_PASSWORD_FILE": secretFile(t, "p", 0o644),
	}
	// Разработка: файл рядом с проектом, права как у обычного файла.
	if _, err := Load(envMap(env)); err != nil {
		t.Fatalf("development refused a 0644 secret file: %v", err)
	}
	// Все проблемы файлов видны сразу, а не по одной за запуск.
	env["SCIBOX_ENV"] = "production"
	env["SCIBOX_OPS_TOKEN_FILE"] = filepath.Join(t.TempDir(), "absent")
	_, err := Load(envMap(env))
	if err == nil || !strings.Contains(err.Error(), "SCIBOX_SMTP_PASSWORD_FILE") || !strings.Contains(err.Error(), "SCIBOX_OPS_TOKEN_FILE") {
		t.Fatalf("err = %v, want both problems", err)
	}
}

func TestProductionAcceptsAllSecretsFromFiles(t *testing.T) {
	for _, k := range []string{"PGHOST", "PGPORT", "PGPASSWORD", "PGSSLMODE", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(k, "")
	}
	env := map[string]string{
		"SCIBOX_PRODUCT_CONFIG":     writeFile(t, `{"name":"SciBox"}`),
		"SCIBOX_ENV":                "production",
		"SCIBOX_PUBLIC_URL":         "https://scibox.example",
		"DATABASE_URL_FILE":         secretFile(t, "postgres://app:Long-Random-Secret@postgres:5432/scibox?sslmode=require\n", 0o600),
		"SCIBOX_SMTP_TLS":           "starttls",
		"SCIBOX_MAIL_FROM":          "SciBox <no-reply@scibox.example>",
		"SCIBOX_S3_ENDPOINT":        "storage.yandexcloud.net",
		"SCIBOX_S3_TLS":             "on",
		"SCIBOX_S3_ACCESS_KEY_FILE": secretFile(t, "YCAJE-real-access-key\n", 0o600),
		"SCIBOX_S3_SECRET_KEY_FILE": secretFile(t, "Real-Secret-Key-Of-The-Service-Account\n", 0o600),
		"SCIBOX_CLAMAV_ADDR":        "clamav:3310",
		"SCIBOX_OPS_ADDR":           "0.0.0.0:9100",
		"SCIBOX_OPS_TOKEN_FILE":     secretFile(t, strings.Repeat("a", 48)+"\n", 0o600),
	}
	if _, err := Load(envMap(env)); err != nil {
		t.Fatalf("safe production settings with file secrets refused: %v", err)
	}
	// Секрет из файла проходит те же проверки боевого режима: пароль из docker-compose по-прежнему запрещён.
	env["SCIBOX_S3_SECRET_KEY_FILE"] = secretFile(t, "scibox-dev-secret\n", 0o600)
	if _, err := Load(envMap(env)); err == nil || !strings.Contains(err.Error(), "development key") {
		t.Fatalf("err = %v, want the development key refused", err)
	}
	// Короткий секрет внутреннего порта из файла тоже отвергается.
	env["SCIBOX_S3_SECRET_KEY_FILE"] = secretFile(t, "Real-Secret-Key-Of-The-Service-Account\n", 0o600)
	env["SCIBOX_OPS_TOKEN_FILE"] = secretFile(t, "short\n", 0o600)
	if _, err := Load(envMap(env)); err == nil || !strings.Contains(err.Error(), "SCIBOX_OPS_TOKEN") {
		t.Fatalf("err = %v, want the short ops token refused", err)
	}
}

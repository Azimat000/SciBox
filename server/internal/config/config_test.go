package config

import (
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
	if cfg.MailFrom != "\"SciBox\" <no-reply@scibox.local>" && cfg.MailFrom != "SciBox <no-reply@scibox.local>" {
		t.Fatalf("mail from = %q", cfg.MailFrom)
	}
}

func TestLoadOverrides(t *testing.T) {
	p := writeFile(t, `{"name":"  НаукаРабота  "}`)
	cfg, err := Load(envMap(map[string]string{
		"SCIBOX_PRODUCT_CONFIG": p,
		"SCIBOX_HTTP_ADDR":      " :9999 ",
		"DATABASE_URL":          "postgres://x@y/z",
		"SCIBOX_SMTP_ADDR":      "mail.internal:25",
		"SCIBOX_PUBLIC_URL":     "https://scibox.example/",
		"SCIBOX_MAIL_FROM":      "Команда <team@scibox.example>",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.DatabaseURL != "postgres://x@y/z" || cfg.Product.Name != "НаукаРабота" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.SMTPAddr != "mail.internal:25" || cfg.PublicURL != "https://scibox.example" || cfg.MailFrom != "Команда <team@scibox.example>" {
		t.Fatalf("mail overrides not applied: %+v", cfg)
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

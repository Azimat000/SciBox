package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Боевой compose (срез 28, D-188) читается как данные: правила, которые держат стенд закрытым, проверяются тестом, а не
// памятью того, кто правит файл. Нарушение правила роняет `make test`.

const prodCompose = "../../../deploy/docker-compose.prod.yml"

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Secrets  map[string]any            `yaml:"secrets"`
	Networks map[string]any            `yaml:"networks"`
}

type composeService struct {
	Image       string         `yaml:"image"`
	Ports       []string       `yaml:"ports"`
	Environment map[string]any `yaml:"environment"`
	Secrets     []string       `yaml:"secrets"`
	Volumes     []string       `yaml:"volumes"`
	ReadOnly    bool           `yaml:"read_only"`
	CapDrop     []string       `yaml:"cap_drop"`
	SecurityOpt []string       `yaml:"security_opt"`
	User        string         `yaml:"user"`
	Privileged  bool           `yaml:"privileged"`
	Profiles    []string       `yaml:"profiles"`
	Restart     string         `yaml:"restart"`
	NetworkMode string         `yaml:"network_mode"`
	Command     any            `yaml:"command"`
}

func loadProdCompose(t *testing.T) composeFile {
	t.Helper()
	raw, err := os.ReadFile(prodCompose)
	if err != nil {
		t.Fatal(err)
	}
	var c composeFile
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("deploy/docker-compose.prod.yml does not parse: %v", err)
	}
	if len(c.Services) == 0 {
		t.Fatal("no services found: the file layout changed, update the test")
	}
	return c
}

// appServices — процессы сервера: копии, воркер и шаг выкладки.
var appServices = []string{"app1", "app2", "worker", "release"}

func TestProdComposeOnlyCaddyListensOnThePublicNetwork(t *testing.T) {
	c := loadProdCompose(t)
	for name, svc := range c.Services {
		for _, p := range svc.Ports {
			switch {
			case name == "caddy":
				// 80, 443 и 443/udp (HTTP/3): ни одного порта кроме веб-портов.
				if !strings.Contains(p, "HTTP_PORT") && !strings.Contains(p, "HTTPS_PORT") {
					t.Errorf("caddy publishes %q: only the web ports are public", p)
				}
			case strings.HasPrefix(p, "127.0.0.1:"):
				// Мониторинг на самом сервере: заходить через ssh -L (T-52).
				if !contains(svc.Profiles, "monitoring") {
					t.Errorf("%s publishes %q on the loopback outside the monitoring profile", name, p)
				}
			default:
				t.Errorf("%s publishes %q on all interfaces: only caddy may be reachable from the internet", name, p)
			}
		}
		if svc.NetworkMode == "host" {
			t.Errorf("%s uses the host network: its ports bypass the firewall", name)
		}
	}
	if caddy := c.Services["caddy"]; len(caddy.Ports) == 0 {
		t.Error("caddy publishes nothing: the site would be unreachable")
	}
}

func TestProdComposeServerProcessesAreLockedDown(t *testing.T) {
	c := loadProdCompose(t)
	for _, name := range appServices {
		svc, ok := c.Services[name]
		if !ok {
			t.Fatalf("service %s is missing", name)
		}
		if !svc.ReadOnly {
			t.Errorf("%s: the file system must be read-only (T-27)", name)
		}
		if !contains(svc.CapDrop, "ALL") {
			t.Errorf("%s: cap_drop must be ALL", name)
		}
		if !contains(svc.SecurityOpt, "no-new-privileges:true") {
			t.Errorf("%s: no-new-privileges must be set", name)
		}
		if svc.User != "" || svc.Privileged {
			t.Errorf("%s: must run as the image's non-root user (user %q, privileged %v)", name, svc.User, svc.Privileged)
		}
		if got := svc.Environment["SCIBOX_ENV"]; got != "production" {
			t.Errorf("%s: SCIBOX_ENV = %v, want production (the settings checks of T-08 must apply)", name, got)
		}
		if got := svc.Environment["SCIBOX_PPROF"]; got != "off" {
			t.Errorf("%s: SCIBOX_PPROF = %v, want off", name, got)
		}
		if got, ok := svc.Environment["SCIBOX_S3_TLS"]; ok && got != "on" {
			t.Errorf("%s: SCIBOX_S3_TLS = %v, want on", name, got)
		}
	}
	for _, name := range []string{"app1", "app2"} {
		if got := c.Services[name].Environment["SCIBOX_BACKGROUND"]; got != "off" {
			t.Errorf("%s: SCIBOX_BACKGROUND = %v: background jobs belong to the worker", name, got)
		}
	}
}

var secretName = regexp.MustCompile(`(PASSWORD|SECRET|TOKEN|ACCESS_KEY|DATABASE_URL|KEY_SECRET|CIPHER_PASS|CRYPT_PASS)$`)

func TestProdComposeKeepsSecretsOutOfTheEnvironment(t *testing.T) {
	c := loadProdCompose(t)
	for name, svc := range c.Services {
		for key, val := range svc.Environment {
			// У pgBackRest свои параметры с окончанием _FILE (repo1-s3-ca-file): это не секрет, а путь к сертификату.
			if strings.HasPrefix(key, "PGBACKREST_") && strings.HasSuffix(key, "_FILE") && !strings.HasSuffix(key, "_FROM_FILE") {
				continue
			}
			if strings.HasSuffix(key, "_FILE") || strings.HasSuffix(key, "_FROM_FILE") {
				v := fmt.Sprint(val)
				if !strings.HasPrefix(v, "/run/secrets/") {
					t.Errorf("%s: %s = %q must point to /run/secrets", name, key, v)
				}
				continue
			}
			if secretName.MatchString(key) {
				t.Errorf("%s: %s looks like a secret passed as a value: use %s_FILE (docker inspect shows environment values)", name, key, key)
			}
		}
	}
}

func TestProdComposeSecretsAreDeclaredAndMounted(t *testing.T) {
	c := loadProdCompose(t)
	for name, svc := range c.Services {
		for _, s := range svc.Secrets {
			if _, ok := c.Secrets[s]; !ok {
				t.Errorf("%s uses the secret %q that is not declared at the top level", name, s)
			}
		}
		// Секрет, на который указывает *_FILE, должен быть смонтирован: через `secrets:` сервиса или томом с секретами.
		for key, val := range svc.Environment {
			v := fmt.Sprint(val)
			isFile := strings.HasSuffix(key, "_FILE") || strings.HasSuffix(key, "_FROM_FILE")
			if !isFile || !strings.HasPrefix(v, "/run/secrets/") {
				continue
			}
			file := strings.TrimPrefix(v, "/run/secrets/")
			if contains(svc.Secrets, file) {
				continue
			}
			mountedVolume := false
			for _, vol := range svc.Volumes {
				if strings.HasSuffix(vol, ":/run/secrets") || strings.HasSuffix(vol, ":/run/secrets:ro") {
					mountedVolume = true
				}
			}
			if !mountedVolume {
				t.Errorf("%s: %s points to %s, but the service mounts neither the secret nor the secrets volume", name, key, v)
			}
		}
	}
}

func TestProdComposeTrustsOnlyOneProxyAddressByDefault(t *testing.T) {
	c := loadProdCompose(t)
	got := fmt.Sprint(c.Services["app1"].Environment["SCIBOX_TRUSTED_PROXIES"])
	if !strings.Contains(got, "172.28.10.2/32") || strings.Contains(got, "0.0.0.0") || strings.Contains(got, "/0") {
		t.Fatalf("SCIBOX_TRUSTED_PROXIES default = %q: X-Forwarded-For may be trusted only from the address of Caddy (T-06, T-31)", got)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

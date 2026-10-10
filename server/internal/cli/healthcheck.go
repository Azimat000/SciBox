package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"scibox/server/internal/config"
)

// healthcheckTimeout — срок одной проверки: Docker ждёт ответа проверки ограниченное время и считает контейнер
// нездоровым при его превышении.
const healthcheckTimeout = 3 * time.Second

var errHealthcheckUsage = errors.New("healthcheck: unknown process kind")

// healthcheckURL выбирает адрес `/readyz` процесса: `serve` (по умолчанию) или `worker`. Адрес берётся из
// SCIBOX_OPS_ADDR; процесс, слушающий все сети (`0.0.0.0` или `::`), проверяется по петле.
func healthcheckURL(args []string, getenv func(string) string) (string, error) {
	def := defaultServeOpsAddr
	switch {
	case len(args) == 0 || args[0] == "serve":
	case args[0] == "worker":
		def = defaultWorkerOpsAddr
	default:
		return "", errHealthcheckUsage
	}
	addr := strings.TrimSpace(getenv("SCIBOX_OPS_ADDR"))
	switch {
	case addr == "":
		addr = def
	case strings.EqualFold(addr, config.OpsOff):
		return "", errors.New("внутренний порт выключен (SCIBOX_OPS_ADDR=off): проверять нечего")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", fmt.Errorf("SCIBOX_OPS_ADDR: ожидается хост:порт, получено %q", addr)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

// runHealthcheck: `healthcheck [serve|worker]` спрашивает внутренний порт своего процесса, готов ли он (`/readyz`).
// Нужна контейнеру: в образе distroless нет ни оболочки, ни curl, а проверка «порт открыт» не видит, что база
// недоступна или миграции не накатаны. Код выхода 0 — готов, 1 — нет, 2 — неверные аргументы.
//
// Настройки читаются из окружения напрямую, без config.Load: проверка запускается каждые несколько секунд и не должна
// зависеть от секретов, файлов и сети, она спрашивает только локальный порт.
func runHealthcheck(ctx context.Context, args []string, env Env) int {
	target, err := healthcheckURL(args, env.Getenv)
	if errors.Is(err, errHealthcheckUsage) {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		fmt.Fprintln(env.Stderr, "healthcheck:", err)
		return 1
	}
	// Без прокси из окружения: порт свой, локальный.
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req) //nolint:gosec // G704: адрес — собственный внутренний порт процесса из настройки администратора
	if err != nil {
		fmt.Fprintln(env.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(env.Stderr, "healthcheck: процесс не готов (ответ %d)\n", resp.StatusCode)
		return 1
	}
	return 0
}

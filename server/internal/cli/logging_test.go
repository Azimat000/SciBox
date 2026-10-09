package cli

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/testdb"
)

// Всё, что пишется в журнал работающего сервера, проходит через маску (T-18): сообщения slog без логгера,
// стандартный log (его использует и сам net/http) и запись о запросе, в адресе которого оказалась почта.
func TestEveryLoggerGoesThroughTheMask(t *testing.T) {
	dbURL := testdb.Create(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prevDefault, prevWriter, prevFlags := slog.Default(), log.Writer(), log.Flags()

	var reqErr error
	r := run(ctx, nil, map[string]string{"DATABASE_URL": dbURL, "SCIBOX_HTTP_ADDR": "127.0.0.1:0"}, func(addr net.Addr) {
		defer cancel()
		slog.Error("default logger: ivan.sidorov@msu.ru from 198.51.100.23 token=Zk3nQ9vW1xYbT7uLmA2cR8dEfGhJ4pSo5iVn0KwXqzE")
		log.Printf("standard log: petr.ivanov@mail.example.org %s", "2001:db8:aaaa:bbbb::1234")
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s/no/such/page/anna.smirnova@msu.ru", addr))
		if err != nil {
			reqErr = err
			return
		}
		_ = resp.Body.Close()
	})
	if reqErr != nil {
		t.Fatal(reqErr)
	}
	if r.code != 0 {
		t.Fatalf("got %+v", r)
	}
	for _, leaked := range []string{"ivan.sidorov", "petr.ivanov", "anna.smirnova", "198.51.100.23", "2001:db8:aaaa:bbbb::1234", "Zk3nQ9vW1xYbT7uLmA2cR8dEfGhJ4pSo5iVn0KwXqzE"} {
		if strings.Contains(r.stderr, leaked) {
			t.Errorf("%q reached the log:\n%s", leaked, r.stderr)
		}
	}
	for _, want := range []string{"***@msu.ru", "***@mail.example.org", "198.51.100.0/24", "2001:db8:aaaa::/48", "token=[скрыто]"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("log lost %q:\n%s", want, r.stderr)
		}
	}
	if slog.Default() != prevDefault || log.Writer() != prevWriter || log.Flags() != prevFlags {
		t.Error("Run must give the default loggers back")
	}
}

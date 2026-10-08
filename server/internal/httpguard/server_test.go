package httpguard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestNewServerSetsEveryTimeout(t *testing.T) {
	d := DefaultTimeouts()
	for name, in := range map[string]Timeouts{"zero": {}, "negative": {Header: -1, Read: -1, Write: -1, Idle: -1, Transfer: -1}} {
		srv := NewServer(http.NotFoundHandler(), in)
		if srv.ReadHeaderTimeout != d.Header || srv.ReadTimeout != d.Read || srv.WriteTimeout != d.Write || srv.IdleTimeout != d.Idle {
			t.Errorf("%s timeouts became %v %v %v %v, want defaults", name, srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
		}
		if srv.MaxHeaderBytes != MaxHeaderBytes {
			t.Errorf("MaxHeaderBytes = %d", srv.MaxHeaderBytes)
		}
	}
	own := Timeouts{Header: time.Second, Read: 2 * time.Second, Write: 3 * time.Second, Idle: 4 * time.Second}
	srv := NewServer(http.NotFoundHandler(), own)
	if srv.ReadHeaderTimeout != time.Second || srv.ReadTimeout != 2*time.Second || srv.WriteTimeout != 3*time.Second || srv.IdleTimeout != 4*time.Second {
		t.Fatalf("own timeouts not kept: %+v", srv)
	}
	if d.Header <= 0 || d.Read <= 0 || d.Write <= 0 || d.Idle <= 0 || d.Transfer < d.Write {
		t.Fatalf("bad defaults: %+v", d)
	}
}

// serve запускает настоящий сервер на свободном порту: сроки соединения работают только на сокете.
func serve(t *testing.T, h http.Handler, tm Timeouts) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(h, tm)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// closedWithin: сервер сам закрыл соединение не позже, чем через limit.
func closedWithin(t *testing.T, c net.Conn, limit time.Duration) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(limit))
	data, err := io.ReadAll(c)
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		t.Fatalf("server kept a slow connection open longer than %v", limit)
	}
	return string(data)
}

// Медленный клиент (T-04, slowloris): кто не дослал заголовки за срок, тот теряет соединение.
func TestSlowHeadersAreCut(t *testing.T) {
	addr := serve(t, http.NotFoundHandler(), Timeouts{Header: 200 * time.Millisecond, Read: time.Second})
	c := dial(t, addr)
	if _, err := fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: x\r\nX-Slow: "); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	closedWithin(t, c, 3*time.Second)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("closed after %v", time.Since(start))
	}
}

// Слишком большие заголовки — ответ 431, а не мегабайт в памяти.
func TestHugeHeadersAreRefused(t *testing.T) {
	addr := serve(t, http.NotFoundHandler(), Timeouts{})
	c := dial(t, addr)
	if _, err := fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: x\r\nX-Big: %s\r\n\r\n", strings.Repeat("a", 2*MaxHeaderBytes)); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status %d", res.StatusCode)
	}
}

// Обычный адрес получает обычный срок чтения тела; долгая передача (Route.Slow) — свой, больший.
func TestSlowTransferGetsItsOwnDeadline(t *testing.T) {
	r := chi.NewRouter()
	g, err := New(Config{Logger: silent(), Transfer: 5 * time.Second, Routes: []Route{
		{Method: http.MethodPost, Pattern: "/api/upload", Slow: true},
	}}, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Use(g.Perimeter)
	read := func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestTimeout)
			return
		}
		_, _ = fmt.Fprintf(w, "read %d", n)
	}
	r.Post("/api/upload", read)
	r.Post("/api/form", read)
	addr := serve(t, r, Timeouts{Header: time.Second, Read: 300 * time.Millisecond, Write: 300 * time.Millisecond})

	// Тело приходит по кусочку за 900 мс: втрое дольше обычного срока чтения.
	send := func(path string) string {
		c := dial(t, addr)
		if _, err := fmt.Fprintf(c, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Length: 4\r\nConnection: close\r\n\r\n", path); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			time.Sleep(300 * time.Millisecond)
			if _, err := c.Write([]byte("a")); err != nil {
				return "write failed: " + err.Error()
			}
		}
		_, _ = c.Write([]byte("a"))
		return closedWithin(t, c, 5*time.Second)
	}
	if got := send("/api/upload"); !strings.Contains(got, "200 OK") || !strings.Contains(got, "read 4") {
		t.Fatalf("slow upload on a transfer route: %q", got)
	}
	if got := send("/api/form"); strings.Contains(got, "read 4") {
		t.Fatalf("ordinary route accepted a body slower than its deadline: %q", got)
	}
}

package antivirus_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"scibox/server/internal/antivirus"
)

// fakeClamd — маленький clamd: разбирает INSTREAM как настоящий и отвечает заданным ответом.
type fakeClamd struct {
	ln       net.Listener
	reply    string        // что ответить на INSTREAM; пусто — закрыть соединение молча
	pong     string        // ответ на PING
	hang     bool          // принять поток и не отвечать
	cutAfter int           // закрыть соединение, получив столько байт потока (0 — не закрывать)
	got      chan []byte   // что пришло в потоке
	cmd      chan string   // какая команда пришла
	delay    time.Duration // пауза перед ответом
}

func newFakeClamd(t *testing.T) *fakeClamd {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeClamd{ln: ln, pong: "PONG", got: make(chan []byte, 8), cmd: make(chan string, 8)}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeClamd) addr() string { return f.ln.Addr().String() }

func (f *fakeClamd) serve() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(c)
	}
}

func (f *fakeClamd) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	cmd := make([]byte, 0, 16)
	one := make([]byte, 1)
	for {
		if _, err := c.Read(one); err != nil {
			return
		}
		if one[0] == 0 {
			break
		}
		cmd = append(cmd, one[0])
	}
	f.cmd <- string(cmd)
	switch string(cmd) {
	case "zPING":
		_, _ = c.Write([]byte(f.pong + "\x00"))
	case "zINSTREAM":
		var stream []byte
		for {
			var hdr [4]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(hdr[:])
			if n == 0 {
				break
			}
			chunk := make([]byte, n)
			if _, err := io.ReadFull(c, chunk); err != nil {
				return
			}
			stream = append(stream, chunk...)
			if f.cutAfter > 0 && len(stream) >= f.cutAfter {
				return
			}
		}
		f.got <- stream
		if f.hang {
			time.Sleep(5 * time.Second)
			return
		}
		time.Sleep(f.delay)
		if f.reply != "" {
			_, _ = c.Write([]byte(f.reply + "\x00"))
		}
	}
}

func TestClamdScanReplies(t *testing.T) {
	tests := []struct {
		name      string
		reply     string
		infected  bool
		signature string
		wantErr   bool
	}{
		{name: "clean", reply: "stream: OK"},
		{name: "infected", reply: "stream: Win.Test.EICAR_HDB-1 FOUND", infected: true, signature: "Win.Test.EICAR_HDB-1"},
		{name: "size limit", reply: "INSTREAM size limit exceeded. ERROR", wantErr: true},
		{name: "scanner error", reply: "stream: Can't allocate memory ERROR", wantErr: true},
		{name: "unknown text", reply: "hello", wantErr: true},
		{name: "found without a name", reply: "stream:  FOUND", wantErr: true},
		{name: "ok that is not ours", reply: "OK", wantErr: true},
		{name: "silence then close", reply: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeClamd(t)
			f.reply = tt.reply
			v, err := antivirus.NewClamd(f.addr()).Scan(t.Context(), strings.NewReader("data"))
			if tt.wantErr {
				if !errors.Is(err, antivirus.ErrUnavailable) {
					t.Fatalf("want ErrUnavailable, got %v (verdict %+v)", err, v)
				}
				if v.Infected {
					t.Fatal("a failure must not look like a verdict")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if v.Infected != tt.infected || v.Signature != tt.signature {
				t.Fatalf("got %+v", v)
			}
		})
	}
}

func TestClamdSendsTheWholeStreamInChunks(t *testing.T) {
	for _, n := range []int{0, 1, 64 << 10, 64<<10 + 1, 3 << 20} {
		f := newFakeClamd(t)
		f.reply = "stream: OK"
		want := bytes.Repeat([]byte("abcdefgh"), n/8+1)[:n]
		if _, err := antivirus.NewClamd(f.addr()).Scan(t.Context(), bytes.NewReader(want)); err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
		if got := <-f.got; !bytes.Equal(got, want) {
			t.Fatalf("%d bytes: clamd received %d bytes, different", n, len(got))
		}
		if cmd := <-f.cmd; cmd != "zINSTREAM" {
			t.Fatalf("command %q", cmd)
		}
	}
}

func TestClamdFailsClosed(t *testing.T) {
	t.Run("clamd cuts the connection during the transfer", func(t *testing.T) {
		f := newFakeClamd(t)
		f.cutAfter = 64 << 10
		_, err := antivirus.NewClamd(f.addr()).Scan(t.Context(), bytes.NewReader(make([]byte, 8<<20)))
		if !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("nobody listens", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		c := antivirus.NewClamd(addr)
		if _, err := c.Scan(t.Context(), strings.NewReader("x")); !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("scan: %v", err)
		}
		if err := c.Ping(t.Context()); !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("ping: %v", err)
		}
	})
	t.Run("clamd never answers and the caller's deadline ends the wait", func(t *testing.T) {
		f := newFakeClamd(t)
		f.hang = true
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := antivirus.NewClamd(f.addr()).Scan(ctx, strings.NewReader("x"))
		if !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("got %v", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatalf("waited %v", time.Since(start))
		}
	})
	t.Run("the caller cancels", func(t *testing.T) {
		f := newFakeClamd(t)
		f.hang = true
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(200*time.Millisecond, cancel)
		_, err := antivirus.NewClamd(f.addr()).Scan(ctx, strings.NewReader("x"))
		if !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a cancelled context stops a long transfer between chunks", func(t *testing.T) {
		f := newFakeClamd(t)
		f.reply = "stream: OK"
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := antivirus.NewClamd(f.addr()).Scan(ctx, bytes.NewReader(make([]byte, 1<<20)))
		if !errors.Is(err, antivirus.ErrUnavailable) {
			t.Fatalf("got %v", err)
		}
	})
}

type brokenReader struct{ err error }

func (b brokenReader) Read([]byte) (int, error) { return 0, b.err }

func TestClamdReadErrorIsNotAScannerFailure(t *testing.T) {
	f := newFakeClamd(t)
	f.reply = "stream: OK"
	boom := errors.New("storage broke")
	_, err := antivirus.NewClamd(f.addr()).Scan(t.Context(), brokenReader{boom})
	if !errors.Is(err, boom) || !antivirus.IsReadError(err) || errors.Is(err, antivirus.ErrUnavailable) {
		t.Fatalf("got %v (read error %v)", err, antivirus.IsReadError(err))
	}
	if antivirus.IsReadError(antivirus.ErrUnavailable) {
		t.Fatal("scanner failure taken for a read error")
	}
}

func TestClamdPing(t *testing.T) {
	f := newFakeClamd(t)
	c := antivirus.NewClamd(f.addr())
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.pong = "WHAT"
	if err := c.Ping(t.Context()); !errors.Is(err, antivirus.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestFakeAndSkip(t *testing.T) {
	ctx := t.Context()
	fake := &antivirus.Fake{}
	if v, err := fake.Scan(ctx, bytes.NewReader(antivirus.EICAR())); err != nil || !v.Infected || v.Signature == "" {
		t.Fatalf("eicar: %+v %v", v, err)
	}
	if v, err := fake.Scan(ctx, strings.NewReader("hello")); err != nil || v.Infected {
		t.Fatalf("clean: %+v %v", v, err)
	}
	boom := errors.New("down")
	fake.Fault = func() error { return boom }
	if _, err := fake.Scan(ctx, strings.NewReader("x")); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if err := fake.Ping(ctx); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if fake.Scans() != 3 {
		t.Fatalf("scans %d", fake.Scans())
	}
	if _, err := (&antivirus.Fake{}).Scan(ctx, brokenReader{boom}); !antivirus.IsReadError(err) {
		t.Fatalf("fake read error: %v", err)
	}
	var s antivirus.Skip
	if v, err := s.Scan(ctx, bytes.NewReader(antivirus.EICAR())); err != nil || v.Infected {
		t.Fatalf("skip: %+v %v", v, err)
	}
	if _, err := s.Scan(ctx, brokenReader{boom}); !antivirus.IsReadError(err) {
		t.Fatalf("skip read error: %v", err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

// Настоящий ClamAV из docker-compose.yml (сервис clamav): единственная проверка, что мы говорим с ним на его языке
// и что тестовый вирус EICAR действительно отклоняется.
func realClamd(t *testing.T) *antivirus.Clamd {
	t.Helper()
	addr := os.Getenv("TEST_CLAMAV_ADDR")
	if addr == "" {
		addr = "localhost:3310"
	}
	c := antivirus.NewClamd(addr)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ClamAV из docker-compose недоступен (make db-up, первый запуск до минуты): %v", err)
	}
	return c
}

func TestRealClamAV(t *testing.T) {
	c := realClamd(t)
	t.Run("EICAR is rejected", func(t *testing.T) {
		v, err := c.Scan(t.Context(), bytes.NewReader(antivirus.EICAR()))
		if err != nil || !v.Infected || !strings.Contains(strings.ToLower(v.Signature), "eicar") {
			t.Fatalf("got %+v %v", v, err)
		}
	})
	t.Run("an honest pdf passes", func(t *testing.T) {
		v, err := c.Scan(t.Context(), strings.NewReader("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n"))
		if err != nil || v.Infected {
			t.Fatalf("got %+v %v", v, err)
		}
	})
	t.Run("a large clean stream passes", func(t *testing.T) {
		v, err := c.Scan(t.Context(), io.LimitReader(zeroes{}, 10<<20))
		if err != nil || v.Infected {
			t.Fatalf("got %+v %v", v, err)
		}
	})
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) { clear(p); return len(p), nil }

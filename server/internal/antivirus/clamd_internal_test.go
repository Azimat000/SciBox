package antivirus

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// scriptedConn — соединение, которое ломается на заданной записи или не отвечает при чтении.
type scriptedConn struct {
	net.Conn
	writes, failAt int // failAt — номер записи, которая ломается (с 1); 0 — не ломать
	readErr        error
	reply          []byte
	wrote          bytes.Buffer
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.writes++
	if c.failAt != 0 && c.writes >= c.failAt {
		return 0, errors.New("connection reset by peer")
	}
	c.wrote.Write(p)
	return len(p), nil
}

func (c *scriptedConn) Read(p []byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	if len(c.reply) == 0 {
		return 0, errors.New("no reply")
	}
	n := copy(p, c.reply)
	c.reply = c.reply[n:]
	return n, nil
}

func (c *scriptedConn) SetDeadline(time.Time) error { return nil }
func (c *scriptedConn) Close() error                { return nil }

func clientOn(conn net.Conn) *Clamd {
	return &Clamd{addr: "scripted", dial: func(context.Context, string, string) (net.Conn, error) { return conn, nil }}
}

func TestClamdBrokenConnectionIsAlwaysUnavailable(t *testing.T) {
	ctx := t.Context()
	cases := map[string]func(c *Clamd) error{
		"ping: the request cannot be sent": func(c *Clamd) error { return c.Ping(ctx) },
		"scan: the command cannot be sent": func(c *Clamd) error { _, err := c.Scan(ctx, bytes.NewReader([]byte("x"))); return err },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(clientOn(&scriptedConn{failAt: 1})); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("ping: no reply", func(t *testing.T) {
		if err := clientOn(&scriptedConn{readErr: errors.New("eof")}).Ping(ctx); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("scan: the connection breaks on the closing zero chunk", func(t *testing.T) {
		// Записи: команда, кусок с данными, нулевой кусок.
		if _, err := clientOn(&scriptedConn{failAt: 3, reply: []byte("stream: OK\x00")}).Scan(ctx, bytes.NewReader([]byte("data"))); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("scan: the connection breaks on a data chunk", func(t *testing.T) {
		if _, err := clientOn(&scriptedConn{failAt: 2, reply: []byte("stream: OK\x00")}).Scan(ctx, bytes.NewReader([]byte("data"))); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("scan: the caller leaves in the middle of a long transfer", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		src := &cancelAfter{n: chunkSize, cancel: cancel}
		_, err := clientOn(&scriptedConn{reply: []byte("stream: OK\x00")}).Scan(cctx, src)
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

// cancelAfter отдаёт нули и отменяет контекст после первых n байт.
type cancelAfter struct {
	n      int
	given  int
	cancel context.CancelFunc
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	c.given += len(p)
	if c.given >= c.n {
		c.cancel()
	}
	clear(p)
	return len(p), nil
}

func TestReadErrorMessageAndWrapping(t *testing.T) {
	boom := errors.New("storage broke")
	err := error(readErr{boom})
	if err.Error() != "storage broke" || !errors.Is(err, boom) || !IsReadError(err) {
		t.Errorf("err = %v", err)
	}
}

func TestFakePingWithoutAFault(t *testing.T) {
	if err := (&Fake{}).Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
}

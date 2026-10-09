package antivirus

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	// chunkSize — по сколько байт файл уходит в clamd.
	chunkSize = 64 << 10
	// dialTimeout — сколько ждать соединения; на всю проверку срок задаёт контекст вызывающего.
	dialTimeout = 3 * time.Second
	// scanTimeout — на сколько секунд хватает одной проверки, если у контекста нет своего срока.
	scanTimeout = 60 * time.Second
	// maxReply — самый длинный ответ clamd, который читаем.
	maxReply = 4 << 10
)

// Clamd — клиент clamd по TCP. Одно соединение на проверку.
type Clamd struct {
	addr string
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewClamd создаёт клиента для адреса «хост:порт».
func NewClamd(addr string) *Clamd {
	d := &net.Dialer{Timeout: dialTimeout}
	return &Clamd{addr: addr, dial: d.DialContext}
}

// connect открывает соединение и привязывает его к контексту: отмена контекста обрывает ввод-вывод.
// Возвращённая функция освобождает всё и должна быть вызвана.
func (c *Clamd) connect(ctx context.Context) (net.Conn, func(), error) {
	conn, err := c.dial(ctx, "tcp", c.addr)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: connect: %w", ErrUnavailable, err)
	}
	deadline := time.Now().Add(scanTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	// Срок один на всю проверку: ввод-вывод по этому соединению не может зависнуть дольше.
	_ = conn.SetDeadline(deadline) // ошибка только у уже закрытого соединения
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	return conn, func() { stop(); _ = conn.Close() }, nil
}

// Ping отправляет PING и ждёт PONG.
func (c *Clamd) Ping(ctx context.Context) error {
	conn, done, err := c.connect(ctx)
	if err != nil {
		return err
	}
	defer done()
	if _, err := conn.Write([]byte("zPING\x00")); err != nil {
		return fmt.Errorf("%w: ping: %w", ErrUnavailable, err)
	}
	reply, err := readReply(conn)
	if err != nil {
		return err
	}
	if reply != "PONG" {
		return fmt.Errorf("%w: ping reply %q", ErrUnavailable, reply)
	}
	return nil
}

// Scan передаёт r в clamd командой INSTREAM и разбирает ответ.
func (c *Clamd) Scan(ctx context.Context, r io.Reader) (Verdict, error) {
	conn, done, err := c.connect(ctx)
	if err != nil {
		return Verdict{}, err
	}
	defer done()
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return Verdict{}, fmt.Errorf("%w: start: %w", ErrUnavailable, err)
	}
	if err := send(ctx, conn, r); err != nil {
		return Verdict{}, err
	}
	reply, err := readReply(conn)
	if err != nil {
		return Verdict{}, err
	}
	return parseReply(reply)
}

// readErr — ошибка чтения самого файла: к clamd она отношения не имеет.
type readErr struct{ err error }

func (e readErr) Error() string { return e.err.Error() }
func (e readErr) Unwrap() error { return e.err }

// send отправляет поток кусками «4 байта длины + данные» и завершает его нулевой длиной.
func send(ctx context.Context, conn net.Conn, r io.Reader) error {
	buf := make([]byte, 4+chunkSize)
	for {
		n, rerr := io.ReadFull(r, buf[4:])
		if n > 0 {
			binary.BigEndian.PutUint32(buf[:4], uint32(n)) //nolint:gosec // G115: n ≤ chunkSize (64 КиБ)
			if _, err := conn.Write(buf[:4+n]); err != nil {
				return writeFailure(err)
			}
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			break
		}
		if rerr != nil {
			return readErr{rerr}
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return writeFailure(err)
	}
	return nil
}

// writeFailure: clamd закрыл соединение посреди передачи (например, файл больше StreamMaxLength) — проверить не вышло.
func writeFailure(err error) error {
	return fmt.Errorf("%w: send: %w", ErrUnavailable, err)
}

// readReply читает ответ до нулевого байта (команды z...).
func readReply(conn net.Conn) (string, error) {
	br := bufio.NewReaderSize(io.LimitReader(conn, maxReply), 512)
	reply, err := br.ReadString(0)
	if err != nil {
		return "", fmt.Errorf("%w: read reply: %w", ErrUnavailable, err)
	}
	return strings.TrimSuffix(reply, "\x00"), nil
}

// parseReply разбирает «stream: OK», «stream: <имя> FOUND» и «<текст> ERROR». Всё остальное — сбой.
func parseReply(reply string) (Verdict, error) {
	body := strings.TrimSpace(reply)
	switch {
	case body == "stream: OK":
		return Verdict{}, nil
	case strings.HasSuffix(body, " FOUND"):
		sig := strings.TrimSuffix(strings.TrimPrefix(body, "stream: "), " FOUND")
		sig = strings.TrimSpace(sig)
		if sig == "" || strings.ContainsAny(sig, "\r\n") {
			return Verdict{}, fmt.Errorf("%w: reply %q", ErrUnavailable, body)
		}
		return Verdict{Infected: true, Signature: sig}, nil
	}
	return Verdict{}, fmt.Errorf("%w: reply %q", ErrUnavailable, body)
}

// IsReadError сообщает, что Scan остановился из-за чтения самого файла, а не из-за сканера.
func IsReadError(err error) bool {
	var re readErr
	return errors.As(err, &re)
}

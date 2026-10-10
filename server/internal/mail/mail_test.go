package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// fakeSMTP — минимальный SMTP-сервер: принимает одно письмо и отдаёт его текст в канал.
func fakeSMTP(t *testing.T, failAt string) (addr string, got <-chan string) {
	t.Helper()
	return fakeSMTPReply(t, failAt, "550 refused", false)
}

// fakeSMTPReply — то же, но на команду failAt отвечает reply; dropOnQuit — на QUIT обрывает соединение без ответа.
func fakeSMTPReply(t *testing.T, failAt, reply string, dropOnQuit bool) (addr string, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		write("220 fake ready")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if inData {
				if line == "." {
					inData = false
					out <- data.String()
					write("250 queued")
					continue
				}
				data.WriteString(line + "\r\n")
				continue
			}
			cmd := strings.ToUpper(line)
			switch {
			case failAt != "" && strings.HasPrefix(cmd, failAt):
				write(reply)
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 hello")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				write("250 ok")
			case strings.HasPrefix(cmd, "DATA"):
				write("354 go ahead")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				if !dropOnQuit {
					write("221 bye")
				}
				return
			default:
				write("250 ok")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSMTPSendDeliversRussianMessage(t *testing.T) {
	addr, got := fakeSMTP(t, "")
	fixed := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s := SMTP{Addr: addr, From: "SciBox <no-reply@scibox.local>", Now: func() time.Time { return fixed }}
	err := s.Send(context.Background(), Message{To: "ivan@example.ru", Subject: "Подтвердите почту", Body: "Здравствуйте!\nПерейдите по ссылке."})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	raw := <-got
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse sent message: %v\n%s", err, raw)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Подтвердите почту" {
		t.Fatalf("subject = %q, %v", subject, err)
	}
	if h := msg.Header.Get("To"); !strings.Contains(h, "ivan@example.ru") {
		t.Fatalf("To = %q", h)
	}
	if h := msg.Header.Get("From"); !strings.Contains(h, "no-reply@scibox.local") {
		t.Fatalf("From = %q", h)
	}
	if h := msg.Header.Get("Date"); !strings.Contains(h, "02 Oct 2026") {
		t.Fatalf("Date = %q", h)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if want := "Здравствуйте!\r\nПерейдите по ссылке."; strings.TrimSpace(string(body)) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestSMTPSendErrors(t *testing.T) {
	addr, _ := fakeSMTP(t, "")
	deadAddr := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		a := ln.Addr().String()
		_ = ln.Close()
		return a
	}()
	cases := []struct {
		name string
		s    SMTP
		m    Message
	}{
		{"bad sender", SMTP{Addr: addr, From: "not an address"}, Message{To: "a@b.ru", Subject: "s", Body: "b"}},
		{"bad recipient", SMTP{Addr: addr, From: "a@b.ru"}, Message{To: "nope", Subject: "s", Body: "b"}},
		{"line break in subject", SMTP{Addr: addr, From: "a@b.ru"}, Message{To: "a@b.ru", Subject: "s\r\nBcc: x@y.ru", Body: "b"}},
		{"server down", SMTP{Addr: deadAddr, From: "a@b.ru"}, Message{To: "a@b.ru", Subject: "s", Body: "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Send(context.Background(), tc.m); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestSMTPSendServerRefusals(t *testing.T) {
	for _, stage := range []string{"MAIL", "RCPT", "DATA"} {
		t.Run(stage, func(t *testing.T) {
			addr, _ := fakeSMTP(t, stage)
			s := SMTP{Addr: addr, From: "a@b.ru", Timeout: 3 * time.Second}
			if err := s.Send(context.Background(), Message{To: "c@d.ru", Subject: "s", Body: "b"}); err == nil {
				t.Fatalf("expected an error when server refuses %s", stage)
			}
		})
	}
}

func TestSMTPSendNoGreeting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close() // сразу закрываем: приветствия нет
		}
	}()
	s := SMTP{Addr: ln.Addr().String(), From: "a@b.ru", Timeout: 3 * time.Second}
	if err := s.Send(context.Background(), Message{To: "c@d.ru", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("expected an error without a greeting")
	}
}

func TestMemory(t *testing.T) {
	m := &Memory{}
	if err := m.Send(context.Background(), Message{To: "a@b.ru", Subject: "1"}); err != nil {
		t.Fatal(err)
	}
	sent := m.Sent()
	if len(sent) != 1 || sent[0].Subject != "1" {
		t.Fatalf("sent = %+v", sent)
	}
	sent[0].Subject = "changed"
	if m.Sent()[0].Subject != "1" {
		t.Fatal("Sent must return a copy")
	}
	m.Err = io.ErrClosedPipe
	if err := m.Send(context.Background(), Message{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("err = %v", err)
	}
	if len(m.Sent()) != 1 {
		t.Fatal("failed send must not be stored")
	}
}

// TestFailureKinds: от вида сбоя зависит судьба письма в очереди (срез 25): постоянный отказ получателя не повторяется,
// сбой службы не тратит попытки письма.
func TestFailureKinds(t *testing.T) {
	deadAddr := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		a := ln.Addr().String()
		_ = ln.Close()
		return a
	}()
	noGreeting := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			if c, err := ln.Accept(); err == nil {
				_ = c.Close()
			}
		}()
		return ln.Addr().String()
	}()
	good := Message{To: "c@d.ru", Subject: "s", Body: "b"}
	tests := []struct {
		name string
		addr func(t *testing.T) string
		from string
		msg  Message
		want Kind
	}{
		{"unknown recipient 550", func(t *testing.T) string { a, _ := fakeSMTPReply(t, "RCPT", "550 no such user", false); return a }, "a@b.ru", good, KindPermanent},
		{"mailbox full 452", func(t *testing.T) string { a, _ := fakeSMTPReply(t, "RCPT", "452 mailbox full", false); return a }, "a@b.ru", good, KindTemporary},
		{"greylisted 450", func(t *testing.T) string { a, _ := fakeSMTPReply(t, "RCPT", "450 try later", false); return a }, "a@b.ru", good, KindTemporary},
		{"content rejected 554 at DATA", func(t *testing.T) string { a, _ := fakeSMTPReply(t, "DATA", "554 spam", false); return a }, "a@b.ru", good, KindPermanent},
		{"sender refused is a service problem", func(t *testing.T) string { a, _ := fakeSMTPReply(t, "MAIL", "550 sender not allowed", false); return a }, "a@b.ru", good, KindService},
		{"server is down", func(*testing.T) string { return deadAddr }, "a@b.ru", good, KindService},
		{"no greeting", func(*testing.T) string { return noGreeting }, "a@b.ru", good, KindService},
		{"bad recipient address", func(t *testing.T) string { a, _ := fakeSMTP(t, ""); return a }, "a@b.ru", Message{To: "nope", Subject: "s", Body: "b"}, KindPermanent},
		{"line break in subject", func(t *testing.T) string { a, _ := fakeSMTP(t, ""); return a }, "a@b.ru", Message{To: "c@d.ru", Subject: "s\r\nBcc: x@y.ru", Body: "b"}, KindPermanent},
		{"bad sender setting", func(t *testing.T) string { a, _ := fakeSMTP(t, ""); return a }, "not an address", good, KindService},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := SMTP{Addr: tt.addr(t), From: tt.from, Timeout: 3 * time.Second}
			err := s.Send(context.Background(), tt.msg)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := KindOf(err); got != tt.want {
				t.Errorf("KindOf(%v) = %d, want %d", err, got, tt.want)
			}
		})
	}
}

func TestKindOfUnknownErrorsIsTemporary(t *testing.T) {
	if got := KindOf(errors.New("whatever")); got != KindTemporary {
		t.Errorf("KindOf(plain error) = %d, want KindTemporary", got)
	}
	wrapped := &Error{Kind: KindPermanent, Err: errors.New("inner")}
	if got := KindOf(errors.Join(errors.New("ctx"), wrapped)); got != KindPermanent {
		t.Errorf("KindOf(joined) = %d, want KindPermanent", got)
	}
	if wrapped.Error() != "inner" || !errors.Is(wrapped, wrapped.Err) {
		t.Error("Error() and Unwrap() must expose the inner error")
	}
}

// Ошибка прощания не должна превращать принятое письмо в «не отправлено»: повтор прислал бы его второй раз.
func TestQuitFailureDoesNotUndoAnAcceptedMessage(t *testing.T) {
	addr, got := fakeSMTPReply(t, "", "", true)
	s := SMTP{Addr: addr, From: "a@b.ru", Timeout: 3 * time.Second}
	if err := s.Send(context.Background(), Message{To: "c@d.ru", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("Send: %v: a message accepted at the end of DATA is delivered even if QUIT fails", err)
	}
	select {
	case <-got:
	default:
		t.Error("the server did not receive the message")
	}
}

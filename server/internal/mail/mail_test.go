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
				write("550 refused")
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 hello")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				write("250 ok")
			case strings.HasPrefix(cmd, "DATA"):
				write("354 go ahead")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				write("221 bye")
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

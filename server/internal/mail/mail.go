// Package mail отправляет письма. Локально письма уходят по SMTP в Mailpit (localhost:1025),
// смотреть их можно на http://localhost:8025. В интернете — через почтовый сервис
// с шифрованием (TLS или STARTTLS) и входом по паролю (D-131).
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// Message — одно письмо с текстовым телом.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender отправляет письма.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Режимы шифрования соединения с почтовым сервером.
const (
	// TLSOff — без шифрования: только для локального Mailpit.
	TLSOff = "off"
	// TLSStartTLS — обычное соединение, которое сразу переходит на шифрование (обычно порт 587).
	TLSStartTLS = "starttls"
	// TLSImplicit — шифрование с первого байта (обычно порт 465).
	TLSImplicit = "tls"
)

// SMTP отправляет письма на SMTP-сервер.
type SMTP struct {
	// Addr — адрес сервера вместе с портом, например localhost:1025.
	Addr string
	// From — отправитель целиком, например «SciBox <no-reply@scibox.local>».
	From string
	// TLS — режим шифрования: TLSOff (по умолчанию, если пусто), TLSStartTLS или TLSImplicit.
	TLS string
	// Username и Password — вход на почтовый сервер; пустой Username означает «без входа».
	// Вход разрешён только по зашифрованному соединению.
	Username string
	Password string
	// TLSConfig подменяется в тестах (свой корневой сертификат); по умолчанию проверка по системным.
	TLSConfig *tls.Config
	// Timeout ограничивает всю отправку; 0 означает 10 секунд.
	Timeout time.Duration
	// Now подменяется в тестах (дата в заголовке письма).
	Now func() time.Time
}

// Send отправляет письмо.
func (s SMTP) Send(ctx context.Context, m Message) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("mail: bad sender %q: %w", s.From, err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("mail: bad recipient %q: %w", m.To, err)
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("mail: subject contains a line break")
	}
	raw, err := s.compose(from, to, m)
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("mail: connect %s: %w", s.Addr, err)
	}
	defer func() { _ = conn.Close() }() // уборка: письмо считается принятым по ответу на DATA и QUIT, а не по закрытию
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("mail: set deadline: %w", err)
	}

	host, _, _ := net.SplitHostPort(s.Addr)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if s.TLSConfig != nil {
		tlsCfg = s.TLSConfig.Clone()
		if tlsCfg.ServerName == "" {
			tlsCfg.ServerName = host
		}
	}
	mode := s.TLS
	if mode == "" {
		mode = TLSOff
	}
	switch mode {
	case TLSOff, TLSStartTLS:
	case TLSImplicit:
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("mail: tls handshake: %w", err)
		}
		conn = tc
	default:
		return fmt.Errorf("mail: unknown TLS mode %q", mode)
	}
	if s.Username != "" && mode == TLSOff {
		return errors.New("mail: login without encryption is not allowed")
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("mail: smtp greeting: %w", err)
	}
	defer func() { _ = c.Close() }() // после QUIT соединение уже закрыто сервером, ошибка здесь ожидаема
	if mode == TLSStartTLS {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("mail: STARTTLS: %w", err)
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, host)); err != nil {
			return fmt.Errorf("mail: login: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: finish body: %w", err)
	}
	return c.Quit()
}

func (s SMTP) compose(from, to *mail.Address, m Message) ([]byte, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from.String())
	fmt.Fprintf(&buf, "To: %s\r\n", to.String())
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", now().Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n"))); err != nil {
		return nil, fmt.Errorf("mail: encode body: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("mail: encode body: %w", err)
	}
	return buf.Bytes(), nil
}

// Memory запоминает письма вместо отправки: для тестов.
type Memory struct {
	mu   sync.Mutex
	sent []Message
	// Err, если задана, возвращается из Send (письмо при этом не запоминается).
	Err error
}

// Send запоминает письмо.
func (m *Memory) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.sent = append(m.sent, msg)
	return nil
}

// Sent возвращает копию списка отправленных писем.
func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.sent...)
}

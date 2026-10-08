package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// testCert выпускает самоподписанный сертификат на 127.0.0.1 и пул, которому он доверен.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// secureSMTPResult — что увидел поддельный сервер.
type secureSMTPResult struct {
	encrypted bool   // письмо пришло по зашифрованному соединению
	login     string // «пользователь:пароль» из AUTH PLAIN, пусто, если входа не было
	data      string
}

// fakeSecureSMTP — поддельный SMTP-сервер с шифрованием: implicit=true — TLS с первого байта (порт 465),
// иначе STARTTLS (порт 587). wantPassword, если не пусто, — единственный верный пароль.
func fakeSecureSMTP(t *testing.T, implicit bool, wantPassword string) (addr string, pool *x509.CertPool, got <-chan secureSMTPResult) {
	t.Helper()
	cert, pool := testCert(t)
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	out := make(chan secureSMTPResult, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		conn := raw
		defer func() { _ = conn.Close() }()
		res := secureSMTPResult{}
		if implicit {
			conn = tls.Server(raw, srvTLS)
			res.encrypted = true
		}
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		write("220 fake secure ready")
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
					res.data = data.String()
					out <- res
					write("250 queued")
					continue
				}
				data.WriteString(line + "\r\n")
				continue
			}
			cmd := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				if res.encrypted {
					write("250-hello\r\n250 AUTH PLAIN")
				} else {
					write("250-hello\r\n250 STARTTLS")
				}
			case strings.HasPrefix(cmd, "STARTTLS"):
				write("220 go ahead")
				conn = tls.Server(raw, srvTLS)
				r = bufio.NewReader(conn)
				res.encrypted = true
			case strings.HasPrefix(cmd, "AUTH PLAIN"):
				dec, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
				parts := strings.Split(string(dec), "\x00")
				if len(parts) != 3 || (wantPassword != "" && parts[2] != wantPassword) {
					write("535 bad credentials")
					continue
				}
				res.login = parts[1] + ":" + parts[2]
				write("235 ok")
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
	return ln.Addr().String(), pool, out
}

func TestSMTPSendEncryptedWithLogin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		implicit bool
	}{
		{"implicit TLS (465)", TLSImplicit, true},
		{"STARTTLS (587)", TLSStartTLS, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, pool, got := fakeSecureSMTP(t, tc.implicit, "secret")
			s := SMTP{
				Addr: addr, From: "SciBox <no-reply@scibox.example>", TLS: tc.mode,
				Username: "no-reply@scibox.example", Password: "secret",
				TLSConfig: &tls.Config{RootCAs: pool}, Timeout: 5 * time.Second,
			}
			if err := s.Send(context.Background(), Message{To: "ivan@example.ru", Subject: "Тема", Body: "Текст"}); err != nil {
				t.Fatalf("Send: %v", err)
			}
			res := <-got
			if !res.encrypted || res.login != "no-reply@scibox.example:secret" || !strings.Contains(res.data, "ivan@example.ru") {
				t.Fatalf("server saw %+v", res)
			}
		})
	}
}

func TestSMTPSendEncryptedWithoutLogin(t *testing.T) {
	addr, pool, got := fakeSecureSMTP(t, true, "")
	s := SMTP{Addr: addr, From: "a@b.ru", TLS: TLSImplicit, TLSConfig: &tls.Config{RootCAs: pool}, Timeout: 5 * time.Second}
	if err := s.Send(context.Background(), Message{To: "c@d.ru", Subject: "s", Body: "b"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res := <-got; !res.encrypted || res.login != "" {
		t.Fatalf("server saw %+v", res)
	}
}

func TestSMTPSendSecureErrors(t *testing.T) {
	msg := Message{To: "c@d.ru", Subject: "s", Body: "b"}
	t.Run("wrong password", func(t *testing.T) {
		addr, pool, _ := fakeSecureSMTP(t, true, "secret")
		s := SMTP{Addr: addr, From: "a@b.ru", TLS: TLSImplicit, Username: "a@b.ru", Password: "nope", TLSConfig: &tls.Config{RootCAs: pool}, Timeout: 3 * time.Second}
		if err := s.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "login") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		addr, _, _ := fakeSecureSMTP(t, true, "")
		s := SMTP{Addr: addr, From: "a@b.ru", TLS: TLSImplicit, Timeout: 3 * time.Second}
		if err := s.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "tls handshake") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("server without STARTTLS", func(t *testing.T) {
		addr, _ := fakeSMTP(t, "")
		s := SMTP{Addr: addr, From: "a@b.ru", TLS: TLSStartTLS, Timeout: 3 * time.Second}
		if err := s.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("login without encryption", func(t *testing.T) {
		addr, _ := fakeSMTP(t, "")
		s := SMTP{Addr: addr, From: "a@b.ru", Username: "a@b.ru", Password: "p", Timeout: 3 * time.Second}
		if err := s.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "without encryption") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown mode", func(t *testing.T) {
		addr, _ := fakeSMTP(t, "")
		s := SMTP{Addr: addr, From: "a@b.ru", TLS: "ssl3", Timeout: 3 * time.Second}
		if err := s.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "unknown TLS mode") {
			t.Fatalf("err = %v", err)
		}
	})
}

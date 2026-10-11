package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// hammer запускает n одинаковых вызовов одновременно и считает, чем они кончились.
func hammer(n int, call func(i int) error) (limited, other int) {
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := call(i)
			mu.Lock()
			defer mu.Unlock()
			if errors.As(err, new(*RateLimitedError)) {
				limited++
			} else {
				other++
			}
		}()
	}
	close(start)
	wg.Wait()
	return limited, other
}

// Пачка параллельных запросов не должна пройти лимит попыток входа целиком (срез 29, AUT-1, CON-1): проверка и запись
// попытки идут одним шагом.
func TestLoginLimitHoldsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	max := e.svc.cfg.Limits.LoginEmail.Max
	meta := e.meta()
	limited, passed := hammer(60, func(i int) error {
		m := meta
		m.IP = fmt.Sprintf("192.0.2.%d", i+1) // разные адреса: считается лимит по почте
		_, _, err := e.svc.Login(bg, email, fmt.Sprintf("wrong password %d!", i), m)
		return err
	})
	if passed > max {
		t.Fatalf("%d password checks went through, the limit is %d", passed, max)
	}
	if limited != 60-passed {
		t.Fatalf("limited = %d, passed = %d", limited, passed)
	}
}

func TestMailLimitHoldsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	max := e.svc.cfg.Limits.MailIP.Max
	meta := e.meta()
	limited, passed := hammer(40, func(int) error {
		_, err := e.svc.Register(bg, RegisterInput{Name: "Иван Петров", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta)
		return err
	})
	if passed > max {
		t.Fatalf("%d registrations went through from one address, the limit is %d", passed, max)
	}
	if limited != 40-passed {
		t.Fatalf("limited = %d, passed = %d", limited, passed)
	}
}

func TestSuccessfulLoginsDoNotUseUpTheAddressLimit(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	meta := e.meta()
	for i := 0; i < e.svc.cfg.Limits.LoginIP.Max+5; i++ {
		if _, _, err := e.svc.Login(bg, email, goodPassword, meta); err != nil {
			t.Fatalf("login %d: %v", i+1, err)
		}
	}
}

// Лимит «по адресу» считает IPv6 по сети /64: смена последних 64 бит не даёт новых попыток (срез 29, AUT-4).
func TestAddressLimitsCountAnIPv6NetworkOnce(t *testing.T) {
	e := newEnv(t)
	n := seq.Add(1)
	meta := Meta{UserAgent: "test-agent"}
	for i := 0; i < e.svc.cfg.Limits.MailIP.Max; i++ {
		meta.IP = fmt.Sprintf("2001:db8:%x:%x::%x", n>>16&0xffff, n&0xffff, i+1)
		if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван Петров", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta); err != nil {
			t.Fatalf("registration %d: %v", i+1, err)
		}
	}
	meta.IP = fmt.Sprintf("2001:db8:%x:%x:aaaa:bbbb:cccc:dddd", n>>16&0xffff, n&0xffff)
	_, err := e.svc.Register(bg, RegisterInput{Name: "Иван Петров", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta)
	if !errors.As(err, new(*RateLimitedError)) {
		t.Fatalf("err = %v: another address of the same /64 must share the limit", err)
	}
	// Соседняя сеть /64 считается отдельно.
	meta.IP = fmt.Sprintf("2001:db8:%x:%x::1", n>>16&0xffff, (n&0xffff)^0x8000)
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван Петров", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta); err != nil {
		t.Fatalf("a different /64: %v", err)
	}
}

// Владельцу подтверждённой почты письмо «вы уже зарегистрированы» уходит не чаще трёх раз в сутки, как бы много
// адресов ни слали запросы; сами запросы отвечают одинаково (срез 29, AUT-4, WEB-3).
func TestRegisterNoticeToAnExistingAccountIsLimitedPerRecipient(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	before := len(e.mailsTo(email))
	for i := 0; i < 8; i++ {
		if _, err := e.svc.Register(bg, RegisterInput{Name: "Кто-то", Email: email, Password: goodPassword, Consent: true}, e.meta()); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	if got := len(e.mailsTo(email)) - before; got != e.svc.cfg.Limits.Notice.Max {
		t.Fatalf("%d notices sent, want %d", got, e.svc.cfg.Limits.Notice.Max)
	}
	e.clock.Advance(25 * time.Hour)
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Кто-то", Email: email, Password: goodPassword, Consent: true}, e.meta()); err != nil {
		t.Fatal(err)
	}
	if got := len(e.mailsTo(email)) - before; got != e.svc.cfg.Limits.Notice.Max+1 {
		t.Fatalf("after a day: %d notices, want %d", got, e.svc.cfg.Limits.Notice.Max+1)
	}
}

// У сессии есть абсолютный предел жизни: пока ею пользуются, срок продлевается, но не дальше предела со дня входа
// (срез 29, AUT-11).
func TestSessionHasAnAbsoluteLifetime(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	ttl, maxAge := e.svc.cfg.SessionTTL, e.svc.cfg.SessionMaxAge
	// Заходим раз в 20 дней: сессия продлевается каждый раз.
	var last Principal
	for elapsed := time.Duration(0); elapsed+20*24*time.Hour < maxAge; elapsed += 20 * 24 * time.Hour {
		e.clock.Advance(20 * 24 * time.Hour)
		p, err := e.svc.Authenticate(bg, s.Token)
		if err != nil {
			t.Fatalf("after %v of use: %v", elapsed+20*24*time.Hour, err)
		}
		last = p
	}
	if limit := s.ExpiresAt.Add(-ttl).Add(maxAge); last.ExpiresAt.After(limit) {
		t.Fatalf("session was extended past the absolute limit: %v > %v", last.ExpiresAt, limit)
	}
	// Дальше предела сессия не живёт, сколько бы раз ни продлевалась.
	e.clock.Advance(maxAge)
	if _, err := e.svc.Authenticate(bg, s.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("past the limit: %v", err)
	}
	// Ноль в настройках не значит «без предела».
	cfg := DefaultConfig("SciBox", "http://localhost:5173")
	cfg.SessionMaxAge = 0
	if got := NewService(sharedPool, e.mailer, cfg, slog.New(slog.NewTextHandler(e.logs, nil))).cfg.SessionMaxAge; got <= 0 {
		t.Fatalf("SessionMaxAge = %v", got)
	}
}

// Старая ссылка подтверждения, открытая в чужом браузере у уже подтверждённого аккаунта, ничего не делает: ни входа, ни
// обнуления пароля (срез 29, самопроверка D-190).
func TestStaleConfirmationLinkDoesNotTouchAConfirmedAccount(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	link := e.register(email)
	// Почту подтвердили сбросом пароля; ссылка подтверждения осталась неиспользованной.
	if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
		t.Fatal(err)
	}
	var tok string
	for _, m := range e.mailsTo(email) {
		if strings.Contains(m.Body, "/reset-password?token=") {
			tok = tokenIn(t, m)
		}
	}
	if err := e.svc.ResetPassword(bg, tok, "another password 77"); err != nil {
		t.Fatal(err)
	}
	other := e.meta()
	other.Binding = "some-other-browser"
	if _, _, err := e.svc.ConfirmEmail(bg, link, other); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("stale link: %v", err)
	}
	if _, _, err := e.login(email, "another password 77"); err != nil {
		t.Fatalf("the password must stay: %v", err)
	}
}

package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Список «Где вы вошли» (T-10): свои живые сессии, текущая отмечена, устройство разобрано.
func TestListSessions(t *testing.T) {
	e := newEnv(t)
	_, s1, email := e.confirmedUser()
	p1, err := e.svc.Authenticate(bg, s1.Token)
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	m := e.meta()
	m.UserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1"
	_, s2, err := e.svc.Login(bg, email, goodPassword, m)
	if err != nil {
		t.Fatal(err)
	}
	// Чужая сессия в список не попадает.
	_, _, _ = e.confirmedUser()

	list, err := e.svc.ListSessions(bg, p1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("sessions: %+v", list)
	}
	// Последняя сверху: вход с телефона минутой позже.
	if list[0].Current || list[0].Device != (Device{Browser: "safari", OS: "ios", Mobile: true}) {
		t.Fatalf("first row: %+v", list[0])
	}
	if !list[1].Current || list[1].ID != p1.SessionID {
		t.Fatalf("second row must be the current session: %+v", list[1])
	}
	p2, _ := e.svc.Authenticate(bg, s2.Token)
	if !list[0].CreatedAt.Equal(e.clock.Now()) || list[0].ID != p2.SessionID {
		t.Fatalf("phone session: %+v", list[0])
	}

	// Просроченная сессия не показывается.
	e.clock.Advance(31 * 24 * time.Hour)
	if list, err = e.svc.ListSessions(bg, p1); err != nil || len(list) != 0 {
		t.Fatalf("expired sessions must not be listed: %+v, %v", list, err)
	}
}

func TestRevokeSession(t *testing.T) {
	e := newEnv(t)
	_, s1, email := e.confirmedUser()
	_, s2, err := e.login(email, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := e.svc.Authenticate(bg, s1.Token)
	p2, _ := e.svc.Authenticate(bg, s2.Token)

	_, sOther, _ := e.confirmedUser()
	pOther, _ := e.svc.Authenticate(bg, sOther.Token)

	// Чужую сессию завершить нельзя, и ответ тот же, что для несуществующей.
	if err := e.svc.RevokeSession(bg, p1, pOther.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke someone else's session: %v", err)
	}
	if _, err := e.svc.Authenticate(bg, sOther.Token); err != nil {
		t.Fatal("someone else's session must survive")
	}
	if err := e.svc.RevokeSession(bg, p1, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown session: %v", err)
	}

	if err := e.svc.RevokeSession(bg, p1, p2.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, s2.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("the revoked session must end")
	}
	if _, err := e.svc.Authenticate(bg, s1.Token); err != nil {
		t.Fatal("the current session must stay")
	}
	if err := e.svc.RevokeSession(bg, p1, p2.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking twice: %v", err)
	}
}

// Свежий пароль (T-12, D-144): вход по паролю даёт его сразу, вход по ссылке из письма нет; Reauth даёт его
// на ReauthTTL.
func TestFreshPassword(t *testing.T) {
	e := newEnv(t)
	_, sConfirm, email := e.confirmedUser()
	p, err := e.svc.Authenticate(bg, sConfirm.Token)
	if err != nil || p.Fresh {
		t.Fatalf("a session from an email link must not count as fresh: %+v, %v", p, err)
	}

	if err := e.svc.Reauth(bg, p, "wrong password here"); fieldsOf(t, err)["password"] != msgPasswordWrong {
		t.Fatalf("wrong password: %v", err)
	}
	if p, _ = e.svc.Authenticate(bg, sConfirm.Token); p.Fresh {
		t.Fatal("a wrong password must not make the session fresh")
	}
	if err := e.svc.Reauth(bg, p, ""); fieldsOf(t, err)["password"] != msgPasswordWrong {
		t.Fatalf("empty password: %v", err)
	}
	if err := e.svc.Reauth(bg, p, goodPassword); err != nil {
		t.Fatal(err)
	}
	if p, _ = e.svc.Authenticate(bg, sConfirm.Token); !p.Fresh {
		t.Fatal("after Reauth the session must be fresh")
	}
	e.clock.Advance(DefaultReauthTTL - time.Second)
	if p, _ = e.svc.Authenticate(bg, sConfirm.Token); !p.Fresh {
		t.Fatal("still fresh just before ReauthTTL")
	}
	e.clock.Advance(time.Second)
	if p, _ = e.svc.Authenticate(bg, sConfirm.Token); p.Fresh {
		t.Fatal("fresh must end at ReauthTTL")
	}

	// Свежесть принадлежит сессии, а не человеку.
	_, sLogin, err := e.login(email, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if pl, _ := e.svc.Authenticate(bg, sLogin.Token); !pl.Fresh {
		t.Fatal("a password login must be fresh right away")
	}
	if p, _ = e.svc.Authenticate(bg, sConfirm.Token); p.Fresh {
		t.Fatal("logging in elsewhere must not refresh this session")
	}
}

// Перебор пароля через повторный ввод упирается в тот же предел, что смена пароля.
func TestReauthIsRateLimited(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)
	for range e.svc.cfg.Limits.PasswordUser.Max {
		if err := e.svc.Reauth(bg, p, "wrong password here"); fieldsOf(t, err)["password"] == "" {
			t.Fatalf("wrong password: %v", err)
		}
	}
	var rl *RateLimitedError
	if err := e.svc.Reauth(bg, p, goodPassword); !errors.As(err, &rl) {
		t.Fatalf("after the limit even the right password must wait: %v", err)
	}
	// Смена пароля считает те же попытки.
	if err := e.svc.ChangePassword(bg, p, goodPassword, "another fine passphrase"); !errors.As(err, &rl) {
		t.Fatalf("change password shares the limit: %v", err)
	}
}

// Нулевой или отрицательный срок в настройках не значит «без срока» или «удалить сразу».
func TestZeroDurationsFallBackToDefaults(t *testing.T) {
	cfg := DefaultConfig("SciBox", "http://localhost:5173")
	cfg.ReauthTTL, cfg.DeletionGrace, cfg.DeviceTTL = 0, -time.Hour, 0
	svc := newService(sharedPool, nil, cfg, nil)
	if svc.cfg.ReauthTTL != DefaultReauthTTL || svc.cfg.DeletionGrace != DefaultDeletionGrace || svc.cfg.DeviceTTL != DefaultDeviceTTL {
		t.Fatalf("defaults not applied: %+v", svc.cfg)
	}
}

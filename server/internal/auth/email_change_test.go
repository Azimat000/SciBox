package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// freshPrincipal — вошедший человек, который только что ввёл пароль.
func (e *env) freshPrincipal(s Session) Principal {
	e.t.Helper()
	p, err := e.svc.Authenticate(bg, s.Token)
	if err != nil {
		e.t.Fatal(err)
	}
	if !p.Fresh {
		if err := e.svc.Reauth(bg, p, goodPassword); err != nil {
			e.t.Fatal(err)
		}
		p.Fresh = true
	}
	return p
}

func TestRequestEmailChangeNeedsFreshPassword(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token) // сессия по ссылке из письма: пароль не вводили
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("without a fresh password: %v", err)
	}
	if len(e.mailsTo(newEmail)) != 0 {
		t.Fatal("nothing must be sent without a fresh password")
	}
}

func TestRequestEmailChangeValidation(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	p := e.freshPrincipal(s)
	cases := []struct {
		name, email, want string
	}{
		{"empty", "  ", msgEmailRequired},
		{"broken", "not-an-email", msgEmailInvalid},
		{"same", strings.ToUpper(email), msgEmailSame},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fieldsOf(t, e.svc.RequestEmailChange(bg, p, tc.email, e.meta()))["email"]; got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Полный путь (T-11, D-145): ссылка на новый адрес → почта сменилась → на старый письмо «это не я».
func TestEmailChangeFlow(t *testing.T) {
	e := newEnv(t)
	u, s, oldEmail := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()

	if err := e.svc.RequestEmailChange(bg, p, " "+strings.ToUpper(newEmail)+" ", e.meta()); err != nil {
		t.Fatal(err)
	}
	m := e.lastMailTo(newEmail)
	if !strings.Contains(m.Subject, "новую почту") || !strings.Contains(m.Body, "/confirm-email-change?token=") {
		t.Fatalf("confirmation mail: %q\n%s", m.Subject, m.Body)
	}
	// Пока ссылку не открыли, почта прежняя.
	if got, _ := e.svc.Authenticate(bg, s.Token); got.User.Email != oldEmail {
		t.Fatal("the email must not change before the link is opened")
	}
	// Ссылку смены почты нельзя использовать как ссылку подтверждения регистрации или сброса.
	tok := tokenIn(t, m)
	if _, _, err := e.svc.ConfirmEmail(bg, tok, e.meta()); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("change link used as a confirm link: %v", err)
	}

	changed, err := e.svc.ConfirmEmailChange(bg, tok)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Email != newEmail || changed.ID != u.ID || !changed.EmailConfirmed {
		t.Fatalf("changed user: %+v", changed)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the link must work once: %v", err)
	}
	// Входить теперь по новой почте, старая не подходит; сессии не завершаются.
	if _, _, err := e.login(newEmail, goodPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.login(oldEmail, goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old email must not log in: %v", err)
	}
	if _, err := e.svc.Authenticate(bg, s.Token); err != nil {
		t.Fatal("changing the email must not end sessions")
	}
	notice := e.lastMailTo(oldEmail)
	if !strings.Contains(notice.Subject, "изменена") || !strings.Contains(notice.Body, "/revert-email-change?token=") ||
		strings.Contains(notice.Body, newEmail) || !strings.Contains(notice.Body, maskEmail(newEmail)) {
		t.Fatalf("notice to the old address must mask the new one and offer a revert link:\n%s", notice.Body)
	}
}

// Занятый адрес: ответ тот же, на занятый адрес уходит «эта почта уже есть», почта не меняется.
func TestEmailChangeToTakenAddress(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	_, _, taken := e.confirmedUser()
	p := e.freshPrincipal(s)
	before := len(e.mailsTo(taken))

	if err := e.svc.RequestEmailChange(bg, p, taken, e.meta()); err != nil {
		t.Fatalf("the answer must look the same as for a free address: %v", err)
	}
	ms := e.mailsTo(taken)
	if len(ms) != before+1 {
		t.Fatalf("want one mail to the taken address, got %d", len(ms)-before)
	}
	if last := ms[len(ms)-1]; !strings.Contains(last.Subject, "уже есть") || strings.Contains(last.Body, "token=") {
		t.Fatalf("taken mail must not carry a link: %q\n%s", last.Subject, last.Body)
	}
	if got, _ := e.svc.Authenticate(bg, s.Token); got.User.Email != email {
		t.Fatal("the email must stay")
	}
}

// Адрес заняли между просьбой и открытием ссылки.
func TestEmailChangeLosesTheAddressBeforeConfirmation(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	tok := tokenIn(t, e.lastMailTo(newEmail))
	other := e.register(newEmail)
	if _, _, err := e.svc.ConfirmEmail(bg, other, e.meta()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tok); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("taken in between: %v", err)
	}
	if got, _ := e.svc.Authenticate(bg, s.Token); got.User.Email != email {
		t.Fatal("the email must stay")
	}
}

// Новая просьба отменяет прежнюю ссылку; пауза между письмами действует.
func TestEmailChangeSupersedesAndCoolsDown(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	p := e.freshPrincipal(s)
	first := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, first, e.meta()); err != nil {
		t.Fatal(err)
	}
	firstTok := tokenIn(t, e.lastMailTo(first))
	second := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, second, e.meta()); err != nil {
		t.Fatal(err)
	}
	if len(e.mailsTo(second)) != 0 {
		t.Fatal("within the cooldown no new link is sent")
	}
	e.clock.Advance(2 * time.Minute)
	if err := e.svc.RequestEmailChange(bg, p, second, e.meta()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, firstTok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the older link must stop working: %v", err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(second))); err != nil {
		t.Fatal(err)
	}
}

func TestEmailChangeLinkExpires(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(25 * time.Hour)
	if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(newEmail))); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired link: %v", err)
	}
}

// «Это не я»: почта возвращается, все сессии завершаются, удаление отменяется, приходит ссылка для пароля.
func TestRevertEmailChange(t *testing.T) {
	e := newEnv(t)
	_, s, oldEmail := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(newEmail))); err != nil {
		t.Fatal(err)
	}
	revert := tokenIn(t, e.lastMailTo(oldEmail))
	// Захватчик заказал удаление аккаунта (сессия ещё жива).
	if _, err := e.svc.RequestDeletion(bg, e.freshPrincipal(mustLogin(t, e, newEmail))); err != nil {
		t.Fatal(err)
	}
	_, attacker, err := e.login(newEmail, goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.svc.RevertEmailChange(bg, revert); err != nil {
		t.Fatal(err)
	}
	m := e.lastMailTo(oldEmail)
	if _, err := e.svc.Authenticate(bg, attacker.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("every session must end after a revert")
	}
	if _, _, err := e.login(newEmail, goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the attacker's address must not log in: %v", err)
	}
	u, _, err := e.login(oldEmail, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if u.DeletionDueAt != nil {
		t.Fatal("a deletion ordered after the change must be cancelled")
	}
	if !strings.Contains(m.Subject+m.Body, "возвращена") || !strings.Contains(m.Body, "/reset-password?token=") {
		t.Fatalf("revert mail: %q\n%s", m.Subject, m.Body)
	}
	if err := e.svc.ResetPassword(bg, tokenIn(t, m), "a fresh secure phrase"); err != nil {
		t.Fatalf("the reset link from the revert mail must work: %v", err)
	}
	if err := e.svc.RevertEmailChange(bg, revert); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revert link works once: %v", err)
	}
}

// Ссылка «это не я» после того, как старый адрес занял кто-то другой.
func TestRevertEmailChangeWhenOldAddressIsTaken(t *testing.T) {
	e := newEnv(t)
	_, s, oldEmail := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(newEmail))); err != nil {
		t.Fatal(err)
	}
	revert := tokenIn(t, e.lastMailTo(oldEmail))
	if _, _, err := e.svc.ConfirmEmail(bg, e.register(oldEmail), e.meta()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RevertEmailChange(bg, revert); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("old address taken: %v", err)
	}
	if _, _, err := e.login(newEmail, goodPassword); err != nil {
		t.Fatal("nothing must change when the revert fails")
	}
}

// Отмена после второй смены: каждая ссылка «это не я» возвращает свой адрес.
func TestRevertAfterTwoChanges(t *testing.T) {
	e := newEnv(t)
	_, s, first := e.confirmedUser()
	p := e.freshPrincipal(s)
	second, third := uniqueEmail(), uniqueEmail()
	for _, to := range []string{second, third} {
		if err := e.svc.RequestEmailChange(bg, p, to, e.meta()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(to))); err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(2 * time.Minute)
	}
	if err := e.svc.RevertEmailChange(bg, tokenIn(t, e.lastMailTo(first))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.login(first, goodPassword); err != nil {
		t.Fatalf("the first address must be back: %v", err)
	}
	// Ссылка со второго адреса тоже рабочая и возвращает свой, второй адрес.
	if err := e.svc.RevertEmailChange(bg, tokenIn(t, e.lastMailTo(second))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.login(second, goodPassword); err != nil {
		t.Fatalf("the second link returns its own address: %v", err)
	}
}

func mustLogin(t *testing.T, e *env, email string) Session {
	t.Helper()
	_, s, err := e.login(email, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"elena.orlova@demo.example.ru": "el***@demo.example.ru",
		"ab@x.ru":                      "a***@x.ru",
		"a@x.ru":                       "***@x.ru",
		"broken":                       "***",
	}
	for in, want := range cases {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

// Просьба сменить почту (срез 29, AUT-8): владелец текущего адреса получает письмо без нового адреса.
func TestEmailChangeRequestNotifiesTheOldAddress(t *testing.T) {
	e := newEnv(t)
	_, s, oldEmail := e.confirmedUser()
	p := e.freshPrincipal(s)
	newEmail := uniqueEmail()
	before := len(e.mailsTo(oldEmail))
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	var note string
	for _, m := range e.mailsTo(oldEmail)[before:] {
		if strings.Contains(m.Subject, "просят сменить почту") {
			note = m.Body
		}
	}
	if note == "" {
		t.Fatal("the current address got no notice")
	}
	if strings.Contains(note, newEmail) || strings.Contains(note, "token=") {
		t.Fatalf("the notice must not hold the new address or a link with a token:\n%s", note)
	}
}

// Ожидающая смена почты гаснет вместе с паролем и с «выйти на других устройствах»: просить её мог вор сеанса.
func TestPendingEmailChangeDiesWithPasswordAndSessionEvents(t *testing.T) {
	for name, act := range map[string]func(e *env, p Principal, email string){
		"change password": func(e *env, p Principal, _ string) {
			if err := e.svc.ChangePassword(bg, p, goodPassword, "another password 77"); err != nil {
				e.t.Fatal(err)
			}
		},
		"revoke other sessions": func(e *env, p Principal, _ string) {
			if err := e.svc.RevokeOtherSessions(bg, p); err != nil {
				e.t.Fatal(err)
			}
		},
		"reset password": func(e *env, _ Principal, email string) {
			if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
				e.t.Fatal(err)
			}
			var tok string
			for _, m := range e.mailsTo(email) {
				if strings.Contains(m.Body, "/reset-password?token=") {
					tok = tokenIn(e.t, m)
				}
			}
			if err := e.svc.ResetPassword(bg, tok, "another password 77"); err != nil {
				e.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			_, s, email := e.confirmedUser()
			p := e.freshPrincipal(s)
			newEmail := uniqueEmail()
			if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
				t.Fatal(err)
			}
			link := tokenIn(t, e.lastMailTo(newEmail))
			e.clock.Advance(2 * time.Minute)
			act(e, p, email)
			if _, err := e.svc.ConfirmEmailChange(bg, link); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("the pending change must be dead: %v", err)
			}
		})
	}
}

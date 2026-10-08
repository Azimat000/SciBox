package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var bg = context.Background()

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	return v.Fields
}

// ---- регистрация ----

func TestRegisterValidation(t *testing.T) {
	cases := []struct {
		name   string
		in     RegisterInput
		fields []string
	}{
		{"everything empty", RegisterInput{}, []string{"name", "email", "password", "consent"}},
		{"no consent", RegisterInput{Name: "Иван", Email: "a@b.ru", Password: goodPassword}, []string{"consent"}},
		{"bad email", RegisterInput{Name: "Иван", Email: "nope", Password: goodPassword, Consent: true}, []string{"email"}},
		{"weak password", RegisterInput{Name: "Иван", Email: "a@b.ru", Password: "short", Consent: true}, []string{"password"}},
		{"password equals email", RegisterInput{Name: "Иван", Email: "ivan.petrov@b.ru", Password: "ivan.petrov@b.ru", Consent: true}, []string{"password"}},
		{"bad name", RegisterInput{Name: "123", Email: "a@b.ru", Password: goodPassword, Consent: true}, []string{"name"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.Register(bg, tc.in, e.meta())
			fields := fieldsOf(t, err)
			if len(fields) != len(tc.fields) {
				t.Fatalf("fields = %v, want %v", fields, tc.fields)
			}
			for _, f := range tc.fields {
				if fields[f] == "" {
					t.Fatalf("no message for %q in %v", f, fields)
				}
			}
			if len(e.mails()) != 0 {
				t.Fatal("a mail must not be sent for an invalid form")
			}
		})
	}
}

func TestRegisterCreatesUnconfirmedAccountAndSendsLink(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	got, err := e.svc.Register(bg, RegisterInput{Name: "  Иван   Петров ", Email: strings.ToUpper(email), Password: goodPassword, Consent: true}, e.meta())
	if err != nil || got != email {
		t.Fatalf("Register = %q, %v; want %q", got, err, email)
	}

	var (
		name, hash, version string
		confirmed           *time.Time
		consentAt           time.Time
	)
	err = sharedPool.QueryRow(bg, `SELECT display_name, password_hash, email_confirmed_at, privacy_policy_version, privacy_consent_at FROM users WHERE email = $1`, email).
		Scan(&name, &hash, &confirmed, &version, &consentAt)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Иван Петров" || !strings.HasPrefix(hash, "$argon2id$") || confirmed != nil || version != PolicyVersion || !consentAt.Equal(e.clock.Now()) {
		t.Fatalf("stored user: %q %q %v %q %v", name, hash, confirmed, version, consentAt)
	}
	if strings.Contains(hash, goodPassword) {
		t.Fatal("password must not be stored in the clear")
	}

	m := e.lastMailTo(email)
	if !strings.Contains(m.Subject, "Подтвердите почту") || !strings.Contains(m.Subject, "SciBox") {
		t.Fatalf("subject = %q", m.Subject)
	}
	if !strings.Contains(m.Body, "http://localhost:5173/confirm-email?token=") || !strings.Contains(m.Body, "Иван Петров") {
		t.Fatalf("body = %q", m.Body)
	}
	raw := tokenIn(t, m)
	var storedHash []byte
	var expires time.Time
	if err := sharedPool.QueryRow(bg, `SELECT t.token_hash, t.expires_at FROM auth_tokens t JOIN users u ON u.id = t.user_id WHERE u.email = $1`, email).Scan(&storedHash, &expires); err != nil {
		t.Fatal(err)
	}
	if string(storedHash) == raw || string(storedHash) != string(hashToken(raw)) {
		t.Fatal("the database must keep only the hash of the token")
	}
	if !expires.Equal(e.clock.Now().Add(48 * time.Hour)) {
		t.Fatalf("expires = %v", expires)
	}
}

func TestRegisterExistingConfirmedAccountChangesNothing(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	before := len(e.mailsTo(email))

	// Тот же человек (или кто-то другой) регистрируется на занятую почту с другим паролем и именем.
	got, err := e.svc.Register(bg, RegisterInput{Name: "Самозванец", Email: email, Password: "another password 1", Consent: true}, e.meta())
	if err != nil || got != email {
		t.Fatalf("Register = %q, %v: the answer must look the same as for a new address", got, err)
	}
	if _, _, err := e.login(email, goodPassword); err != nil {
		t.Fatalf("the old password must still work: %v", err)
	}
	if _, _, err := e.login(email, "another password 1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the new password must not work: %v", err)
	}
	ms := e.mailsTo(email)
	if len(ms) != before+1 {
		t.Fatalf("want one extra mail, got %d", len(ms)-before)
	}
	last := ms[len(ms)-1]
	if !strings.Contains(last.Subject, "уже есть") || !strings.Contains(last.Body, "/login") || !strings.Contains(last.Body, "/forgot-password") || strings.Contains(last.Body, "token=") {
		t.Fatalf("already-registered mail: %q / %q", last.Subject, last.Body)
	}
	if !strings.Contains(last.Body, "Иван Петров") {
		t.Fatal("the mail must address the real owner, not the impostor")
	}
}

func TestRegisterExistingUnconfirmedLatestRegistrationWins(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	first := e.register(email)

	// Сразу же второй раз: пауза между письмами ещё идёт, письмо не уходит, но пароль заменяется.
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Пётр", Email: email, Password: "second password 22", Consent: true}, e.meta()); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mailsTo(email)); n != 1 {
		t.Fatalf("mails during cooldown = %d, want 1", n)
	}
	// Подтверждаем по первой ссылке: она ещё жива, но пароль уже второй.
	if _, _, err := e.svc.ConfirmEmail(bg, first, e.meta()); err != nil {
		t.Fatalf("first link must still work during cooldown: %v", err)
	}
	if _, _, err := e.login(email, goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first password must be replaced: %v", err)
	}
	u, _, err := e.login(email, "second password 22")
	if err != nil || u.Name != "Пётр" {
		t.Fatalf("second registration must win: %v %+v", err, u)
	}
}

func TestRegisterExistingUnconfirmedAfterCooldownReplacesToken(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	first := e.register(email)
	e.clock.Advance(2 * time.Minute)
	e.register(email)
	second := tokenIn(t, e.lastMailTo(email))
	if first == second {
		t.Fatal("a new token must be issued")
	}
	if _, _, err := e.svc.ConfirmEmail(bg, first, e.meta()); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old link must stop working: %v", err)
	}
	if _, _, err := e.svc.ConfirmEmail(bg, second, e.meta()); err != nil {
		t.Fatalf("new link must work: %v", err)
	}
}

func TestRegisterEmailIsCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: strings.ToUpper(email), Password: goodPassword, Consent: true}, e.meta()); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM users WHERE lower(email) = $1`, email); n != 1 {
		t.Fatalf("users with this address = %d, want 1", n)
	}
}

func TestRegisterMailRateLimitPerIP(t *testing.T) {
	e := newEnv(t)
	meta := e.meta()
	for i := 0; i < e.svc.cfg.Limits.MailIP.Max; i++ {
		if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	_, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta)
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != time.Hour {
		t.Fatalf("err = %v, want rate limit for one hour", err)
	}
	// Другой адрес не пострадал.
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}, e.meta()); err != nil {
		t.Fatalf("another IP must be allowed: %v", err)
	}
	// Через час снова можно.
	e.clock.Advance(time.Hour + time.Second)
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}, meta); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	// Ограничение общее для всех «писем по запросу».
	m2 := e.meta()
	for i := 0; i < e.svc.cfg.Limits.MailIP.Max; i++ {
		if err := e.svc.RequestPasswordReset(bg, uniqueEmail(), m2); err != nil {
			t.Fatal(err)
		}
	}
	for name, call := range map[string]func() error{
		"forgot": func() error { return e.svc.RequestPasswordReset(bg, uniqueEmail(), m2) },
		"resend": func() error { return e.svc.ResendConfirmation(bg, uniqueEmail(), m2) },
		"as well": func() error {
			_, err := e.svc.Register(bg, RegisterInput{Name: "И", Email: uniqueEmail(), Password: goodPassword, Consent: true}, m2)
			return err
		},
	} {
		if err := call(); !errors.As(err, &rl) {
			t.Fatalf("%s: err = %v, want rate limit", name, err)
		}
	}
}

func TestMailFailureIsLoggedNotReturned(t *testing.T) {
	e := newEnv(t)
	e.mailer.Err = errors.New("smtp is down")
	email := uniqueEmail()
	if _, err := e.svc.Register(bg, RegisterInput{Name: "Иван", Email: email, Password: goodPassword, Consent: true}, e.meta()); err != nil {
		t.Fatalf("registration must not depend on the mail server: %v", err)
	}
	e.svc.Flush()
	logs := e.logs.String()
	if !strings.Contains(logs, "send mail") || !strings.Contains(logs, "smtp is down") {
		t.Fatalf("failure must be logged: %q", logs)
	}
	if strings.Contains(logs, email) {
		t.Fatal("the recipient address must not go to the log")
	}
}

// ---- подтверждение почты ----

func TestConfirmEmail(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	tok := e.register(email)
	u, sess, err := e.svc.ConfirmEmail(bg, tok, e.meta())
	if err != nil {
		t.Fatal(err)
	}
	if !u.EmailConfirmed || u.Email != email || u.Name != "Иван Петров" || sess.Token == "" || !sess.ExpiresAt.Equal(e.clock.Now().Add(30*24*time.Hour)) {
		t.Fatalf("user %+v session %+v", u, sess)
	}
	p, err := e.svc.Authenticate(bg, sess.Token)
	if err != nil || p.User.ID != u.ID {
		t.Fatalf("the new session must be valid: %v", err)
	}
}

func TestConfirmEmailTokenRules(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func() string{
		"unknown": func() string { return "no-such-token" },
		"empty":   func() string { return "" },
		"used": func() string {
			tok := e.register(uniqueEmail())
			if _, _, err := e.svc.ConfirmEmail(bg, tok, e.meta()); err != nil {
				t.Fatal(err)
			}
			return tok
		},
		"expired": func() string {
			tok := e.register(uniqueEmail())
			e.clock.Advance(48*time.Hour + time.Second)
			return tok
		},
		"password reset token is not a confirmation token": func() string {
			u, _, email := e.confirmedUser()
			_ = u
			e.clock.Advance(2 * time.Minute)
			if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
				t.Fatal(err)
			}
			return tokenIn(t, e.lastMailTo(email))
		},
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			tok := token()
			if _, _, err := e.svc.ConfirmEmail(bg, tok, e.meta()); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestConfirmEmailLinkWorksOnlyOnceEvenWhenRacing(t *testing.T) {
	e := newEnv(t)
	tok := e.register(uniqueEmail())
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := e.svc.ConfirmEmail(bg, tok, e.meta()); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1", successes)
	}
}

func TestResendConfirmation(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	first := e.register(email)

	// В паузу письмо не уходит, но ответ тот же.
	if err := e.svc.ResendConfirmation(bg, email, e.meta()); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mailsTo(email)); n != 1 {
		t.Fatalf("mails = %d, want 1 during cooldown", n)
	}
	e.clock.Advance(2 * time.Minute)
	if err := e.svc.ResendConfirmation(bg, email, e.meta()); err != nil {
		t.Fatal(err)
	}
	if n := len(e.mailsTo(email)); n != 2 {
		t.Fatalf("mails = %d, want 2", n)
	}
	second := tokenIn(t, e.lastMailTo(email))
	if _, _, err := e.svc.ConfirmEmail(bg, first, e.meta()); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the first link must be superseded: %v", err)
	}
	if _, _, err := e.svc.ConfirmEmail(bg, second, e.meta()); err != nil {
		t.Fatal(err)
	}
}

func TestResendConfirmationSilentCases(t *testing.T) {
	e := newEnv(t)
	_, _, confirmed := e.confirmedUser()
	before := len(e.mails())
	for name, email := range map[string]string{
		"already confirmed": confirmed,
		"unknown address":   uniqueEmail(),
		"not an address":    "nonsense",
		"empty":             "",
	} {
		if err := e.svc.ResendConfirmation(bg, email, e.meta()); err != nil {
			t.Fatalf("%s: %v (the answer must not reveal anything)", name, err)
		}
	}
	if n := len(e.mails()); n != before {
		t.Fatalf("no mail expected, got %d new", n-before)
	}
}

// ---- вход ----

func TestLoginTable(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	unconfirmed := uniqueEmail()
	e.register(unconfirmed)

	cases := []struct {
		name, email, password string
		want                  error
	}{
		{"right credentials", email, goodPassword, nil},
		{"email in other case", strings.ToUpper(email), goodPassword, nil},
		{"email with spaces", "  " + email + "  ", goodPassword, nil},
		{"wrong password", email, goodPassword + "x", ErrInvalidCredentials},
		{"empty password", email, "", ErrInvalidCredentials},
		{"unknown address", uniqueEmail(), goodPassword, ErrInvalidCredentials},
		{"empty address", "", goodPassword, ErrInvalidCredentials},
		{"not an address", "nonsense", goodPassword, ErrInvalidCredentials},
		{"unconfirmed, right password", unconfirmed, goodPassword, ErrEmailNotConfirmed},
		{"unconfirmed, wrong password", unconfirmed, "wrong password!!", ErrInvalidCredentials},
		{"absurdly long password", email, strings.Repeat("x", 5000), ErrInvalidCredentials},
		{"absurdly long address", strings.Repeat("a", 300) + "@b.ru", goodPassword, ErrInvalidCredentials},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, s, err := e.login(tc.email, tc.password)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (s.Token == "" || u.Email != email) {
				t.Fatalf("expected a session for %s, got %+v %+v", email, u, s)
			}
			if tc.want != nil && (s.Token != "" || u.ID.String() != "00000000-0000-0000-0000-000000000000") {
				t.Fatal("a failed login must not return a session or a user")
			}
		})
	}
}

func TestLoginRecordsSessionDetails(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	meta := Meta{IP: "198.51.100.7", UserAgent: strings.Repeat("я", 500)}
	_, s, err := e.svc.Login(bg, email, goodPassword, meta)
	if err != nil {
		t.Fatal(err)
	}
	var ip, ua string
	if err := sharedPool.QueryRow(bg, `SELECT ip, user_agent FROM sessions WHERE token_hash = $1`, hashToken(s.Token)).Scan(&ip, &ua); err != nil {
		t.Fatal(err)
	}
	if ip != "198.51.100.7" || len([]rune(ua)) != 300 {
		t.Fatalf("ip=%q ua length=%d", ip, len([]rune(ua)))
	}
	if countRows(t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, []byte(s.Token)) != 0 {
		t.Fatal("the raw token must not be stored")
	}
}

func TestLoginThrottlePerEmail(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	lim := e.svc.cfg.Limits.LoginEmail
	for i := 0; i < lim.Max; i++ {
		if _, _, err := e.login(email, "wrong password!!"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		e.clock.Advance(time.Minute)
	}
	// Дальше даже верный пароль не пускает: перебор не должен продолжаться.
	_, _, err := e.login(email, goodPassword)
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want rate limit", err)
	}
	// Первая неудача была 5 минут назад: ждать ещё 10 минут.
	if rl.RetryAfter != 10*time.Minute {
		t.Fatalf("RetryAfter = %v, want 10m", rl.RetryAfter)
	}
	// Другая почта с того же адреса не заблокирована.
	_, _, other := e.confirmedUser()
	if _, _, err := e.login(other, goodPassword); err != nil {
		t.Fatalf("other account: %v", err)
	}
	// Окно прошло, и верный пароль снова работает.
	e.clock.Advance(11 * time.Minute)
	if _, _, err := e.login(email, goodPassword); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestLoginThrottleAlsoCoversUnknownAddresses(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail() // не зарегистрирован
	for i := 0; i < e.svc.cfg.Limits.LoginEmail.Max; i++ {
		_, _, _ = e.login(email, goodPassword)
	}
	if _, _, err := e.login(email, goodPassword); !errors.As(err, new(*RateLimitedError)) {
		t.Fatalf("err = %v: unknown addresses must be throttled exactly like real ones", err)
	}
}

func TestLoginThrottlePerIP(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	meta := e.meta()
	// Один адрес перебирает много разных почт: на каждую почту попыток мало, но с адреса их много.
	for i := 0; i < e.svc.cfg.Limits.LoginIP.Max; i++ {
		if _, _, err := e.svc.Login(bg, uniqueEmail(), goodPassword, meta); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, _, err := e.svc.Login(bg, email, goodPassword, meta); !errors.As(err, new(*RateLimitedError)) {
		t.Fatalf("err = %v, want rate limit from this IP", err)
	}
	if _, _, err := e.svc.Login(bg, email, goodPassword, e.meta()); err != nil {
		t.Fatalf("a different IP must be allowed: %v", err)
	}
}

func TestSuccessfulLoginResetsFailureCounter(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	for round := 0; round < 3; round++ {
		for i := 0; i < e.svc.cfg.Limits.LoginEmail.Max-1; i++ {
			_, _, _ = e.login(email, "wrong password!!")
		}
		if _, _, err := e.login(email, goodPassword); err != nil {
			t.Fatalf("round %d: %v (a success must reset the counter)", round, err)
		}
	}
}

func TestLoginWithCorruptStoredHashIsAnInternalError(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	if _, err := sharedPool.Exec(bg, `UPDATE users SET password_hash = 'garbage' WHERE email = $1`, email); err != nil {
		t.Fatal(err)
	}
	_, _, err := e.login(email, goodPassword)
	if !errors.Is(err, ErrBadHash) || errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrBadHash (a server problem, not the user's)", err)
	}
}

// ---- сессии ----

func TestAuthenticateLifecycle(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	u, s, err := e.login(email, goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	p, err := e.svc.Authenticate(bg, s.Token)
	if err != nil || p.User.ID != u.ID || p.Refreshed || !p.ExpiresAt.Equal(s.ExpiresAt) || !p.User.EmailConfirmed {
		t.Fatalf("fresh session: %+v %v", p, err)
	}

	e.clock.Advance(30 * time.Minute)
	p, _ = e.svc.Authenticate(bg, s.Token)
	if p.Refreshed {
		t.Fatal("the session must not be refreshed more often than once per refresh interval")
	}

	e.clock.Advance(40 * time.Minute) // всего 70 минут
	p, err = e.svc.Authenticate(bg, s.Token)
	if err != nil || !p.Refreshed || !p.ExpiresAt.Equal(e.clock.Now().Add(30*24*time.Hour)) {
		t.Fatalf("session must be extended: %+v %v", p, err)
	}

	// Пока человек заходит, сессия живёт дольше исходных 30 дней.
	e.clock.Advance(29 * 24 * time.Hour)
	if _, err := e.svc.Authenticate(bg, s.Token); err != nil {
		t.Fatalf("an active session must outlive the original expiry: %v", err)
	}

	// Бросили на 31 день: сессии нет, а строка в базе убрана.
	e.clock.Advance(31 * 24 * time.Hour)
	if _, err := e.svc.Authenticate(bg, s.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session: %v", err)
	}
	if countRows(t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, hashToken(s.Token)) != 0 {
		t.Fatal("the expired session must be deleted")
	}
}

func TestAuthenticateRejectsUnknownAndEmptyTokens(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "nonsense", strings.Repeat("A", 43)} {
		if _, err := e.svc.Authenticate(bg, tok); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q: %v", tok, err)
		}
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	_, s1, _ := e.login(email, goodPassword)
	_, s2, _ := e.login(email, goodPassword)
	if err := e.svc.Logout(bg, s1.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, s1.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("the signed-out session must be gone")
	}
	if _, err := e.svc.Authenticate(bg, s2.Token); err != nil {
		t.Fatalf("other sessions must survive: %v", err)
	}
	// Повторный и пустой выход ничего не ломают.
	for _, tok := range []string{s1.Token, "", "nonsense"} {
		if err := e.svc.Logout(bg, tok); err != nil {
			t.Fatalf("Logout(%q): %v", tok, err)
		}
	}
}

func TestRevokeOtherSessions(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	_, s1, _ := e.login(email, goodPassword)
	_, s2, _ := e.login(email, goodPassword)
	_, otherUserSession, _ := e.confirmedUser()
	p1, _ := e.svc.Authenticate(bg, s1.Token)
	if err := e.svc.RevokeOtherSessions(bg, p1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, s1.Token); err != nil {
		t.Fatalf("the current session must stay: %v", err)
	}
	if _, err := e.svc.Authenticate(bg, s2.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("other sessions must be revoked")
	}
	if _, err := e.svc.Authenticate(bg, otherUserSession.Token); err != nil {
		t.Fatalf("another person's session must not be touched: %v", err)
	}
}

// ---- сброс пароля ----

func TestPasswordResetFullCycle(t *testing.T) {
	e := newEnv(t)
	_, sess, email := e.confirmedUser()
	e.clock.Advance(2 * time.Minute)
	if err := e.svc.RequestPasswordReset(bg, strings.ToUpper(email), e.meta()); err != nil {
		t.Fatal(err)
	}
	m := e.lastMailTo(email)
	if !strings.Contains(m.Subject, "Сброс пароля") || !strings.Contains(m.Body, "/reset-password?token=") {
		t.Fatalf("mail: %q / %q", m.Subject, m.Body)
	}
	tok := tokenIn(t, m)

	if err := e.svc.ResetPassword(bg, tok, "brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, sess.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("all sessions must be revoked after a reset")
	}
	if _, _, err := e.login(email, goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password must stop working: %v", err)
	}
	if _, _, err := e.login(email, "brand new passphrase"); err != nil {
		t.Fatalf("new password must work: %v", err)
	}
	if last := e.lastMailTo(email); !strings.Contains(last.Subject, "изменён") {
		t.Fatalf("the owner must be told about the change: %q", last.Subject)
	}
	// Ссылка одноразовая.
	if err := e.svc.ResetPassword(bg, tok, "yet another passphrase"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reuse: %v", err)
	}
}

func TestPasswordResetWeakPasswordDoesNotBurnTheLink(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	tok := tokenIn(t, e.lastMailTo(email))

	for name, pw := range map[string]string{"short": "abc", "common": "1234567890", "email": email} {
		fields := fieldsOf(t, e.svc.ResetPassword(bg, tok, pw))
		if fields["password"] == "" {
			t.Fatalf("%s: no password message in %v", name, fields)
		}
	}
	if err := e.svc.ResetPassword(bg, tok, "a good new passphrase"); err != nil {
		t.Fatalf("the link must survive rejected passwords: %v", err)
	}
}

func TestPasswordResetTokenRules(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	first := tokenIn(t, e.lastMailTo(email))
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	second := tokenIn(t, e.lastMailTo(email))
	confirmTok := e.register(uniqueEmail())

	for name, tok := range map[string]string{
		"superseded":                   first,
		"unknown":                      "nope",
		"empty":                        "",
		"confirmation token not valid": confirmTok,
	} {
		if err := e.svc.ResetPassword(bg, tok, "a good new passphrase"); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	e.clock.Advance(time.Hour + time.Second)
	if err := e.svc.ResetPassword(bg, second, "a good new passphrase"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired: %v", err)
	}
}

func TestPasswordResetUnlocksAndConfirmsTheAccount(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	for i := 0; i < e.svc.cfg.Limits.LoginEmail.Max; i++ {
		_, _, _ = e.login(email, "wrong password!!")
	}
	if _, _, err := e.login(email, goodPassword); !errors.As(err, new(*RateLimitedError)) {
		t.Fatalf("setup: %v", err)
	}
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	if err := e.svc.ResetPassword(bg, tokenIn(t, e.lastMailTo(email)), "a good new passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.login(email, "a good new passphrase"); err != nil {
		t.Fatalf("a reset must also lift the lock: %v", err)
	}

	// Человек так и не подтвердил почту, но получил письмо для сброса и прошёл по ссылке: почта подтверждена.
	unconfirmed := uniqueEmail()
	e.register(unconfirmed)
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, unconfirmed, e.meta())
	if err := e.svc.ResetPassword(bg, tokenIn(t, e.lastMailTo(unconfirmed)), "a good new passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.login(unconfirmed, "a good new passphrase"); err != nil {
		t.Fatalf("reset link proves ownership of the mailbox: %v", err)
	}
}

func TestPasswordResetRequestSilentCases(t *testing.T) {
	e := newEnv(t)
	_, _, known := e.confirmedUser()
	e.clock.Advance(2 * time.Minute)
	before := len(e.mails())
	for name, email := range map[string]string{"unknown": uniqueEmail(), "garbage": "nonsense", "empty": ""} {
		if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
			t.Fatalf("%s: %v (the answer must not reveal anything)", name, err)
		}
	}
	if n := len(e.mails()); n != before {
		t.Fatalf("no mail expected for unknown addresses, got %d", n-before)
	}
	// Известный адрес: письмо уходит, но в паузу повторное не уходит.
	_ = e.svc.RequestPasswordReset(bg, known, e.meta())
	_ = e.svc.RequestPasswordReset(bg, known, e.meta())
	if n := len(e.mails()) - before; n != 1 {
		t.Fatalf("mails to a known address = %d, want 1 (cooldown)", n)
	}
}

// ---- настройки ----

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	_, _, email := e.confirmedUser()
	_, s1, _ := e.login(email, goodPassword)
	_, s2, _ := e.login(email, goodPassword)
	p, _ := e.svc.Authenticate(bg, s1.Token)

	if err := e.svc.ChangePassword(bg, p, goodPassword, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, s1.Token); err != nil {
		t.Fatalf("the session that changed the password must stay: %v", err)
	}
	if _, err := e.svc.Authenticate(bg, s2.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("other sessions must end after a password change")
	}
	if _, _, err := e.login(email, goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password must stop working")
	}
	if _, _, err := e.login(email, "a brand new passphrase"); err != nil {
		t.Fatal(err)
	}
	if last := e.lastMailTo(email); !strings.Contains(last.Subject, "изменён") {
		t.Fatalf("the owner must be notified: %q", last.Subject)
	}
}

func TestChangePasswordValidation(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)

	cases := []struct {
		name, current, next string
		fields              []string
	}{
		{"wrong current", "wrong password!!", "a brand new passphrase", []string{"current_password"}},
		{"weak new", goodPassword, "short", []string{"new_password"}},
		{"new equals email", goodPassword, email, []string{"new_password"}},
		{"new equals current", goodPassword, goodPassword, []string{"new_password"}},
		{"both wrong", "wrong password!!", "short", []string{"current_password", "new_password"}},
		{"absurd current", strings.Repeat("x", 5000), "a brand new passphrase", []string{"current_password"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Неверные «текущие пароли» тоже считаются, поэтому счётчик чистим между случаями.
			if _, err := sharedPool.Exec(bg, `DELETE FROM rate_events WHERE kind = $1`, kindPasswordUser); err != nil {
				t.Fatal(err)
			}
			fields := fieldsOf(t, e.svc.ChangePassword(bg, p, tc.current, tc.next))
			if len(fields) != len(tc.fields) {
				t.Fatalf("fields = %v, want %v", fields, tc.fields)
			}
			for _, f := range tc.fields {
				if fields[f] == "" {
					t.Fatalf("no message for %q: %v", f, fields)
				}
			}
		})
	}
	if _, _, err := e.login(email, goodPassword); err != nil {
		t.Fatalf("a rejected change must keep the old password: %v", err)
	}
}

func TestChangePasswordThrottlesGuessing(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)
	for i := 0; i < e.svc.cfg.Limits.PasswordUser.Max; i++ {
		_ = e.svc.ChangePassword(bg, p, "wrong password!!", "a brand new passphrase")
	}
	// Даже с верным текущим паролем: человек с украденной сессией не должен подбирать пароль без конца.
	err := e.svc.ChangePassword(bg, p, goodPassword, "a brand new passphrase")
	if !errors.As(err, new(*RateLimitedError)) {
		t.Fatalf("err = %v, want rate limit", err)
	}
	e.clock.Advance(16 * time.Minute)
	if err := e.svc.ChangePassword(bg, p, goodPassword, "a brand new passphrase"); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestChangePasswordWithCorruptStoredHashIsAnInternalError(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)
	if _, err := sharedPool.Exec(bg, `UPDATE users SET password_hash = 'garbage' WHERE email = $1`, email); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ChangePassword(bg, p, goodPassword, "a brand new passphrase"); !errors.Is(err, ErrBadHash) {
		t.Fatalf("err = %v, want ErrBadHash", err)
	}
}

func TestUpdateName(t *testing.T) {
	e := newEnv(t)
	u, s, _ := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)
	got, err := e.svc.UpdateName(bg, p, "  Анна   Сидорова ")
	if err != nil || got.Name != "Анна Сидорова" || got.ID != u.ID {
		t.Fatalf("UpdateName = %+v, %v", got, err)
	}
	p2, _ := e.svc.Authenticate(bg, s.Token)
	if p2.User.Name != "Анна Сидорова" {
		t.Fatalf("name must be stored: %q", p2.User.Name)
	}
	for _, bad := range []string{"", "   ", "123", strings.Repeat("я", 121)} {
		if fields := fieldsOf(t, func() error { _, err := e.svc.UpdateName(bg, p, bad); return err }()); fields["name"] == "" {
			t.Fatalf("%q: %v", bad, fields)
		}
	}
}

// ---- уборка ----

func TestCleanupRemovesOnlyStaleRows(t *testing.T) {
	e := newEnv(t)
	_, oldSession, _ := e.confirmedUser()
	meta := e.meta()
	_, _, _ = e.svc.Login(bg, uniqueEmail(), goodPassword, meta) // оставляет событие для ограничения частоты

	e.clock.Advance(31 * 24 * time.Hour) // всё созданное выше устарело
	_, freshSession, _ := e.confirmedUser()
	freshEvent := e.meta()
	_, _, _ = e.svc.Login(bg, uniqueEmail(), goodPassword, freshEvent)

	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(bg, freshSession.Token); err != nil {
		t.Fatalf("a fresh session must survive cleanup: %v", err)
	}
	if countRows(t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, hashToken(oldSession.Token)) != 0 {
		t.Fatal("an expired session must be removed")
	}
	if countRows(t, `SELECT count(*) FROM rate_events WHERE key = $1`, meta.IP) != 0 {
		t.Fatal("old rate events must be removed")
	}
	if countRows(t, `SELECT count(*) FROM rate_events WHERE key = $1`, freshEvent.IP) == 0 {
		t.Fatal("fresh rate events must stay")
	}
	if countRows(t, `SELECT count(*) FROM auth_tokens WHERE expires_at < $1`, e.clock.Now().Add(-24*time.Hour)) != 0 {
		t.Fatal("long expired tokens must be removed")
	}
}

func TestRunCleanupStopsWithContextAndLogsFailures(t *testing.T) {
	e := newEnv(t)
	_, s, _ := e.confirmedUser()
	e.clock.Advance(31 * 24 * time.Hour)

	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { e.svc.RunCleanup(ctx, 10*time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for countRows(t, `SELECT count(*) FROM sessions WHERE token_hash = $1`, hashToken(s.Token)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("RunCleanup did not clean the expired session")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunCleanup must stop when its context is cancelled")
	}

	// Если база недоступна, ошибка пишется в журнал, а цикл живёт дальше.
	broken, err := pgxpool.New(bg, sharedPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	e.svc = newService(broken, e.mailer, e.svc.cfg, e.svc.logger)
	ctx2, cancel2 := context.WithCancel(bg)
	defer cancel2()
	go e.svc.RunCleanup(ctx2, 10*time.Millisecond)
	deadline = time.Now().Add(5 * time.Second)
	for !strings.Contains(e.logs.String(), "auth cleanup") {
		if time.Now().After(deadline) {
			t.Fatal("cleanup failure must be logged")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- перегруженный хеширование ----

func TestHashingTimeoutIsReportedNotSwallowed(t *testing.T) {
	e := newEnv(t)
	_, s, email := e.confirmedUser()
	p, _ := e.svc.Authenticate(bg, s.Token)
	e.clock.Advance(2 * time.Minute)
	_ = e.svc.RequestPasswordReset(bg, email, e.meta())
	resetTok := tokenIn(t, e.lastMailTo(email))

	// Все места для хеширования заняты, ждать некогда.
	for i := 0; i < cap(e.svc.hasher.sem); i++ {
		e.svc.hasher.sem <- struct{}{}
	}
	// Каждый вызов получает свой срок: иначе первый «съест» время остальных, и до хеширования они не дойдут.
	var ctx context.Context
	calls := map[string]func() error{
		"register": func() error {
			_, err := e.svc.Register(ctx, RegisterInput{Name: "Иван", Email: uniqueEmail(), Password: goodPassword, Consent: true}, e.meta())
			return err
		},
		"login": func() error { _, _, err := e.svc.Login(ctx, email, goodPassword, e.meta()); return err },
		"reset": func() error { return e.svc.ResetPassword(ctx, resetTok, "a good new passphrase") },
		"change": func() error {
			return e.svc.ChangePassword(ctx, p, goodPassword, "a good new passphrase")
		},
		"change, wrong current": func() error {
			return e.svc.ChangePassword(ctx, p, "wrong password!!", "a good new passphrase")
		},
	}
	for name, call := range calls {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(bg, 300*time.Millisecond) //nolint:fatcontext // намеренно: замыкания выше читают ctx, у каждого вызова свой срок от bg
		if err := call(); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: err = %v, want deadline exceeded", name, err)
		}
		cancel()
	}
	for i := 0; i < cap(e.svc.hasher.sem); i++ {
		<-e.svc.hasher.sem
	}
}

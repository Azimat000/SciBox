package auth

import (
	"strings"
	"testing"
	"time"
)

func TestParseUserAgent(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want Device
	}{
		{"chrome on windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
			Device{Browser: "chrome", OS: "windows"}},
		{"safari on mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Safari/605.1.15",
			Device{Browser: "safari", OS: "macos"}},
		{"safari on iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1",
			Device{Browser: "safari", OS: "ios", Mobile: true}},
		{"chrome on iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0 Mobile/15E148 Safari/604.1",
			Device{Browser: "chrome", OS: "ios", Mobile: true}},
		{"firefox on linux", "Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0",
			Device{Browser: "firefox", OS: "linux"}},
		{"firefox on iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/143.0 Mobile/15E148 Safari/605.1.15",
			Device{Browser: "firefox", OS: "ios", Mobile: true}},
		{"edge on windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0",
			Device{Browser: "edge", OS: "windows"}},
		{"yandex on android", "Mozilla/5.0 (Linux; Android 15; SM-S921B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 YaBrowser/25.8.1.100 Mobile Safari/537.36",
			Device{Browser: "yandex", OS: "android", Mobile: true}},
		{"opera on windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 OPR/124.0.0.0",
			Device{Browser: "opera", OS: "windows"}},
		{"samsung on android tablet", "Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/28.0 Chrome/130.0.0.0 Safari/537.36",
			Device{Browser: "samsung", OS: "android"}},
		{"chromebook", "Mozilla/5.0 (X11; CrOS x86_64 16181.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36",
			Device{Browser: "chrome", OS: "chromeos"}},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Safari/604.1",
			Device{Browser: "safari", OS: "ios"}},
		{"command line client", "curl/8.10.1", Device{}},
		{"empty", "", Device{}},
		{"test agent", "test-agent", Device{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseUserAgent(tc.ua); got != tc.want {
				t.Fatalf("ParseUserAgent = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := []struct {
		d    Device
		want string
	}{
		{Device{Browser: "chrome", OS: "windows"}, "Chrome, Windows"},
		{Device{Browser: "yandex", OS: "android", Mobile: true}, "Яндекс Браузер, Android"},
		{Device{Browser: "firefox"}, "Firefox"},
		{Device{OS: "linux"}, "браузер на Linux"},
		{Device{}, "неизвестный браузер"},
	}
	for _, tc := range cases {
		if got := tc.d.Label(); got != tc.want {
			t.Errorf("%+v.Label() = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// Письмо «вход с нового устройства» (T-13): первый вход из браузера — письмо, дальше из того же браузера — нет.
func TestLoginFromNewDeviceSendsMail(t *testing.T) {
	e := newEnv(t)
	u, confirmSess, email := e.confirmedUser()
	if confirmSess.Device == "" {
		t.Fatal("confirming the email must remember the browser")
	}
	if e.gotMail(email, "нового устройства") {
		t.Fatal("confirming the email from this browser is not a new device")
	}

	// Тот же браузер (его cookie устройства) — письма нет.
	m := e.meta()
	m.Device = confirmSess.Device
	_, s1, err := e.svc.Login(bg, email, goodPassword, m)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Device != confirmSess.Device || e.gotMail(email, "нового устройства") {
		t.Fatalf("known browser: device %q, mail sent: %v", s1.Device, e.gotMail(email, "нового устройства"))
	}

	// Другой браузер без cookie — письмо с браузером, временем и адресом.
	other := e.meta()
	other.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	_, s2, err := e.svc.Login(bg, email, goodPassword, other)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Device == "" || s2.Device == confirmSess.Device {
		t.Fatalf("a new browser must get its own device token, got %q", s2.Device)
	}
	mail := e.lastMailTo(email)
	for _, want := range []string{"нового устройства", "Chrome, Windows", other.IP, "2 октября 2026, 15:00 (МСК)", "/account/security", u.Name} {
		if !strings.Contains(mail.Subject+mail.Body, want) {
			t.Fatalf("new device mail lacks %q:\n%s\n%s", want, mail.Subject, mail.Body)
		}
	}

	// Этот браузер теперь знакомый.
	before := len(e.mailsTo(email))
	other.Device = s2.Device
	if _, _, err := e.svc.Login(bg, email, goodPassword, other); err != nil {
		t.Fatal(err)
	}
	if len(e.mailsTo(email)) != before {
		t.Fatal("the second login from the same browser must not send a mail")
	}
}

// Чужая или испорченная cookie устройства не принимается как есть: браузер получает свой токен.
func TestDeviceTokenFromAnotherPersonOrGarbage(t *testing.T) {
	e := newEnv(t)
	_, sA, _ := e.confirmedUser()
	_, _, emailB := e.confirmedUser()

	// Браузер человека A, в него входит B: для B это новый браузер (письмо), токен тот же — браузер один.
	m := e.meta()
	m.Device = sA.Device
	_, s, err := e.svc.Login(bg, emailB, goodPassword, m)
	if err != nil {
		t.Fatal(err)
	}
	if s.Device != sA.Device || !e.gotMail(emailB, "нового устройства") {
		t.Fatalf("shared browser: token kept %v, mail %v", s.Device == sA.Device, e.gotMail(emailB, "нового устройства"))
	}

	// Слишком длинная строка в cookie заменяется новым токеном.
	m = e.meta()
	m.Device = strings.Repeat("x", maxDeviceToken+1)
	if _, s, err = e.svc.Login(bg, emailB, goodPassword, m); err != nil {
		t.Fatal(err)
	}
	if s.Device == m.Device || len(s.Device) > maxDeviceToken {
		t.Fatalf("garbage device token must be replaced, got %d chars", len(s.Device))
	}
}

func TestCleanupForgetsOldDevices(t *testing.T) {
	e := newEnv(t)
	u, _, _ := e.confirmedUser()
	if n := countRows(t, "SELECT count(*) FROM known_devices WHERE user_id = $1", u.ID); n != 1 {
		t.Fatalf("known devices after confirmation: %d", n)
	}
	e.clock.Advance(DefaultDeviceTTL - time.Hour)
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, "SELECT count(*) FROM known_devices WHERE user_id = $1", u.ID); n != 1 {
		t.Fatal("a device seen within the cookie lifetime must stay")
	}
	e.clock.Advance(2 * time.Hour)
	if err := e.svc.Cleanup(bg); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, "SELECT count(*) FROM known_devices WHERE user_id = $1", u.ID); n != 0 {
		t.Fatal("a device not seen for longer than the cookie lifetime must be forgotten")
	}
}

package auth

import (
	"strings"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	long := strings.Repeat("a", 250) + "@b.ru"
	cases := []struct {
		in, want, msg string
	}{
		{"ivan@example.ru", "ivan@example.ru", ""},
		{"  Ivan.Petrov@Example.RU  ", "ivan.petrov@example.ru", ""},
		{"a+tag@sub.example.ru", "a+tag@sub.example.ru", ""},
		{"", "", msgEmailRequired},
		{"   ", "", msgEmailRequired},
		{"ivan", "", msgEmailInvalid},
		{"ivan@", "", msgEmailInvalid},
		{"@example.ru", "", msgEmailInvalid},
		{"ivan@example", "", msgEmailInvalid},
		{"ivan@.ru", "", msgEmailInvalid},
		{"ivan@example.", "", msgEmailInvalid},
		{"ivan@exa..mple.ru", "", msgEmailInvalid},
		{"iv an@example.ru", "", msgEmailInvalid},
		{"Ivan <ivan@example.ru>", "", msgEmailInvalid},
		{"a@b.ru, c@d.ru", "", msgEmailInvalid},
		{"\"ivan\"@example.ru", "", msgEmailInvalid},
		{long, "", msgEmailInvalid},
		{"иван@пример.рф", "иван@пример.рф", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, msg := NormalizeEmail(tc.in)
			if got != tc.want || msg != tc.msg {
				t.Fatalf("NormalizeEmail(%q) = %q, %q; want %q, %q", tc.in, got, msg, tc.want, tc.msg)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in, want, msg string
	}{
		{"Иван Петров", "Иван Петров", ""},
		{"  Иван   Петров\t", "Иван Петров", ""},
		{"Zhang Wei", "Zhang Wei", ""},
		{"李", "李", ""},
		{"", "", msgNameRequired},
		{"   \t", "", msgNameRequired},
		{"1234", "", msgNameInvalid},
		{"Иван\x00", "", msgNameInvalid},
		{strings.Repeat("Я", 120), strings.Repeat("Я", 120), ""},
		{strings.Repeat("Я", 121), "", msgNameTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, msg := NormalizeName(tc.in)
			if got != tc.want || msg != tc.msg {
				t.Fatalf("NormalizeName(%q) = %q, %q; want %q, %q", tc.in, got, msg, tc.want, tc.msg)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name, password, email, msg string
	}{
		{"fine long phrase", "correct horse battery", "ivan@example.ru", ""},
		{"fine cyrillic", "длинный русский пароль", "ivan@example.ru", ""},
		{"fine without email", "correct horse battery", "", ""},
		{"exactly 10", "abcde12345", "ivan@example.ru", ""},
		{"empty", "", "ivan@example.ru", msgPasswordRequired},
		{"9 characters", "abcde1234", "ivan@example.ru", msgPasswordShort},
		{"10 cyrillic letters count as 10 not 20", "привет123", "ivan@example.ru", msgPasswordShort},
		{"128 ok", strings.Repeat("abcde12345", 12) + "abcdefgh", "ivan@example.ru", ""},
		{"129 too long", strings.Repeat("abcde12345", 12) + "abcdefghi", "ivan@example.ru", msgPasswordLong},
		{"same as email", "ivan@example.ru", "ivan@example.ru", msgPasswordEmail},
		{"same as email other case", "IVAN@example.ru", "ivan@example.ru", msgPasswordEmail},
		{"same as email local part", "ivanpetrov", "ivanpetrov@example.ru", msgPasswordEmail},
		{"common digits", "1234567890", "ivan@example.ru", msgPasswordWeak},
		{"common word", "Password123", "ivan@example.ru", msgPasswordWeak},
		{"common russian", "ЙЦУКЕНГШЩЗ", "ivan@example.ru", msgPasswordWeak},
		{"few distinct characters", "aaaaaaaaaaaa", "ivan@example.ru", msgPasswordWeak},
		{"four distinct characters", "abababcdcdcd", "ivan@example.ru", msgPasswordWeak},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePassword(tc.password, tc.email); got != tc.msg {
				t.Fatalf("ValidatePassword(%q) = %q, want %q", tc.password, got, tc.msg)
			}
		})
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := newValidation("email", "x")
	if err.Error() == "" || err.Fields["email"] != "x" {
		t.Fatalf("unexpected: %+v", err)
	}
}

func TestNewTokenIsRandomAndHashed(t *testing.T) {
	a, ha := newToken()
	b, hb := newToken()
	if a == b || string(ha) == string(hb) {
		t.Fatal("tokens must be unique")
	}
	if len(a) < 40 {
		t.Fatalf("token too short: %d", len(a))
	}
	if string(hashToken(a)) != string(ha) || len(ha) != 32 {
		t.Fatal("hash must be the SHA-256 of the token")
	}
	if string(hashToken(a)) == string(hashToken(b)) {
		t.Fatal("different tokens, different hashes")
	}
}

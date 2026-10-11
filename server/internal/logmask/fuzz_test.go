package logmask

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzMask: любая строка проходит маску без паники; в результате нет управляющих знаков (строка журнала не
// ломается переносом), нет почты, которую маска обязана скрыть, и повторный проход ничего не меняет.
func FuzzMask(f *testing.F) {
	for _, s := range []string{
		"user@example.com", "ip 203.0.113.77 port", "fe80::1%eth0", "password=hunter2", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789",
		"postgres://u:secret@db/x", "a\nb\rc", "::::::::", strings.Repeat("a", 5000), "1.2.3.4.5.6.7.8", "x@y@z", "token=", "ÿ@ÿ.ru",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Mask(in)
		for _, r := range out {
			if unicode.IsControl(r) {
				t.Fatalf("Mask(%q) = %q: control character U+%04X", in, out, r)
			}
		}
		if again := Mask(out); again != out {
			t.Fatalf("Mask is not idempotent: %q -> %q -> %q", in, out, again)
		}
		if emailRe.MatchString(out) {
			t.Fatalf("Mask(%q) = %q still holds an e-mail address", in, out)
		}
	})
}

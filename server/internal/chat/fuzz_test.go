package chat

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Фаззинг разбора ввода чата (срез 40, T-98, D-196): текст сообщения и ключ листания не роняют разбор, принятое
// соответствует правилам и разбирается заново так же.

func FuzzCleanText(f *testing.F) {
	for _, s := range []string{"", " ", "Привет", "a\r\nb", "\x00", "a\u202eb", " ", "😀", strings.Repeat("я", MaxText+1), "\xff\xfe", "\t\n x \n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		text, problem := cleanText(raw)
		if problem != "" {
			if text != "" {
				t.Fatalf("cleanText(%q) refused but returned %q", raw, text)
			}
			return
		}
		if text == "" || text != strings.TrimSpace(text) || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxText || strings.ContainsRune(text, '\r') {
			t.Fatalf("cleanText(%q) accepted %q", raw, text)
		}
		for _, r := range text {
			if (unicode.IsControl(r) && r != '\n' && r != '\t') || isBidiControl(r) {
				t.Fatalf("cleanText(%q) kept %U", raw, r)
			}
		}
		if again, p := cleanText(text); p != "" || again != text {
			t.Fatalf("cleanText is not idempotent on %q", text)
		}
	})
}

func FuzzDecodeCursor(f *testing.F) {
	for _, s := range []string{"", "MToy", "Zm9vOmJhcg", "-", "MTIzOjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMA", strings.Repeat("A", 500)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		at, id, ok := decodeCursor(s)
		if !ok {
			return
		}
		at2, id2, ok2 := decodeCursor(encodeCursor(at, id))
		if !ok2 || !at2.Equal(at) || id2 != id {
			t.Fatalf("cursor %q does not survive re-encoding", s)
		}
	})
}

func FuzzMailSafe(f *testing.F) {
	for _, s := range []string{"", "Анна\nИльина", "a\u202eb", "\x00", strings.Repeat("я", 500)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := mailSafe(s, mailNameMax)
		if utf8.RuneCountInString(out) > mailNameMax || strings.ContainsAny(out, "\n\r") {
			t.Fatalf("mailSafe(%q) = %q", s, out)
		}
		for _, r := range out {
			if unicode.IsControl(r) || isBidiControl(r) {
				t.Fatalf("mailSafe(%q) kept %U", s, r)
			}
		}
	})
}

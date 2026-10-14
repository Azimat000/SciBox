package skills

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestKey(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ПЦР", "пцр"},
		{"  Microsoft   Excel ", "microsoft excel"},
		{"Бухгалтерский учёт", "бухгалтерский учет"},
		{"ЁЛКА", "елка"},
		{"масс‑спектрометрия", "масс-спектрометрия"}, // неразрывный дефис
		{"масс–спектрометрия", "масс-спектрометрия"}, // en dash
		{"масс—спектрометрия", "масс-спектрометрия"}, // em dash
		{"масс спектрометрия", "масс спектрометрия"},
		{"C++", "c++"},
		{"C#", "c#"},
		{".NET", ".net"},
		{"a\tb\nc", "a b c"},
		{"zero\u200bwidth", "zero width"},
		{"bad\xffutf", "bad utf"},
		{"nul\x00in", "nul in"},
		{"", ""},
		{"   ", ""},
	} {
		if got := Key(c.in); got != c.want {
			t.Errorf("Key(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"масс-спектрометрия", "масс спектрометрия"},
		{"c++ и c#", "c++ и c#"},
		{".net core", "net core"},
		{"яр (ядерный магнитный резонанс)", "яр ядерный магнитный резонанс"},
		{"!!", ""},
	} {
		if got := WordsText(c.in); got != c.want {
			t.Errorf("WordsText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseQuery(t *testing.T) {
	q := parseQuery("  Масс-Спектрометрии  ")
	if q.key != "масс-спектрометрии" || q.prefix != "масс-спектрометрии%" || strings.Join(q.words, ",") != "масс,спектрометрии" || q.tsq != "масс:* & спектрометрии:*" {
		t.Fatalf("query = %+v", q)
	}
	// Знаки LIKE экранируются, служебные знаки to_tsquery не попадают в запрос по основам слов.
	q = parseQuery(`50%_off & (a|b) !c:* \`)
	if !strings.Contains(q.prefix, `\%`) || !strings.Contains(q.prefix, `\_`) || !strings.HasSuffix(q.prefix, `\\%`) {
		t.Fatalf("prefix = %q", q.prefix)
	}
	if strings.ContainsAny(q.tsq, "|!()\\'") || strings.Contains(q.tsq, "::") {
		t.Fatalf("tsq has operators: %q", q.tsq)
	}
	// Знаки без букв дают пустой запрос по основам слов.
	if q := parseQuery("+++ ###"); q.tsq != "" {
		t.Fatalf("tsq = %q", q.tsq)
	}
	if q := parseQuery("   "); q.key != "" {
		t.Fatalf("empty = %+v", q)
	}
	// Длинный запрос обрезается, слов не больше предела.
	long := parseQuery(strings.Repeat("слово ", 40))
	if utf8.RuneCountInString(long.key) > MaxQuery || len(long.words) > maxWords {
		t.Fatalf("long = %d %d", utf8.RuneCountInString(long.key), len(long.words))
	}
	if strings.HasSuffix(long.key, " ") {
		t.Fatalf("key must not end with a space: %q", long.key)
	}
}

// FuzzKey: ключ не падает, не содержит краевых пробелов и управляющих знаков, повторный ключ даёт то же; запрос
// подсказки собирается из чего угодно и не пропускает знаки to_tsquery.
func FuzzKey(f *testing.F) {
	for _, s := range []string{"ПЦР", "  Ёж  ", "a\x00b", "\xff\xfe", "C++ & (x|y)!:*", "масс‑спектр", strings.Repeat("я", 300)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k := Key(s)
		if k != strings.TrimSpace(k) || strings.Contains(k, "  ") || !utf8.ValidString(k) {
			t.Fatalf("Key(%q) = %q", s, k)
		}
		if Key(k) != k {
			t.Fatalf("Key is not idempotent: %q → %q → %q", s, k, Key(k))
		}
		q := parseQuery(s)
		if strings.ContainsAny(q.tsq, "|!()\\'<>") {
			t.Fatalf("tsq %q from %q", q.tsq, s)
		}
		if utf8.RuneCountInString(q.key) > MaxQuery {
			t.Fatalf("key too long: %d", utf8.RuneCountInString(q.key))
		}
	})
}

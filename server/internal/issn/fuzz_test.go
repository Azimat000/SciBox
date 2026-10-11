package issn

import (
	"regexp"
	"strings"
	"testing"
)

var canonical = regexp.MustCompile(`^[0-9]{4}-[0-9]{3}[0-9X]$`)

// checkDigit — независимая запись правила ISO 3297 (веса 8…2, остаток по 11), чтобы тест не повторял код.
func checkDigit(d string) byte {
	sum := 0
	for i := range 7 {
		sum += int(d[i]-'0') * (8 - i)
	}
	switch c := (11 - sum%11) % 11; c {
	case 10:
		return 'X'
	default:
		return byte('0' + c)
	}
}

// FuzzNormalize: что бы ни пришло, ответ либо «не ISSN», либо запись одного вида, которая сама годится на вход.
func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"0028-0836", "00280836", "issn 0028 0836", "ISSN:2049-3630", "2049-363x", "", "‐‑–", "0000-0000", "ISSN", "\x00", "１２３４５６７８"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := Normalize(in)
		if !ok {
			if out != "" {
				t.Fatalf("not ok but %q returned for %q", out, in)
			}
			return
		}
		if !canonical.MatchString(out) {
			t.Fatalf("Normalize(%q) = %q: not in canonical form", in, out)
		}
		if out[8] != checkDigit(strings.ReplaceAll(out, "-", "")) {
			t.Fatalf("Normalize(%q) = %q: wrong check digit", in, out)
		}
		again, ok2 := Normalize(out)
		if !ok2 || again != out {
			t.Fatalf("Normalize is not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}

// FuzzNormalizeAcceptsEveryValidISSN: любые семь цифр с правильной контрольной цифрой принимаются в обоих написаниях.
func FuzzNormalizeAcceptsEveryValidISSN(f *testing.F) {
	f.Add(uint32(2800836))
	f.Add(uint32(0))
	f.Add(uint32(9999999))
	f.Fuzz(func(t *testing.T, n uint32) {
		d := []byte("0000000")
		v := n % 10_000_000
		for i := 6; i >= 0; i-- {
			d[i] = byte('0' + v%10)
			v /= 10
		}
		full := string(d) + string(checkDigit(string(d)))
		want := full[:4] + "-" + full[4:]
		for _, in := range []string{full, want, "ISSN " + want, strings.ToLower(want)} {
			if got, ok := Normalize(in); !ok || got != want {
				t.Fatalf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
			}
		}
	})
}

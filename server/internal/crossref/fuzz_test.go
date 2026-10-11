package crossref

import (
	"net/url"
	"strings"
	"testing"
	"unicode"
)

// FuzzNormalizeDOI: принятый DOI короткий, строчный, без пробелов, управляющих и форматирующих знаков (в том числе
// переключателей направления письма), сам годится на вход и безопасно ложится в путь запроса.
func FuzzNormalizeDOI(f *testing.F) {
	for _, s := range []string{
		"10.1038/nature12373", "https://doi.org/10.1038/NATURE12373.", "doi:10.1000/182", "DOI.ORG/10.1000/182",
		"10.1234/a b", "10.1234/\u202eevil", "10.1234/a\u2028b", "10.1234/a\u00a0b", "10.12/x", "", "10.1234/\x00",
		"10.1234/" + strings.Repeat("a", 300), "10.1234/a?b#c", "10.1234/<script>",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := NormalizeDOI(in)
		if !ok {
			if out != "" {
				t.Fatalf("not ok but %q returned for %q", out, in)
			}
			return
		}
		if len(out) > maxDOILen || out != strings.ToLower(out) {
			t.Fatalf("NormalizeDOI(%q) = %q: too long or not lower case", in, out)
		}
		for _, r := range out {
			if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
				t.Fatalf("NormalizeDOI(%q) = %q: contains U+%04X", in, out, r)
			}
		}
		if again, ok2 := NormalizeDOI(out); !ok2 || again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
		esc := escapeDOI(out)
		if strings.ContainsAny(esc, "?# <>\"") {
			t.Fatalf("escapeDOI(%q) = %q: unsafe in a path", out, esc)
		}
		if back, err := url.PathUnescape(esc); err != nil || back != out {
			t.Fatalf("escapeDOI does not round-trip: %q -> %q -> %q (%v)", out, esc, back, err)
		}
	})
}

func TestNormalizeDOIRefusesDotSegments(t *testing.T) {
	for _, in := range []string{"10.1234/../../x", "10.1234/a/./b", "10.1234/.."} {
		if got, ok := NormalizeDOI(in); ok {
			t.Errorf("NormalizeDOI(%q) = %q, want a refusal", in, got)
		}
	}
	if got, ok := NormalizeDOI("10.1234/a.b/c..d"); !ok || got != "10.1234/a.b/c..d" {
		t.Errorf("a dot inside a segment is fine: %q %v", got, ok)
	}
}

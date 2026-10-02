package orgs

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Институт физики", "institut-fiziki"},
		{"ИТМО", "itmo"},
		{"Казанский (Приволжский) федеральный университет", "kazanskiy-privolzhskiy-federalnyy-universitet"},
		{"Объединённый институт ядерных исследований", "obedinennyy-institut-yadernykh-issledovaniy"},
		{"  Щербаков & Ко, R&D центр  ", "shcherbakov-ko-r-d-tsentr"},
		{"Лаборатория 42", "laboratoriya-42"},
		{"Ъ", fallbackSlug},
		{"!!!", fallbackSlug},
		{"日本語", fallbackSlug},
		{"Аб-Вг", "ab-vg"},
		{"Съезд", "sezd"},
		{"а ь б", "a-b"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugifyIsAlwaysValid(t *testing.T) {
	long := strings.Repeat("Институт физики твёрдого тела и микроэлектроники ", 5)
	got := slugify(long)
	if len(got) > maxSlugBase {
		t.Errorf("slug too long: %d", len(got))
	}
	if strings.HasSuffix(got, "-") || strings.HasPrefix(got, "-") || strings.Contains(got, "--") {
		t.Errorf("bad dashes in %q", got)
	}
	// Одно длинное слово без дефисов режется просто по длине.
	oneWord := slugify(strings.Repeat("щ", 60))
	if len(oneWord) != maxSlugBase {
		t.Errorf("one long word: len %d, want %d", len(oneWord), maxSlugBase)
	}
}

func TestSlugCandidate(t *testing.T) {
	cases := []struct {
		base string
		n    int
		want string
	}{
		{"ifan", 1, "ifan"},
		{"ifan", 0, "ifan"},
		{"ifan", 2, "ifan-2"},
		{"ifan", 7, "ifan-7"},
		{"new", 1, "new-2"},
		{"new", 2, "new-2"},
		{"new", 3, "new-3"},
	}
	for _, c := range cases {
		if got := slugCandidate(c.base, c.n); got != c.want {
			t.Errorf("slugCandidate(%q, %d) = %q, want %q", c.base, c.n, got, c.want)
		}
	}
}

package orgs

import (
	"errors"
	"strings"
	"testing"

	"scibox/server/internal/auth"
)

func TestNormalizeName(t *testing.T) {
	long := strings.Repeat("а", 201)
	cases := []struct {
		in, want, msg string
	}{
		{"  Институт   физики  ", "Институт физики", ""},
		{"ИФ", "ИФ", ""},
		{"", "", msgNameRequired},
		{"   \t ", "", msgNameRequired},
		{"И", "", msgNameShort},
		{long, "", msgNameLong},
		{strings.Repeat("а", 200), strings.Repeat("а", 200), ""},
		{"12345", "", msgNameInvalid},
		{"Институт\x00", "", msgNameInvalid},
		{"Lab 7", "Lab 7", ""},
	}
	for _, c := range cases {
		got, msg := normalizeName(c.in)
		if got != c.want || msg != c.msg {
			t.Errorf("normalizeName(%q) = %q, %q; want %q, %q", c.in, got, msg, c.want, c.msg)
		}
	}
}

func TestNormalizeCity(t *testing.T) {
	cases := []struct{ in, want, msg string }{
		{" Санкт-Петербург ", "Санкт-Петербург", ""},
		{"Ростов  на   Дону", "Ростов на Дону", ""},
		{"", "", msgCityRequired},
		{strings.Repeat("я", 101), "", msgCityLong},
		{"1234", "", msgCityInvalid},
		{"Томск\x07", "", msgCityInvalid},
	}
	for _, c := range cases {
		got, msg := normalizeCity(c.in)
		if got != c.want || msg != c.msg {
			t.Errorf("normalizeCity(%q) = %q, %q; want %q, %q", c.in, got, msg, c.want, c.msg)
		}
	}
}

func TestNormalizeWebsite(t *testing.T) {
	cases := []struct{ in, want, msg string }{
		{"", "", ""},
		{"   ", "", ""},
		{"example.ru", "https://example.ru", ""},
		{" https://example.ru/lab?x=1 ", "https://example.ru/lab?x=1", ""},
		{"http://sub.example.ru", "http://sub.example.ru", ""},
		{"ftp://example.ru", "", msgWebsiteInvalid},
		{"https://user:pass@example.ru", "", msgWebsiteInvalid},
		{"https://localhost", "", msgWebsiteInvalid},
		{"https://.example.ru", "", msgWebsiteInvalid},
		{"https://example.ru.", "", msgWebsiteInvalid},
		{"https://exa..mple.ru", "", msgWebsiteInvalid},
		{"https://", "", msgWebsiteInvalid},
		{"exa mple.ru", "", msgWebsiteInvalid},
		{"https://example.ru/<script>", "", msgWebsiteInvalid},
		{"https://exa%zzmple.ru", "", msgWebsiteInvalid},
		{"https://example.ru/" + strings.Repeat("a", 300), "", msgWebsiteLong},
		{strings.Repeat("a", 301), "", msgWebsiteLong},
	}
	for _, c := range cases {
		got, msg := normalizeWebsite(c.in)
		if got != c.want || msg != c.msg {
			t.Errorf("normalizeWebsite(%q) = %q, %q; want %q, %q", c.in, got, msg, c.want, c.msg)
		}
	}
}

// Адрес, который стал длиннее лимита после разбора (например, из-за добавленного https://), тоже не принимается.
func TestNormalizeWebsiteLengthAfterParsing(t *testing.T) {
	in := "example.ru/" + strings.Repeat("a", 285)
	if _, msg := normalizeWebsite(in); msg != msgWebsiteLong {
		t.Errorf("msg = %q, want %q", msg, msgWebsiteLong)
	}
}

func TestNormalizeDescription(t *testing.T) {
	cases := []struct{ in, want, msg string }{
		{"", "", ""},
		{"  Строка один\r\nСтрока два\rТри  ", "Строка один\nСтрока два\nТри", ""},
		{"Табуляция\tрядом", "Табуляция\tрядом", ""},
		{"Есть\x00ноль", "", msgDescInvalid},
		{strings.Repeat("я", 51), "", "long"},
	}
	for _, c := range cases {
		got, msg := normalizeDescription(c.in, 50, "long")
		if got != c.want || msg != c.msg {
			t.Errorf("normalizeDescription(%q) = %q, %q; want %q, %q", c.in, got, msg, c.want, c.msg)
		}
	}
}

func TestNormalizeTopics(t *testing.T) {
	got, msg := normalizeTopics([]string{" Квантовые   точки ", "", "квантовые точки", "Нейросети"})
	if msg != "" || strings.Join(got, "|") != "Квантовые точки|Нейросети" {
		t.Errorf("got %q, %q", got, msg)
	}
	if got, msg := normalizeTopics(nil); msg != "" || got == nil || len(got) != 0 {
		t.Errorf("nil topics: %v, %q; want empty non-nil slice", got, msg)
	}
	var many []string
	for i := 0; i < 11; i++ {
		many = append(many, "Тема "+strings.Repeat("я", i+1))
	}
	if _, msg := normalizeTopics(many); msg != msgTopicsMany {
		t.Errorf("many: %q", msg)
	}
	if _, msg := normalizeTopics(many[:10]); msg != "" {
		t.Errorf("ten topics must pass: %q", msg)
	}
	if _, msg := normalizeTopics([]string{"я"}); msg != msgTopicShort {
		t.Errorf("short: %q", msg)
	}
	if _, msg := normalizeTopics([]string{"Тема\x00х"}); msg != msgTopicShort {
		t.Errorf("control: %q", msg)
	}
	if _, msg := normalizeTopics([]string{strings.Repeat("я", 81)}); msg != msgTopicLong {
		t.Errorf("long: %q", msg)
	}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	return v.Fields
}

func TestValidateOrg(t *testing.T) {
	ok, err := validateOrg(OrgInput{Name: "  Институт  ", Kind: KindInstitute, City: " Москва ", Website: "example.ru", Description: " Текст "})
	if err != nil || ok != (orgFields{name: "Институт", kind: KindInstitute, city: "Москва", website: "https://example.ru", description: "Текст"}) {
		t.Fatalf("got %+v, %v", ok, err)
	}
	for _, k := range OrgKinds {
		if _, err := validateOrg(OrgInput{Name: "Институт", Kind: k, City: "Москва"}); err != nil {
			t.Errorf("kind %q must pass: %v", k, err)
		}
	}
	_, err = validateOrg(OrgInput{Name: "", Kind: "bank", City: "", Website: "ftp://x", Description: strings.Repeat("я", 5001)})
	f := fieldsOf(t, err)
	want := map[string]string{"name": msgNameRequired, "kind": msgOrgKind, "city": msgCityRequired, "website": msgWebsiteInvalid, "description": msgDescOrgLong}
	if len(f) != len(want) {
		t.Fatalf("fields = %v", f)
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %s = %q, want %q", k, f[k], v)
		}
	}
}

func TestValidateUnit(t *testing.T) {
	ok, err := validateUnit(UnitInput{Name: "Лаборатория", Kind: UnitLaboratory, Description: "Текст", Topics: []string{"Тема", "тема"}})
	if err != nil || ok.name != "Лаборатория" || len(ok.topics) != 1 {
		t.Fatalf("got %+v, %v", ok, err)
	}
	for _, k := range UnitKinds {
		if _, err := validateUnit(UnitInput{Name: "Кафедра", Kind: k}); err != nil {
			t.Errorf("kind %q must pass: %v", k, err)
		}
	}
	_, err = validateUnit(UnitInput{Name: "x", Kind: "club", Description: strings.Repeat("я", 3001), Topics: []string{"я"}})
	f := fieldsOf(t, err)
	want := map[string]string{"name": msgNameShort, "kind": msgUnitKind, "description": msgDescUnitLong, "topics": msgTopicShort}
	if len(f) != len(want) {
		t.Fatalf("fields = %v", f)
	}
	for k, v := range want {
		if f[k] != v {
			t.Errorf("field %s = %q, want %q", k, f[k], v)
		}
	}
}

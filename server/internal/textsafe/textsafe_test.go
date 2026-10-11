package textsafe

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestValid(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "обычный текст": true, "tab\tand\nnewline": true, "a\x00b": false, "\xff": false, "ok\xc3": false, "emoji 🙂": true,
	} {
		if got := Valid(in); got != want {
			t.Errorf("Valid(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidRequestURL(t *testing.T) {
	for in, want := range map[string]bool{
		"/api/vacancies":               true,
		"/api/vacancies?q=физика":      true,
		"/api/vacancies?q=%00":         false,
		"/api/vacancies?q=%ff":         false,
		"/api/vacancies?%00=1":         false,
		"/api/organizations/%00":       false,
		"/api/organizations/%ff":       false,
		"/api/vacancies?q=a&q=%00":     false,
		"/api/vacancies?q=%zz&field=1": true, // неразобранное обработчик всё равно не увидит
	} {
		u, err := url.ParseRequestURI(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := ValidRequestURL(u); got != want {
			t.Errorf("ValidRequestURL(%q) = %v, want %v", in, got, want)
		}
	}
}

type doc struct {
	Name  string
	Tags  []string
	Inner *struct{ Note string }
	Any   any
	Map   map[string]string
	raw   string
	Blob  json.RawMessage
}

func TestValidValue(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want bool
	}{
		{"clean", doc{Name: "Иван", Tags: []string{"a"}, Map: map[string]string{"k": "v"}}, true},
		{"nul in field", doc{Name: "a\x00"}, false},
		{"nul in slice", doc{Tags: []string{"ok", "a\x00"}}, false},
		{"nul in pointer", doc{Inner: &struct{ Note string }{Note: "\x00"}}, false},
		{"nul in interface", doc{Any: "\x00"}, false},
		{"nul in map key", doc{Map: map[string]string{"\x00": "v"}}, false},
		{"nul in map value", doc{Map: map[string]string{"k": "\x00"}}, false},
		{"invalid utf8", doc{Name: "\xff"}, false},
		{"unexported is not a body field", doc{raw: "\x00"}, true},
		{"raw bytes are not text", doc{Blob: json.RawMessage("\x00")}, true},
		{"nil pointer", (*doc)(nil), true},
		{"number", 5, true},
	}
	for _, c := range cases {
		if got := ValidValue(c.v); got != c.want {
			t.Errorf("%s: ValidValue = %v, want %v", c.name, got, c.want)
		}
	}
	// Самоссылка не зацикливает проверку.
	type loop struct{ Next *loop }
	l := &loop{}
	l.Next = l
	if ValidValue(l) {
		t.Error("a cyclic value must be refused, not looped over")
	}
}

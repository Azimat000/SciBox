package references

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	ok := func(name, email string) RefereeInput { return RefereeInput{Name: name, Email: email} }
	cases := []struct {
		name      string
		in        []RefereeInput
		forbidden []string
		wantErr   string
		wantEmail []string
	}{
		{"empty", nil, nil, "", []string{}},
		{"one", []RefereeInput{ok("Иван Петров", "Petrov@Example.ru")}, nil, "", []string{"petrov@example.ru"}},
		{"three", []RefereeInput{ok("Аа", "a@example.ru"), ok("Бб", "b@example.ru"), ok("Вв", "c@example.ru")}, nil, "", []string{"a@example.ru", "b@example.ru", "c@example.ru"}},
		{"four", []RefereeInput{ok("Аа", "a@example.ru"), ok("Бб", "b@example.ru"), ok("Вв", "c@example.ru"), ok("Гг", "d@example.ru")}, nil, "Можно указать не больше 3 рекомендателей", nil},
		{"no name", []RefereeInput{ok("  ", "a@example.ru")}, nil, "Рекомендатель 1: укажите, как зовут рекомендателя", nil},
		{"short name", []RefereeInput{ok("И", "a@example.ru")}, nil, "Рекомендатель 1: имя слишком короткое", nil},
		{"long name", []RefereeInput{ok(strings.Repeat("я", 121), "a@example.ru")}, nil, "Рекомендатель 1: имя длиннее 120 знаков", nil},
		{"bad email", []RefereeInput{ok("Иван Петров", "не почта")}, nil, "Рекомендатель 1:", nil},
		{"second is bad", []RefereeInput{ok("Иван Петров", "a@example.ru"), ok("Мария", "bad")}, nil, "Рекомендатель 2:", nil},
		{"long relation", []RefereeInput{{Name: "Иван Петров", Email: "a@example.ru", Relation: strings.Repeat("я", 121)}}, nil, "Рекомендатель 1: пояснение длиннее 120 знаков", nil},
		{"own email", []RefereeInput{ok("Иван Петров", "Me@Example.ru")}, []string{"me@example.ru"}, "Рекомендатель 1: нельзя просить рекомендацию у себя", nil},
		{"duplicate", []RefereeInput{ok("Иван Петров", "a@example.ru"), ok("Другой Человек", "A@example.ru")}, nil, "Рекомендатель 2: эта почта уже есть среди рекомендателей", nil},
	}
	for _, c := range cases {
		got, msg := Normalize(c.in, c.forbidden...)
		if c.wantErr == "" && msg != "" || c.wantErr != "" && !strings.HasPrefix(msg, c.wantErr) {
			t.Errorf("%s: message %q, want %q", c.name, msg, c.wantErr)
			continue
		}
		if c.wantErr != "" {
			if got != nil {
				t.Errorf("%s: returned referees despite the error", c.name)
			}
			continue
		}
		var emails []string
		for _, r := range got {
			emails = append(emails, r.Email)
		}
		if len(emails) != len(c.wantEmail) || strings.Join(emails, ",") != strings.Join(c.wantEmail, ",") {
			t.Errorf("%s: emails %v, want %v", c.name, emails, c.wantEmail)
		}
	}
}

func TestNormalizeTidiesNames(t *testing.T) {
	got, msg := Normalize([]RefereeInput{{Name: "  Иван   Петров ", Email: "a@example.ru", Relation: "  коллега   по  лаборатории "}})
	if msg != "" || got[0].Name != "Иван Петров" || got[0].Relation != "коллега по лаборатории" {
		t.Errorf("%+v %q", got, msg)
	}
}

func TestLowerFirst(t *testing.T) {
	for in, want := range map[string]string{"Укажите": "укажите", "Имя": "имя", "abc": "abc", "": "", "Ёлка": "ёлка"} {
		if got := lowerFirst(in); got != want {
			t.Errorf("lowerFirst(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRuDate(t *testing.T) {
	// 31 декабря 22:00 UTC — это уже 1 января по Москве.
	got := ruDate(mustTime("2026-12-31T22:00:00Z"))
	if got != "1 января 2027" {
		t.Errorf("ruDate = %q", got)
	}
}

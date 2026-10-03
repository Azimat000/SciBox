package matching

import (
	"strings"
	"testing"
	"time"
)

func TestPluralRu(t *testing.T) {
	for n, want := range map[int]string{
		0: "вакансий", 1: "вакансия", 2: "вакансии", 4: "вакансии", 5: "вакансий", 10: "вакансий", 11: "вакансий", 12: "вакансий",
		14: "вакансий", 21: "вакансия", 22: "вакансии", 25: "вакансий", 101: "вакансия", 111: "вакансий", 112: "вакансий", -2: "вакансии",
	} {
		if got := pluralRu(n, "вакансия", "вакансии", "вакансий"); got != want {
			t.Errorf("%d: %q, ожидали %q", n, got, want)
		}
	}
}

func TestDateText(t *testing.T) {
	if got := dateText(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)); got != "2 ноября 2026 г." {
		t.Errorf("%q", got)
	}
	if got := dateText(time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)); got != "31 января 2027 г." {
		t.Errorf("%q", got)
	}
}

func TestParseDay(t *testing.T) {
	if d := parseDay("2026-11-14"); d == nil || d.Day() != 14 || d.Month() != 11 {
		t.Errorf("%v", d)
	}
	for _, bad := range []string{"", "14.11.2026", "2026-13-40", "завтра"} {
		if d := parseDay(bad); d != nil {
			t.Errorf("%q: ожидали nil, получили %v", bad, d)
		}
	}
}

func TestDigestNotice(t *testing.T) {
	d := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	one := []digestItem{{Title: "Старший научный сотрудник", Org: "Институт катализа", City: "Новосибирск", Deadline: &d}}

	title, body := digestNotice("  химия   Новосибирск ", 1, one)
	if title != "Новая вакансия по поиску «химия Новосибирск»" {
		t.Errorf("заголовок: %q", title)
	}
	for _, want := range []string{"- Старший научный сотрудник — Институт катализа, Новосибирск. Срок подачи: 14 ноября 2026 г.\n", "по ссылке ниже"} {
		if !strings.Contains(body, want) {
			t.Errorf("в тексте нет %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "И ещё") {
		t.Errorf("все вакансии перечислены, «и ещё» не нужно:\n%s", body)
	}

	title, body = digestNotice("физика", 23, []digestItem{{Title: "А", Org: "Б"}, {Title: "В\nГ", Org: "Д", Deadline: nil}})
	if title != "По поиску «физика»: 23 новые вакансии" {
		t.Errorf("заголовок: %q", title)
	}
	if !strings.Contains(body, "И ещё 21 вакансия.") {
		t.Errorf("нет «и ещё»:\n%s", body)
	}
	if !strings.Contains(body, "- В Г — Д.") || strings.Contains(body, "Срок подачи") {
		t.Errorf("перенос строки в названии и отсутствие срока:\n%s", body)
	}
	if title, _ = digestNotice("х", 5, one); !strings.Contains(title, "5 новых вакансий") {
		t.Errorf("пять: %q", title)
	}
}

func TestReminderNotice(t *testing.T) {
	d := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		left  int
		title string
	}{
		{0, "Сегодня последний день подачи: «Вакансия»"},
		{-1, "Сегодня последний день подачи: «Вакансия»"},
		{1, "Срок подачи завтра: «Вакансия»"},
		{2, "Срок подачи через 2 дня: «Вакансия»"},
		{5, "Срок подачи через 5 дней: «Вакансия»"},
		{7, "Срок подачи через 7 дней: «Вакансия»"},
	} {
		title, body := reminderNotice("Вакансия", "Институт", d, c.left)
		if title != c.title {
			t.Errorf("%d: %q, ожидали %q", c.left, title, c.title)
		}
		if !strings.Contains(body, "Последний день подачи: 14 ноября 2026 г.\n") || strings.Contains(body, "г..") || !strings.Contains(body, "Институт") || !strings.Contains(body, "ещё не откликались") {
			t.Errorf("%d: текст:\n%s", c.left, body)
		}
	}
	if title, _ := reminderNotice("Длинное\nназвание  вакансии", "О", d, 1); strings.ContainsAny(title, "\r\n") || !strings.Contains(title, "Длинное название вакансии") {
		t.Errorf("заголовок не должен ломаться: %q", title)
	}
}

func TestLinesAndOneLine(t *testing.T) {
	if got := lines("а", "", "  ", "б"); got != "а\nб" {
		t.Errorf("%q", got)
	}
	if got := oneLine("  а \n\t б  "); got != "а б" {
		t.Errorf("%q", got)
	}
}

func TestFirstMessageIsStable(t *testing.T) {
	if got := firstMessage(map[string]string{"q": "б", "field": "а", "region": "в"}); got != "а" {
		t.Errorf("сообщение первого по алфавиту поля: %q", got)
	}
}

func TestSortStableKeepsOrderOfEquals(t *testing.T) {
	type p struct{ k, id int }
	items := []p{{2, 1}, {1, 2}, {2, 3}, {1, 4}}
	sortStable(items, func(a, b p) bool { return a.k < b.k })
	want := []p{{1, 2}, {1, 4}, {2, 1}, {2, 3}}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("%v, ожидали %v", items, want)
		}
	}
}

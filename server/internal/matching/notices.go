package matching

import (
	"fmt"
	"strings"
	"time"
)

// Тексты уведомлений и писем. Уведомление показывается и на сайте, и в письме (D-084); письма этих двух видов человек
// может отключить (D-106).

var months = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

// dateText — «2 ноября 2026 г.» из даты (срок подачи хранится как день без времени).
func dateText(d time.Time) string {
	return fmt.Sprintf("%d %s %d г.", d.Day(), months[d.Month()-1], d.Year())
}

// pluralRu выбирает форму слова по числу: 1 вакансия, 2 вакансии, 5 вакансий.
func pluralRu(n int, one, few, many string) string {
	n = max(n, -n)
	switch {
	case n%100 >= 11 && n%100 <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	}
	return many
}

// lines склеивает непустые строки.
func lines(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

// oneLine убирает переносы и лишние пробелы: названия в заголовке уведомления не должны ломать строку.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// digestItem — вакансия в письме о новых вакансиях.
type digestItem struct {
	Title, Org, City string
	Deadline         *time.Time
}

func (d digestItem) line() string {
	place := d.Org
	if d.City != "" {
		place += ", " + d.City
	}
	s := "- " + oneLine(d.Title) + " — " + place + "."
	if d.Deadline != nil {
		s += " Срок подачи: " + dateText(*d.Deadline) // «г.» в конце даты уже с точкой
	}
	return s
}

// digestNotice — письмо о новых вакансиях по сохранённому поиску: первые вакансии списком и сколько ещё.
func digestNotice(searchName string, total int, shown []digestItem) (title, body string) {
	name := oneLine(searchName)
	if total == 1 {
		title = "Новая вакансия по поиску «" + name + "»"
	} else {
		title = fmt.Sprintf("По поиску «%s»: %d %s", name, total, pluralRu(total, "новая вакансия", "новые вакансии", "новых вакансий"))
	}
	list := make([]string, len(shown))
	for i, it := range shown {
		list[i] = it.line()
	}
	more := ""
	if total > len(shown) {
		n := total - len(shown)
		more = fmt.Sprintf("И ещё %d %s.", n, pluralRu(n, "вакансия", "вакансии", "вакансий"))
	}
	return title, lines(strings.Join(list, "\n"), more, "Все найденные вакансии открываются по ссылке ниже.")
}

// reminderNotice — напоминание о сроке подачи вакансии из избранного.
func reminderNotice(vacancy, org string, deadline time.Time, left int) (title, body string) {
	v := oneLine(vacancy)
	switch {
	case left <= 0:
		title = "Сегодня последний день подачи: «" + v + "»"
	case left == 1:
		title = "Срок подачи завтра: «" + v + "»"
	default:
		title = fmt.Sprintf("Срок подачи через %d %s: «%s»", left, pluralRu(left, "день", "дня", "дней"), v)
	}
	return title, lines(
		fmt.Sprintf("Вакансия «%s», %s.", v, org),
		"Последний день подачи: "+dateText(deadline), // «г.» в конце даты уже с точкой
		"Вы добавили вакансию в избранное и ещё не откликались на неё.",
	)
}

package offers

import (
	"fmt"
	"strings"
)

// Тексты уведомлений и писем. Уведомление показывается и на сайте, и в письме (D-084).

func lines(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func vacancyLine(title, org string) string {
	return fmt.Sprintf("Вакансия «%s», %s.", title, org)
}

func withNote(label, text string) string {
	if text == "" {
		return ""
	}
	return label + ": " + text
}

// receivedNotice — что получает учёный, когда его пригласили.
func receivedNotice(vacancy, org, message string) (title, body string) {
	return "Вас приглашают на вакансию", lines(vacancyLine(vacancy, org), withNote("Сообщение организации", message),
		"Ответьте на приглашение на сайте. Если вакансия вам подходит, откликнитесь на неё.")
}

// cancelledNotice — приглашение отозвано.
func cancelledNotice(vacancy, org string) (title, body string) {
	return "Приглашение отозвано", lines(vacancyLine(vacancy, org), "Организация отозвала приглашение.")
}

// answeredNotice — что получают сотрудники организации, когда учёный ответил. Пустое имя — профиль анонимный и человек
// не открыл имя этой организации (ответ «Не сейчас» не открывает): в тексте имени нет (D-199).
func answeredNotice(name, vacancy, org, action, note string) (title, body string) {
	who := "Соискатель: " + name + "."
	if name == "" {
		who = "Соискатель с анонимным профилем."
	}
	if action == ActionInterested {
		return "Приглашение: ответ «Интересно»", lines(vacancyLine(vacancy, org), who+" Ответил «Интересно».", withNote("Записка", note))
	}
	return "Приглашение: ответ «Не сейчас»", lines(vacancyLine(vacancy, org), who+" Ответил «Не сейчас».", withNote("Записка", note))
}

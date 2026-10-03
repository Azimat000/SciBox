package applications

import (
	"fmt"
	"strings"
	"time"

	"scibox/server/internal/dbgen"
)

var months = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

// momentText — «15 октября 2026, 14:00 (МСК)». Время приглашений везде московское (D-089).
func momentText(t time.Time) string {
	t = t.In(moscow)
	return fmt.Sprintf("%d %s %d, %02d:%02d (МСК)", t.Day(), months[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

// lines склеивает непустые строки в текст уведомления: уведомление показывается и на сайте, и в письме.
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

// placeText: «Онлайн: ссылка» или «Адрес: …».
func placeText(kind, place string) string {
	if kind == PlaceOnline {
		return "Онлайн: " + place
	}
	return "Адрес: " + place
}

// invitationNotice — что получает соискатель, когда ему отправили приглашение.
func invitationNotice(vacancy, org string, v validInvitation) (title, body string) {
	head := vacancyLine(vacancy, org)
	switch v.Kind {
	case InvInterview:
		return "Приглашение на собеседование", lines(head, "Когда: "+momentText(*v.StartsAt), placeText(*v.PlaceKind, v.Place),
			withNote("Сообщение организации", v.Message), "Подтвердите время или предложите другое на странице отклика.")
	case InvContacts:
		return "Организация передала контакты", lines(head, withNote("Контактное лицо", v.ContactName), withNote("Почта", v.ContactEmail),
			withNote("Телефон", v.Phone), withNote("Сообщение организации", v.Message))
	default:
		return "Организация просит оставить контакты", lines(head, withNote("Сообщение организации", v.Message),
			"Ответьте на странице отклика: напишите, как с вами связаться и когда удобно.")
	}
}

// kindWord — вид приглашения словами, для текстов об отмене.
func kindWord(kind string) string {
	switch kind {
	case InvInterview:
		return "собеседование"
	case InvContacts:
		return "контакты организации"
	}
	return "просьбу оставить контакты"
}

// answerNotice — что получает организация, когда соискатель ответил на приглашение. Контакты соискателя в текст
// не попадают: их видно в карточке отклика, а письмо остаётся коротким.
func answerNotice(applicant, vacancy string, inv dbgen.ApplicationInvitation, v validAnswer) (title, body string) {
	head := fmt.Sprintf("Кандидат: %s. Вакансия «%s».", applicant, vacancy)
	switch {
	case v.Status == InvConfirmed:
		return "Кандидат подтвердил собеседование", lines(head, "Когда: "+momentText(*inv.StartsAt))
	case v.Status == InvProposed:
		return "Кандидат предложил другое время", lines(head, "Предложено: "+momentText(*v.ProposedAt),
			withNote("Сообщение", v.Note), "Принять это время или отправить новое приглашение можно в карточке отклика.")
	default:
		return "Кандидат ответил на просьбу о контактах", lines(head, "Контакты и удобное время указаны в карточке отклика.")
	}
}

package references

import (
	"fmt"
	"strings"
	"time"

	"scibox/server/internal/mail"
)

var months = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

var moscow = time.FixedZone("MSK", 3*60*60)

// ruDate: «2 ноября 2026» по московскому времени.
func ruDate(t time.Time) string {
	t = t.In(moscow)
	return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
}

// requestMail — письмо рекомендателю со ссылкой. Ссылка действует до expires и срабатывает один раз.
func (s *Service) requestMail(app AppInfo, ref Referee, token string, expires time.Time) mail.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Здравствуйте, %s!\n\n", ref.Name)
	fmt.Fprintf(&b, "%s откликается на позицию «%s» в организации «%s» и просит вас написать рекомендательное письмо.\n\n", app.ApplicantName, app.VacancyTitle, app.OrgName)
	b.WriteString("Регистрироваться не нужно. Письмо можно набрать прямо на странице или приложить PDF. Его получит только организация: сам кандидат письмо не увидит.\n\n")
	fmt.Fprintf(&b, "Ссылка работает до %s и срабатывает один раз:\n%s/recommend?token=%s\n\n", ruDate(expires), s.cfg.PublicURL, token)
	b.WriteString("Если вы не знакомы с кандидатом или не хотите писать письмо, на той же странице есть кнопка «Отказаться». Можно и просто не отвечать.\n\n")
	fmt.Fprintf(&b, "—\n%s. Это письмо отправлено автоматически, отвечать на него не нужно.\n", s.cfg.ProductName)
	return mail.Message{To: ref.Email, Subject: fmt.Sprintf("%s просит рекомендательное письмо", app.ApplicantName), Body: b.String()}
}

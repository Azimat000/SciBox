package auth

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/mail"
)

// link строит ссылку на страницу сайта; token (если есть) кладётся в адрес.
func (s *Service) link(path, token string) string {
	u := s.cfg.PublicURL + path
	if token != "" {
		u += "?token=" + url.QueryEscape(token)
	}
	return u
}

func (s *Service) confirmMail(u dbgen.User, token string) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Подтвердите почту на " + s.cfg.ProductName,
		Body: lines(
			// Имя здесь не подставляется: его ввёл тот, кто регистрировал адрес, а письмо уходит от нашего домена на чужую
			// почту (срез 29, CRY-7).
			"Здравствуйте!",
			"",
			"Чтобы завершить регистрацию на "+s.cfg.ProductName+", подтвердите почту. Откройте ссылку:",
			"",
			s.link("/confirm-email", token),
			"",
			"Ссылка действует двое суток. Если вы не регистрировались, ничего делать не нужно: без подтверждения аккаунт не заработает.",
		),
	}
}

func (s *Service) resetMail(u dbgen.User, token string) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Сброс пароля на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Вы просили сбросить пароль на "+s.cfg.ProductName+". Чтобы задать новый, откройте ссылку:",
			"",
			s.link("/reset-password", token),
			"",
			"Ссылка действует один час и сработает один раз. Если вы ничего не просили, ничего делать не нужно: пароль останется прежним.",
		),
	}
}

func (s *Service) alreadyRegisteredMail(u dbgen.User) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Эта почта уже есть на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Кто-то указал вашу почту при регистрации на "+s.cfg.ProductName+", но аккаунт с ней уже есть.",
			"",
			"Войти: "+s.link("/login", ""),
			"Забыли пароль: "+s.link("/forgot-password", ""),
			"",
			"Если это были не вы, ничего делать не нужно.",
		),
	}
}

func (s *Service) passwordChangedMail(u dbgen.User) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Пароль на " + s.cfg.ProductName + " изменён",
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Пароль от вашего аккаунта на "+s.cfg.ProductName+" только что изменили.",
			"",
			"Если это сделали не вы, сбросьте пароль: "+s.link("/forgot-password", ""),
		),
	}
}

func lines(l ...string) string { return strings.Join(l, "\n") + "\n" }

// ---- письма среза 20: устройства, смена почты, удаление аккаунта ----

var (
	moscow = time.FixedZone("MSK", 3*60*60)
	months = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
)

// moscowDate — «22 октября 2026», moscowTime — «22 октября 2026, 14:05 (МСК)».
func moscowDate(t time.Time) string {
	m := t.In(moscow)
	return fmt.Sprintf("%d %s %d", m.Day(), months[m.Month()-1], m.Year())
}

func moscowTime(t time.Time) string {
	return moscowDate(t) + ", " + t.In(moscow).Format("15:04") + " (МСК)"
}

func (s *Service) newDeviceMail(u dbgen.User, meta Meta, at time.Time) mail.Message {
	where := ParseUserAgent(meta.UserAgent).Label()
	if meta.IP != "" {
		where += ", адрес " + meta.IP
	}
	return mail.Message{
		To:      u.Email,
		Subject: "Вход в аккаунт " + s.cfg.ProductName + " с нового устройства",
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"В ваш аккаунт на "+s.cfg.ProductName+" вошли с устройства, с которого раньше не входили.",
			"",
			"Когда: "+moscowTime(at),
			"Откуда: "+where,
			"",
			"Если это были вы, ничего делать не нужно.",
			"Если нет, смените пароль и завершите чужие сеансы: "+s.link("/account/security", ""),
		),
	}
}

func (s *Service) changeEmailMail(u dbgen.User, newEmail, token string) mail.Message {
	return mail.Message{
		To:      newEmail,
		Subject: "Подтвердите новую почту на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Вы указали этот адрес как новую почту аккаунта на "+s.cfg.ProductName+". Чтобы сменить почту, откройте ссылку:",
			"",
			s.link("/confirm-email-change", token),
			"",
			"Ссылка действует сутки и сработает один раз. Пока вы её не открыли, почта аккаунта остаётся прежней.",
			"Если вы ничего не меняли, просто удалите это письмо.",
		),
	}
}

// emailChangeAskedMail уходит на текущий адрес, когда в аккаунте просят сменить почту. Нового адреса в письме нет.
func (s *Service) emailChangeAskedMail(u dbgen.User) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "В вашем аккаунте просят сменить почту на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"В вашем аккаунте на "+s.cfg.ProductName+" попросили сменить почту для входа. Пока ссылку из письма на новый адрес не откроют, ничего не меняется.",
			"",
			"Если это были вы, ничего делать не нужно. Если нет, смените пароль: "+s.link("/forgot-password", ""),
			"После смены пароля просьба о смене почты отменяется.",
		),
	}
}

func (s *Service) emailTakenMail(email string) mail.Message {
	return mail.Message{
		To:      email,
		Subject: "Эта почта уже есть на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте!",
			"",
			"Кто-то хотел сделать этот адрес почтой своего аккаунта на "+s.cfg.ProductName+", но с ним уже есть другой аккаунт. Почта не изменена.",
			"",
			"Если это были вы и у вас два аккаунта, войдите в тот, где эта почта: "+s.link("/login", ""),
			"Если нет, ничего делать не нужно.",
		),
	}
}

func (s *Service) emailChangedMail(old dbgen.User, newEmail, revertToken string) mail.Message {
	return mail.Message{
		To:      old.Email,
		Subject: "Почта аккаунта на " + s.cfg.ProductName + " изменена",
		Body: lines(
			"Здравствуйте, "+old.DisplayName+"!",
			"",
			"Почта вашего аккаунта на "+s.cfg.ProductName+" изменена. Теперь письма приходят на "+maskEmail(newEmail)+".",
			"",
			"Если это сделали не вы, откройте ссылку: мы вернём прежнюю почту, завершим все сеансы и пришлём ссылку для нового пароля.",
			"",
			s.link("/revert-email-change", revertToken),
			"",
			"Ссылка действует 7 дней.",
		),
	}
}

func (s *Service) emailRevertedMail(u dbgen.User, resetToken string) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Почта аккаунта на " + s.cfg.ProductName + " возвращена",
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Мы вернули аккаунту эту почту и завершили все сеансы. Если удаление аккаунта было назначено, оно отменено.",
			"Задайте новый пароль: тот, кто сменил почту, мог знать прежний.",
			"",
			s.link("/reset-password", resetToken),
			"",
			"Ссылка действует один час. Если не успеете, запросите новую: "+s.link("/forgot-password", ""),
		),
	}
}

func (s *Service) deletionScheduledMail(u dbgen.User, due time.Time) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Аккаунт на " + s.cfg.ProductName + " будет удалён " + moscowDate(due),
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Вы попросили удалить аккаунт на "+s.cfg.ProductName+". Профиль уже скрыт, на всех устройствах вы вышли из аккаунта.",
			"",
			"Данные сотрутся "+moscowTime(due)+": профиль, избранное, поиски, уведомления. Отклики у организаций останутся без вашего имени, письма, профиля и файлов.",
			"",
			"Передумали? Войдите до этого срока и нажмите «Отменить удаление»: "+s.link("/account/security", ""),
		),
	}
}

func (s *Service) deletionCancelledMail(u User) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Удаление аккаунта на " + s.cfg.ProductName + " отменено",
		Body: lines(
			"Здравствуйте, "+u.Name+"!",
			"",
			"Удаление аккаунта отменено, всё остаётся как было. Профиль снова виден тем, кому был виден раньше.",
			"",
			"Если отменили не вы, смените пароль: "+s.link("/account/security", ""),
		),
	}
}

func (s *Service) deletionBlockedMail(u dbgen.User, orgs []erasure.Org) mail.Message {
	names := make([]string, len(orgs))
	for i, o := range orgs {
		names[i] = "«" + o.Name + "»"
	}
	return mail.Message{
		To:      u.Email,
		Subject: "Не удалось удалить аккаунт на " + s.cfg.ProductName,
		Body: lines(
			"Здравствуйте, "+u.DisplayName+"!",
			"",
			"Мы не удалили ваш аккаунт: вы единственный владелец организации "+strings.Join(names, ", ")+", и в ней есть другие сотрудники. Удаление отменено, профиль снова виден.",
			"",
			"Сделайте владельцем кого-то из сотрудников и попросите удалить аккаунт заново: "+s.link("/account/security", ""),
		),
	}
}

func (s *Service) accountDeletedMail(u dbgen.User) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Аккаунт на " + s.cfg.ProductName + " удалён",
		Body: lines(
			"Здравствуйте!",
			"",
			"Ваш аккаунт на "+s.cfg.ProductName+" удалён, данные стёрты. Это последнее письмо от нас на этот адрес.",
		),
	}
}

// maskEmail прячет середину адреса: «el***@demo.example.ru». Письмо на старый адрес мог прочитать не владелец
// нового, поэтому новый адрес целиком не пишем.
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return "***"
	}
	r := []rune(local)
	keep := min(2, len(r)-1)
	return string(r[:max(keep, 0)]) + "***@" + domain
}

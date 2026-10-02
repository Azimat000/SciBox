package auth

import (
	"net/url"
	"strings"

	"scibox/server/internal/dbgen"
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
			"Здравствуйте, "+u.DisplayName+"!",
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

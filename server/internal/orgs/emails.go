package orgs

import (
	"net/url"
	"strings"

	"scibox/server/internal/access"
	"scibox/server/internal/mail"
)

// roleTitle — название роли в письме.
func roleTitle(r access.Role) string {
	switch r {
	case access.RoleOwner:
		return "владельца"
	case access.RoleHR:
		return "кадровика"
	default:
		return "руководителя подразделения"
	}
}

func (s *Service) inviteLink(token string) string {
	return s.cfg.PublicURL + "/invitations/accept?token=" + url.QueryEscape(token)
}

func (s *Service) inviteMail(inviter, orgName string, role access.Role, unitName *string, to, token string) mail.Message {
	what := "в роли " + roleTitle(role)
	if unitName != nil {
		what += " подразделения «" + *unitName + "»"
	}
	return mail.Message{
		To:      to,
		Subject: "Приглашение в организацию «" + orgName + "» на " + s.cfg.ProductName,
		Body: strings.Join([]string{
			"Здравствуйте!",
			"",
			inviter + " приглашает вас в организацию «" + orgName + "» на " + s.cfg.ProductName + " " + what + ".",
			"",
			"Чтобы принять приглашение, откройте ссылку и войдите в аккаунт с этой почтой (если аккаунта ещё нет, зарегистрируйтесь):",
			"",
			s.inviteLink(token),
			"",
			"Ссылка действует семь дней. Если вы не ждали приглашения, ничего делать не нужно.",
		}, "\n") + "\n",
	}
}

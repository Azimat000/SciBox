package profiles

import "strings"

// Ссылка на GitHub (срез 32). Разбор строгий и целиком в памяти: в сеть мы не ходим (иначе получили бы SSRF и
// зависимость от чужого сервиса), а адрес не доверяем ни одной библиотеке, которая «исправляет» ввод. Строка
// проходит узкий вход: схема http(s), хост ровно github.com или www.github.com, один сегмент пути. Всё, что стандартный
// разборщик адресов стерпел бы (логин и пароль в адресе, порт, запрос, якорь, процентное кодирование, юникодные
// двойники букв), здесь не пройдёт, потому что в разрешённом списке этого нет.

const (
	githubPrefix   = "https://github.com/"
	githubMaxLogin = 39
	// githubMaxInput — предел длины ввода до разбора: любой правильный адрес короче, длинное отсекается сразу.
	githubMaxInput = 120
)

// Причины отказа разборщика ссылки. Сообщение человеку выбирает validateCore.
type githubReject int

const (
	githubOK githubReject = iota
	githubBadLink
	githubIsRepo
	githubReserved
)

// githubReservedPaths — первые сегменты адресов GitHub, которые не профили (разделы самого сайта). Список составлен
// вручную по известным разделам; он неполный, и это безопасно: пропущенный раздел остаётся ссылкой на github.com
// с логином по правилам, а не адресом другого сайта. Спорные названия, под которыми мог бы жить настоящий аккаунт,
// в список не включены.
var githubReservedPaths = map[string]bool{
	"about": true, "account": true, "apps": true, "assets": true, "blog": true, "business": true,
	"codespaces": true, "collections": true, "community": true, "contact": true, "copilot": true,
	"customer-stories": true, "dashboard": true, "discussions": true, "education": true,
	"enterprise": true, "events": true, "explore": true, "features": true, "gist": true, "gists": true, "git": true,
	"github": true, "help": true, "home": true, "integrations": true, "issues": true, "join": true,
	"login": true, "logout": true, "marketplace": true, "mobile": true,
	"new": true, "nonprofit": true, "notifications": true, "open-source": true, "organizations": true, "orgs": true,
	"partners": true, "password_reset": true, "personal": true, "premium-support": true, "pricing": true,
	"projects": true, "pulls": true, "readme": true, "resources": true, "search": true, "security": true,
	"session": true, "sessions": true, "settings": true, "signup": true, "site": true,
	"solutions": true, "sponsors": true, "stars": true, "support": true, "team": true, "teams": true, "terms": true,
	"topics": true, "trending": true, "trust": true, "tos": true, "user": true, "users": true, "watching": true,
	"why-github": true, "privacy": true, "sitemap": true, "api": true,
}

// normalizeGitHub приводит ссылку на профиль GitHub к виду https://github.com/<логин>. Пустой ввод допустим и даёт
// пустую строку. Принимает «github.com/логин», «www.github.com/логин», с http:// и https:// и со слешем в конце;
// регистр схемы и хоста не важен, регистр логина сохраняется как введён.
func normalizeGitHub(raw string) (string, githubReject) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", githubOK
	}
	if len(s) > githubMaxInput {
		return "", githubBadLink
	}
	// Только печатный ASCII без пробелов: юникодные двойники букв, переводы строк, табуляции, нулевой знак и
	// управляющие знаки не доходят до разбора вовсе.
	for i := range len(s) {
		if c := s[i]; c <= ' ' || c >= 0x7f {
			return "", githubBadLink
		}
	}
	rest := s
	if cut, ok := cutPrefixFold(rest, "https://"); ok {
		rest = cut
	} else if cut, ok := cutPrefixFold(rest, "http://"); ok {
		rest = cut
	}
	// Двоеточие значит схему (javascript:, data:), порт или логин с паролем; @ — логин в адресе; ? и # — запрос и
	// якорь; % — процентное кодирование; \ и ; — приёмы обхода разбора. Ни одного из них в верном адресе нет.
	if strings.ContainsAny(rest, ":@?#%\\;") {
		return "", githubBadLink
	}
	host, path, ok := strings.Cut(rest, "/")
	if !ok {
		return "", githubBadLink
	}
	if host = strings.ToLower(host); host != "github.com" && host != "www.github.com" {
		return "", githubBadLink
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "", githubBadLink
	}
	if strings.Contains(path, "/") {
		// github.com/логин/репозиторий и глубже: это не профиль. Отдельная причина, чтобы человек понял, что поправить.
		login, rest, _ := strings.Cut(path, "/")
		if rest != "" && rest[0] != '/' && validGitHubLogin(login) && !githubReservedPaths[strings.ToLower(login)] {
			return "", githubIsRepo
		}
		return "", githubBadLink
	}
	if !validGitHubLogin(path) {
		return "", githubBadLink
	}
	if githubReservedPaths[strings.ToLower(path)] {
		return "", githubReserved
	}
	return githubPrefix + path, githubOK
}

// validGitHubLogin: от 1 до 39 знаков, латинские буквы и цифры, дефисы только одиночные и не в начале и не в конце.
func validGitHubLogin(s string) bool {
	if s == "" || len(s) > githubMaxLogin {
		return false
	}
	prevHyphen := true // дефис в самом начале запрещён так же, как два дефиса подряд
	for i := range len(s) {
		switch c := s[i]; {
		case c == '-':
			if prevHyphen {
				return false
			}
			prevHyphen = true
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			prevHyphen = false
		default:
			return false
		}
	}
	return !prevHyphen // дефис в конце запрещён
}

// cutPrefixFold отрезает префикс без учёта регистра (префикс задан строчными ASCII-буквами).
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

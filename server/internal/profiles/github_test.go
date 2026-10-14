package profiles

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Срез 32: ссылка на GitHub. Разборщик строгий и не ходит в сеть; каждая строка таблицы - один приём обхода.

func TestNormalizeGitHubAccepted(t *testing.T) {
	long := strings.Repeat("a", 39)
	tests := []struct{ name, in, want string }{
		{"без схемы", "github.com/torvalds", "https://github.com/torvalds"},
		{"https", "https://github.com/torvalds", "https://github.com/torvalds"},
		{"http переписывается в https", "http://github.com/torvalds", "https://github.com/torvalds"},
		{"www", "www.github.com/torvalds", "https://github.com/torvalds"},
		{"www с https", "https://www.github.com/torvalds", "https://github.com/torvalds"},
		{"слеш в конце", "https://github.com/torvalds/", "https://github.com/torvalds"},
		{"регистр схемы и хоста", "HTTPS://GitHub.COM/Torvalds", "https://github.com/Torvalds"},
		{"регистр логина сохраняется", "github.com/Elena-Orlova", "https://github.com/Elena-Orlova"},
		{"пробелы по краям", "  github.com/torvalds \t", "https://github.com/torvalds"},
		{"перевод строки по краям", "\ngithub.com/torvalds\r\n", "https://github.com/torvalds"},
		{"цифры", "github.com/1337", "https://github.com/1337"},
		{"один знак", "github.com/a", "https://github.com/a"},
		{"дефис внутри", "github.com/a-b-c", "https://github.com/a-b-c"},
		{"39 знаков", "github.com/" + long, "https://github.com/" + long},
		{"пусто", "", ""},
		{"только пробелы", " \t ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, why := normalizeGitHub(tc.in)
			if why != githubOK || got != tc.want {
				t.Errorf("%q -> %q, причина %d; ждали %q", tc.in, got, why, tc.want)
			}
		})
	}
}

func TestNormalizeGitHubRejected(t *testing.T) {
	tests := []struct {
		name string
		in   string
		why  githubReject
	}{
		// Хост.
		{"поддомен-приманка", "https://github.com.evil.ru/torvalds", githubBadLink},
		{"адрес чужого сайта с путём github.com", "https://evil.ru/github.com/torvalds", githubBadLink},
		{"без схемы, чужой сайт с путём github.com", "evil.ru/github.com/torvalds", githubBadLink},
		{"логин в адресе", "https://github.com@evil.ru", githubBadLink},
		{"логин и пароль", "https://user:pass@github.com/torvalds", githubBadLink},
		{"имя до @ похоже на github", "https://github.com@evil.ru/torvalds", githubBadLink},
		{"порт", "github.com:8080/torvalds", githubBadLink},
		{"порт со схемой", "https://github.com:443/torvalds", githubBadLink},
		{"другой поддомен", "https://gist.github.com/torvalds", githubBadLink},
		{"api.github.com", "https://api.github.com/users/torvalds", githubBadLink},
		{"похожий хост", "https://githuh.com/torvalds", githubBadLink},
		{"нет github", "https://gitlab.com/torvalds", githubBadLink},
		{"хост без точки", "https://github/torvalds", githubBadLink},
		{"точка в конце хоста", "https://github.com./torvalds", githubBadLink},
		{"только хост", "github.com", githubBadLink},
		{"хост со слешем", "https://github.com/", githubBadLink},
		{"два слеша после схемы", "https:///github.com/torvalds", githubBadLink},
		{"схема без слешей", "https:github.com/torvalds", githubBadLink},
		{"схемы ftp", "ftp://github.com/torvalds", githubBadLink},
		{"схема javascript", "javascript:alert(1)", githubBadLink},
		{"javascript с github", "javascript://github.com/torvalds%0Aalert(1)", githubBadLink},
		{"схема data", "data:text/html,<script>alert(1)</script>", githubBadLink},
		{"схема file", "file:///etc/passwd", githubBadLink},
		{"схема относительная", "//github.com/torvalds", githubBadLink},
		{"логин без хоста", "torvalds", githubBadLink},
		{"с @ перед логином", "@torvalds", githubBadLink},
		// Путь.
		{"запрос", "https://github.com/torvalds?tab=repositories", githubBadLink},
		{"якорь", "https://github.com/torvalds#readme", githubBadLink},
		{"процентное кодирование", "https://github.com/tor%76alds", githubBadLink},
		{"процентный слеш", "https://github.com/a%2Fb", githubBadLink},
		{"обратный слеш", "https://github.com\\@evil.ru/torvalds", githubBadLink},
		{"точка с запятой", "https://github.com/torvalds;evil", githubBadLink},
		{"две точки в пути", "https://github.com/../torvalds", githubBadLink},
		{"одна точка", "https://github.com/./torvalds", githubBadLink},
		{"двойной слеш", "https://github.com//torvalds", githubBadLink},
		{"два слеша в конце", "https://github.com/torvalds//", githubBadLink},
		{"точка в логине", "https://github.com/tor.valds", githubBadLink},
		{"подчёркивание", "https://github.com/tor_valds", githubBadLink},
		// Логин.
		{"начинается с дефиса", "github.com/-torvalds", githubBadLink},
		{"кончается дефисом", "github.com/torvalds-", githubBadLink},
		{"два дефиса подряд", "github.com/tor--valds", githubBadLink},
		{"только дефис", "github.com/-", githubBadLink},
		{"40 знаков", "github.com/" + strings.Repeat("a", 40), githubBadLink},
		// Юникод и управляющие знаки.
		{"кириллическая «а» вместо латинской", "https://github.com/tor\u0430lds", githubBadLink},
		{"кириллическая «о» в хосте", "https://github.c\u043em/torvalds", githubBadLink},
		{"полноширинная точка", "https://github\uff0ecom/torvalds", githubBadLink},
		{"полноширинный слеш", "https://github.com\uff0ftorvalds", githubBadLink},
		{"знак нулевой ширины", "https://github.com/tor\u200bvalds", githubBadLink},
		{"punycode-хост", "https://xn--githb-0qa.com/torvalds", githubBadLink},
		{"punycode как логин", "github.com/xn--80ak6aa92e", githubBadLink},
		{"неразрывный пробел", "github.com/tor\u00a0valds", githubBadLink},
		{"пробел внутри", "github.com/tor valds", githubBadLink},
		{"пробел перед слешем", "github.com /torvalds", githubBadLink},
		{"табуляция внутри", "github.com/tor\tvalds", githubBadLink},
		{"перевод строки внутри", "github.com/torvalds\nevil.ru", githubBadLink},
		{"возврат каретки внутри", "github.com/tor\rvalds", githubBadLink},
		{"нулевой знак", "github.com/tor\x00valds", githubBadLink},
		{"знак DEL", "github.com/tor\x7Fvalds", githubBadLink},
		{"невалидный UTF-8", "github.com/\xff\xfe", githubBadLink},
		{"две ссылки через пробел", "github.com/torvalds github.com/evil", githubBadLink},
		{"очень длинная строка", "github.com/" + strings.Repeat("a", 5000), githubBadLink},
		// Репозитории и служебные страницы.
		{"репозиторий", "https://github.com/torvalds/linux", githubIsRepo},
		{"репозиторий без схемы", "github.com/torvalds/linux/", githubIsRepo},
		{"раздел репозитория", "https://github.com/torvalds/linux/issues", githubIsRepo},
		{"служебный orgs", "https://github.com/orgs", githubReserved},
		{"служебный settings", "github.com/settings", githubReserved},
		{"служебный login", "github.com/login", githubReserved},
		{"служебный features, другой регистр", "github.com/Features", githubReserved},
		{"служебный путь с продолжением", "https://github.com/settings/profile", githubBadLink},
		{"служебный orgs с продолжением", "https://github.com/orgs/acme/people", githubBadLink},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, why := normalizeGitHub(tc.in)
			if why != tc.why || got != "" {
				t.Errorf("%q -> %q, причина %d; ждали отказ с причиной %d", tc.in, got, why, tc.why)
			}
		})
	}
}

// checkGitHubInvariant — то, что должно быть верно для любого ввода: принятое имеет ровно один вид и разбирается само в
// себя, а отказ не несёт значения.
func checkGitHubInvariant(t *testing.T, in string) {
	t.Helper()
	got, why := normalizeGitHub(in)
	if why != githubOK {
		if got != "" {
			t.Fatalf("%q: отказ с непустым значением %q", in, got)
		}
		return
	}
	if got == "" {
		// Пусто принимается только для пустого ввода.
		if strings.TrimSpace(in) != "" {
			t.Fatalf("%q: непустой ввод дал пустое значение без отказа", in)
		}
		return
	}
	login, ok := strings.CutPrefix(got, "https://github.com/")
	if !ok || !validGitHubLogin(login) || githubReservedPaths[strings.ToLower(login)] {
		t.Fatalf("%q: на выходе не профиль GitHub: %q", in, got)
	}
	if !utf8.ValidString(got) || len(got) > 19+githubMaxLogin {
		t.Fatalf("%q: выход вне допустимого вида: %q", in, got)
	}
	if again, why := normalizeGitHub(got); why != githubOK || again != got {
		t.Fatalf("%q: повторный разбор %q даёт %q (%d)", in, got, again, why)
	}
}

// Для нескольких тысяч сгенерированных строк (без случайности: перебор сочетаний кусочков) выполняется инвариант.
func TestNormalizeGitHubInvariantOnCombinations(t *testing.T) {
	schemes := []string{"", "https://", "http://", "HTTPS://", "ftp://", "javascript:", "//", "https:/", "https:///"}
	hosts := []string{"github.com", "www.github.com", "GitHub.COM", "github.com.evil.ru", "evil.ru", "github.com:8080", "user@github.com", "githuh.com", "gist.github.com", "github.c\u043em", ""}
	paths := []string{"", "/", "/torvalds", "/torvalds/", "/a-b", "/-a", "/a-", "/a--b", "/settings", "/torvalds/linux", "/tor\u0430lds", "/torvalds?x=1", "/torvalds#a", "/%61", "/tor valds", "/..", "//torvalds", "/" + strings.Repeat("z", 39), "/" + strings.Repeat("z", 40)}
	suffixes := []string{"", " ", "\n", "/", "@evil.ru"}
	seen := 0
	for _, sc := range schemes {
		for _, h := range hosts {
			for _, p := range paths {
				for _, sf := range suffixes {
					checkGitHubInvariant(t, sc+h+p+sf)
					seen++
				}
			}
		}
	}
	if seen < 1000 {
		t.Fatalf("перебрано слишком мало сочетаний: %d", seen)
	}
}

// FuzzNormalizeGitHub: на любой вход разборщик не паникует и держит инвариант (принятое - всегда профиль GitHub).
func FuzzNormalizeGitHub(f *testing.F) {
	for _, s := range []string{
		"", "github.com/torvalds", "https://github.com/torvalds/", "http://www.github.com/a-b", "github.com.evil.ru/x",
		"evil.ru/github.com/x", "https://github.com@evil.ru", "github.com:8080", "javascript:alert(1)", "data:text/html,x",
		"https://github.com/tor\u0430lds", "github.com/a b", "github.com/a\nb", "github.com/%2e%2e", "github.com/-a", "github.com/a--b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkGitHubInvariant(t, in)
	})
}

func TestValidGitHubLogin(t *testing.T) {
	for in, want := range map[string]bool{
		"a": true, "A1": true, "a-b": true, "0": true, "": false, "-": false, "a-": false, "-a": false, "a--b": false,
		"a_b": false, "a.b": false, "я": false, strings.Repeat("a", 39): true, strings.Repeat("a", 40): false,
	} {
		if got := validGitHubLogin(in); got != want {
			t.Errorf("validGitHubLogin(%q) = %v, ждали %v", in, got, want)
		}
	}
}

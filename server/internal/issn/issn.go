// Package issn проверяет и приводит к одному виду ISSN журнала (срез 14). Отдельный пакет, потому что ISSN нужен и
// справочнику журналов (internal/journals), и профилям (internal/profiles), а они друг от друга не зависят.
package issn

import "strings"

// Normalize приводит ISSN к виду «1234-567X»: принимает запись с дефисом и без, с пробелами, приставкой «ISSN»
// и строчной «x». ok == false, если это не ISSN или не сходится контрольная цифра (ISO 3297: веса 8…2, остаток по 11).
func Normalize(s string) (string, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimSpace(strings.TrimPrefix(s, "ISSN"))
	s = strings.TrimPrefix(s, ":")
	s = strings.NewReplacer("-", "", " ", "", "‐", "", "‑", "", "–", "").Replace(s)
	if len(s) != 8 {
		return "", false
	}
	sum := 0
	for i := 0; i < 7; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return "", false
		}
		sum += int(c-'0') * (8 - i)
	}
	check := (11 - sum%11) % 11
	want := byte('0' + check)
	if check == 10 {
		want = 'X'
	}
	if s[7] != want {
		return "", false
	}
	return s[:4] + "-" + s[4:], true
}

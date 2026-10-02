package orgs

import (
	"strconv"
	"strings"
)

// Адрес организации строится из названия: русские буквы переводятся в латиницу, всё остальное становится дефисом.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
}

const (
	maxSlugBase  = 60
	fallbackSlug = "org"
)

// reservedSlugs — адреса, которые заняты страницами сайта (/organizations/new).
var reservedSlugs = map[string]bool{"new": true}

// slugify делает из названия основу адреса: строчная латиница, цифры и одиночные дефисы, не длиннее 60 знаков.
func slugify(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(name) {
		piece, known := translit[r]
		if !known {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				pendingDash = b.Len() > 0
				continue
			}
			piece = string(r)
		}
		if piece == "" {
			continue // мягкий и твёрдый знаки в адрес не попадают
		}
		if pendingDash {
			b.WriteByte('-')
			pendingDash = false
		}
		b.WriteString(piece)
	}
	s := b.String()
	if len(s) > maxSlugBase {
		s = s[:maxSlugBase]
		if i := strings.LastIndexByte(s, '-'); i > maxSlugBase/2 {
			s = s[:i]
		}
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		return fallbackSlug
	}
	return s
}

// slugCandidate: основа для n = 1, основа-2, основа-3 и так далее; адреса из reservedSlugs пропускаются.
func slugCandidate(base string, n int) string {
	if n <= 1 && !reservedSlugs[base] {
		return base
	}
	if n <= 1 {
		n = 2
	}
	return base + "-" + strconv.Itoa(n)
}

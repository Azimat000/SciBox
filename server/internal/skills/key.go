// Package skills — справочник навыков для подсказок в профиле (срез 33, D-211): разбор файла справочника, загрузка
// заменой целиком, поиск подсказок (начало слова, словоформы, опечатки) и приведение навыков человека к
// каноническому написанию.
//
// Профиль хранит навыки строками (profiles.research_skills, general_skills; D-136), справочник на него не ссылается
// ключами: замена справочника профили не ломает. Навык, которого нет в справочнике, остаётся «своим» навыком человека
// и в чужих подсказках не показывается (D-212).
package skills

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Виды навыков (D-136): научные (методы, приборы, программы, области) и общие.
const (
	KindResearch = "research"
	KindGeneral  = "general"
)

// ValidKind — известный вид навыка.
func ValidKind(kind string) bool { return kind == KindResearch || kind == KindGeneral }

// MaxLen — длина названия навыка в знаках (D-136); справочник и профиль держат один предел.
const MaxLen = 60

// Key — ключ сравнения навыков: без различий регистра, «ё» и «е», видов тире и пробелов. Два написания с одним
// ключом — один навык (поэтому «Спектроскопия ЯМР» и «спектроскопия  ямр» не повторяются в профиле).
// Ключ не обрезает знаки препинания и не склеивает слова: «C++» и «C» разные навыки.
func Key(s string) string {
	s = strings.ToValidUTF8(s, " ")
	var b strings.Builder
	b.Grow(len(s))
	space := true // пробел в начале и подряд идущие не пишутся
	for _, r := range s {
		switch {
		case r == 'ё' || r == 'Ё':
			r = 'е'
		case isDash(r):
			r = '-'
		case unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u200b' || r == '\ufeff':
			r = ' '
		default:
			r = unicode.ToLower(r)
		}
		if r == ' ' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}

// isDash — знаки, которые люди ставят вместо дефиса: «масс‑спектрометрия», «масс–спектрометрия».
func isDash(r rune) bool {
	switch r {
	case '‐', '‑', '‒', '–', '—', '―', '−':
		return true
	}
	return false
}

// keepInWord — знаки, которые остаются внутри слова при поиске: «C++», «C#».
func keepInWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#'
}

// Words делит ключ на слова: всё, кроме букв, цифр, «+» и «#», разделяет («масс-спектрометрия» → «масс»,
// «спектрометрия»; «.NET» → «net»).
func Words(key string) []string {
	return strings.FieldsFunc(key, func(r rune) bool { return !keepInWord(r) })
}

// WordsText — слова ключа через пробел: так они лежат в skill_terms.words.
func WordsText(key string) string { return strings.Join(Words(key), " ") }

// Limits запроса подсказки.
const (
	// MaxQuery — длиннее запрос обрезается.
	MaxQuery = MaxLen
	// maxWords — больше слов в запросе не учитываем.
	maxWords = 6
)

// query — разобранный запрос подсказки.
type query struct {
	key    string
	prefix string   // key с экранированием для LIKE и «%» в конце
	words  []string // слова запроса, до maxWords
	tsq    string   // запрос по основам слов или пусто
}

// parseQuery готовит запрос к поиску; пустой ключ означает «искать нечего».
func parseQuery(raw string) query {
	key := Key(raw)
	if utf8.RuneCountInString(key) > MaxQuery {
		key = strings.TrimRight(string([]rune(key)[:MaxQuery]), " ")
	}
	q := query{key: key}
	if key == "" {
		return q
	}
	q.prefix = escapeLike(key) + "%"
	words := Words(key)
	if len(words) > maxWords {
		words = words[:maxWords]
	}
	q.words = words
	// to_tsquery понимает служебные знаки (& | ! ( ) :), поэтому в него идут только буквы и цифры.
	// Слово со знаком «+» или «#» («C++») основ не имеет: обрезанное до «c» оно нашло бы всё, что начинается с «c».
	// Такие запросы ищутся только по началу слов.
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if strings.IndexFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) >= 0 {
			return q
		}
		parts = append(parts, w+":*")
	}
	q.tsq = strings.Join(parts, " & ")
	return q
}

// escapeLike экранирует знаки шаблона LIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ErrKind — неизвестный вид навыка в запросе.
var ErrKind = errors.New("skills: unknown kind")

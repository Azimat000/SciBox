package chat

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"scibox/server/internal/textsafe"
)

// Сообщения о неверном тексте (поле text в ответе 422).
const (
	msgEmpty    = "Напишите сообщение"
	msgTooLong  = "Сообщение не длиннее 4000 знаков"
	msgBadChars = "В сообщении есть недопустимые знаки"
)

// cleanText проверяет и приводит текст сообщения: без пробелов по краям, переносы строк сохраняются (CRLF становится LF),
// от 1 до MaxText знаков. Второе значение — что сказать человеку, пустое, если текст годится.
func cleanText(raw string) (string, string) {
	if !textsafe.Valid(raw) {
		return "", msgBadChars
	}
	text := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if text == "" {
		return "", msgEmpty
	}
	if utf8.RuneCountInString(text) > MaxText {
		return "", msgTooLong
	}
	for _, r := range text {
		if (r != '\n' && r != '\t' && unicode.IsControl(r)) || isBidiControl(r) {
			return "", msgBadChars
		}
	}
	return text, ""
}

// isBidiControl — знаки, которые меняют направление письма: ими подменяют вид адреса и имени в тексте (T-98).
func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

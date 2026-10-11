package auth

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Пределы полей.
const (
	MinPasswordLen = 10
	MaxPasswordLen = 128
	MaxNameLen     = 120
	MaxEmailLen    = 254
)

// Тексты ошибок полей: показываются человеку как есть.
//
//nolint:gosec // G101: тексты подсказок о пароле, а не сами пароли
const (
	msgNameRequired     = "Укажите, как вас зовут"
	msgNameTooLong      = "Имя слишком длинное: не больше 120 знаков"
	msgNameInvalid      = "В имени должны быть буквы"
	msgEmailRequired    = "Укажите почту"
	msgEmailInvalid     = "Похоже, в адресе почты опечатка. Он должен выглядеть так: name@example.ru"
	msgPasswordRequired = "Придумайте пароль"
	msgPasswordShort    = "Пароль слишком короткий: нужно не меньше 10 знаков"
	msgPasswordLong     = "Пароль слишком длинный: не больше 128 знаков"
	msgPasswordWeak     = "Этот пароль слишком простой, его легко угадать. Возьмите длинную фразу или набор слов"
	msgPasswordEmail    = "Пароль не должен совпадать с почтой"
	msgPasswordWrong    = "Пароль указан неверно"
	msgEmailSame        = "Это и так почта вашего аккаунта"
	msgConsentRequired  = "Без согласия на обработку персональных данных мы не можем создать аккаунт"
)

// ValidationError — одно или несколько полей не прошли проверку.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "auth: validation failed" }

func newValidation(field, message string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: message}}
}

// NormalizeEmail приводит почту к виду для хранения и проверяет, что она похожа на адрес.
func NormalizeEmail(raw string) (string, string) {
	email := strings.TrimSpace(raw)
	if email == "" {
		return "", msgEmailRequired
	}
	// «?», «&», «=», «#», «%» формально допустимы в локальной части, но в ссылке mailto: превращают адрес в параметры
	// (bcc, body, subject): контактную почту в профиле открывают сотрудники организаций (срез 29, WEB-4).
	if len(email) > MaxEmailLen || strings.ContainsAny(email, " \t\r\n<>,;\"?&=#%") {
		return "", msgEmailInvalid
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", msgEmailInvalid
	}
	local, domain, _ := strings.Cut(email, "@")
	if local == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", msgEmailInvalid
	}
	return strings.ToLower(email), ""
}

// NormalizeName убирает лишние пробелы и проверяет имя.
func NormalizeName(raw string) (string, string) {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "", msgNameRequired
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return "", msgNameTooLong
	}
	hasLetter := false
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", msgNameInvalid
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	if !hasLetter {
		return "", msgNameInvalid
	}
	return name, ""
}

// ValidatePassword проверяет новый пароль. email нужен, чтобы пароль не совпадал с почтой.
func ValidatePassword(password, email string) string {
	n := passwordLen(password)
	switch {
	case n == 0:
		return msgPasswordRequired
	case n < MinPasswordLen:
		return msgPasswordShort
	case n > MaxPasswordLen:
		return msgPasswordLong
	}
	lower := strings.ToLower(password)
	if email != "" && (lower == strings.ToLower(email) || lower == strings.ToLower(strings.SplitN(email, "@", 2)[0])) {
		return msgPasswordEmail
	}
	distinct := map[rune]struct{}{}
	for _, r := range lower {
		distinct[r] = struct{}{}
	}
	if len(distinct) < 5 || isCommonPassword(lower) {
		return msgPasswordWeak
	}
	return ""
}

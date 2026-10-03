package references

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"scibox/server/internal/auth"
)

// Тексты ошибок полей.
const (
	msgNameRequired = "Укажите, как зовут рекомендателя"
	msgNameShort    = "Имя слишком короткое"
	msgNameLong     = "Имя длиннее 120 знаков"
	msgRelLong      = "Пояснение длиннее 120 знаков"
	msgSelf         = "Нельзя просить рекомендацию у себя: укажите почту другого человека"
	msgDuplicate    = "Эта почта уже есть среди рекомендателей"
)

// validateReferee проверяет одного рекомендателя; forbidden — почты, которые указывать нельзя (свои), в нижнем регистре.
func validateReferee(in RefereeInput, forbidden []string) (Referee, map[string]string) {
	errs := map[string]string{}
	name := strings.Join(strings.Fields(in.Name), " ")
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		errs["name"] = msgNameRequired
	case n < minNameRunes:
		errs["name"] = msgNameShort
	case n > maxNameRunes:
		errs["name"] = msgNameLong
	}
	email, msg := auth.NormalizeEmail(in.Email)
	if msg != "" {
		errs["email"] = msg
	} else {
		for _, f := range forbidden {
			if strings.EqualFold(f, email) {
				errs["email"] = msgSelf
			}
		}
	}
	rel := strings.Join(strings.Fields(in.Relation), " ")
	if utf8.RuneCountInString(rel) > maxRelRunes {
		errs["relation"] = msgRelLong
	}
	return Referee{Name: name, Email: email, Relation: rel}, errs
}

// Normalize проверяет список рекомендателей в отклике. Возвращает проверенный список и одно сообщение для поля
// `referees` (пусто, если всё в порядке): первая найденная ошибка с номером рекомендателя.
func Normalize(in []RefereeInput, forbidden ...string) ([]Referee, string) {
	if len(in) > MaxPerApplication {
		return nil, fmt.Sprintf("Можно указать не больше %d рекомендателей", MaxPerApplication)
	}
	out := make([]Referee, 0, len(in))
	seen := map[string]bool{}
	for i, r := range in {
		ref, errs := validateReferee(r, forbidden)
		for _, field := range []string{"name", "email", "relation"} {
			if msg, bad := errs[field]; bad {
				return nil, fmt.Sprintf("Рекомендатель %d: %s", i+1, lowerFirst(msg))
			}
		}
		if seen[ref.Email] {
			return nil, fmt.Sprintf("Рекомендатель %d: %s", i+1, lowerFirst(msgDuplicate))
		}
		seen[ref.Email] = true
		out = append(out, ref)
	}
	return out, ""
}

// lowerFirst: «Укажите…» → «укажите…» (по знакам, а не по байтам).
func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToLower(string(r)) + s[n:]
}

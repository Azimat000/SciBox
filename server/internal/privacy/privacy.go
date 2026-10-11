// Package privacy решает, кто видит профиль учёного и его контакты (D-010). Это критичная зона (docs/TESTING.md):
// ошибка здесь открывает скрытый профиль или контакты чужим людям. Пакет не знает про базу и HTTP; все остальные
// пакеты спрашивают про приватность только здесь.
package privacy

import (
	"strconv"
	"strings"
)

// Visibility — кому виден профиль. Строки лежат в базе, не менять.
type Visibility string

const (
	// Hidden — профиль скрыт: его видит только владелец. По умолчанию.
	Hidden Visibility = "hidden"
	// Anonymous — сотрудникам организаций виден только научный облик (степень, специальности, метрики): без имени, места
	// работы и всего, по чему человека легко узнать (D-199). Имя открывается организации, на вакансию которой человек
	// откликнулся или приглашение которой принял словом «Интересно».
	Anonymous Visibility = "anonymous"
	// Orgs — профиль виден сотрудникам организаций (любой роли), вошедшим в аккаунт.
	Orgs Visibility = "orgs"
	// Public — профиль виден всем, в том числе без входа.
	Public Visibility = "public"
)

// Modes — все режимы от закрытого к открытому.
var Modes = []Visibility{Hidden, Anonymous, Orgs, Public}

// Parse превращает строку в режим; ok == false, если такого режима нет.
func Parse(s string) (Visibility, bool) {
	switch v := Visibility(s); v {
	case Hidden, Anonymous, Orgs, Public:
		return v, true
	}
	return "", false
}

// Viewer — кто смотрит профиль. Нулевое значение — аноним без входа.
type Viewer struct {
	// Owner — человек смотрит собственный профиль.
	Owner bool
	// Staff — человек вошёл в аккаунт и состоит хотя бы в одной организации.
	Staff bool
	// Blocked — хотя бы в одной из организаций смотрящего учёный скрыл профиль (D-200): ни в каком режиме этот человек
	// профиля не видит. Имеет смысл только вместе со Staff; без Staff значение игнорируется.
	Blocked bool
	// Revealed — хотя бы одной организации смотрящего учёный открыл имя (откликнулся или ответил «Интересно», D-201).
	Revealed bool
}

// Disclosure — сколько о человеке видит смотрящий.
type Disclosure int

const (
	// Closed — профиля для смотрящего нет (неотличим от несуществующего).
	Closed Disclosure = iota
	// Masked — только научный облик, без имени и всего узнаваемого (режим Anonymous, имя не открыто).
	Masked
	// Open — профиль целиком, как его видит этот смотрящий.
	Open
)

// Disclose — единственное место, где решается, сколько видит смотрящий. Неизвестный режим закрыт для всех, кроме
// владельца (лучше спрятать лишнее, чем показать скрытое). Скрытие от организации сильнее любого режима, но владельца
// не касается.
func (v Visibility) Disclose(who Viewer) Disclosure {
	if who.Owner {
		return Open
	}
	if who.Staff && who.Blocked {
		return Closed
	}
	switch v {
	case Public:
		return Open
	case Orgs:
		if who.Staff {
			return Open
		}
	case Anonymous:
		switch {
		case !who.Staff:
		case who.Revealed:
			return Open
		default:
			return Masked
		}
	}
	return Closed
}

// CanView — может ли этот человек открыть профиль (хотя бы в виде анонимной карточки).
func (v Visibility) CanView(who Viewer) bool { return v.Disclose(who) != Closed }

// CanSeeContacts — контактную почту видит владелец и сотрудники организаций, если профиль им виден целиком.
// Аноним и вошедший без организации контактов не видят даже у публичного профиля: их нельзя собрать парсером.
func (v Visibility) CanSeeContacts(who Viewer) bool {
	return v.Disclose(who) == Open && (who.Owner || who.Staff)
}

// CatalogModes — профили в каких режимах видит в каталоге учёных такой смотрящий (D-097). Решает Disclose, своих
// правил здесь нет. Собственный профиль в каталог не попадает, поэтому владельцем смотрящий не считается. Скрытие от
// организаций и раскрытие имени зависят от самого профиля и проверяются запросом по каждой строке, здесь они не видны.
func CatalogModes(who Viewer) []Visibility {
	who.Owner, who.Blocked, who.Revealed = false, false, false
	var out []Visibility
	for _, m := range Modes {
		if m.CanView(who) {
			out = append(out, m)
		}
	}
	return out
}

// MaskedFacts — научный облик анонимного профиля: всё, что известно организации, пока имя не открыто (D-199).
type MaskedFacts struct {
	Degree      string   // none | candidate | doctor
	Level       int      // карьерный уровень R1–R4
	HIndex      int      // лучшее из введённых значений; 0 — не указан
	Specialties []string // коды ВАК
}

// maxLabelSpecialties — сколько специальностей попадает в подпись.
const maxLabelSpecialties = 2

// MaskedLabel — подпись анонимной карточки: «Кандидат наук, 1.4.4, R2, h-index 9». Единственный текст, которым
// сервер называет такого человека в списках, уведомлениях и письмах вместо имени.
func MaskedLabel(f MaskedFacts) string {
	parts := make([]string, 0, 5)
	switch f.Degree {
	case "candidate":
		parts = append(parts, "Кандидат наук")
	case "doctor":
		parts = append(parts, "Доктор наук")
	default:
		parts = append(parts, "Без учёной степени")
	}
	for i, code := range f.Specialties {
		if i == maxLabelSpecialties {
			break
		}
		parts = append(parts, code)
	}
	if f.Level >= 1 && f.Level <= 4 {
		parts = append(parts, "R"+strconv.Itoa(f.Level))
	}
	if f.HIndex > 0 {
		parts = append(parts, "h-index "+strconv.Itoa(f.HIndex))
	}
	return strings.Join(parts, ", ")
}

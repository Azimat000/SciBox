// Package privacy решает, кто видит профиль учёного и его контакты (D-010). Это критичная зона (docs/TESTING.md):
// ошибка здесь открывает скрытый профиль или контакты чужим людям. Пакет не знает про базу и HTTP; все остальные
// пакеты спрашивают про приватность только здесь.
package privacy

// Visibility — кому виден профиль. Строки лежат в базе, не менять.
type Visibility string

const (
	// Hidden — профиль скрыт: его видит только владелец. По умолчанию.
	Hidden Visibility = "hidden"
	// Orgs — профиль виден сотрудникам организаций (любой роли), вошедшим в аккаунт.
	Orgs Visibility = "orgs"
	// Public — профиль виден всем, в том числе без входа.
	Public Visibility = "public"
)

// Modes — все режимы от закрытого к открытому.
var Modes = []Visibility{Hidden, Orgs, Public}

// Parse превращает строку в режим; ok == false, если такого режима нет.
func Parse(s string) (Visibility, bool) {
	switch v := Visibility(s); v {
	case Hidden, Orgs, Public:
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
}

// CanView — может ли этот человек открыть профиль в таком режиме. Неизвестный режим закрыт для всех, кроме владельца
// (лучше спрятать лишнее, чем показать скрытое).
func (v Visibility) CanView(who Viewer) bool {
	if who.Owner {
		return true
	}
	switch v {
	case Public:
		return true
	case Orgs:
		return who.Staff
	}
	return false
}

// CanSeeContacts — контактную почту видит владелец и сотрудники организаций, если профиль им вообще виден.
// Аноним и вошедший без организации контактов не видят даже у публичного профиля: их нельзя собрать парсером.
func (v Visibility) CanSeeContacts(who Viewer) bool {
	return v.CanView(who) && (who.Owner || who.Staff)
}

// CatalogModes — профили в каких режимах видит в каталоге учёных такой смотрящий (D-097). Решает CanView, своих
// правил здесь нет. Собственный профиль в каталог не попадает, поэтому владельцем смотрящий не считается.
func CatalogModes(who Viewer) []Visibility {
	who.Owner = false
	var out []Visibility
	for _, m := range Modes {
		if m.CanView(who) {
			out = append(out, m)
		}
	}
	return out
}

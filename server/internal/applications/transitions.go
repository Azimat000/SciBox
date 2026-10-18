package applications

// Переходы отклика, которые делает организация (D-088). Автор отклика только отзывает его (withdrawable в model.go).
// Таблица одна: и сервис, и подсказки для сайта берут допустимое отсюда.
//
//	sent   → viewed (само, когда карточку открыли), rejected
//	viewed → rejected, accepted
//
// rejected, accepted и withdrawn конечные: пересмотреть решение нельзя. Приглашений внутри отклика нет (D-256):
// о собеседовании договариваются в переписке, которая открыта до решения и после «принят».
var staffMoves = map[string][]string{
	StatusSent:   {StatusViewed, StatusRejected},
	StatusViewed: {StatusRejected, StatusAccepted},
}

// decisions — статусы, которые организация ставит сама кнопкой (просмотр получается как следствие).
var decisions = []string{StatusRejected, StatusAccepted}

// CanStaffMove — может ли организация перевести отклик из from в to.
func CanStaffMove(from, to string) bool {
	for _, s := range staffMoves[from] {
		if s == to {
			return true
		}
	}
	return false
}

// CanDecide — можно ли поставить решение to (отказ или принят) отклику в статусе from.
func CanDecide(from, to string) bool {
	return isOneOf(to, decisions) && CanStaffMove(from, to)
}

// DecisionsFrom — какие решения доступны из статуса from (для кнопок на странице), в порядке «принять, отказать».
func DecisionsFrom(from string) []string {
	out := []string{}
	for _, to := range []string{StatusAccepted, StatusRejected} {
		if CanDecide(from, to) {
			out = append(out, to)
		}
	}
	return out
}

// fromStatuses — из каких статусов разрешён переход в to; нужно запросу, который меняет статус только из ожидаемых.
func fromStatuses(to string) []string {
	var out []string
	for _, from := range Statuses {
		if CanStaffMove(from, to) {
			out = append(out, from)
		}
	}
	return out
}

func isOneOf(s string, list []string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

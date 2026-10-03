package vacancies

// transitions — единственная таблица допустимых смен статуса: откуда → куда.
//
//	черновик → опубликована → закрыта → архив
//	закрыта → опубликована (повторное открытие), архив → закрыта (возврат из архива)
//
// Из черновика сразу в архив нельзя (черновик удаляют); опубликованную сначала закрывают.
var transitions = map[string][]string{
	StatusDraft:     {StatusPublished},
	StatusPublished: {StatusClosed},
	StatusClosed:    {StatusPublished, StatusArchived},
	StatusArchived:  {StatusClosed},
}

// CanTransition отвечает, можно ли перевести вакансию из статуса from в статус to.
func CanTransition(from, to string) bool {
	for _, next := range transitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

// NextStatuses — куда можно перевести вакансию из статуса from.
func NextStatuses(from string) []string {
	return append([]string{}, transitions[from]...)
}

// PubliclyVisible — вакансию видят все: опубликованную и закрытую (по ссылке). Черновик и архив видят только те, кто ведёт вакансии.
func PubliclyVisible(status string) bool {
	return status == StatusPublished || status == StatusClosed
}

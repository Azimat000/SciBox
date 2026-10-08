package vacancies

import "scibox/server/internal/audit"

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

// auditAction — какая запись попадает в журнал при смене статуса. Каждая допустимая пара из таблицы transitions
// имеет своё действие (проверяет тест): новую пару нельзя добавить, не решив, как она выглядит в журнале.
var auditAction = map[[2]string]audit.Action{
	{StatusDraft, StatusPublished}:  audit.VacancyPublished,
	{StatusPublished, StatusClosed}: audit.VacancyClosed,
	{StatusClosed, StatusPublished}: audit.VacancyReopened,
	{StatusClosed, StatusArchived}:  audit.VacancyArchived,
	{StatusArchived, StatusClosed}:  audit.VacancyRestored,
}

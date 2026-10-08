package applications

// Переходы отклика, которые делает организация (D-088). Автор отклика только отзывает его (withdrawable в model.go).
// Таблица одна: и сервис, и подсказки для сайта берут допустимое отсюда.
//
//	sent    → viewed (само, когда карточку открыли), invited (приглашение), rejected
//	viewed  → invited, rejected, accepted
//	invited → rejected, accepted           (новое приглашение статус не меняет)
//
// rejected, accepted и withdrawn конечные: пересмотреть решение нельзя.
var staffMoves = map[string][]string{
	StatusSent:    {StatusViewed, StatusInvited, StatusRejected},
	StatusViewed:  {StatusInvited, StatusRejected, StatusAccepted},
	StatusInvited: {StatusRejected, StatusAccepted},
}

// decisions — статусы, которые организация ставит сама кнопкой (остальные получаются как следствие: просмотр, приглашение).
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

// CanInvite — можно ли отправить приглашение отклику в статусе from (в том числе повторное, когда он уже «приглашён»).
func CanInvite(from string) bool { return from == StatusInvited || CanStaffMove(from, StatusInvited) }

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

// ---- приглашения ----

// Виды приглашений (D-011).
const (
	InvInterview = "interview"        // собеседование: дата и время, ссылка или адрес
	InvContacts  = "contacts"         // организация сообщает свои контакты
	InvRequest   = "request_contacts" // организация просит оставить контакты и удобное время
)

// InvitationKinds — все виды.
var InvitationKinds = []string{InvInterview, InvContacts, InvRequest}

// Состояния приглашения.
const (
	InvPending   = "pending"   // ждёт ответа соискателя
	InvConfirmed = "confirmed" // собеседование подтверждено (соискателем или организацией, приняв его время)
	InvProposed  = "proposed"  // соискатель предложил другое время, ждёт организацию
	InvAnswered  = "answered"  // соискатель оставил контакты
	InvShared    = "shared"    // контакты организации переданы, ответа не требуется
	InvCancelled = "cancelled" // отменено организацией, заменено новым или закрыто решением
)

// InvitationStatuses — все состояния.
var InvitationStatuses = []string{InvPending, InvConfirmed, InvProposed, InvAnswered, InvShared, InvCancelled}

// Ответы соискателя.
const (
	AnswerConfirm = "confirm" // собеседование: подходит
	AnswerPropose = "propose" // собеседование: предложить другое время
	AnswerReply   = "reply"   // просьба оставить контакты: контакты и удобное время
)

// openInvitation — состояния, из которых приглашение можно отменить (организацией или решением по отклику).
var openInvitation = []string{InvPending, InvProposed, InvConfirmed}

// CanCancelInvitation — можно ли отменить приглашение в таком состоянии.
func CanCancelInvitation(status string) bool { return isOneOf(status, openInvitation) }

// InitialInvitationStatus — с какого состояния начинается приглашение вида kind (пусто — вида нет).
func InitialInvitationStatus(kind string) string {
	switch kind {
	case InvInterview, InvRequest:
		return InvPending
	case InvContacts:
		return InvShared
	}
	return ""
}

// AnswerTarget — в какое состояние приглашение вида kind переходит от ответа action; ok == false, если такой ответ
// этому виду не положен. Отвечать можно один раз, пока приглашение ждёт ответа (InvPending): это защита
// организации от потока писем (каждый ответ уведомляет всех, кто разбирает отклики).
func AnswerTarget(kind, action string) (status string, ok bool) {
	switch {
	case kind == InvInterview && action == AnswerConfirm:
		return InvConfirmed, true
	case kind == InvInterview && action == AnswerPropose:
		return InvProposed, true
	case kind == InvRequest && action == AnswerReply:
		return InvAnswered, true
	}
	return "", false
}

// CanAnswerInvitation — ждёт ли приглашение ответа соискателя.
func CanAnswerInvitation(status string) bool { return status == InvPending }

// CanAcceptProposal — можно ли принять предложенное время (только собеседование, где соискатель предложил своё).
func CanAcceptProposal(kind, status string) bool {
	return kind == InvInterview && status == InvProposed
}

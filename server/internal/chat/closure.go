package chat

// State — в каком состоянии переписка по отклику и приглашению. Закрытие не хранится, оно следует из состояния
// (CHAT.md, «Кто и когда пишет»): отказ, отзыв отклика, «Не сейчас» и отзыв приглашения закрывают переписку для обеих
// сторон, «Принят» её не закрывает.
type State struct {
	// Source и Value — состояние отклика, а пока отклика нет, приглашения на вакансию (contracts/chat/enums.json).
	Source string
	Value  string
	// Closed — писать, править, удалять и печатать нельзя.
	Closed bool
	// Reason — причина закрытия (closed_reason), пустая у открытой.
	Reason string
}

// StateOf определяет состояние по статусу отклика и статусу приглашения; nil значит «такого нет». blocked — учёный
// заблокировал организацию вакансии (срез 38, D-237): переписка закрывается, если её не закрыло что-то из состояния
// отклика или приглашения (решение организации остаётся причиной, раз оно было раньше).
// Отклик главнее приглашения: если учёный после приглашения откликнулся, переписка живёт по отклику.
func StateOf(application, offer *string, blocked bool) State {
	st := stateByStatus(application, offer)
	if blocked && !st.Closed {
		st.Closed, st.Reason = true, ClosedBlocked
	}
	return st
}

func stateByStatus(application, offer *string) State {
	switch {
	case application != nil:
		st := State{Source: sourceApplication, Value: *application}
		switch *application {
		case statusApplicationRejected:
			st.Closed, st.Reason = true, ClosedRejected
		case statusApplicationWithdrawn:
			st.Closed, st.Reason = true, ClosedWithdrawn
		}
		return st
	case offer != nil:
		st := State{Source: sourceOffer, Value: *offer}
		switch *offer {
		case statusOfferDeclined:
			st.Closed, st.Reason = true, ClosedOfferDeclined
		case statusOfferCancelled:
			st.Closed, st.Reason = true, ClosedOfferCancelled
		}
		return st
	}
	// Без отклика и приглашения переписка бы уже ушла вместе с ними (CASCADE); если такая всё же попалась, она закрыта.
	return State{Source: sourceApplication, Value: statusApplicationWithdrawn, Closed: true, Reason: ClosedWithdrawn}
}

// audience — кому сообщается каждое событие: тот, кто его не совершал. Так число непрочитанных не показывает человеку его
// же действие (отклик, ответ на приглашение).
var audience = map[string]Side{
	EventApplicationSent:      SideOrg,
	EventApplicationViewed:    SideScientist,
	EventInvitationCreated:    SideScientist,
	EventInvitationAnswered:   SideOrg,
	EventInvitationAccepted:   SideScientist,
	EventInvitationCancelled:  SideScientist,
	EventApplicationDecided:   SideScientist,
	EventApplicationWithdrawn: SideOrg,
	EventOfferCreated:         SideScientist,
	EventOfferAnswered:        SideOrg,
	EventOfferCancelled:       SideScientist,
	EventChatBlocked:          SideOrg,
	EventChatUnblocked:        SideOrg,
}

// EventTypes — все виды строк о событии.
func EventTypes() []string {
	out := make([]string, 0, len(audience))
	for t := range audience {
		out = append(out, t)
	}
	return out
}

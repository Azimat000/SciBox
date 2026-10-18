package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
)

// Строки о событиях откликов и приглашений (срез 38, D-236). Отклики и приглашения вызывают Record в транзакции самого
// события, после того как оно записано: откат события откатывает переписку и строку, повтор события строки не плодит
// (повторов у событий нет: каждое проходит один раз, дважды оно упирается в состояние отклика).
//
// Данные строк показываются обеим сторонам и лежат в базе как есть. В них нет имени, почты и ничего, по чему узнают
// анонимного учёного (T-97): только номера, виды, статусы и тексты, которые человек написал в самом событии (письмо к
// отклику, записка к ответу, сообщение приглашения). Формы данных заданы типами ниже и сверяются с
// contracts/chat/messages.json тестом `TestEventDataFollowsTheContract`.

// Entry — событие, о котором переписка дописывает строку.
type Entry struct {
	// VacancyID и UserID — пара «вакансия × учёный», которой принадлежит переписка: нужны, если переписки ещё нет.
	VacancyID uuid.UUID
	UserID    uuid.UUID
	// ApplicationID или OfferID — отклик или приглашение на вакансию, к которым относится событие.
	ApplicationID *uuid.UUID
	OfferID       *uuid.UUID
	// Type — вид строки (Event…), Data — её данные (типы *Data ниже), At — время события.
	Type string
	Data any
	At   time.Time
}

// Record пишет строку о событии в переписку пары «вакансия × учёный» и возвращает её номер в переписке. Если переписки ещё
// нет (отклик, приглашение, прежние записи до переноса), она заводится здесь же.
func Record(ctx context.Context, q *dbgen.Queries, e Entry) (int, error) {
	id, err := locate(ctx, q, e)
	if err != nil {
		return 0, err
	}
	return AppendSystem(ctx, q, id, e.Type, e.Data, e.At)
}

// locate находит переписку по отклику или приглашению, а если её нет, заводит. Пара «учёный × вакансия» одна: приглашение
// и отклик одного человека на одну вакансию живут в одной переписке.
func locate(ctx context.Context, q *dbgen.Queries, e Entry) (uuid.UUID, error) {
	var (
		id  uuid.UUID
		err error
	)
	switch {
	case e.ApplicationID != nil:
		id, err = q.FindChatByApplication(ctx, e.ApplicationID)
	case e.OfferID != nil:
		id, err = q.FindChatByOffer(ctx, e.OfferID)
	default:
		return uuid.Nil, errors.New("chat: event has neither application nor offer")
	}
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("chat: find by link: %w", err)
	}
	return Open(ctx, q, OpenInput{VacancyID: e.VacancyID, UserID: e.UserID, ApplicationID: e.ApplicationID, OfferID: e.OfferID}, e.At)
}

// ---- данные строк ----

// ApplicationSentData — отклик отправлен (application.sent).
type ApplicationSentData struct {
	ApplicationID uuid.UUID `json:"application_id"`
	CoverLetter   string    `json:"cover_letter"`
}

// ApplicationData — события, у которых из данных только отклик: просмотрен (пустые данные), отозван.
type ApplicationData struct {
	ApplicationID uuid.UUID `json:"application_id"`
}

// ApplicationDecidedData — решение организации (application.decided).
type ApplicationDecidedData struct {
	ApplicationID uuid.UUID `json:"application_id"`
	Decision      string    `json:"decision"`
	Note          string    `json:"note"`
}

// OfferCreatedData — приглашение на вакансию (offer.created). Status при создании всегда pending, дальше его меняют
// следующие строки.
type OfferCreatedData struct {
	OfferID   uuid.UUID `json:"offer_id"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CanAnswer bool      `json:"can_answer"`
}

// OfferAnsweredData — ответ учёного: interested или declined (offer.answered).
type OfferAnsweredData struct {
	OfferID uuid.UUID `json:"offer_id"`
	Answer  string    `json:"answer"`
	Note    string    `json:"note"`
}

// OfferData — события, у которых из данных только приглашение: отозвано (offer.cancelled).
type OfferData struct {
	OfferID uuid.UUID `json:"offer_id"`
}

package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Link — что показывают у отклика и приглашения про их переписку (поле chat, contracts/chat/links.json). Started — в
// ней уже есть сообщения людей: тогда кнопка «Перейти к переписке», иначе «Написать».
type Link struct {
	ID       uuid.UUID `json:"id"`
	Unread   int       `json:"unread"`
	CanWrite bool      `json:"can_write"`
	Started  bool      `json:"started"`
}

// Links — переписки, найденные по откликам и приглашениям. Нет записи, значит у отклика или приглашения переписки нет
// (поле chat остаётся пустым).
type Links struct {
	byApplication map[uuid.UUID]*Link
	byOffer       map[uuid.UUID]*Link
}

// Application — переписка отклика или nil.
func (l Links) Application(id uuid.UUID) *Link { return l.byApplication[id] }

// Offer — переписка приглашения на вакансию или nil.
func (l Links) Offer(id uuid.UUID) *Link { return l.byOffer[id] }

// LinksOf находит переписки по номерам откликов и приглашений одним запросом. Права на сами отклики и приглашения
// проверил вызывающий: он их и вернул смотрящему. side — чьими глазами считается «непрочитанное».
func LinksOf(ctx context.Context, q *dbgen.Queries, side Side, viewer uuid.UUID, applications, offers []uuid.UUID) (Links, error) {
	out := Links{byApplication: map[uuid.UUID]*Link{}, byOffer: map[uuid.UUID]*Link{}}
	if len(applications) == 0 && len(offers) == 0 {
		return out, nil
	}
	rows, err := q.ListChatLinks(ctx, dbgen.ListChatLinksParams{
		Side: string(side), ViewerID: viewer, ApplicationIds: nonNil(applications), OfferIds: nonNil(offers),
	})
	if err != nil {
		return Links{}, fmt.Errorf("chat: list links: %w", err)
	}
	for _, r := range rows {
		st := StateOf(r.ApplicationStatus, r.OfferStatus, r.Blocked)
		link := &Link{ID: r.ID, Unread: int(r.Unread), CanWrite: !st.Closed, Started: r.Started}
		if r.ApplicationID != nil {
			out.byApplication[*r.ApplicationID] = link
		}
		if r.OfferID != nil {
			out.byOffer[*r.OfferID] = link
		}
	}
	return out, nil
}

func nonNil(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

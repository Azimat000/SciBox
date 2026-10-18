package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
)

// Сборка событий потока для каждого получателя (срез 39, D-243). Вид сообщения зависит от смотрящего: свой или чужой
// (is_me, client_id, editable_until), виден ли организации учёный (D-199). Поэтому уведомление несёт только строку
// журнала, а событие для человека собирается здесь.

// Данные событий (contracts/chat/events.json).
type (
	streamMessage struct {
		ChatID  uuid.UUID `json:"chat_id"`
		Side    Side      `json:"side"`
		Message Message   `json:"message"`
	}
	streamRead struct {
		ChatID     uuid.UUID `json:"chat_id"`
		Side       Side      `json:"side"`
		ReaderSide Side      `json:"reader_side"`
		UpTo       int       `json:"up_to"`
	}
	streamTyping struct {
		ChatID uuid.UUID    `json:"chat_id"`
		Side   Side         `json:"side"`
		Author typingAuthor `json:"author"`
	}
	typingAuthor struct {
		Side      Side   `json:"side"`
		Name      string `json:"name"`
		Anonymous bool   `json:"anonymous"`
	}
	streamState struct {
		ChatID       uuid.UUID `json:"chat_id"`
		Side         Side      `json:"side"`
		Status       Status    `json:"status"`
		NeedsAnswer  bool      `json:"needs_answer"`
		CanWrite     bool      `json:"can_write"`
		ClosedReason *string   `json:"closed_reason"`
	}
)

// newFrame собирает событие потока: данные одной строкой JSON.
func newFrame(id int64, name string, data any, at time.Time) (frame, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return frame{}, fmt.Errorf("chat: marshal %s: %w", name, err)
	}
	return frame{id: id, name: name, data: b, at: at}, nil
}

// primitiveFrame собирает событие из данных, в которых только строки, числа и логические значения (без RawMessage):
// сериализация такого значения не может не удаться.
func primitiveFrame(id int64, name string, data any, at time.Time) frame {
	b, _ := json.Marshal(data)
	return frame{id: id, name: name, data: b, at: at}
}

// wire — событие в виде, как оно идёт по соединению (Server-Sent Events).
func (f frame) wire() []byte {
	out := make([]byte, 0, len(f.data)+len(f.name)+32)
	if f.id > 0 {
		out = append(out, "id: "...)
		out = appendInt(out, f.id)
		out = append(out, '\n')
	}
	out = append(out, "event: "...)
	out = append(out, f.name...)
	out = append(out, "\ndata: "...)
	out = append(out, f.data...)
	return append(out, '\n', '\n')
}

func appendInt(b []byte, n int64) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(b, tmp[i:]...)
}

// eventView — всё, что нужно, чтобы собрать событие сообщения для любого получателя: шапка переписки, сообщение,
// авторы-сотрудники.
type eventView struct {
	row   dbgen.GetChatRow
	msg   dbgen.ChatMessage
	staff map[uuid.UUID]authorInfo
}

// eventView читает переписку и сообщение события; false — их уже нет (переписка стёрта вместе с аккаунтом).
func (s *Service) eventView(ctx context.Context, ev event) (eventView, bool, error) {
	row, err := s.q.GetChat(ctx, dbgen.GetChatParams{ViewerID: uuid.Nil, ID: ev.ChatID})
	if errors.Is(err, pgx.ErrNoRows) {
		return eventView{}, false, nil
	}
	if err != nil {
		return eventView{}, false, fmt.Errorf("chat: event chat: %w", err)
	}
	msg, err := s.q.GetChatMessage(ctx, dbgen.GetChatMessageParams{ChatID: ev.ChatID, Seq: num.Int32(ev.Seq)})
	if errors.Is(err, pgx.ErrNoRows) {
		return eventView{}, false, nil
	}
	if err != nil {
		return eventView{}, false, fmt.Errorf("chat: event message: %w", err)
	}
	staff, err := s.authors(ctx, s.q, row.OrgID, []dbgen.ChatMessage{msg})
	if err != nil {
		return eventView{}, false, err
	}
	return eventView{row: row, msg: msg, staff: staff}, true, nil
}

// frames собирает события для одного получателя: само сообщение и, если это строка о событии, состояние переписки.
func (s *Service) framesFor(ev event, v eventView, side Side, viewer uuid.UUID) ([]frame, error) {
	seen := side == SideScientist || nameSeen(v.row.Visibility, v.row.HiddenFrom, v.row.Revealed)
	m := s.toMessage(ev.ChatID, v.msg, viewer, v.row.ScientistName, seen, v.staff)
	f, err := newFrame(ev.ID, ev.Kind, streamMessage{ChatID: ev.ChatID, Side: side, Message: m}, ev.CreatedAt)
	if err != nil {
		return nil, err
	}
	out := []frame{f}
	if ev.Kind == evCreated && v.msg.Kind == KindSystem {
		st := StateOf(v.row.ApplicationStatus, v.row.OfferStatus, v.row.Blocked)
		out = append(out, primitiveFrame(0, "chat.state", streamState{
			ChatID: ev.ChatID, Side: side, Status: Status{Source: st.Source, Value: st.Value},
			NeedsAnswer: needsAnswer(side, st, v.row.OfferStatus),
			CanWrite:    !st.Closed, ClosedReason: reasonPtr(st),
		}, time.Time{}))
	}
	return out, nil
}

// sideFor — кем человек приходится переписке события по своим правам: учёный переписки или сотрудник, в правах которого
// организация или подразделение её вакансии. Человек, который сам учёный этой переписки, участвует как учёный.
func sideFor(ev event, user uuid.UUID, sc scope) (Side, bool) {
	if user == ev.UserID {
		return SideScientist, true
	}
	if _, ok := sc.orgs[ev.OrgID]; ok {
		return SideOrg, true
	}
	if ev.UnitID != nil {
		if _, ok := sc.units[*ev.UnitID]; ok {
			return SideOrg, true
		}
	}
	return "", false
}

// build собирает события журнала для получателей rcpts (по порядку; пустое — этому ничего не положено) и список людей,
// у которых из-за события могло измениться число непрочитанных.
func (h *Hub) build(ctx context.Context, ev event, rcpts []target) ([][]frame, []uuid.UUID) {
	out := make([][]frame, len(rcpts))
	var dirty []uuid.UUID
	switch ev.Kind {
	case evArchived:
		if ev.ActorID != nil {
			dirty = append(dirty, *ev.ActorID)
		}
	case evRead:
		if ev.Side == nil || !Side(*ev.Side).Valid() {
			return out, nil
		}
		reader := Side(*ev.Side)
		for i, t := range rcpts {
			own := ev.ActorID != nil && t.st.user.ID == *ev.ActorID
			if !own && t.side == reader {
				// Коллега по организации: у него своя отметка, а «прочитано» для него значило бы чужое.
				continue
			}
			out[i] = []frame{primitiveFrame(ev.ID, evRead, streamRead{ChatID: ev.ChatID, Side: t.side, ReaderSide: reader, UpTo: ev.UpTo}, ev.CreatedAt)}
			if own {
				dirty = append(dirty, t.st.user.ID)
			}
		}
	case evCreated, evEdited, evDeleted:
		v, ok, err := h.svc.eventView(ctx, ev)
		if err != nil {
			if ctx.Err() == nil {
				h.log.Warn("chat event not delivered", "kind", ev.Kind, "err", err)
			}
			return out, nil
		}
		if !ok {
			return out, nil
		}
		for i, t := range rcpts {
			frames, err := h.svc.framesFor(ev, v, t.side, t.st.user.ID)
			if err != nil {
				h.log.Warn("chat event not built", "kind", ev.Kind, "err", err)
				continue
			}
			out[i] = frames
			// Новое сообщение возвращает переписку из архива у всех участников: число непрочитанных меняется у каждого.
			if ev.Kind == evCreated {
				dirty = append(dirty, t.st.user.ID)
			}
		}
	}
	return out, dirty
}

// deliver раздаёт одно событие журнала потокам копии, которым оно положено.
func (h *Hub) deliver(ctx context.Context, ev event) {
	h.obs.EventDispatched(ev.Kind)
	rcpts := h.recipients(ev)
	if len(rcpts) == 0 {
		return
	}
	per, dirty := h.build(ctx, ev, rcpts)
	for i, frames := range per {
		for _, f := range frames {
			h.offer(rcpts[i].st, f)
		}
	}
	h.markDirty(dirty...)
}

// deliverTyping показывает собеседнику, что человек печатает. Коллеге по организации не показывается: его ответ пишется
// отдельно, а подсказка «коллега печатает» в этой версии не нужна.
func (h *Hub) deliverTyping(_ context.Context, n typingNote) {
	ev := event{ChatID: n.ChatID, UserID: n.Scientist, OrgID: n.OrgID, UnitID: n.UnitID}
	for _, t := range h.recipients(ev) {
		if t.side == n.Side {
			continue
		}
		h.offer(t.st, primitiveFrame(0, "chat.typing", streamTyping{
			ChatID: n.ChatID, Side: t.side, Author: typingAuthor{Side: n.Side, Name: n.Name, Anonymous: n.Anonymous},
		}, time.Time{}))
	}
}

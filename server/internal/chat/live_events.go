package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
)

// Живое обновление чата (срез 39, D-242…D-244): как событие попадает в журнал и в уведомление PostgreSQL.

// Виды событий журнала. Строки лежат в базе (CHECK в миграции 00033) и совпадают с видами потока там, где сайт их знает.
const (
	evCreated  = "message.created"
	evEdited   = "message.edited"
	evDeleted  = "message.deleted"
	evRead     = "chat.read"
	evArchived = "chat.archived" // человек убрал переписку в архив или вернул: у его вкладок меняется число непрочитанных
)

// Каналы PostgreSQL.
const (
	channelEvents = "chat_events"
	channelTyping = "chat_typing"
)

// publish пишет событие в журнал в транзакции q и объявляет его: уведомление дойдёт только после фиксации, откат
// убирает и строку, и объявление (D-243). Организация и подразделение берутся из самой переписки.
func publish(ctx context.Context, q *dbgen.Queries, p dbgen.PublishChatEventParams) error {
	if _, err := q.PublishChatEvent(ctx, p); err != nil {
		return fmt.Errorf("chat: publish %s: %w", p.Kind, err)
	}
	return nil
}

// event — строка журнала такой, какой она приходит в уведомлении (row_to_json) и из запросов к журналу.
type event struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	ChatID    uuid.UUID  `json:"chat_id"`
	UserID    uuid.UUID  `json:"user_id"`
	OrgID     uuid.UUID  `json:"org_id"`
	UnitID    *uuid.UUID `json:"unit_id"`
	Seq       int        `json:"seq"`
	ActorID   *uuid.UUID `json:"actor_id"`
	Side      *string    `json:"side"`
	UpTo      int        `json:"up_to"`
	CreatedAt time.Time  `json:"created_at"`
}

func eventOf(r dbgen.ChatEvent) event {
	return event{
		ID: r.ID, Kind: r.Kind, ChatID: r.ChatID, UserID: r.UserID, OrgID: r.OrgID, UnitID: r.UnitID, Seq: int(r.Seq),
		ActorID: r.ActorID, Side: r.Side, UpTo: int(r.UpTo), CreatedAt: r.CreatedAt,
	}
}

// parseEvent разбирает полезную нагрузку уведомления.
func parseEvent(payload string) (event, error) {
	var e event
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return event{}, fmt.Errorf("chat: parse event: %w", err)
	}
	if e.ID <= 0 || e.ChatID == uuid.Nil || e.Kind == "" {
		return event{}, fmt.Errorf("chat: parse event: incomplete %q", payload)
	}
	return e, nil
}

// typingNote — «печатает»: в базу не пишется, объявление несёт всё нужное само (D-243): кому показать (учёный
// переписки, организация, подразделение), от кого (имя и признак анонимности уже решены там, где известны права).
type typingNote struct {
	ChatID uuid.UUID `json:"c"`
	// UserID — кто печатает; Side — с какой стороны.
	UserID uuid.UUID `json:"u"`
	Side   Side      `json:"s"`
	// Scientist, OrgID, UnitID — переписка: её учёный, организация и подразделение вакансии.
	Scientist uuid.UUID  `json:"x"`
	OrgID     uuid.UUID  `json:"o"`
	UnitID    *uuid.UUID `json:"n"`
	// Name — имя печатающего; Anonymous — оно организации не показывается (D-199).
	Name      string `json:"nm"`
	Anonymous bool   `json:"an"`
}

// payload — нагрузка уведомления. Поля note строки, числа и логические значения: сериализация не может не удаться.
func (n typingNote) payload() string {
	b, _ := json.Marshal(n)
	return string(b)
}

func parseTyping(payload string) (typingNote, error) {
	var n typingNote
	if err := json.Unmarshal([]byte(payload), &n); err != nil {
		return typingNote{}, fmt.Errorf("chat: parse typing: %w", err)
	}
	if n.ChatID == uuid.Nil || n.UserID == uuid.Nil || n.Scientist == uuid.Nil || n.OrgID == uuid.Nil || !n.Side.Valid() {
		return typingNote{}, fmt.Errorf("chat: parse typing: incomplete %q", payload)
	}
	return n, nil
}

// TypingEvery — как часто «печатает» от одного человека в одной переписке доходит до собеседника; чаще молча
// пропускается (contracts/chat/typing.json).
const TypingEvery = 3 * time.Second

// typingGate не пропускает «печатает» чаще TypingEvery на пару «человек, переписка». Считает копия сама: при двух
// копиях человек может пройти дважды за срок, этого достаточно (на всех лежит ещё предел частоты периметра).
type typingGate struct {
	mu   sync.Mutex
	last map[typingKey]time.Time
}

type typingKey struct{ chat, user uuid.UUID }

// typingGateSweepAt — сколько записей копится, прежде чем старые выбрасываются.
const typingGateSweepAt = 4096

func newTypingGate() *typingGate { return &typingGate{last: map[typingKey]time.Time{}} }

// allow сообщает, можно ли пропустить «печатает» сейчас, и запоминает время.
func (g *typingGate) allow(chat, user uuid.UUID, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := typingKey{chat: chat, user: user}
	if at, ok := g.last[k]; ok && now.Sub(at) < TypingEvery {
		return false
	}
	if len(g.last) >= typingGateSweepAt {
		for key, at := range g.last {
			if now.Sub(at) >= TypingEvery {
				delete(g.last, key)
			}
		}
	}
	g.last[k] = now
	return true
}

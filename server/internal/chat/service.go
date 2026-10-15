package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/access"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
	"scibox/server/internal/privacy"
)

// DB — то, что сервису нужно от базы: запросы и транзакции.
type DB interface {
	dbgen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service — вся логика переписок.
type Service struct {
	db  DB
	q   *dbgen.Queries
	now func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool) *Service { return newService(pool) }

func newService(db DB) *Service {
	return &Service{db: db, q: dbgen.New(db), now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("chat: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("chat: commit: %w", err)
	}
	return nil
}

// ---- вход для откликов и приглашений (срез 38) ----

// OpenInput — откуда выросла переписка.
type OpenInput struct {
	VacancyID uuid.UUID
	// UserID — учёный.
	UserID        uuid.UUID
	ApplicationID *uuid.UUID
	OfferID       *uuid.UUID
}

// Open заводит переписку пары «учёный × вакансия» или находит уже существующую, дополняя её ссылкой на отклик или
// приглашение. Вызывается в транзакции отклика или приглашения: откатилось событие, откатилась и переписка.
func Open(ctx context.Context, q *dbgen.Queries, in OpenInput, now time.Time) (uuid.UUID, error) {
	id, err := q.OpenChat(ctx, dbgen.OpenChatParams{
		VacancyID: in.VacancyID, UserID: in.UserID, ApplicationID: in.ApplicationID, OfferID: in.OfferID, Now: now,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("chat: open: %w", err)
	}
	return id, nil
}

// AppendSystem дописывает в переписку строку о событии (отклик, приглашение, решение…) и возвращает её номер. Данные
// события кладутся как есть и показываются обеим сторонам: имён, почт и всего, по чему узнают анонимного учёного, в них
// быть не должно (T-97). Вызывается в той же транзакции, что и само событие.
func AppendSystem(ctx context.Context, q *dbgen.Queries, chatID uuid.UUID, eventType string, data any, at time.Time) (int, error) {
	to, ok := audience[eventType]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownEvent, eventType)
	}
	raw := []byte("{}")
	if data != nil {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return 0, fmt.Errorf("chat: marshal event data: %w", err)
		}
	}
	seq, err := q.BumpChat(ctx, dbgen.BumpChatParams{ToSide: string(to), Now: at, ID: chatID})
	if err != nil {
		return 0, fmt.Errorf("chat: bump: %w", err)
	}
	if err := q.InsertChatMessage(ctx, dbgen.InsertChatMessageParams{
		ChatID: chatID, Seq: seq, Kind: KindSystem, ToSide: string(to), EventType: eventType, EventData: raw, CreatedAt: at,
	}); err != nil {
		return 0, fmt.Errorf("chat: insert system message: %w", err)
	}
	if err := q.UnarchiveChat(ctx, dbgen.UnarchiveChatParams{ChatID: chatID, Now: at}); err != nil {
		return 0, fmt.Errorf("chat: unarchive: %w", err)
	}
	return int(seq), nil
}

// ---- участник и состояние ----

// who — кем человек приходится переписке.
type who struct {
	side  Side
	actor access.Actor // сторона организации
}

func unitOf(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return access.NoUnit
	}
	return *id
}

// participant решает, участник ли человек переписки и с какой стороны. Учёный — только хозяин переписки; сотрудник —
// тот, кому по правам организации положено видеть отклики этой вакансии (access.ViewApplications), проверка при каждом
// запросе: потерял роль, потерял переписку. Всем остальным переписка «не существует».
func (s *Service) participant(ctx context.Context, q *dbgen.Queries, user auth.User, row dbgen.GetChatRow) (who, error) {
	if row.UserID == user.ID {
		return who{side: SideScientist}, nil
	}
	actor, err := orgs.ActorOf(ctx, q, row.OrgID, user.ID)
	if err != nil {
		return who{}, err
	}
	if !actor.Can(access.ViewApplications, unitOf(row.UnitID)) {
		return who{}, ErrNotFound
	}
	return who{side: SideOrg, actor: actor}, nil
}

// load читает переписку и проверяет, что человек в ней участник.
func (s *Service) load(ctx context.Context, q *dbgen.Queries, user auth.User, id uuid.UUID) (dbgen.GetChatRow, who, error) {
	row, err := q.GetChat(ctx, dbgen.GetChatParams{ViewerID: user.ID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return dbgen.GetChatRow{}, who{}, ErrNotFound
	}
	if err != nil {
		return dbgen.GetChatRow{}, who{}, fmt.Errorf("chat: load: %w", err)
	}
	w, err := s.participant(ctx, q, user, row)
	if err != nil {
		return dbgen.GetChatRow{}, who{}, err
	}
	return row, w, nil
}

// lockState берёт замки в порядке «отклик или приглашение, потом переписка» и возвращает свежее состояние. Решение
// организации идёт в том же порядке (отклик, потом строка в переписке): отправка и закрытие выстраиваются в очередь, и
// сообщение не попадёт в переписку, закрытую до него (T-106).
func (s *Service) lockState(ctx context.Context, q *dbgen.Queries, chatID uuid.UUID, application, offer *uuid.UUID) (State, error) {
	st, a, o, err := s.lockOnce(ctx, q, chatID, application, offer)
	if err != nil {
		return State{}, err
	}
	if !sameID(a, application) || !sameID(o, offer) {
		// Пока ждали замок, к переписке привязали отклик или приглашение. Замок переписки теперь наш, привязки меняться
		// не могут: берём замки на то, что привязано, ещё раз.
		st, _, _, err = s.lockOnce(ctx, q, chatID, a, o)
	}
	return st, err
}

// lockOnce берёт замки на отклик и приглашение, затем на переписку; возвращает состояние по ним и то, что к переписке
// привязано на самом деле.
func (s *Service) lockOnce(ctx context.Context, q *dbgen.Queries, chatID uuid.UUID, application, offer *uuid.UUID) (State, *uuid.UUID, *uuid.UUID, error) {
	var appStatus, offerStatus *string
	if application != nil {
		st, err := q.LockApplicationStatus(ctx, *application)
		if err != nil {
			return State{}, nil, nil, fmt.Errorf("chat: lock application: %w", err)
		}
		appStatus = &st
	}
	if offer != nil {
		st, err := q.LockOfferStatus(ctx, *offer)
		if err != nil {
			return State{}, nil, nil, fmt.Errorf("chat: lock offer: %w", err)
		}
		offerStatus = &st
	}
	lk, err := q.LockChat(ctx, chatID)
	if err != nil {
		return State{}, nil, nil, fmt.Errorf("chat: lock chat: %w", err)
	}
	// Блокировка читается после замка отдельным запросом: блокировка организации берёт тот же замок и фиксируется до его
	// снятия (T-105, T-106).
	blocked, err := q.IsChatBlocked(ctx, chatID)
	if err != nil {
		return State{}, nil, nil, fmt.Errorf("chat: check block: %w", err)
	}
	return StateOf(appStatus, offerStatus, blocked), lk.ApplicationID, lk.OfferID, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ---- сборка ответов ----

// messageID — номер сообщения в ответе: выводится из переписки и номера, отдельно не хранится.
func messageID(chatID uuid.UUID, seq int) uuid.UUID {
	return uuid.NewSHA1(chatID, []byte(strconv.Itoa(seq)))
}

// nameSeen — показывается ли организации имя учёного (privacy.ShowsName).
func nameSeen(visibility *string, hiddenFrom, revealed bool) bool {
	concealed := hiddenFrom || (visibility != nil && privacy.Visibility(*visibility) == privacy.Anonymous)
	return privacy.ShowsName(concealed, revealed)
}

// profileSeen — открыт ли организации профиль учёного (для ссылки в шапке).
func profileSeen(visibility *string, hiddenFrom, revealed bool) bool {
	if visibility == nil {
		return false
	}
	vis, ok := privacy.Parse(*visibility)
	return ok && vis.CanView(privacy.Viewer{Staff: true, HiddenFrom: hiddenFrom, Revealed: revealed})
}

// authorInfo — кого знает лента об авторах организации: имя и состоит ли ещё в ней.
type authorInfo struct {
	name   string
	member bool
}

// authorOf описывает автора сообщения глазами смотрящего.
func authorOf(viewer uuid.UUID, side Side, authorID *uuid.UUID, scientistName string, scientistSeen bool, staff map[uuid.UUID]authorInfo) *Author {
	a := &Author{Side: side, IsMe: authorID != nil && *authorID == viewer}
	if side == SideScientist {
		if scientistSeen {
			a.Name = scientistName
		} else {
			a.Anonymous = true
		}
		return a
	}
	info, known := staff[idOrNil(authorID)]
	if authorID == nil || !known || !info.member {
		a.Former = true
		return a
	}
	a.Name = info.name
	return a
}

func idOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func (s *Service) toMessage(chatID uuid.UUID, m dbgen.ChatMessage, viewer uuid.UUID, scientistName string, scientistSeen bool, staff map[uuid.UUID]authorInfo) Message {
	seq := int(m.Seq)
	out := Message{
		ID: messageID(chatID, seq), Seq: seq, Kind: m.Kind, CreatedAt: m.CreatedAt.UTC(),
		EditedAt: utcPtr(m.EditedAt), Deleted: m.DeletedAt != nil,
	}
	if m.Kind == KindSystem {
		out.Event = &Event{Type: m.EventType, Data: json.RawMessage(m.EventData)}
		return out
	}
	side := Side(deref(m.AuthorSide))
	out.Author = authorOf(viewer, side, m.AuthorUserID, scientistName, scientistSeen, staff)
	if m.DeletedAt != nil {
		return out
	}
	out.Text = m.Body
	if out.Author.IsMe {
		until := m.CreatedAt.Add(EditWindow).UTC()
		out.EditableUntil, out.ClientID = &until, m.ClientID
	}
	return out
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

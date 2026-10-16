package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
)

// Пределы против спама и злоупотреблений (срез 40, T-99, D-248). Минутный предел отправки стоит в периметре
// (httpapi.chatSends); здесь суточные и те, что зависят от переписки, а значит проверяются под замком в транзакции
// (I-80): пачка параллельных запросов не проходит проверку с одним и тем же нулём.
const (
	// DefaultPersonPerDay — сообщений в сутки от одного человека во всех его переписках.
	DefaultPersonPerDay = 600
	// DefaultOrgPerDay — сообщений в сутки от всей организации (все сотрудники вместе).
	DefaultOrgPerDay = 2000
	// DefaultUnanswered — сколько текстов подряд пишет одна сторона, пока другая не ответила своим текстом.
	DefaultUnanswered = 5
	// DefaultComplaintsPerDay — жалоб в сутки от одного человека.
	DefaultComplaintsPerDay = 3
	// DefaultComplaintPerChat — как часто можно жаловаться на одну переписку.
	DefaultComplaintPerChat = 7 * 24 * time.Hour
	// DefaultEditsPerDay — настоящих исправлений текста в сутки от одного человека. Правка рассылается собеседнику событием;
	// без предела можно было бы гонять собеседнику поток событий, меняя один текст туда и обратно.
	DefaultEditsPerDay = 200
	// DefaultMailPerDay — писем о непрочитанном в сутки одному получателю, как бы много переписок ни ждало.
	DefaultMailPerDay = 10

	// day — окно суточных пределов (скользящее, как у остальных счётчиков rate_events).
	day = 24 * time.Hour

	// Виды счётчиков в таблице rate_events (она общая; ключ — номер человека или организации).
	kindSendPerson = "chat_send"
	kindSendOrg    = "chat_send_org"
	kindMail       = "chat_mail"
	kindEdit       = "chat_edit"

	// Ключи замков жалоб.
	lockComplaint = "chat_complaint"
)

// Limits — пределы чата. Нулевое или отрицательное значение не значит «без предела»: берётся значение по умолчанию.
type Limits struct {
	PersonPerDay     int
	OrgPerDay        int
	Unanswered       int
	ComplaintsPerDay int
	ComplaintPerChat time.Duration
	MailPerDay       int
	EditsPerDay      int
}

// DefaultLimits — пределы, выбранные пользователем (D-248).
func DefaultLimits() Limits {
	return Limits{
		PersonPerDay: DefaultPersonPerDay, OrgPerDay: DefaultOrgPerDay, Unanswered: DefaultUnanswered,
		ComplaintsPerDay: DefaultComplaintsPerDay, ComplaintPerChat: DefaultComplaintPerChat, MailPerDay: DefaultMailPerDay,
		EditsPerDay: DefaultEditsPerDay,
	}
}

// normalized заменяет нули и отрицательные значения значениями по умолчанию: «0» не должно значить «без предела».
func (l Limits) normalized() Limits {
	d := DefaultLimits()
	pick := func(v, def int) int {
		if v < 1 {
			return def
		}
		return v
	}
	l.PersonPerDay = pick(l.PersonPerDay, d.PersonPerDay)
	l.OrgPerDay = pick(l.OrgPerDay, d.OrgPerDay)
	l.Unanswered = pick(l.Unanswered, d.Unanswered)
	l.ComplaintsPerDay = pick(l.ComplaintsPerDay, d.ComplaintsPerDay)
	l.MailPerDay = pick(l.MailPerDay, d.MailPerDay)
	l.EditsPerDay = pick(l.EditsPerDay, d.EditsPerDay)
	if l.ComplaintPerChat <= 0 {
		l.ComplaintPerChat = d.ComplaintPerChat
	}
	return l
}

// WithLimits задаёт пределы (для настройки и тестов).
func (s *Service) WithLimits(l Limits) *Service {
	s.limits = l.normalized()
	return s
}

// Ошибки пределов.
var (
	// ErrAwaitingReply — подряд написано столько текстов, что пора дождаться ответа собеседника.
	ErrAwaitingReply = errors.New("chat: waiting for the other side to answer")
	// ErrClientIDReused — повтор отправки с тем же номером, но другим текстом: молча вернуть прежнее сообщение значило бы
	// выдать новый текст за отправленный.
	ErrClientIDReused = errors.New("chat: client id was used for another text")
	// ErrAlreadyComplained — на эту переписку человек уже жаловался на этой неделе.
	ErrAlreadyComplained = errors.New("chat: already complained about this chat")
)

// spendRate записывает событие в счётчик вида kind по ключу key, если в окне их меньше max. Замок на ключ берётся до
// подсчёта и держится до конца транзакции.
func spendRate(ctx context.Context, q *dbgen.Queries, now time.Time, kind, key string, window time.Duration, maxEvents int) error {
	if err := q.LockRateKey(ctx, kind+":"+key); err != nil {
		return fmt.Errorf("chat: lock rate key: %w", err)
	}
	row, err := q.CountRateEvents(ctx, dbgen.CountRateEventsParams{Kind: kind, Key: key, At: now.Add(-window)})
	if err != nil {
		return fmt.Errorf("chat: count rate events: %w", err)
	}
	if row.Events >= int64(maxEvents) {
		return &auth.RateLimitedError{RetryAfter: max(row.Oldest.Add(window).Sub(now), time.Second)}
	}
	if err := q.RecordRateEvent(ctx, dbgen.RecordRateEventParams{Kind: kind, Key: key, At: now}); err != nil {
		return fmt.Errorf("chat: record rate event: %w", err)
	}
	return nil
}

// checkSendLimits проверяет пределы отправки текста: не дожидаясь ответа, в сутки на человека и (для сотрудника) на
// организацию. Вызывается в транзакции отправки после проверки прав, закрытия и повтора, до записи сообщения: отказ
// ничего не меняет. Замки берутся всегда в одном порядке (человек, потом организация), чтобы не было взаимных ожиданий.
func (s *Service) checkSendLimits(ctx context.Context, q *dbgen.Queries, user auth.User, chatID uuid.UUID, side Side, orgID uuid.UUID, now time.Time) error {
	n, err := q.CountUnansweredChatText(ctx, dbgen.CountUnansweredChatTextParams{ChatID: chatID, Side: string(side)})
	if err != nil {
		return fmt.Errorf("chat: count unanswered: %w", err)
	}
	if n >= int64(s.limits.Unanswered) {
		return ErrAwaitingReply
	}
	if err := spendRate(ctx, q, now, kindSendPerson, user.ID.String(), day, s.limits.PersonPerDay); err != nil {
		return err
	}
	if side == SideOrg {
		return spendRate(ctx, q, now, kindSendOrg, orgID.String(), day, s.limits.OrgPerDay)
	}
	return nil
}

// checkComplaintLimits проверяет пределы жалоб: в сутки на человека и одна на переписку за ComplaintPerChat.
func (s *Service) checkComplaintLimits(ctx context.Context, q *dbgen.Queries, user auth.User, chatID uuid.UUID, now time.Time) error {
	if err := q.LockRateKey(ctx, lockComplaint+":"+user.ID.String()); err != nil {
		return fmt.Errorf("chat: lock complaint key: %w", err)
	}
	again, err := q.HasChatComplaintSince(ctx, dbgen.HasChatComplaintSinceParams{UserID: user.ID, ChatID: &chatID, Since: now.Add(-s.limits.ComplaintPerChat)})
	if err != nil {
		return fmt.Errorf("chat: has complaint: %w", err)
	}
	if again {
		return ErrAlreadyComplained
	}
	n, err := q.CountChatComplaintsSince(ctx, dbgen.CountChatComplaintsSinceParams{UserID: user.ID, Since: now.Add(-day)})
	if err != nil {
		return fmt.Errorf("chat: count complaints: %w", err)
	}
	if n >= int64(s.limits.ComplaintsPerDay) {
		oldest := now
		if t, err := q.OldestChatComplaintSince(ctx, dbgen.OldestChatComplaintSinceParams{UserID: user.ID, Since: now.Add(-day)}); err == nil {
			oldest = t
		}
		return &auth.RateLimitedError{RetryAfter: max(oldest.Add(day).Sub(now), time.Second)}
	}
	return nil
}

// mailRecipients отбирает получателей, которым в этот день ещё можно отправить письмо о непрочитанном, и записывает
// им событие. Остальным письма не будет (переписка всё равно ждёт в шапке сайта). Замки берутся в порядке номеров, иначе
// две копии задачи с общими получателями ждали бы друг друга.
func (s *Service) mailRecipients(ctx context.Context, q *dbgen.Queries, ids []uuid.UUID, now time.Time) ([]uuid.UUID, error) {
	ordered := slices.SortedFunc(slices.Values(ids), func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	var out []uuid.UUID
	for _, id := range slices.Compact(ordered) {
		err := spendRate(ctx, q, now, kindMail, id.String(), day, s.limits.MailPerDay)
		var rl *auth.RateLimitedError
		switch {
		case err == nil:
			out = append(out, id)
		case errors.As(err, &rl):
			// Предел исчерпан: этому получателю письма не будет.
		default:
			return nil, err
		}
	}
	return out, nil
}

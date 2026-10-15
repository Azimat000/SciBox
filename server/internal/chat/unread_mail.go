package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/num"
	"scibox/server/internal/orgs"
)

// Письма о непрочитанных сообщениях (срез 39, D-245). Поводом служит текст человека, который сторона не прочитала за
// UnreadMailAfter. Письмо одно на переписку и сторону, пока человек её не откроет; текста сообщения в письме нет.

// Сроки писем о непрочитанном.
const (
	// UnreadMailAfter — сколько сообщение должно оставаться непрочитанным.
	UnreadMailAfter = 15 * time.Minute
	// UnreadMailMaxAge — старше этого срока непрочитанное писем не вызывает (после переноса прежних записей, простоя).
	UnreadMailMaxAge = 48 * time.Hour
	// unreadMailBatch — сколько писем за один проход задачи; остальные ждут следующего (раз в минуту).
	unreadMailBatch = 200
	// anonymousSender — как называется учёный в письме организации, пока его имя ей не показывается (T-97).
	anonymousSender = "Анонимный соискатель"
)

// MailConfig — как подписываются письма о сообщениях и откуда берутся ссылки.
type MailConfig struct {
	ProductName string
	// PublicURL — адрес сайта без косой черты на конце.
	PublicURL string
}

// WithMail задаёт настройки писем. Без них задача писем отказывается работать: ссылка без адреса сайта бесполезна.
func (s *Service) WithMail(cfg MailConfig) *Service {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	s.mail = cfg
	return s
}

// SendUnreadMail ставит в очередь письма о сообщениях, которые не прочитаны UnreadMailAfter, и возвращает число писем.
// Безопасна при нескольких копиях и повторе: строка берётся под замком, письмо и отметка пишутся в одной транзакции.
func (s *Service) SendUnreadMail(ctx context.Context) (int, error) {
	if s.mail.PublicURL == "" {
		return 0, fmt.Errorf("chat: unread mail: public url is not set")
	}
	now := s.now()
	if _, err := s.q.DeleteStaleChatUnreadMail(ctx, now.Add(-UnreadMailMaxAge)); err != nil {
		return 0, fmt.Errorf("chat: drop stale unread mail: %w", err)
	}
	sent := 0
	for range unreadMailBatch {
		n, more, err := s.sendOneUnreadMail(ctx, now)
		sent += n
		if err != nil {
			return sent, err
		}
		if !more {
			break
		}
	}
	return sent, nil
}

// sendOneUnreadMail берёт одну строку, которой пора, и обрабатывает её; more — строки могут быть ещё.
func (s *Service) sendOneUnreadMail(ctx context.Context, now time.Time) (sent int, more bool, err error) {
	err = s.inTx(ctx, func(q *dbgen.Queries) error {
		rows, err := q.ClaimChatUnreadMail(ctx, dbgen.ClaimChatUnreadMailParams{
			Due: now.Add(-UnreadMailAfter), Oldest: now.Add(-UnreadMailMaxAge), Batch: 1,
		})
		if err != nil {
			return fmt.Errorf("chat: claim unread mail: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		more = true
		r := rows[0]
		side := Side(r.Side)
		read := r.ScientistRead
		if side == SideOrg {
			read = r.OrgRead
		}
		unread, err := q.HasUnreadChatText(ctx, dbgen.HasUnreadChatTextParams{ChatID: r.ChatID, Side: r.Side, Read: read})
		if err != nil {
			return fmt.Errorf("chat: check unread text: %w", err)
		}
		if !unread {
			// Прочитали или удалили, пока ждали: письма не будет.
			return q.DeleteChatUnreadMail(ctx, dbgen.DeleteChatUnreadMailParams{ChatID: r.ChatID, Side: r.Side})
		}
		facts, err := q.GetChatMailFacts(ctx, r.ChatID)
		if err != nil {
			return fmt.Errorf("chat: mail facts: %w", err)
		}
		recipients := []uuid.UUID{r.UserID}
		from := facts.OrgName
		if side == SideOrg {
			recipients, err = orgs.UsersWhoCan(ctx, q, r.OrgID, access.ViewApplications, unitOf(facts.UnitID))
			if err != nil {
				return err
			}
			from = anonymousSender
			if nameSeen(facts.Visibility, facts.HiddenFrom, facts.Revealed) {
				from = facts.ScientistName
			}
		}
		subject, body := s.unreadMailText(r.ChatID, facts.VacancyTitle, from)
		for _, uid := range recipients {
			if side == SideOrg && uid == r.UserID {
				continue // учёный, который сам работает в этой организации, читает как учёный
			}
			n, err := q.EnqueueChatMailToUser(ctx, dbgen.EnqueueChatMailToUserParams{UserID: uid, Subject: subject, Body: body, Now: now})
			if err != nil {
				return fmt.Errorf("chat: enqueue unread mail: %w", err)
			}
			sent += int(n) // выключивший письма (email_messages) не получает его и не считается
		}
		if err := q.MarkChatUnreadMailNotified(ctx, dbgen.MarkChatUnreadMailNotifiedParams{ChatID: r.ChatID, Side: r.Side, Now: now}); err != nil {
			return fmt.Errorf("chat: mark unread mail: %w", err)
		}
		return nil
	})
	return sent, more, err
}

// oneLine сворачивает пробелы и переносы: тема письма не может быть многострочной.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// unreadMailText собирает тему и тело письма. Текста сообщения в нём нет: почта защищена хуже сайта (CHAT.md).
func (s *Service) unreadMailText(chatID uuid.UUID, vacancy, from string) (subject, body string) {
	vacancy = oneLine(vacancy)
	subject = "Новое сообщение по вакансии «" + vacancy + "»"
	var b strings.Builder
	b.WriteString("У вас есть непрочитанное сообщение.\n\n")
	b.WriteString("Вакансия: «" + vacancy + "»\n")
	b.WriteString("От: " + oneLine(from) + "\n\n")
	b.WriteString("Открыть переписку: " + s.mail.PublicURL + "/chats/" + chatID.String() + "\n\n")
	b.WriteString("—\n" + s.mail.ProductName + ". Это письмо отправлено автоматически, отвечать на него не нужно.\n")
	b.WriteString("Письма о новых сообщениях можно отключить в настройках аккаунта: " + s.mail.PublicURL + "/account\n")
	return subject, b.String()
}

// PruneEvents удаляет из журнала событий записи старше retention и возвращает их число. Партиями, пока есть что
// удалять (но не больше maxBatches партий за проход).
func (s *Service) PruneEvents(ctx context.Context, retention time.Duration, batch, maxBatches int) (int, error) {
	before := s.now().Add(-retention)
	total := 0
	for range maxBatches {
		n, err := s.q.DeleteOldChatEvents(ctx, dbgen.DeleteOldChatEventsParams{Before: before, Batch: num.Int32(batch)})
		if err != nil {
			return total, fmt.Errorf("chat: prune events: %w", err)
		}
		total += int(n)
		if int(n) < batch {
			break
		}
	}
	return total, nil
}

// UnreadMailStats — состояние очереди писем о сообщениях для метрик.
type UnreadMailStats struct {
	// Waiting — строк, ждущих письма (включая те, которым ещё не пора); OldestWaiting — как давно ждёт самая старая.
	Waiting       int64
	OldestWaiting time.Duration
}

// UnreadMailStats читает состояние очереди.
func (s *Service) UnreadMailStats(ctx context.Context) (UnreadMailStats, error) {
	r, err := s.q.ChatUnreadMailStats(ctx)
	if err != nil {
		return UnreadMailStats{}, fmt.Errorf("chat: unread mail stats: %w", err)
	}
	out := UnreadMailStats{Waiting: r.Waiting}
	if r.Waiting > 0 {
		out.OldestWaiting = max(0, s.now().Sub(r.Oldest))
	}
	return out, nil
}

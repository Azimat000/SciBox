// Package notifications — уведомления людям: на сайте (колокольчик) и письмом (срез 8).
//
// Критичная зона (docs/TESTING.md, «рассылки»): письмо не должно уйти дважды, не туда или не тому.
// Уведомление и письмо создаются в одной транзакции с событием, из-за которого они появились, поэтому не теряются
// при падении сервера. Сам отправитель (worker.go) берёт письма из очереди outbox, отправляет и повторяет при сбоях.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	mailer "scibox/server/internal/mail"
	"scibox/server/internal/num"
)

// ErrNotFound — уведомления нет или оно чужое.
var ErrNotFound = errors.New("notifications: not found")

// ErrInvalid — программная ошибка: уведомление собрано неверно (пустой заголовок, внешняя ссылка и т. п.).
var ErrInvalid = errors.New("notifications: invalid notice")

// Виды уведомлений, письма которых человек может отключить (D-106). Остальные письма идут всегда.
const (
	KindSavedSearch      = "saved_search"      // новые вакансии по сохранённому поиску
	KindDeadlineReminder = "deadline_reminder" // напоминание о сроке подачи
)

// Виды писем в настройках и в запросе к очереди.
const (
	mailNewVacancies = "new_vacancies"
	mailDeadlines    = "deadlines"
)

// mailCategory — к какому виду писем относится уведомление. Пусто: письмо отключить нельзя.
func mailCategory(kind string) string {
	switch kind {
	case KindSavedSearch:
		return mailNewVacancies
	case KindDeadlineReminder:
		return mailDeadlines
	}
	return ""
}

// Пределы текстов (совпадают с CHECK в базе).
const (
	maxTitle = 300
	maxLink  = 300
)

// Config — настройки.
type Config struct {
	// ProductName подписывает письма.
	ProductName string
	// PublicURL — адрес сайта, из него строятся ссылки в письмах.
	PublicURL string
}

// DB — то, что сервису нужно от базы.
type DB = dbgen.DBTX

// Service — уведомления и очередь писем.
type Service struct {
	q   *dbgen.Queries
	cfg Config
	now func() time.Time
}

// NewService собирает сервис.
func NewService(pool *pgxpool.Pool, cfg Config) *Service { return newService(pool, cfg) }

func newService(db DB, cfg Config) *Service {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	return &Service{q: dbgen.New(db), cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

// Notice — одно уведомление человеку.
type Notice struct {
	UserID uuid.UUID
	// Kind — машинное название события (application_received и т. п.): по нему сайт выбирает значок.
	Kind  string
	Title string
	Body  string
	// Link — адрес страницы на сайте, начинается с «/». Пусто, если открывать нечего.
	Link string
}

func (n Notice) valid() error {
	switch {
	case n.UserID == uuid.Nil, strings.TrimSpace(n.Kind) == "", strings.TrimSpace(n.Title) == "":
		return fmt.Errorf("%w: user, kind and title are required", ErrInvalid)
	case utf8.RuneCountInString(n.Title) > maxTitle, utf8.RuneCountInString(n.Link) > maxLink:
		return fmt.Errorf("%w: text is too long", ErrInvalid)
	case strings.ContainsAny(n.Title, "\r\n"):
		return fmt.Errorf("%w: title has a line break", ErrInvalid)
	case n.Link != "" && (!strings.HasPrefix(n.Link, "/") || strings.HasPrefix(n.Link, "//") || strings.ContainsAny(n.Link, " \r\n\\")):
		// Ссылка только внутри сайта: в письме с нашим именем не должно быть чужих адресов.
		return fmt.Errorf("%w: link must be a path on this site", ErrInvalid)
	}
	return nil
}

// Emit создаёт уведомление на сайте и письмо на почту аккаунта. q привязан к транзакции события.
// Адрес письма берётся из аккаунта получателя в самом запросе, подставить чужой нельзя. Если человек отключил письма этого
// вида (напоминания и новые вакансии, D-106), остаётся только уведомление на сайте.
func (s *Service) Emit(ctx context.Context, q *dbgen.Queries, n Notice) error {
	if err := n.valid(); err != nil {
		return err
	}
	now := s.now()
	if err := q.InsertNotification(ctx, dbgen.InsertNotificationParams{UserID: n.UserID, Kind: n.Kind, Title: n.Title, Body: n.Body, Link: n.Link, Now: now}); err != nil {
		return fmt.Errorf("notifications: insert: %w", err)
	}
	if err := q.EnqueueMailToUser(ctx, dbgen.EnqueueMailToUserParams{UserID: n.UserID, Subject: n.Title, Body: s.mailBody(n.Body, n.Link), Now: now, Category: mailCategory(n.Kind)}); err != nil {
		return fmt.Errorf("notifications: enqueue mail: %w", err)
	}
	return nil
}

// Enqueue ставит в очередь письмо на любой адрес (рекомендателю, у которого нет аккаунта). q привязан к транзакции события.
func (s *Service) Enqueue(ctx context.Context, q *dbgen.Queries, m mailer.Message) error {
	return enqueue(ctx, q, m, s.now())
}

func enqueue(ctx context.Context, q *dbgen.Queries, m mailer.Message, now time.Time) error {
	if _, err := mail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("%w: bad recipient %q", ErrInvalid, m.To)
	}
	if strings.TrimSpace(m.Subject) == "" || strings.ContainsAny(m.Subject, "\r\n") {
		return fmt.Errorf("%w: bad subject", ErrInvalid)
	}
	if err := q.EnqueueMail(ctx, dbgen.EnqueueMailParams{ToEmail: m.To, Subject: m.Subject, Body: m.Body, Now: now}); err != nil {
		return fmt.Errorf("notifications: enqueue mail: %w", err)
	}
	return nil
}

// mailBody — тело письма-уведомления: текст, ссылка, подпись.
func (s *Service) mailBody(body, link string) string {
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	if link != "" {
		b.WriteString("Открыть: " + s.cfg.PublicURL + link + "\n\n")
	}
	b.WriteString("—\n" + s.cfg.ProductName + ". Это письмо отправлено автоматически, отвечать на него не нужно.\n")
	return b.String()
}

// ---- чтение ----

// Item — уведомление в списке.
type Item struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Link      string    `json:"link"`
	CreatedAt time.Time `json:"created_at"`
	Read      bool      `json:"read"`
}

// List — страница уведомлений.
type List struct {
	Items  []Item `json:"items"`
	Total  int64  `json:"total"`
	Unread int64  `json:"unread"`
}

// Default и предельный размер страницы.
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// List отдаёт уведомления человека: новые сверху. unreadOnly — только непрочитанные.
func (s *Service) List(ctx context.Context, user auth.User, limit, offset int, unreadOnly bool) (List, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	offset = max(offset, 0)
	rows, err := s.q.ListNotifications(ctx, dbgen.ListNotificationsParams{UserID: user.ID, UnreadOnly: unreadOnly, Lim: num.Int32(limit), Off: num.Int32(offset)})
	if err != nil {
		return List{}, fmt.Errorf("notifications: list: %w", err)
	}
	counts, err := s.q.CountNotifications(ctx, user.ID)
	if err != nil {
		return List{}, fmt.Errorf("notifications: count: %w", err)
	}
	out := List{Items: make([]Item, 0, len(rows)), Total: counts.Total, Unread: counts.Unread}
	for _, r := range rows {
		out.Items = append(out.Items, Item{ID: r.ID, Kind: r.Kind, Title: r.Title, Body: r.Body, Link: r.Link, CreatedAt: r.CreatedAt, Read: r.ReadAt != nil})
	}
	return out, nil
}

// UnreadCount — сколько у человека непрочитанных (для цифры на колокольчике).
func (s *Service) UnreadCount(ctx context.Context, user auth.User) (int64, error) {
	n, err := s.q.CountUnreadNotifications(ctx, user.ID)
	if err != nil {
		return 0, fmt.Errorf("notifications: count unread: %w", err)
	}
	return n, nil
}

// MarkRead отмечает своё уведомление прочитанным. Чужое или несуществующее — ErrNotFound. Повторно можно.
func (s *Service) MarkRead(ctx context.Context, user auth.User, id uuid.UUID) error {
	ok, err := s.q.NotificationExists(ctx, dbgen.NotificationExistsParams{ID: id, UserID: user.ID})
	if err != nil {
		return fmt.Errorf("notifications: check: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if err := s.q.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{ID: id, UserID: user.ID, Now: s.now()}); err != nil {
		return fmt.Errorf("notifications: mark read: %w", err)
	}
	return nil
}

// MarkAllRead отмечает все свои уведомления прочитанными.
func (s *Service) MarkAllRead(ctx context.Context, user auth.User) error {
	if err := s.q.MarkAllNotificationsRead(ctx, dbgen.MarkAllNotificationsReadParams{UserID: user.ID, Now: s.now()}); err != nil {
		return fmt.Errorf("notifications: mark all read: %w", err)
	}
	return nil
}

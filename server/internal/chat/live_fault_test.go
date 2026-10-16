package chat

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scibox/server/internal/testkit"
)

// Сбои базы в живом потоке и письмах: ошибка не теряется там, где её ждёт вызывающий, и не рвёт поток там, где он должен
// пережить недоступную базу (D-244).

func TestUnreadMailAndEventsReportDatabaseFailures(t *testing.T) {
	cases := map[string]func(t *testing.T, w *world) func(db testkit.DB) error{
		"send unread mail": func(t *testing.T, w *world) func(testkit.DB) error {
			sci := w.Applicant("Анна Ильина")
			chat := w.chatOf(sci, w.vacancy.ID, "sent")
			w.send(sci, chat, "Привет")
			w.clock.Advance(UnreadMailAfter + time.Minute)
			if _, err := testkit.Pool.Exec(bg, `DELETE FROM chat_unread_mail WHERE chat_id <> $1`, chat); err != nil {
				t.Fatal(err)
			}
			return func(db testkit.DB) error {
				s := svcOn(w, db)
				s.WithMail(MailConfig{ProductName: "SciBox", PublicURL: "https://scibox.example.ru"})
				_, err := s.SendUnreadMail(bg)
				return err
			}
		},
		"send unread mail to the scientist": func(t *testing.T, w *world) func(testkit.DB) error {
			sci := w.Applicant("Борис Сомов")
			chat := w.chatOf(sci, w.vacancy.ID, "sent")
			w.send(w.tm.Owner, chat, "Здравствуйте")
			w.clock.Advance(UnreadMailAfter + time.Minute)
			if _, err := testkit.Pool.Exec(bg, `DELETE FROM chat_unread_mail WHERE chat_id <> $1`, chat); err != nil {
				t.Fatal(err)
			}
			return func(db testkit.DB) error {
				s := svcOn(w, db)
				s.WithMail(MailConfig{ProductName: "SciBox", PublicURL: "https://scibox.example.ru"})
				_, err := s.SendUnreadMail(bg)
				return err
			}
		},
		"unread mail stats": func(t *testing.T, w *world) func(testkit.DB) error {
			return func(db testkit.DB) error { _, err := svcOn(w, db).UnreadMailStats(bg); return err }
		},
		"prune events": func(t *testing.T, w *world) func(testkit.DB) error {
			return func(db testkit.DB) error { _, err := svcOn(w, db).PruneEvents(bg, time.Hour, 10, 2); return err }
		},
		"open a stream": func(t *testing.T, w *world) func(testkit.DB) error {
			return func(db testkit.DB) error {
				_, err := NewHub(svcOn(w, db), nil, HubConfig{}, w.Log, nil).open(bg, w.tm.Owner.User, uuid.New())
				return err
			}
		},
		"build the view of an event": func(t *testing.T, w *world) func(testkit.DB) error {
			sci := w.Applicant("Вера Светлова")
			chat := w.chatOf(sci, w.vacancy.ID, "sent")
			m := w.send(w.tm.Owner, chat, "Привет")
			return func(db testkit.DB) error {
				_, _, err := svcOn(w, db).eventView(bg, event{Kind: evCreated, ChatID: chat, Seq: m.Seq})
				return err
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
				w := newWorld(t)
				return prepare(t, w)
			})
		})
	}
}

// Журнал недоступен при запуске: раздача не умирает, а ждёт базу и повторяет; пока ждёт, потоки не принимаются (T-120).
// До среза 41 Run возвращал ошибку, копия оставалась жить с потоками, которые ничего не получали.
func TestHubWaitsForTheJournalAtStart(t *testing.T) {
	w := newWorld(t)
	db := &brokenDB{DB: testkit.Pool}
	db.down.Store(true)
	h := NewHub(svcOn(w, db), nil, HubConfig{}, w.Log, &rejectCounter{})
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()
	sci := w.Applicant("Анна Ильина")
	waitFor(t, func() bool { return h.waiting.Load() })

	if _, err := h.open(bg, sci.User, uuid.New()); !errors.Is(err, ErrHubBusy) {
		t.Fatalf("a stream while the hub waits for the journal: %v", err)
	}
	if got := h.obs.(*rejectCounter).by[RejectStart]; got != 1 {
		t.Errorf("rejected as %q: %d", RejectStart, got)
	}
	db.down.Store(false)
	waitFor(t, func() bool { return !h.waiting.Load() })
	st, err := h.open(bg, sci.User, uuid.New())
	if err != nil {
		t.Fatalf("a stream after the journal came back: %v", err)
	}
	h.remove(st)
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v", err)
	}
}

// Остановка раньше, чем ответила база, не ошибка и не зависание.
func TestHubStopsWhileWaitingForTheJournal(t *testing.T) {
	w := newWorld(t)
	db := &brokenDB{DB: testkit.Pool}
	db.down.Store(true)
	ctx, cancel := context.WithTimeout(bg, 300*time.Millisecond)
	defer cancel()
	if err := NewHub(svcOn(w, db), nil, HubConfig{}, w.Log, nil).Run(ctx); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

// rejectCounter считает отказы в потоке по причинам; остальные наблюдения ему не нужны.
type rejectCounter struct {
	nopObserver
	mu sync.Mutex
	by map[string]int
}

func (r *rejectCounter) StreamRejected(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = map[string]int{}
	}
	r.by[reason]++
}

// brokenDB — база, которую тест может «отключить»: все обращения тогда сразу получают ошибку.
type brokenDB struct {
	testkit.DB
	down atomic.Bool
}

func (b *brokenDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if b.down.Load() {
		return pgconn.CommandTag{}, testkit.ErrFault
	}
	return b.DB.Exec(ctx, sql, args...)
}

func (b *brokenDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if b.down.Load() {
		return nil, testkit.ErrFault
	}
	return b.DB.Query(ctx, sql, args...)
}

type downRow struct{}

func (downRow) Scan(...any) error { return testkit.ErrFault }

func (b *brokenDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if b.down.Load() {
		return downRow{}
	}
	return b.DB.QueryRow(ctx, sql, args...)
}

func (b *brokenDB) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.down.Load() {
		return nil, testkit.ErrFault
	}
	return b.DB.Begin(ctx)
}

// Пока база недоступна, поток не рвётся и не отдаёт лишнего; когда она вернулась, события снова идут.
func TestStreamSurvivesDatabaseOutage(t *testing.T) {
	w := newWorld(t)
	db := &brokenDB{DB: testkit.Pool}
	svc := svcOn(w, db)
	l := newLiveOn(t, &world{World: w.World, svc: svc, clock: w.clock, tm: w.tm, vacancy: w.vacancy, other: w.other}, func(c *HubConfig) {
		c.Recheck, c.Poll = 80*time.Millisecond, 60*time.Millisecond
	})
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	owner := l.stream(w.tm.Owner, "")
	owner.until("unread.count", wait)

	db.down.Store(true)
	// Проверки сессии и прав, опрос журнала, пересчёт непрочитанного получают ошибки, поток живёт.
	l.hub.markDirty(w.tm.Owner.ID)
	owner.none(500*time.Millisecond, "message.created")
	db.down.Store(false)

	w.send(sci, chat, "База вернулась")
	got := owner.until("message.created", wait)
	if jstr(got.data, "message", "text") != "База вернулась" {
		t.Errorf("after the outage: %s", got.raw)
	}
	if l.obs.closedCount(CloseSession) != 0 || l.obs.closedCount(CloseRights) != 0 {
		t.Errorf("the stream was closed by an outage: session %d, rights %d", l.obs.closedCount(CloseSession), l.obs.closedCount(CloseRights))
	}
}

// Прямые проверки раздачи при недоступной базе: каждая ветка отвечает «ничего не делаем», а не падает.
func TestHubStepsToleratesFailures(t *testing.T) {
	w := newWorld(t)
	db := &brokenDB{DB: testkit.Pool}
	h := NewHub(svcOn(w, db), nil, HubConfig{}, w.Log, nil)
	st, err := h.open(bg, w.tm.Owner.User, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	db.down.Store(true)
	// Проверка сессии и прав: база не отвечает, поток остаётся.
	if !h.recheck(bg, st) {
		t.Error("an outage must not close the stream")
	}
	// Пересчёт непрочитанного не удался: ничего не отправлено.
	h.refreshUnread(bg, w.tm.Owner.ID)
	if len(st.out) != 0 {
		t.Error("nothing is sent when the count failed")
	}
	// Добор и опрос журнала сообщают, что не удалось.
	shards := []chan work{make(chan work, 1)}
	if h.catchUp(bg, shards, time.Now()) || h.pollNew(bg, shards) {
		t.Error("an outage must be reported to the dispatcher")
	}
	// Событие, которое нельзя собрать, не рассылается.
	ev := event{ID: 1, Kind: evCreated, ChatID: uuid.New(), UserID: w.tm.Owner.ID, OrgID: w.tm.OrgID}
	h.deliver(bg, ev)
	if len(st.out) != 0 {
		t.Error("nothing is sent for an event that cannot be built")
	}
	db.down.Store(false)

	// Повторное снятие и снятие чужого потока ничего не ломают; потеря прав после снятия не возвращает его в списки.
	h.remove(st)
	h.remove(st)
	h.setScope(st, scope{})
	if h.Streams() != 0 {
		t.Errorf("streams: %d", h.Streams())
	}
}

// Мусор в уведомлениях пропускается, раздача живёт; неудачное подключение LISTEN повторяется.
func TestListenerSkipsGarbageAndRetriesConnecting(t *testing.T) {
	l := newLive(t)
	sci := l.Applicant("Анна Ильина")
	chat := l.chatOf(sci, l.vacancy.ID, "sent")
	owner := l.stream(l.tm.Owner, "")
	owner.until("unread.count", wait)
	for _, q := range []string{
		`SELECT pg_notify('chat_events', '{')`, `SELECT pg_notify('chat_events', '{"id":0}')`,
		`SELECT pg_notify('chat_typing', '{')`, `SELECT pg_notify('chat_typing', '{}')`,
	} {
		if _, err := testkit.Pool.Exec(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	l.send(sci, chat, "После мусора")
	owner.until("message.created", wait)

	// Подключиться к базе не удаётся: раздача повторяет и выходит по отмене.
	bad, err := pgx.ParseConfig("postgres://scibox@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(l.svc, bad, HubConfig{Poll: 50 * time.Millisecond}, l.Log, nil)
	ctx, cancel := context.WithTimeout(bg, 600*time.Millisecond)
	defer cancel()
	if err := h.Run(ctx); err != nil {
		t.Errorf("Run: %v", err)
	}
}

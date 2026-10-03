package notifications

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"scibox/server/internal/mail"
	"scibox/server/internal/testkit"
)

func svcOn(db testkit.DB) *Service {
	return newService(db, Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"})
}

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	cases := map[string]func(t *testing.T) func(db testkit.DB) error{
		"emit": func(t *testing.T) func(testkit.DB) error {
			p := testkit.NewWorld(t).User("Елена")
			return func(db testkit.DB) error {
				s := svcOn(db)
				return s.Emit(bg, s.q, Notice{UserID: p.ID, Kind: "k", Title: "t"})
			}
		},
		"enqueue": func(t *testing.T) func(testkit.DB) error {
			return func(db testkit.DB) error {
				s := svcOn(db)
				return s.Enqueue(bg, s.q, mail.Message{To: "a@example.ru", Subject: "s"})
			}
		},
		"queue send": func(t *testing.T) func(testkit.DB) error {
			return func(db testkit.DB) error {
				return NewQueue(db).Send(bg, mail.Message{To: "a@example.ru", Subject: "s"})
			}
		},
		"list": func(t *testing.T) func(testkit.DB) error {
			p := testkit.NewWorld(t).User("Елена")
			return func(db testkit.DB) error { _, err := svcOn(db).List(bg, p.User, 10, 0, false); return err }
		},
		"unread count": func(t *testing.T) func(testkit.DB) error {
			p := testkit.NewWorld(t).User("Елена")
			return func(db testkit.DB) error { _, err := svcOn(db).UnreadCount(bg, p.User); return err }
		},
		"mark read": func(t *testing.T) func(testkit.DB) error {
			p := testkit.NewWorld(t).User("Елена")
			s := newSvc()
			emit(t, s, Notice{UserID: p.ID, Kind: "k", Title: "t"})
			l, _ := s.List(bg, p.User, 1, 0, false)
			return func(db testkit.DB) error { return svcOn(db).MarkRead(bg, p.User, l.Items[0].ID) }
		},
		"mark all read": func(t *testing.T) func(testkit.DB) error {
			p := testkit.NewWorld(t).User("Елена")
			return func(db testkit.DB) error { return svcOn(db).MarkAllRead(bg, p.User) }
		},
		"worker: sent": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fs", 1)
			return func(db testkit.DB) error {
				w := NewWorker(db, &flaky{}, quiet)
				w.Batch = 1
				_, err := w.RunOnce(bg)
				return err
			}
		},
		"worker: retry": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fr", 1)
			return func(db testkit.DB) error {
				w := NewWorker(db, &flaky{err: func(mail.Message) error { return errors.New("down") }}, quiet)
				w.Batch = 1
				_, err := w.RunOnce(bg)
				return err
			}
		},
		"worker: give up": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fg", 1)
			return func(db testkit.DB) error {
				w := NewWorker(db, &flaky{err: func(mail.Message) error { return errors.New("down") }}, quiet)
				w.Batch, w.MaxAttempts = 1, 1
				_, err := w.RunOnce(bg)
				return err
			}
		},
		"worker: cleanup": func(t *testing.T) func(testkit.DB) error {
			return func(db testkit.DB) error { return NewWorker(db, &flaky{}, quiet).Cleanup(bg) }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) { testkit.RunFaults(t, prepare) })
	}
}

// logBuffer собирает записи журнала, чтобы проверить, что сбой не прошёл молча.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// Ошибка базы в проходе отправителя не останавливает Run: он пишет в журнал и пробует снова.
func TestRunSurvivesDatabaseFailures(t *testing.T) {
	for _, c := range []struct {
		name    string
		failAt  int32
		wantLog string
	}{
		{"pass fails", 1, "outbox pass failed"},
		{"cleanup fails", 2, "outbox cleanup failed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Очередь пуста: первый проход делает одно обращение (захват), второе — очистка.
			drain(t, newWorker(&flaky{}, nil))
			db, _ := testkit.Faulty(testkit.Pool, c.failAt)
			logs := &logBuffer{}
			f := &flaky{}
			w := NewWorker(db, f, slog.New(slog.NewTextHandler(logs, nil)))
			ctx, cancel := contextWithTimeout(5 * time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); w.Run(ctx, 10*time.Millisecond) }()
			time.Sleep(200 * time.Millisecond) // первый проход и очистка уже прошли на пустой очереди
			addr := queueMails(t, "survive", 1)[0]
			deadline := time.Now().Add(4 * time.Second)
			for f.to(addr) == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			<-done
			if f.to(addr) != 1 {
				t.Errorf("the mail was not delivered after a transient database failure (%d)", f.to(addr))
			}
			if !strings.Contains(logs.String(), c.wantLog) {
				t.Errorf("failure was not logged (%q):\n%s", c.wantLog, logs.String())
			}
		})
	}
}

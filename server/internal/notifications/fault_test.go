package notifications

import (
	"context"
	"errors"
	"testing"

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
		"worker: service failure": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fd", 1)
			return func(db testkit.DB) error {
				w := NewWorker(db, &flaky{err: func(mail.Message) error { return &mail.Error{Kind: mail.KindService, Err: errors.New("down")} }}, quiet)
				w.Batch = 1
				_, err := w.RunOnce(bg)
				return err
			}
		},
		"worker: permanent refusal": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fp", 1)
			return func(db testkit.DB) error {
				w := NewWorker(db, &flaky{err: func(mail.Message) error { return &mail.Error{Kind: mail.KindPermanent, Err: errors.New("no")} }}, quiet)
				w.Batch = 1
				_, err := w.RunOnce(bg)
				return err
			}
		},
		"worker: release after shutdown": func(t *testing.T) func(testkit.DB) error {
			queueMails(t, "fl", 2)
			return func(db testkit.DB) error {
				ctx, stop := context.WithCancel(bg)
				defer stop()
				w := NewWorker(db, &flaky{err: func(mail.Message) error { stop(); return errors.New("down") }}, quiet)
				w.Batch = 2
				_, err := w.RunOnce(ctx)
				return err
			}
		},
		"worker: queue stats": func(t *testing.T) func(testkit.DB) error {
			return func(db testkit.DB) error {
				_, err := NewWorker(db, &flaky{}, quiet).QueueStats(bg)
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

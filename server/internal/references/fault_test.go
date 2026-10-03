package references

import (
	"testing"
	"time"

	"scibox/server/internal/files"
	"scibox/server/internal/testkit"
)

func svcOn(w *world, db testkit.DB) *Service {
	s := newService(db, w.notes, w.svc.cfg)
	s.now = w.clock.Now
	return s
}

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	type prep func(t *testing.T, w *world) func(s *Service) error
	cases := map[string]prep{
		"add": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			app := w.application(me)
			return func(s *Service) error { _, err := s.Add(bg, me.User, app, referee()); return err }
		},
		"resend": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			app := w.application(me)
			req, _ := w.add(me, app)
			w.clock.Advance(48 * time.Hour)
			return func(s *Service) error { _, err := s.Resend(bg, me.User, app, req.ID); return err }
		},
		"cancel": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			app := w.application(me)
			req, _ := w.add(me, app)
			return func(s *Service) error { return s.Cancel(bg, me.User, app, req.ID) }
		},
		"list for applicant": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			app := w.application(me)
			w.add(me, app)
			return func(s *Service) error { _, err := s.ListForApplicant(bg, app); return err }
		},
		"list for staff": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			app := w.application(me)
			_, token := w.add(me, app)
			if err := w.svc.Submit(bg, token, LetterInput{Text: "письмо"}, &files.Upload{Name: "a.pdf", Data: pdfBytes()}); err != nil {
				t.Fatal(err)
			}
			return func(s *Service) error { _, err := s.ListForStaff(bg, app); return err }
		},
		"lookup": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			_, token := w.add(me, w.application(me))
			return func(s *Service) error { _, err := s.Lookup(bg, token); return err }
		},
		"submit text": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			_, token := w.add(me, w.application(me))
			return func(s *Service) error { return s.Submit(bg, token, LetterInput{Text: "письмо"}, nil) }
		},
		"submit pdf": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			_, token := w.add(me, w.application(me))
			return func(s *Service) error {
				return s.Submit(bg, token, LetterInput{}, &files.Upload{Name: "a.pdf", Data: pdfBytes()})
			}
		},
		"decline": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			_, token := w.add(me, w.application(me))
			return func(s *Service) error { return s.Decline(bg, token) }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
				w := newWorld(t)
				call := prepare(t, w)
				return func(db testkit.DB) error { return call(svcOn(w, db)) }
			})
		})
	}
}

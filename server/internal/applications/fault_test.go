package applications

import (
	"testing"
	"time"

	"scibox/server/internal/files"
	"scibox/server/internal/references"
	"scibox/server/internal/testkit"
)

func svcOn(w *world, db testkit.DB) *Service {
	s := newService(db, w.Prof, w.refs, w.notes, testConfig())
	s.now = w.clock.Now
	return s
}

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	type prep func(t *testing.T, w *world) func(s *Service) error
	cases := map[string]prep{
		"apply": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			in := w.input(me)
			in.Referees = []references.RefereeInput{refereeIn()}
			return func(s *Service) error {
				_, err := s.Apply(bg, me.User, in, []files.Upload{pdf("a.pdf")})
				return err
			}
		},
		"apply without extras": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			in := w.input(me)
			return func(s *Service) error { _, err := s.Apply(bg, me.User, in, nil); return err }
		},
		"mine": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			w.mustApply(me, nil)
			return func(s *Service) error { _, err := s.Mine(bg, me.User, 20, 0); return err }
		},
		"get as applicant": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil, pdf("a.pdf"))
			return func(s *Service) error { _, err := s.Get(bg, me.User, d.ID); return err }
		},
		"get as staff": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			return func(s *Service) error { _, err := s.Get(bg, w.tm.HeadA.User, d.ID); return err }
		},
		"file as applicant": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			return func(s *Service) error { _, _, err := s.File(bg, me.User, d.ID, d.CV.ID); return err }
		},
		"file as staff": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			return func(s *Service) error { _, _, err := s.File(bg, w.tm.HeadA.User, d.ID, d.CV.ID); return err }
		},
		"withdraw": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			return func(s *Service) error { return s.Withdraw(bg, me.User, d.ID) }
		},
		"for vacancy (free)": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			return func(s *Service) error { _, err := s.ForVacancy(bg, me.User, w.vacancy.ID); return err }
		},
		"for vacancy (applied)": func(t *testing.T, w *world) func(*Service) error {
			me := w.Applicant("Мария")
			w.mustApply(me, nil)
			return func(s *Service) error { _, err := s.ForVacancy(bg, me.User, w.vacancy.ID); return err }
		},
		"for vacancy (staff)": func(t *testing.T, w *world) func(*Service) error {
			return func(s *Service) error { _, err := s.ForVacancy(bg, w.tm.HeadA.User, w.vacancy.ID); return err }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
				w := newWorld(t)
				w.clock.Advance(time.Millisecond)
				call := prepare(t, w)
				return func(db testkit.DB) error { return call(svcOn(w, db)) }
			})
		})
	}
}

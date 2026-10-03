package offers

import (
	"testing"

	"scibox/server/internal/testkit"
)

func svcOn(w *world, db testkit.DB) *Service {
	s := newService(db, w.notes, testConfig())
	s.now = w.clock.Now
	return s
}

type faultPrep func(t *testing.T, w *world) func(s *Service) error

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	cases := map[string]faultPrep{
		"invite": func(t *testing.T, w *world) func(*Service) error {
			_, profile := w.scientist("Учёный", "public")
			return func(s *Service) error {
				_, err := s.Invite(bg, w.tm.HR.User, InviteInput{VacancyID: w.vacancy.ID, ProfileID: profile, Message: "Привет"})
				return err
			}
		},
		"cancel": func(t *testing.T, w *world) func(*Service) error {
			_, profile := w.scientist("Учёный", "public")
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			return func(s *Service) error { return s.Cancel(bg, w.tm.HR.User, o.ID) }
		},
		"answer": func(t *testing.T, w *world) func(*Service) error {
			sci, profile := w.scientist("Учёный", "public")
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			return func(s *Service) error {
				return s.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested, Note: "да"})
			}
		},
		"get": func(t *testing.T, w *world) func(*Service) error {
			sci, profile := w.scientist("Учёный", "public")
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			return func(s *Service) error { _, err := s.Get(bg, sci.User, o.ID); return err }
		},
		"mine": func(t *testing.T, w *world) func(*Service) error {
			sci, profile := w.scientist("Учёный", "public")
			w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			return func(s *Service) error { _, err := s.Mine(bg, sci.User, StatusPending, 10, 0); return err }
		},
		"sent": func(t *testing.T, w *world) func(*Service) error {
			_, profile := w.scientist("Учёный", "public")
			w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			return func(s *Service) error {
				_, err := s.Sent(bg, w.tm.HR.User, SentFilter{Status: StatusPending})
				return err
			}
		},
		"targets": func(t *testing.T, w *world) func(*Service) error {
			_, profile := w.scientist("Учёный", "public")
			return func(s *Service) error { _, err := s.Targets(bg, w.tm.HR.User, profile); return err }
		},
		"targets for a person outside organizations": func(t *testing.T, w *world) func(*Service) error {
			_, profile := w.scientist("Учёный", "public")
			return func(s *Service) error { _, err := s.Targets(bg, w.tm.Out.User, profile); return err }
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

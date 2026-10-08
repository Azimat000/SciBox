package matching

import (
	"testing"
	"time"

	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

func svcOn(w *world, db testkit.DB) *Service {
	s := newService(db, w.Vac, w.notes, testConfig(), silent())
	s.now = w.clock.Now
	return s
}

type faultPrep func(t *testing.T, w *world) func(s *Service) error

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
// (Собственные запросы notifications и vacancies идут через общий пул: их сбои проверены в тех пакетах.)
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	cases := map[string]faultPrep{
		"add favorite": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			return func(s *Service) error { return s.AddFavorite(bg, p.User, v.ID) }
		},
		"add favorite again": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			w.addFavorite(p, v.ID)
			return func(s *Service) error { return s.AddFavorite(bg, p.User, v.ID) }
		},
		"remove favorite": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			w.addFavorite(p, v.ID)
			return func(s *Service) error { return s.RemoveFavorite(bg, p.User, v.ID) }
		},
		"favorite ids": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			return func(s *Service) error { _, err := s.FavoriteIDs(bg, p.User); return err }
		},
		"favorites": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			w.addFavorite(p, v.ID)
			return func(s *Service) error { _, err := s.Favorites(bg, p.User, 20, 0); return err }
		},
		"favorites beyond the last page": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			w.addFavorite(p, v.ID)
			return func(s *Service) error { _, err := s.Favorites(bg, p.User, 20, 40); return err }
		},
		"deadlines": func(t *testing.T, w *world) func(*Service) error {
			p, v := w.User("Анна"), w.publish(vacancyOpts{})
			w.addFavorite(p, v.ID)
			return func(s *Service) error { _, err := s.Deadlines(bg, p.User); return err }
		},
		"matches": func(t *testing.T, w *world) func(*Service) error {
			p := w.seeker("Анна", profiles.CoreInput{Specialties: []string{"1.4.4"}})
			w.publish(vacancyOpts{specialties: []string{"1.4.4"}})
			return func(s *Service) error { _, err := s.Matches(bg, p.User, 20, 0); return err }
		},
		"matches without a profile": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			return func(s *Service) error { _, err := s.Matches(bg, p.User, 20, 0); return err }
		},
		"create search": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			return func(s *Service) error {
				_, err := s.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a"})
				return err
			}
		},
		"searches": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			return func(s *Service) error { _, err := s.Searches(bg, p.User); return err }
		},
		"search": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			saved, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a"})
			return func(s *Service) error { _, err := s.Search(bg, p.User, saved.ID); return err }
		},
		"update search": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			saved, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a", Frequency: FreqOff})
			return func(s *Service) error {
				_, err := s.UpdateSearch(bg, p.User, saved.ID, SearchInput{Name: "у", Frequency: FreqDaily})
				return err
			}
		},
		"delete search": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			saved, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a"})
			return func(s *Service) error { return s.DeleteSearch(bg, p.User, saved.ID) }
		},
		"digests": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			mark := marker()
			w.clock.Set(time.Now().UTC().Truncate(time.Microsecond))
			if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=" + mark, Frequency: FreqInstant}); err != nil {
				t.Fatal(err)
			}
			w.publish(vacancyOpts{title: "Новая " + mark})
			w.clock.Advance(5 * time.Minute)
			return func(s *Service) error { _, err := s.SendDigests(bg); return err }
		},
		"reminders": func(t *testing.T, w *world) func(*Service) error {
			p := w.User("Анна")
			w.clock.Set(remindNow)
			v := w.publish(vacancyOpts{})
			w.setDeadline(v.ID, oct(12))
			w.addFavorite(p, v.ID)
			w.backdateFavorite(p, v.ID, msk(2026, 10, 1, 12, 0))
			return func(s *Service) error { _, err := s.SendReminders(bg); return err }
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

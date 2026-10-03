package profiles

import (
	"testing"
)

// Каждое обращение к базе в операции по очереди «ломается»; ошибка должна дойти до вызывающего.
func TestDatabaseFailuresAreNeverSwallowed(t *testing.T) {
	cases := map[string]func(t *testing.T, w *world) func(s *Service) error{
		"own": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.Own(bg, p.User); return err }
		},
		"own with data": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			if _, err := w.svc.SaveCore(bg, p.User, goodCore()); err != nil {
				t.Fatal(err)
			}
			w.addItem(p, goodPublication())
			return func(s *Service) error { _, err := s.Own(bg, p.User); return err }
		},
		"get as anonymous": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			w.setVisibility(p, "public", false)
			own, _ := w.svc.Own(bg, p.User)
			return func(s *Service) error { _, err := s.Get(bg, own.Profile.ID, nil); return err }
		},
		"get as staff": func(t *testing.T, w *world) func(*Service) error {
			p, staff := w.user("Елена"), w.staff("Сотрудник")
			w.setVisibility(p, "orgs", false)
			own, _ := w.svc.Own(bg, p.User)
			return func(s *Service) error { _, err := s.Get(bg, own.Profile.ID, &staff.User); return err }
		},
		"get as owner": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			own, _ := w.svc.Own(bg, p.User)
			return func(s *Service) error { _, err := s.Get(bg, own.Profile.ID, &p.User); return err }
		},
		"save core": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.SaveCore(bg, p.User, goodCore()); return err }
		},
		"save core without specialties": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.SaveCore(bg, p.User, CoreInput{}); return err }
		},
		"set privacy": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.SetPrivacy(bg, p.User, "public", true); return err }
		},
		"add item": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.AddItem(bg, p.User, goodPublication()); return err }
		},
		"add item without doi": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			in := ItemInput{Kind: KindTeaching, ItemFields: validItem(KindTeaching)}
			return func(s *Service) error { _, err := s.AddItem(bg, p.User, in); return err }
		},
		"update item": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			it := w.addItem(p, goodPublication())
			return func(s *Service) error { _, err := s.UpdateItem(bg, p.User, it.ID, goodPublication()); return err }
		},
		"delete item": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			it := w.addItem(p, goodPublication())
			return func(s *Service) error { return s.DeleteItem(bg, p.User, it.ID) }
		},
		"lookup doi (new profile)": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, err := s.LookupDOI(bg, p.User, "10.1234/x"); return err }
		},
		"lookup doi (existing profile)": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			w.addItem(p, goodPublication())
			return func(s *Service) error { _, err := s.LookupDOI(bg, p.User, "10.1234/other"); return err }
		},
		"cv": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			return func(s *Service) error { _, _, err := s.CV(bg, p.User, nil); return err }
		},
		"for application": func(t *testing.T, w *world) func(*Service) error {
			p := w.user("Елена")
			if _, err := w.svc.SaveCore(bg, p.User, goodCore()); err != nil {
				t.Fatal(err)
			}
			return func(s *Service) error { _, err := s.ForApplication(bg, p.User, "a@example.ru"); return err }
		},
		"cv of another": func(t *testing.T, w *world) func(*Service) error {
			p, q := w.user("Елена"), w.user("Другой")
			w.setVisibility(p, "public", false)
			own, _ := w.svc.Own(bg, p.User)
			return func(s *Service) error { _, _, err := s.CV(bg, q.User, &own.Profile.ID); return err }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) { runFaults(t, prepare) })
	}
}

func TestCorruptItemIsReported(t *testing.T) {
	w := newWorld(t)
	p := w.user("Елена")
	own, _ := w.svc.Own(bg, p.User)
	if _, err := sharedPool.Exec(bg, `INSERT INTO profile_items (profile_id, kind, data, created_at, updated_at) VALUES ($1, 'grant', '"text"', now(), now())`, own.Profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Own(bg, p.User); err == nil {
		t.Error("повреждённая запись должна давать ошибку, а не пустой раздел")
	}
}

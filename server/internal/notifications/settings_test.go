package notifications

import (
	"net/http"
	"testing"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

func TestMailCategory(t *testing.T) {
	for kind, want := range map[string]string{
		KindSavedSearch: "new_vacancies", KindDeadlineReminder: "deadlines",
		"application_received": "", "offer_received": "", "": "", "other": "",
	} {
		if got := mailCategory(kind); got != want {
			t.Errorf("%q: %q, ожидали %q", kind, got, want)
		}
	}
}

func TestSettingsDefaultToEverythingOn(t *testing.T) {
	w := testkit.NewWorld(t)
	p := w.User("Елена")
	got, err := newSvc().Settings(bg, p.User)
	if err != nil || !got.EmailNewVacancies || !got.EmailDeadlines {
		t.Errorf("%+v %v", got, err)
	}
}

func TestSettingsAreSavedPerPerson(t *testing.T) {
	w := testkit.NewWorld(t)
	a, b := w.User("Анна"), w.User("Борис")
	s := newSvc()
	saved, err := s.SaveSettings(bg, a.User, Settings{EmailNewVacancies: false, EmailDeadlines: true})
	if err != nil || saved.EmailNewVacancies || !saved.EmailDeadlines {
		t.Fatalf("%+v %v", saved, err)
	}
	if got, _ := s.Settings(bg, a.User); got.EmailNewVacancies || !got.EmailDeadlines {
		t.Errorf("Анна: %+v", got)
	}
	if got, _ := s.Settings(bg, b.User); !got.EmailNewVacancies || !got.EmailDeadlines {
		t.Errorf("у Бориса остаётся всё включённым: %+v", got)
	}
	// Повторное сохранение заменяет прежнее.
	if _, err := s.SaveSettings(bg, a.User, Settings{EmailNewVacancies: true, EmailDeadlines: false}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Settings(bg, a.User); !got.EmailNewVacancies || got.EmailDeadlines {
		t.Errorf("после замены: %+v", got)
	}
}

// Отключаются только письма двух видов; уведомление на сайте остаётся всегда.
func TestEmitHonoursEmailSettings(t *testing.T) {
	w := testkit.NewWorld(t)
	s := newSvc()
	for _, c := range []struct {
		name     string
		settings Settings
		kind     string
		mail     bool
	}{
		{"всё включено: о новых вакансиях", Settings{true, true}, KindSavedSearch, true},
		{"всё включено: о сроках", Settings{true, true}, KindDeadlineReminder, true},
		{"выключены новые вакансии: письма нет", Settings{false, true}, KindSavedSearch, false},
		{"выключены новые вакансии: о сроках идёт", Settings{false, true}, KindDeadlineReminder, true},
		{"выключены сроки: письма нет", Settings{true, false}, KindDeadlineReminder, false},
		{"выключены сроки: о новых вакансиях идёт", Settings{true, false}, KindSavedSearch, true},
		{"всё выключено: отклик всё равно приходит письмом", Settings{false, false}, "application_received", true},
		{"всё выключено: приглашение тоже", Settings{false, false}, "offer_received", true},
	} {
		p := w.User("Получатель")
		if _, err := s.SaveSettings(bg, p.User, c.settings); err != nil {
			t.Fatal(err)
		}
		emit(t, s, Notice{UserID: p.ID, Kind: c.kind, Title: "Заголовок", Body: "Текст"})
		list, _ := s.List(bg, p.User, 10, 0, false)
		if list.Total != 1 {
			t.Errorf("%s: уведомлений на сайте %d, должно остаться 1", c.name, list.Total)
		}
		if got := len(outboxFor(t, p.Email)); (got == 1) != c.mail {
			t.Errorf("%s: писем %d, ожидали письмо = %v", c.name, got, c.mail)
		}
	}
}

func TestHTTPSettings(t *testing.T) {
	a, _ := newAPI(t)
	me := a.User("Я")
	for _, method := range []string{"GET", "PUT"} {
		if r := a.Do(nil, method, "/api/notification-settings", map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s без входа: %d", method, r.Code)
		}
	}
	r := a.Do(&me, "GET", "/api/notification-settings", nil)
	if j := r.JSON(t); r.Code != 200 || j["email_new_vacancies"] != true || j["email_deadlines"] != true {
		t.Errorf("по умолчанию: %d %s", r.Code, r.Raw)
	}
	r = a.Do(&me, "PUT", "/api/notification-settings", map[string]any{"email_new_vacancies": false, "email_deadlines": true})
	if j := r.JSON(t); r.Code != 200 || j["email_new_vacancies"] != false || j["email_deadlines"] != true {
		t.Errorf("сохранение: %d %s", r.Code, r.Raw)
	}
	r = a.Do(&me, "GET", "/api/notification-settings", nil)
	if j := r.JSON(t); j["email_new_vacancies"] != false {
		t.Errorf("прочитано обратно: %s", r.Raw)
	}
	for name, body := range map[string]any{
		"пусто": map[string]any{}, "одно поле": map[string]any{"email_deadlines": true}, "другое поле": map[string]any{"email_new_vacancies": true},
	} {
		if r := a.Do(&me, "PUT", "/api/notification-settings", body); r.Code != 422 {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	for name, body := range map[string]any{
		"сломанный JSON": "{broken", "лишнее поле": map[string]any{"email_new_vacancies": true, "email_deadlines": true, "x": 1},
		"не логическое": map[string]any{"email_new_vacancies": "да", "email_deadlines": true},
	} {
		if r := a.Do(&me, "PUT", "/api/notification-settings", body); r.Code != 400 {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	// Ничего из неверного не сохранилось.
	if j := a.Do(&me, "GET", "/api/notification-settings", nil).JSON(t); j["email_new_vacancies"] != false || j["email_deadlines"] != true {
		t.Errorf("после ошибок: %v", j)
	}
}

func TestHTTPSettingsInternalError(t *testing.T) {
	a, svc := newAPI(t)
	me := a.User("Я")
	healthy := svc.q
	for _, method := range []string{"GET", "PUT"} {
		db, _ := testkit.Faulty(testkit.Pool, 1)
		svc.q = dbgenFor(db)
		r := a.Do(&me, method, "/api/notification-settings", map[string]any{"email_new_vacancies": true, "email_deadlines": true})
		svc.q = healthy
		if r.Code != 500 || r.ErrCode(t) != "internal" {
			t.Errorf("%s: %d %s", method, r.Code, r.Raw)
		}
	}
}

func TestSettingsFailuresAreNeverSwallowed(t *testing.T) {
	testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
		p := testkit.NewWorld(t).User("Елена")
		return func(db testkit.DB) error { _, err := svcOn(db).Settings(bg, p.User); return err }
	})
	testkit.RunFaults(t, func(t *testing.T) func(testkit.DB) error {
		p := testkit.NewWorld(t).User("Елена")
		return func(db testkit.DB) error {
			_, err := svcOn(db).SaveSettings(bg, p.User, Settings{EmailNewVacancies: true})
			return err
		}
	})
}

func dbgenFor(db testkit.DB) *dbgen.Queries { return dbgen.New(db) }

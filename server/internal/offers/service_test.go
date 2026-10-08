package offers

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// ---- кто может приглашать ----

// Каждая пара «кто → на какую вакансию → можно ли». Отказ ничего не меняет: ни приглашения, ни уведомления, ни письма,
// ни счётчика частоты.
func TestWhoCanInvite(t *testing.T) {
	w := newWorld(t)
	vacB := w.tm.Published(&w.tm.UnitB.ID)
	vacOrg := w.tm.Published(nil)
	seeker := w.User("Соискатель")
	w.FillProfile(seeker)

	tests := []struct {
		vacancy vacancies.Detail
		label   string
		allowed map[string]bool
	}{
		{w.vacancy, "подразделение А", map[string]bool{"владелец": true, "кадровик": true, "руководитель А": true, "руководитель Б": false, "посторонний": false}},
		{vacB, "подразделение Б", map[string]bool{"владелец": true, "кадровик": true, "руководитель А": false, "руководитель Б": true, "посторонний": false}},
		{vacOrg, "вся организация", map[string]bool{"владелец": true, "кадровик": true, "руководитель А": false, "руководитель Б": false, "посторонний": false}},
	}
	for _, tc := range tests {
		for _, p := range append(people(w), struct {
			name string
			who  testkit.Person
		}{"соискатель", seeker}) {
			t.Run(tc.label+"/"+p.name, func(t *testing.T) {
				sci, profile := w.scientist("Учёный", "public")
				before := w.snap(tc.vacancy.ID, sci, p.who)
				o, err := w.invite(p.who, tc.vacancy.ID, profile, "Приглашаем вас")
				if tc.allowed[p.name] {
					if err != nil {
						t.Fatalf("должно быть можно: %v", err)
					}
					if o.Status != StatusPending || o.Scientist == nil || o.Scientist.ProfileID != profile || !o.CanCancel || o.CanAnswer {
						t.Errorf("приглашение: %+v", o)
					}
					return
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("должен быть отказ «нет такой вакансии», получили %v", err)
				}
				if o.ID != uuid.Nil {
					t.Errorf("вместе с отказом ушли данные: %+v", o)
				}
				if after := w.snap(tc.vacancy.ID, sci, p.who); after != before {
					t.Errorf("отказ оставил следы: было %+v, стало %+v", before, after)
				}
			})
		}
	}
}

func TestInviteUnknownVacancy(t *testing.T) {
	w := newWorld(t)
	_, profile := w.scientist("Учёный", "public")
	if _, err := w.invite(w.tm.Owner, uuid.New(), profile, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующая вакансия: %v", err)
	}
}

// Кого можно пригласить: приватность профиля решает пакет privacy; организация видит то, что положено сотруднику.
func TestWhoCanBeInvited(t *testing.T) {
	w := newWorld(t)
	for _, tc := range []struct {
		visibility string
		ok         bool
	}{{"hidden", false}, {"orgs", true}, {"public", true}} {
		t.Run(tc.visibility, func(t *testing.T) {
			sci, profile := w.scientist("Учёный", tc.visibility)
			before := w.snap(w.vacancy.ID, sci, w.tm.HR)
			_, err := w.invite(w.tm.HR, w.vacancy.ID, profile, "")
			if tc.ok && err != nil {
				t.Fatalf("профиль виден организации, а приглашение не прошло: %v", err)
			}
			if !tc.ok {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("скрытый профиль должен быть «нет такого»: %v", err)
				}
				if after := w.snap(w.vacancy.ID, sci, w.tm.HR); after != before {
					t.Errorf("отказ оставил следы: %+v → %+v", before, after)
				}
			}
		})
	}
	if _, err := w.invite(w.tm.HR, w.vacancy.ID, uuid.New(), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий профиль: %v", err)
	}
}

// Нельзя приглашать тех, кто сам ведёт эту вакансию (в том числе себя); руководитель чужого подразделения — обычный учёный.
func TestStaffCannotBeInvitedToTheirOwnVacancy(t *testing.T) {
	w := newWorld(t)
	profileOf := func(p testkit.Person, vis string) uuid.UUID {
		w.FillProfile(p)
		if _, err := w.Prof.SetPrivacy(bg, p.User, vis, false); err != nil {
			t.Fatal(err)
		}
		own, _ := w.Prof.Own(bg, p.User)
		return own.Profile.ID
	}
	owner, hr, headA, headB := profileOf(w.tm.Owner, "public"), profileOf(w.tm.HR, "public"), profileOf(w.tm.HeadA, "public"), profileOf(w.tm.HeadB, "public")
	vacOrg := w.tm.Published(nil)
	for _, c := range []struct {
		name    string
		by      testkit.Person
		vacancy uuid.UUID
		target  uuid.UUID
		want    error
	}{
		{"владелец сам себя", w.tm.Owner, w.vacancy.ID, owner, ErrStaffInvitee},
		{"владелец зовёт кадровика", w.tm.Owner, w.vacancy.ID, hr, ErrStaffInvitee},
		{"кадровик зовёт владельца", w.tm.HR, w.vacancy.ID, owner, ErrStaffInvitee},
		{"кадровик зовёт руководителя этого подразделения", w.tm.HR, w.vacancy.ID, headA, ErrStaffInvitee},
		{"кадровик зовёт руководителя чужого подразделения", w.tm.HR, w.vacancy.ID, headB, nil},
		{"вакансия на всю организацию: руководитель подразделения ей не заведует", w.tm.HR, vacOrg.ID, headA, nil},
		{"руководитель сам себя", w.tm.HeadA, w.vacancy.ID, headA, ErrStaffInvitee},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.invite(c.by, c.vacancy, c.target, "")
			if !errors.Is(err, c.want) {
				t.Errorf("получили %v, ожидали %v", err, c.want)
			}
		})
	}
}

// ---- правила вакансии ----

func TestInviteVacancyMustBeOpen(t *testing.T) {
	w := newWorld(t)
	draft, err := w.Vac.Create(bg, w.tm.Owner.User, w.tm.Slug, testkit.VacancyInput(&w.tm.UnitA.ID))
	if err != nil {
		t.Fatal(err)
	}
	closed := w.tm.Published(&w.tm.UnitA.ID)
	w.tm.Status(closed.ID, vacancies.StatusClosed)
	archived := w.tm.Published(&w.tm.UnitA.ID)
	w.tm.Status(archived.ID, vacancies.StatusClosed)
	w.tm.Status(archived.ID, vacancies.StatusArchived)

	for name, id := range map[string]uuid.UUID{"черновик": draft.ID, "закрытая": closed.ID, "архивная": archived.ID} {
		t.Run(name, func(t *testing.T) {
			sci, profile := w.scientist("Учёный", "public")
			before := w.snap(id, sci, w.tm.HR)
			if _, err := w.invite(w.tm.HR, id, profile, ""); !errors.Is(err, ErrVacancyClosed) {
				t.Errorf("получили %v", err)
			}
			if after := w.snap(id, sci, w.tm.HR); after != before {
				t.Errorf("отказ оставил следы: %+v → %+v", before, after)
			}
		})
	}
	// Закрытая вакансия, открытая снова, принимает приглашения.
	w.tm.Status(closed.ID, vacancies.StatusPublished)
	_, profile := w.scientist("Учёный", "public")
	w.mustInvite(w.tm.HR, closed.ID, profile, "")
}

// Срок подачи считается по Москве, последний день включительно (D-057).
func TestInviteDeadline(t *testing.T) {
	w := newWorld(t)
	deadline, err := time.Parse("2006-01-02", w.vacancy.Deadline)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		now  time.Time
		ok   bool
	}{
		{"за день до срока", deadline.Add(-24 * time.Hour), true},
		{"последний день, 23:59 по Москве", deadline.Add(20*time.Hour + 59*time.Minute + 59*time.Second), true},
		{"следующий день, 00:00 по Москве", deadline.Add(21 * time.Hour), false},
		{"через месяц после срока", deadline.Add(30 * 24 * time.Hour), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.clock.Set(tc.now)
			sci, profile := w.scientist("Учёный", "public")
			before := w.snap(w.vacancy.ID, sci, w.tm.HR)
			_, err := w.invite(w.tm.HR, w.vacancy.ID, profile, "")
			if tc.ok != (err == nil) || (!tc.ok && !errors.Is(err, ErrDeadlinePassed)) {
				t.Fatalf("ok=%v, ошибка %v", tc.ok, err)
			}
			if !tc.ok {
				if after := w.snap(w.vacancy.ID, sci, w.tm.HR); after != before {
					t.Errorf("отказ оставил следы: %+v → %+v", before, after)
				}
			}
		})
	}
}

// Вакансия без срока принимает приглашения всегда.
func TestInviteWithoutDeadline(t *testing.T) {
	w := newWorld(t)
	in := testkit.VacancyInput(&w.tm.UnitA.ID)
	in.Deadline = ""
	d, err := w.Vac.Create(bg, w.tm.Owner.User, w.tm.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Vac.SetStatus(bg, w.tm.Owner.User, d.ID, vacancies.StatusPublished); err != nil {
		t.Fatal(err)
	}
	w.clock.Set(time.Now().UTC().Truncate(time.Microsecond).AddDate(5, 0, 0))
	_, profile := w.scientist("Учёный", "public")
	w.mustInvite(w.tm.HR, d.ID, profile, "")
}

// ---- один раз, отклик, лимит ----

func TestOneLiveOfferPerPersonAndVacancy(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	other := w.tm.Published(&w.tm.UnitA.ID)
	first := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")

	before := w.snap(w.vacancy.ID, sci, w.tm.Owner)
	if _, err := w.invite(w.tm.Owner, w.vacancy.ID, profile, ""); !errors.Is(err, ErrAlreadyOffered) {
		t.Fatalf("повторное приглашение: %v", err)
	}
	if after := w.snap(w.vacancy.ID, sci, w.tm.Owner); after != before {
		t.Errorf("отказ оставил следы (в том числе потраченный лимит): %+v → %+v", before, after)
	}
	// Другая вакансия — другое приглашение.
	w.mustInvite(w.tm.HR, other.ID, profile, "")

	// Ответ учёного не открывает дорогу к новому приглашению.
	if err := w.svc.Answer(bg, sci.User, first.ID, AnswerInput{Action: ActionDeclined}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.invite(w.tm.HR, w.vacancy.ID, profile, ""); !errors.Is(err, ErrAlreadyOffered) {
		t.Errorf("после ответа «Не сейчас» звать снова нельзя: %v", err)
	}

	// После отзыва можно пригласить снова.
	third := w.tm.Published(&w.tm.UnitA.ID)
	o := w.mustInvite(w.tm.HR, third.ID, profile, "")
	if err := w.svc.Cancel(bg, w.tm.HR.User, o.ID); err != nil {
		t.Fatal(err)
	}
	again := w.mustInvite(w.tm.HR, third.ID, profile, "Попробуем ещё раз")
	if again.ID == o.ID || again.Status != StatusPending {
		t.Errorf("новое приглашение: %+v", again)
	}
}

func TestSimultaneousInvitesMakeOneOffer(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	var wg sync.WaitGroup
	results := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = w.invite(w.tm.HR, w.vacancy.ID, profile, "")
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrAlreadyOffered):
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("успешных приглашений %d, ожидали одно", ok)
	}
	if s := w.snap(w.vacancy.ID, sci, w.tm.HR); s.offers != 1 || s.notices != 1 || s.mails != 1 || s.rates != 1 {
		t.Errorf("следы: %+v", s)
	}
}

// Если человек уже откликнулся, приглашать его незачем; отозванный отклик не мешает.
func TestInviteAfterApplication(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	w.apply(sci, w.vacancy.ID, "sent")
	before := w.snap(w.vacancy.ID, sci, w.tm.HR)
	if _, err := w.invite(w.tm.HR, w.vacancy.ID, profile, ""); !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("уже есть отклик: %v", err)
	}
	if after := w.snap(w.vacancy.ID, sci, w.tm.HR); after != before {
		t.Errorf("отказ оставил следы: %+v → %+v", before, after)
	}
	sci2, profile2 := w.scientist("Учёный 2", "public")
	w.apply(sci2, w.vacancy.ID, "withdrawn")
	w.mustInvite(w.tm.HR, w.vacancy.ID, profile2, "")
}

func TestInviteRateLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg = Config{Invite: Limit{Max: 3, Window: time.Hour}}
	var vacs []uuid.UUID
	for range 5 {
		vacs = append(vacs, w.tm.Published(&w.tm.UnitA.ID).ID)
	}
	_, profile := w.scientist("Учёный", "public")
	for i := range 3 {
		w.mustInvite(w.tm.HR, vacs[i], profile, "")
	}
	_, err := w.invite(w.tm.HR, vacs[3], profile, "")
	var rl *auth.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > time.Hour {
		t.Fatalf("четвёртое приглашение: %v", err)
	}
	// Лимит у каждого свой, а ошибка проверки лимит не тратит.
	w.mustInvite(w.tm.Owner, vacs[3], profile, "")
	for range 5 {
		if _, err := w.invite(w.tm.Owner, vacs[4], profile, strings.Repeat("я", 1001)); err == nil {
			t.Fatal("длинное сообщение прошло")
		}
	}
	w.mustInvite(w.tm.Owner, vacs[4], profile, "")
	// Окно прошло — лимит снова свободен.
	w.clock.Advance(time.Hour + time.Second)
	w.mustInvite(w.tm.HR, w.tm.Published(&w.tm.UnitA.ID).ID, profile, "")
}

func TestInviteMessage(t *testing.T) {
	w := newWorld(t)
	long := strings.Repeat("я", 1001)
	for _, tc := range []struct {
		name, msg, want string // want: ожидаемое сохранённое сообщение; пусто — ошибка поля
		ok              bool
	}{
		{"без сообщения", "", "", true},
		{"пробелы по краям", "  Здравствуйте!  \n", "Здравствуйте!", true},
		{"ровно предел", strings.Repeat("я", 1000), strings.Repeat("я", 1000), true},
		{"с переносом строки и табуляцией", "Строка\n\tвторая", "Строка\n\tвторая", true},
		{"на знак длиннее", long, "", false},
		{"управляющий знак", "Привет\x00мир", "", false},
		{"пробелы не считаются в длину по краям", " " + strings.Repeat("я", 1000) + " ", strings.Repeat("я", 1000), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sci, profile := w.scientist("Учёный", "public")
			vac := w.tm.Published(&w.tm.UnitA.ID)
			o, err := w.invite(w.tm.HR, vac.ID, profile, tc.msg)
			if tc.ok {
				if err != nil || o.Message != tc.want {
					t.Fatalf("сообщение %q, ошибка %v", o.Message, err)
				}
				return
			}
			var verr *auth.ValidationError
			if !errors.As(err, &verr) || verr.Fields["message"] == "" {
				t.Fatalf("ожидали ошибку поля message: %v", err)
			}
			if s := w.snap(vac.ID, sci, w.tm.HR); s.offers != 0 || s.notices != 0 {
				t.Errorf("ошибка оставила следы: %+v", s)
			}
		})
	}
}

// ---- уведомления ----

func TestInviteNotifiesTheScientistOnly(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёная Анна", "public")
	staffBefore := map[string]int{}
	for _, p := range people(w) {
		staffBefore[p.name] = testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, p.who.ID)
	}
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Ждём вас в лаборатории")

	var title, body, link string
	if err := testkit.Pool.QueryRow(bg, `SELECT title, body, link FROM notifications WHERE user_id = $1 AND kind = 'offer_received'`, sci.ID).Scan(&title, &body, &link); err != nil {
		t.Fatalf("уведомления нет: %v", err)
	}
	if title != "Вас приглашают на вакансию" || link != "/offers/"+o.ID.String() {
		t.Errorf("уведомление: %q, %q", title, link)
	}
	for _, part := range []string{w.vacancy.Title, "Институт набора", "Ждём вас в лаборатории"} {
		if !strings.Contains(body, part) {
			t.Errorf("в тексте нет %q: %s", part, body)
		}
	}
	// Имя сотрудника, который пригласил, учёному не сообщается (D-096).
	if strings.Contains(body, "Кадровик") || strings.Contains(body, w.tm.HR.Email) {
		t.Errorf("в тексте есть сотрудник: %s", body)
	}
	if mailCount(t, sci.Email) != 1 {
		t.Errorf("писем учёному %d", mailCount(t, sci.Email))
	}
	for _, p := range people(w) {
		if now := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, p.who.ID); now != staffBefore[p.name] {
			t.Errorf("%s получил лишнее уведомление", p.name)
		}
	}
}

// ---- ответ учёного ----

func TestAnswer(t *testing.T) {
	for _, tc := range []struct {
		action, status, noteLabel string
	}{{ActionInterested, StatusInterested, "«Интересно»"}, {ActionDeclined, StatusDeclined, "«Не сейчас»"}} {
		t.Run(tc.action, func(t *testing.T) {
			w := newWorld(t)
			sci, profile := w.scientist("Анна Ильина", "public")
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			w.clock.Advance(time.Minute)
			if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: tc.action, Note: "  Спасибо, подумаю  "}); err != nil {
				t.Fatal(err)
			}
			got, err := w.svc.Get(bg, sci.User, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status || got.AnswerNote != "Спасибо, подумаю" || got.AnsweredAt == nil || got.CanAnswer || got.CanCancel {
				t.Errorf("после ответа: %+v", got)
			}
			// Организация видит ответ; отозвать уже нельзя.
			sent, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{})
			if len(sent.Items) != 1 || sent.Items[0].Status != tc.status || sent.Items[0].AnswerNote != "Спасибо, подумаю" || sent.Items[0].CanCancel {
				t.Errorf("у организации: %+v", sent.Items)
			}
			// Уведомления: те, кто ведёт вакансию подразделения А, и только они.
			want := map[string]bool{"владелец": true, "кадровик": true, "руководитель А": true, "руководитель Б": false, "посторонний": false}
			for _, p := range people(w) {
				n := notificationsOf(t, p.who, "offer_"+tc.action)
				if want[p.name] != (n == 1) || n > 1 {
					t.Errorf("%s: уведомлений %d", p.name, n)
				}
			}
			if n := notificationsOf(t, sci, "offer_"+tc.action); n != 0 {
				t.Errorf("учёный получил уведомление о собственном ответе")
			}
			var title, body, link string
			if err := testkit.Pool.QueryRow(bg, `SELECT title, body, link FROM notifications WHERE user_id = $1 AND kind = $2`, w.tm.HR.ID, "offer_"+tc.action).Scan(&title, &body, &link); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(title, tc.noteLabel) || link != "/sent-offers" || !strings.Contains(body, "Анна Ильина") || !strings.Contains(body, "Спасибо, подумаю") || !strings.Contains(body, w.vacancy.Title) {
				t.Errorf("текст: %q %q %q", title, body, link)
			}
			if mailCount(t, w.tm.HR.Email) < 1 {
				t.Errorf("кадровику не ушло письмо")
			}
		})
	}
}

// Вакансия на всю организацию: ответ получают владелец и кадровик, руководители подразделений нет.
func TestAnswerToOrganizationWideVacancy(t *testing.T) {
	w := newWorld(t)
	vac := w.tm.Published(nil)
	sci, profile := w.scientist("Учёный", "public")
	o := w.mustInvite(w.tm.HR, vac.ID, profile, "")
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}
	for _, p := range people(w) {
		want := p.name == "владелец" || p.name == "кадровик"
		if got := notificationsOf(t, p.who, "offer_interested") == 1; got != want {
			t.Errorf("%s: получил уведомление = %v", p.name, got)
		}
	}
}

func TestAnswerRules(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	stranger, _ := w.scientist("Чужой учёный", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")

	// Отвечает только приглашённый: остальные получают «нет такого», и ничего не меняется.
	for _, p := range append(people(w), struct {
		name string
		who  testkit.Person
	}{"чужой учёный", stranger}) {
		if err := w.svc.Answer(bg, p.who.User, o.ID, AnswerInput{Action: ActionInterested}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s ответил на чужое приглашение: %v", p.name, err)
		}
	}
	if w.status(o.ID) != StatusPending {
		t.Fatal("чужой ответ изменил приглашение")
	}
	if err := w.svc.Answer(bg, sci.User, uuid.New(), AnswerInput{Action: ActionInterested}); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующее приглашение: %v", err)
	}

	// Проверка ответа; ошибка ответ не тратит.
	for _, tc := range []struct {
		name  string
		in    AnswerInput
		field string
	}{
		{"нет действия", AnswerInput{}, "action"},
		{"неизвестное действие", AnswerInput{Action: "maybe"}, "action"},
		{"статус вместо действия", AnswerInput{Action: StatusPending}, "action"},
		{"длинная записка", AnswerInput{Action: ActionInterested, Note: strings.Repeat("я", 1001)}, "note"},
		{"управляющий знак в записке", AnswerInput{Action: ActionInterested, Note: "а\x07б"}, "note"},
	} {
		var verr *auth.ValidationError
		if err := w.svc.Answer(bg, sci.User, o.ID, tc.in); !errors.As(err, &verr) || verr.Fields[tc.field] == "" {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if w.status(o.ID) != StatusPending {
		t.Fatal("ошибка проверки изменила приглашение")
	}

	// Записка по границе проходит; второй ответ невозможен.
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested, Note: strings.Repeat("я", 1000)}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionInterested, ActionDeclined} {
		if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: action}); !errors.Is(err, ErrBadState) {
			t.Errorf("второй ответ %s: %v", action, err)
		}
	}
	if w.status(o.ID) != StatusInterested || notificationsOf(t, w.tm.HR, "offer_interested") != 1 || notificationsOf(t, w.tm.HR, "offer_declined") != 0 {
		t.Errorf("второй ответ оставил следы")
	}
}

func TestSimultaneousAnswersCountOnce(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	var wg sync.WaitGroup
	results := make([]error, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := ActionInterested
			if i%2 == 1 {
				action = ActionDeclined
			}
			results[i] = w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: action})
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrBadState):
			t.Errorf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("принятых ответов %d, ожидали один", ok)
	}
	n := notificationsOf(t, w.tm.HR, "offer_interested") + notificationsOf(t, w.tm.HR, "offer_declined")
	if n != 1 {
		t.Errorf("кадровик получил %d уведомлений", n)
	}
}

// ---- отзыв организацией ----

func TestWhoCanCancel(t *testing.T) {
	w := newWorld(t)
	seeker, _ := w.scientist("Соискатель", "public")
	for _, p := range append(people(w), struct {
		name string
		who  testkit.Person
	}{"приглашённый", seeker}) {
		t.Run(p.name, func(t *testing.T) {
			sci, profile := w.scientist("Учёный", "public")
			o := w.mustInvite(w.tm.Owner, w.vacancy.ID, profile, "")
			before := w.snap(w.vacancy.ID, sci, p.who)
			err := w.svc.Cancel(bg, p.who.User, o.ID)
			allowed := p.name == "владелец" || p.name == "кадровик" || p.name == "руководитель А"
			if allowed {
				if err != nil || w.status(o.ID) != StatusCancelled {
					t.Fatalf("отзыв: %v, статус %s", err, w.status(o.ID))
				}
				if notificationsOf(t, sci, "offer_cancelled") != 1 || mailCount(t, sci.Email) != 2 {
					t.Errorf("учёному не сообщили об отзыве")
				}
				return
			}
			if !errors.Is(err, ErrNotFound) || w.status(o.ID) != StatusPending {
				t.Fatalf("отказ: %v, статус %s", err, w.status(o.ID))
			}
			if after := w.snap(w.vacancy.ID, sci, p.who); after != before {
				t.Errorf("отказ оставил следы: %+v → %+v", before, after)
			}
		})
	}
	if err := w.svc.Cancel(bg, w.tm.Owner.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующее приглашение: %v", err)
	}
}

func TestCancelStates(t *testing.T) {
	w := newWorld(t)
	for _, tc := range []struct {
		name  string
		setup func(sci testkit.Person, o Offer)
	}{
		{"уже отозвано", func(_ testkit.Person, o Offer) {
			if err := w.svc.Cancel(bg, w.tm.HR.User, o.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"учёный ответил «Интересно»", func(sci testkit.Person, o Offer) {
			if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
				t.Fatal(err)
			}
		}},
		{"учёный ответил «Не сейчас»", func(sci testkit.Person, o Offer) {
			if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionDeclined}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sci, profile := w.scientist("Учёный", "public")
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
			tc.setup(sci, o)
			status := w.status(o.ID)
			notes := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, sci.ID)
			if err := w.svc.Cancel(bg, w.tm.HR.User, o.ID); !errors.Is(err, ErrBadState) {
				t.Fatalf("получили %v", err)
			}
			if w.status(o.ID) != status || testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1`, sci.ID) != notes {
				t.Errorf("отказ оставил следы")
			}
		})
	}
}

// Отозванное приглашение учёный уже не видит и не может на него ответить.
func TestCancelledOfferIsGoneForTheScientist(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if err := w.svc.Cancel(bg, w.tm.HR.User, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, sci.User, o.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v", err)
	}
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); !errors.Is(err, ErrBadState) {
		t.Errorf("Answer: %v", err)
	}
	if l, _ := w.svc.Mine(bg, sci.User, "", 0, 0); l.Total != 0 || len(l.Items) != 0 || l.Pending != 0 {
		t.Errorf("Mine: %+v", l)
	}
	// Организация отозванное видит на своей вкладке.
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Status: StatusCancelled}); l.Total != 1 || l.Items[0].CanCancel {
		t.Errorf("Sent: %+v", l)
	}
}

// ---- чтение ----

func TestGet(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	other, _ := w.scientist("Другой", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Привет")

	got, err := w.svc.Get(bg, sci.User, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != o.ID || got.Message != "Привет" || got.Status != StatusPending || !got.CanAnswer || got.CanCancel || got.Scientist != nil || got.ApplicationID != nil {
		t.Errorf("приглашение: %+v", got)
	}
	v := got.Vacancy
	if v.ID != w.vacancy.ID || v.Title != w.vacancy.Title || v.OrgSlug != w.tm.Slug || v.OrgName == "" || v.UnitName != w.tm.UnitA.Name || v.City != "Новосибирск" || v.Status != "published" || v.Deadline == nil || *v.Deadline != w.vacancy.Deadline {
		t.Errorf("вакансия: %+v", v)
	}
	// Чужие приглашения и несуществующие одинаково «нет такого».
	for _, who := range []testkit.Person{other, w.tm.HR, w.tm.Owner} {
		if _, err := w.svc.Get(bg, who.User, o.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("чужой открыл приглашение: %v", err)
		}
	}
	if _, err := w.svc.Get(bg, sci.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующее: %v", err)
	}
	// Когда учёный откликнулся, в приглашении видно его отклик.
	w.apply(sci, w.vacancy.ID, "sent")
	got, _ = w.svc.Get(bg, sci.User, o.ID)
	if got.ApplicationID == nil {
		t.Errorf("отклик не виден в приглашении")
	}
}

func TestMine(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	other, otherProfile := w.scientist("Другой", "public")
	var ids []uuid.UUID
	for range 4 {
		vac := w.tm.Published(&w.tm.UnitA.ID)
		ids = append(ids, w.mustInvite(w.tm.HR, vac.ID, profile, "").ID)
		w.clock.Advance(time.Minute)
	}
	w.mustInvite(w.tm.HR, w.vacancy.ID, otherProfile, "")
	if err := w.svc.Answer(bg, sci.User, ids[0], AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Answer(bg, sci.User, ids[1], AnswerInput{Action: ActionDeclined}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Cancel(bg, w.tm.HR.User, ids[3]); err != nil {
		t.Fatal(err)
	}

	l, err := w.svc.Mine(bg, sci.User, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 3 || l.Pending != 1 || l.Counts[StatusInterested] != 1 || l.Counts[StatusDeclined] != 1 || l.Counts[StatusPending] != 1 {
		t.Errorf("числа: %+v", l)
	}
	if got := []uuid.UUID{l.Items[0].ID, l.Items[1].ID, l.Items[2].ID}; !slices.Equal(got, []uuid.UUID{ids[2], ids[1], ids[0]}) {
		t.Errorf("порядок (новые сверху, без отозванных): %v", got)
	}
	if !l.Items[0].CanAnswer || l.Items[1].CanAnswer || l.Items[0].Scientist != nil {
		t.Errorf("признаки: %+v", l.Items)
	}

	// Отбор по состоянию и страницы.
	for st, want := range map[string]int{StatusPending: 1, StatusInterested: 1, StatusDeclined: 1} {
		if l, _ := w.svc.Mine(bg, sci.User, st, 0, 0); l.Total != want || len(l.Items) != want || (want == 1 && l.Items[0].Status != st) {
			t.Errorf("вкладка %s: %+v", st, l)
		}
	}
	if l, _ := w.svc.Mine(bg, sci.User, "", 2, 0); len(l.Items) != 2 || l.Total != 3 {
		t.Errorf("страница: %+v", l)
	}
	if l, _ := w.svc.Mine(bg, sci.User, "", 2, 2); len(l.Items) != 1 {
		t.Errorf("вторая страница: %+v", l)
	}
	if l, _ := w.svc.Mine(bg, sci.User, "", 1000, -5); len(l.Items) != 3 {
		t.Errorf("предел и смещение: %+v", l)
	}
	for _, bad := range []string{StatusCancelled, "done"} {
		var verr *auth.ValidationError
		if _, err := w.svc.Mine(bg, sci.User, bad, 0, 0); !errors.As(err, &verr) || verr.Fields["status"] == "" {
			t.Errorf("статус %q: %v", bad, err)
		}
	}
	// У другого человека свои приглашения; у человека без приглашений пустой список.
	if l, _ := w.svc.Mine(bg, other.User, "", 0, 0); l.Total != 1 || l.Pending != 1 {
		t.Errorf("чужие: %+v", l)
	}
	nobody := w.User("Никто")
	if l, _ := w.svc.Mine(bg, nobody.User, "", 0, 0); l.Total != 0 || l.Items == nil || l.Counts[StatusPending] != 0 {
		t.Errorf("пусто: %+v", l)
	}
	// Отклик на вакансию виден в списке.
	w.apply(sci, l0Vacancy(t, w, ids[2]), "sent")
	l, _ = w.svc.Mine(bg, sci.User, "", 0, 0)
	if l.Items[0].ApplicationID == nil {
		t.Errorf("отклик не виден в списке: %+v", l.Items[0])
	}
}

func l0Vacancy(t *testing.T, w *world, offer uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT vacancy_id FROM vacancy_offers WHERE id = $1`, offer).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Список организации по ролям: владелец и кадровик видят всё, руководитель — только свои подразделения.
func TestSentByRole(t *testing.T) {
	w := newWorld(t)
	vacB := w.tm.Published(&w.tm.UnitB.ID)
	vacOrg := w.tm.Published(nil)
	seeker, _ := w.scientist("Соискатель", "public")
	for i, vac := range []uuid.UUID{w.vacancy.ID, vacB.ID, vacOrg.ID} {
		_, profile := w.scientist("Учёный "+string(rune('А'+i)), "public")
		w.mustInvite(w.tm.Owner, vac, profile, "")
	}
	for _, tc := range []struct {
		name string
		who  testkit.Person
		want int
	}{{"владелец", w.tm.Owner, 3}, {"кадровик", w.tm.HR, 3}, {"руководитель А", w.tm.HeadA, 1}, {"руководитель Б", w.tm.HeadB, 1}, {"посторонний", w.tm.Out, 0}, {"учёный", seeker, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := w.svc.Sent(bg, tc.who.User, SentFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if l.Total != tc.want || len(l.Items) != tc.want || l.Counts[StatusPending] != tc.want {
				t.Errorf("видно %d (всего %d), ожидали %d", len(l.Items), l.Total, tc.want)
			}
			if l.Items == nil {
				t.Errorf("пустой список должен быть [], не null")
			}
		})
	}
}

func TestSentFilters(t *testing.T) {
	w := newWorld(t)
	vac2 := w.tm.Published(&w.tm.UnitA.ID)
	var ids []uuid.UUID
	var scis []testkit.Person
	for i := range 4 {
		sci, profile := w.scientist([]string{"Анна", "Борис", "Вера", "Глеб"}[i], "public")
		vac := w.vacancy.ID
		if i >= 2 {
			vac = vac2.ID
		}
		ids = append(ids, w.mustInvite(w.tm.HR, vac, profile, "").ID)
		scis = append(scis, sci)
		w.clock.Advance(time.Minute)
	}
	if err := w.svc.Answer(bg, scis[0].User, ids[0], AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Answer(bg, scis[1].User, ids[1], AnswerInput{Action: ActionDeclined}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Cancel(bg, w.tm.HR.User, ids[3]); err != nil {
		t.Fatal(err)
	}

	l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{})
	if l.Total != 4 || l.Counts[StatusPending] != 1 || l.Counts[StatusInterested] != 1 || l.Counts[StatusDeclined] != 1 || l.Counts[StatusCancelled] != 1 {
		t.Errorf("числа: %+v", l.Counts)
	}
	if names := []string{l.Items[0].Scientist.Name, l.Items[3].Scientist.Name}; names[0] != "Глеб" || names[1] != "Анна" {
		t.Errorf("порядок (новые сверху): %v", names)
	}
	for st, want := range map[string]int{StatusPending: 1, StatusInterested: 1, StatusDeclined: 1, StatusCancelled: 1} {
		if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Status: st}); l.Total != want || len(l.Items) != want {
			t.Errorf("вкладка %s: %d", st, len(l.Items))
		}
	}
	// По вакансии: числа считаются только по ней.
	l, _ = w.svc.Sent(bg, w.tm.HR.User, SentFilter{VacancyID: &vac2.ID})
	if l.Total != 2 || l.Counts[StatusPending] != 1 || l.Counts[StatusCancelled] != 1 || l.Counts[StatusInterested] != 0 {
		t.Errorf("по вакансии: %+v %+v", l.Total, l.Counts)
	}
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{VacancyID: &vac2.ID, Status: StatusCancelled}); l.Total != 1 || l.Items[0].Scientist.Name != "Глеб" {
		t.Errorf("вакансия и статус: %+v", l.Items)
	}
	// Чужая вакансия даёт пустой список, а не ошибку.
	nobody := uuid.New()
	if l, err := w.svc.Sent(bg, w.tm.HR.User, SentFilter{VacancyID: &nobody}); err != nil || l.Total != 0 {
		t.Errorf("неизвестная вакансия: %+v %v", l, err)
	}
	// Страницы.
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Limit: 3}); len(l.Items) != 3 || l.Total != 4 {
		t.Errorf("страница: %d", len(l.Items))
	}
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Limit: 3, Offset: 3}); len(l.Items) != 1 {
		t.Errorf("вторая страница: %d", len(l.Items))
	}
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Limit: 999, Offset: -1}); len(l.Items) != 4 {
		t.Errorf("предел и смещение: %d", len(l.Items))
	}
	var verr *auth.ValidationError
	if _, err := w.svc.Sent(bg, w.tm.HR.User, SentFilter{Status: "done"}); !errors.As(err, &verr) || verr.Fields["status"] == "" {
		t.Errorf("статус: %v", err)
	}
	// Признаки карточки.
	pend := w.svc.mustSent(t, w.tm.HR.User, StatusPending)
	if !pend.CanCancel || pend.CanAnswer || pend.Scientist == nil || pend.Scientist.ProfileID == uuid.Nil || pend.Vacancy.UnitName != w.tm.UnitA.Name {
		t.Errorf("карточка: %+v", pend)
	}
}

func (s *Service) mustSent(t *testing.T, user auth.User, status string) Offer {
	t.Helper()
	l, err := s.Sent(bg, user, SentFilter{Status: status})
	if err != nil || len(l.Items) == 0 {
		t.Fatalf("список %s: %v", status, err)
	}
	return l.Items[0]
}

// Учёный потом скрыл профиль: приглашение остаётся у обеих сторон.
func TestOfferSurvivesProfileHiding(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if _, err := w.Prof.SetPrivacy(bg, sci.User, "hidden", false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, sci.User, o.ID); err != nil {
		t.Errorf("учёный потерял приглашение: %v", err)
	}
	if l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{}); l.Total != 1 {
		t.Errorf("организация потеряла приглашение")
	}
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
		t.Errorf("ответ: %v", err)
	}
	// Новое приглашение скрытому профилю уже не отправить.
	vac := w.tm.Published(&w.tm.UnitA.ID)
	if _, err := w.invite(w.tm.HR, vac.ID, profile, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("приглашение скрытому: %v", err)
	}
}

// ---- «куда можно пригласить» ----

func TestTargets(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Учёный", "public")
	vacB := w.tm.Published(&w.tm.UnitB.ID)
	closed := w.tm.Published(&w.tm.UnitA.ID)
	w.tm.Status(closed.ID, vacancies.StatusClosed)
	if _, err := w.Vac.Create(bg, w.tm.Owner.User, w.tm.Slug, testkit.VacancyInput(&w.tm.UnitA.ID)); err != nil { // черновик
		t.Fatal(err)
	}
	ids := func(ts []Target) []uuid.UUID {
		out := make([]uuid.UUID, len(ts))
		for i, x := range ts {
			out[i] = x.ID
		}
		return out
	}
	has := func(ts []Target, id uuid.UUID) bool { return slices.Contains(ids(ts), id) }

	owner, err := w.svc.Targets(bg, w.tm.Owner.User, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !has(owner, w.vacancy.ID) || !has(owner, vacB.ID) || has(owner, closed.ID) {
		t.Errorf("владелец: открытые вакансии обеих лабораторий без закрытой: %v", ids(owner))
	}
	for _, x := range owner {
		if x.ID == w.vacancy.ID && (x.Title != w.vacancy.Title || x.OrgSlug != w.tm.Slug || x.UnitName != w.tm.UnitA.Name || x.Deadline == nil || x.Offered || x.Applied) {
			t.Errorf("описание: %+v", x)
		}
	}
	// Права: руководитель видит свои вакансии, посторонний с публичным профилем — пустой список.
	if got, _ := w.svc.Targets(bg, w.tm.HeadA.User, profile); !has(got, w.vacancy.ID) || has(got, vacB.ID) {
		t.Errorf("руководитель А: %v", ids(got))
	}
	if got, err := w.svc.Targets(bg, w.tm.Out.User, profile); err != nil || len(got) != 0 {
		t.Errorf("посторонний: %v %v", ids(got), err)
	}
	// Флаги «уже приглашён» и «уже откликнулся».
	w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	w.apply(sci, vacB.ID, "sent")
	got, _ := w.svc.Targets(bg, w.tm.HR.User, profile)
	for _, x := range got {
		switch x.ID {
		case w.vacancy.ID:
			if !x.Offered || x.Applied {
				t.Errorf("приглашён: %+v", x)
			}
		case vacB.ID:
			if x.Offered || !x.Applied {
				t.Errorf("откликнулся: %+v", x)
			}
		}
	}
	// Отозванное приглашение флагом «приглашён» уже не отмечено.
	l, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{})
	if err := w.svc.Cancel(bg, w.tm.HR.User, l.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, x := range func() []Target { g, _ := w.svc.Targets(bg, w.tm.HR.User, profile); return g }() {
		if x.ID == w.vacancy.ID && x.Offered {
			t.Errorf("отозванное приглашение всё ещё отмечено")
		}
	}
	// Срок подачи прошёл — вакансии в списке нет.
	deadline, _ := time.Parse("2006-01-02", w.vacancy.Deadline)
	w.clock.Set(deadline.Add(21 * time.Hour))
	if got, _ := w.svc.Targets(bg, w.tm.HR.User, profile); has(got, w.vacancy.ID) {
		t.Errorf("вакансия с прошедшим сроком в списке")
	}
}

func TestTargetsRespectPrivacy(t *testing.T) {
	w := newWorld(t)
	for _, tc := range []struct {
		visibility string
		viewer     string
		ok         bool
	}{
		{"hidden", "кадровик", false}, {"hidden", "посторонний", false},
		{"orgs", "кадровик", true}, {"orgs", "посторонний без организации", false},
		{"public", "кадровик", true}, {"public", "посторонний без организации", true},
	} {
		t.Run(tc.visibility+"/"+tc.viewer, func(t *testing.T) {
			_, profile := w.scientist("Учёный", tc.visibility)
			who := w.tm.HR
			if tc.viewer != "кадровик" {
				who = w.User("Без организации")
			}
			got, err := w.svc.Targets(bg, who.User, profile)
			if tc.ok && err != nil {
				t.Fatalf("%v", err)
			}
			if !tc.ok && (!errors.Is(err, ErrNotFound) || got != nil) {
				t.Fatalf("получили %v, %v", got, err)
			}
		})
	}
	if _, err := w.svc.Targets(bg, w.tm.HR.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий профиль: %v", err)
	}
	// Себя пригласить нельзя: список пуст.
	own, _ := w.Prof.Own(bg, w.tm.Owner.User)
	if _, err := w.Prof.SetPrivacy(bg, w.tm.Owner.User, "public", false); err != nil {
		t.Fatal(err)
	}
	if got, err := w.svc.Targets(bg, w.tm.Owner.User, own.Profile.ID); err != nil || len(got) != 0 {
		t.Errorf("свой профиль: %v %v", got, err)
	}
}

func TestDefaultConfigAndNewService(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Invite.Max != 20 || cfg.Invite.Window != 24*time.Hour {
		t.Errorf("DefaultConfig: %+v", cfg)
	}
	s := NewService(testkit.Pool, nil, cfg)
	if s == nil || s.now == nil || s.today().Location() != time.UTC {
		t.Errorf("NewService: %+v", s)
	}
}

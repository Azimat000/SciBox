package offers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

// Срез 15: приглашения анонимных профилей и профилей, скрытых от организации (D-199, D-200).

const anonName = "Анна Седова-Редкая"

// anonScientist — человек с анонимным профилем; имя редкое, чтобы искать его в текстах.
func (w *world) anonScientist() (testkit.Person, uuid.UUID) {
	w.T.Helper()
	p, id := w.scientist(anonName, "anon")
	// Степень и редкий h-index: по ним карточку находят в каталоге общей тестовой базы.
	core := profiles.CoreInput{
		Headline: "Научный сотрудник, лаборатория катализа", City: "Новосибирск", RegionCode: "54", Degree: profiles.DegreeCandidate,
		DegreeSpecialty: "1.4.4", Specialties: []string{"1.4.4"}, HRsci: new(296), ContactEmail: "anna.sedova@example.ru",
	}
	if _, err := w.Prof.SaveCore(bg, p.User, core); err != nil {
		w.T.Fatal(err)
	}
	return p, id
}

func (w *world) sent() []Offer {
	w.T.Helper()
	l, err := w.svc.Sent(bg, w.tm.Owner.User, SentFilter{})
	if err != nil {
		w.T.Fatal(err)
	}
	return l.Items
}

func (w *world) auditSubject(action string) *audit.Person {
	w.T.Helper()
	page, err := audit.List(bg, dbgen.New(testkit.Pool), w.tm.OrgID, 0, 50)
	if err != nil {
		w.T.Fatal(err)
	}
	for _, e := range page.Items {
		if string(e.Action) == action {
			return e.Subject
		}
	}
	w.T.Fatalf("в журнале нет записи %s", action)
	return nil
}

func hide(t *testing.T, w *world, p testkit.Person, org uuid.UUID) {
	t.Helper()
	if _, err := w.Prof.SetHiddenOrgs(bg, p.User, []uuid.UUID{org}); err != nil {
		t.Fatal(err)
	}
}

func TestAnonymousProfileCanBeInvitedWithoutRevealingTheName(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonScientist()

	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Приходите")
	if o.Scientist == nil || !o.Scientist.Anonymous || o.Scientist.Name != "" || o.Scientist.ProfileID != profile {
		t.Errorf("ответ приглашающему: %+v", o.Scientist)
	}
	if n := notificationsOf(t, sci, "offer_received"); n != 1 {
		t.Errorf("приглашённый получил %d уведомлений", n)
	}
	// Список организации, журнал организации: имени нет.
	if got := w.sent(); len(got) != 1 || got[0].Scientist == nil || !got[0].Scientist.Anonymous || got[0].Scientist.Name != "" {
		t.Errorf("список организации: %+v", got)
	}
	if subj := w.auditSubject("offer.sent"); subj == nil || !subj.Anonymous || subj.Name != "" || subj.ID != uuid.Nil {
		t.Errorf("журнал: %+v", subj)
	}
	// Самому приглашённому приглашение показано как раньше, имени организации достаточно.
	if _, err := w.svc.Get(bg, sci.User, o.ID); err != nil {
		t.Error(err)
	}
}

func TestDeclinedAnonymousOfferStaysAnonymous(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonScientist()
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionDeclined, Note: "Не сейчас"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA} {
		var body string
		if err := testkit.Pool.QueryRow(bg, `SELECT body FROM notifications WHERE user_id = $1 AND kind = 'offer_declined'`, p.ID).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "Седова") || strings.Contains(body, "Анна") || !strings.Contains(body, "анонимным профилем") {
			t.Errorf("текст уведомления: %q", body)
		}
		var mailBody string
		if err := testkit.Pool.QueryRow(bg, `SELECT COALESCE(body, '') FROM outbox WHERE to_email = $1 ORDER BY created_at DESC LIMIT 1`, p.Email).Scan(&mailBody); err == nil && strings.Contains(mailBody, "Седова") {
			t.Errorf("в письме организации имя: %q", mailBody)
		}
	}
	if got := w.sent(); len(got) != 1 || !got[0].Scientist.Anonymous || got[0].Status != StatusDeclined {
		t.Errorf("список после отказа: %+v", got[0])
	}
	if subj := w.auditSubject("offer.sent"); subj == nil || !subj.Anonymous {
		t.Errorf("журнал после отказа: %+v", subj)
	}
	// Страница профиля осталась безымянной.
	page, err := w.Prof.Get(bg, profile, &w.tm.Owner.User)
	if err != nil || !page.Profile.Anonymous || page.Profile.Name != "" {
		t.Errorf("страница: %+v, %v", page.Profile, err)
	}
}

func TestInterestedOpensTheNameToThatOrganizationOnly(t *testing.T) {
	w := newWorld(t)
	other := w.Team() // другая организация, к которой человек не обращался
	sci, profile := w.anonScientist()
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}

	var body string
	if err := testkit.Pool.QueryRow(bg, `SELECT body FROM notifications WHERE user_id = $1 AND kind = 'offer_interested'`, w.tm.Owner.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, anonName) {
		t.Errorf("после «Интересно» в уведомлении должно быть имя: %q", body)
	}
	if got := w.sent(); got[0].Scientist.Anonymous || got[0].Scientist.Name != anonName {
		t.Errorf("список организации: %+v", got[0].Scientist)
	}
	if subj := w.auditSubject("offer.sent"); subj == nil || subj.Anonymous || subj.Name != anonName {
		t.Errorf("журнал: %+v", subj)
	}
	// Страница и резюме для приглашавшей организации открываются целиком (и контакт), для другой остаются безымянными.
	page, err := w.Prof.Get(bg, profile, &w.tm.HeadB.User)
	if err != nil || page.Profile.Anonymous || page.Profile.Name != anonName || page.Profile.Headline == "" {
		t.Errorf("страница для приглашавшей организации: %+v, %v", page.Profile, err)
	}
	page, err = w.Prof.Get(bg, profile, &other.Owner.User)
	if err != nil || !page.Profile.Anonymous || page.Profile.Name != "" {
		t.Errorf("страница для другой организации: %+v, %v", page.Profile, err)
	}
	// Каталог: у приглашавшей организации имя, у другой карточка безымянная.
	cardOf := func(v testkit.Person) profiles.CatalogCard {
		res, err := w.Prof.Catalog(bg, profiles.CatalogParams{HMin: 296, Limit: 50}, &v.User)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Items {
			if c.ID == profile {
				return c
			}
		}
		t.Fatalf("карточки нет в каталоге у %s", v.Email)
		return profiles.CatalogCard{}
	}
	if c := cardOf(w.tm.Owner); c.Anonymous || c.Name != anonName || c.Headline == "" {
		t.Errorf("карточка у приглашавшей организации: %+v", c)
	}
	if c := cardOf(other.Owner); !c.Anonymous || c.Name != "" || c.Headline != "" || c.City != "" {
		t.Errorf("карточка у другой организации: %+v", c)
	}
}

func TestApplicationOpensTheNameToThatOrganization(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonScientist()
	w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	// Человек откликнулся на вакансию этой организации (отклик создаёт пакет applications; здесь строка напрямую).
	w.apply(sci, w.vacancy.ID, "sent")
	if got := w.sent(); got[0].Scientist.Anonymous || got[0].Scientist.Name != anonName {
		t.Errorf("после отклика имя должно открыться: %+v", got[0].Scientist)
	}
	// Отозванный отклик открытого не закрывает задним числом? — закрывает: имя снова скрыто на страницах и в списках.
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET status = 'withdrawn' WHERE user_id = $1`, sci.ID); err != nil {
		t.Fatal(err)
	}
	if got := w.sent(); !got[0].Scientist.Anonymous {
		t.Errorf("после отзыва отклика имя снова должно быть скрыто: %+v", got[0].Scientist)
	}
}

func TestHiddenFromOrganizationCannotBeInvited(t *testing.T) {
	w := newWorld(t)
	other := w.Team()
	sci, profile := w.scientist("Борис Скрытов", "public")
	hide(t, w, sci, w.tm.OrgID)

	if _, err := w.invite(w.tm.Owner, w.vacancy.ID, profile, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("приглашение скрывшемуся от организации: %v", err)
	}
	if _, err := w.svc.Targets(bg, w.tm.HR.User, profile); !errors.Is(err, ErrNotFound) {
		t.Errorf("цели приглашения: %v", err)
	}
	// Организации, от которой человек не скрывался, приглашать можно.
	v := other.Published(nil)
	if _, err := w.svc.Invite(bg, other.Owner.User, InviteInput{VacancyID: v.ID, ProfileID: profile}); err != nil {
		t.Errorf("другая организация: %v", err)
	}
	// А сотрудник скрытой организации, который состоит ещё и в другой, всё равно не может: профиль для него закрыт.
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'hr', now())`, other.OrgID, w.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Invite(bg, w.tm.HR.User, InviteInput{VacancyID: v.ID, ProfileID: profile}); !errors.Is(err, ErrNotFound) {
		t.Errorf("сотрудник двух организаций: %v", err)
	}
}

// Человек скрылся от организации уже после приглашения: приглашение живёт (D-100), но имя в её списках скрыто, пока он
// не ответил «Интересно».
func TestHidingAfterAnOfferConcealsTheNameInOrganizationLists(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Вера Прячущаяся", "public")
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if got := w.sent(); got[0].Scientist.Name != "Вера Прячущаяся" {
		t.Fatalf("до скрытия имя должно быть: %+v", got[0].Scientist)
	}
	hide(t, w, sci, w.tm.OrgID)
	if got := w.sent(); !got[0].Scientist.Anonymous || got[0].Scientist.Name != "" {
		t.Errorf("после скрытия: %+v", got[0].Scientist)
	}
	if subj := w.auditSubject("offer.sent"); subj == nil || !subj.Anonymous {
		t.Errorf("журнал после скрытия: %+v", subj)
	}
	// Ответ «Интересно» — сознательный шаг: имя открывается и скрытой организации.
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}
	if got := w.sent(); got[0].Scientist.Anonymous || got[0].Scientist.Name != "Вера Прячущаяся" {
		t.Errorf("после «Интересно»: %+v", got[0].Scientist)
	}
	// А страница профиля скрытой организации остаётся закрытой: имя открыто в приглашении, а не в каталоге.
	if _, err := w.Prof.Get(bg, profile, &w.tm.Owner.User); err == nil {
		t.Error("страница профиля открылась организации, от которой человек скрылся")
	}
}

// В ответе организации нет сведений, по которым анонимный профиль узнают: проверка по всему JSON.
func TestAnonymousOfferJSONHasNoName(t *testing.T) {
	w := newWorld(t)
	_, profile := w.anonScientist()
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	body, err := json.Marshal(struct {
		Sent []Offer
		One  Offer
	}{w.sent(), o})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Седова") || strings.Contains(string(body), "Анна") {
		t.Errorf("в JSON есть имя: %s", body)
	}
}

package offers

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

const anonName = "Секретов Анонимус"

// anonymousScientist — учёный с анонимным профилем: кандидат наук, специальность 1.4.4, h-index 11.
func (w *world) anonymousScientist() (testkit.Person, uuid.UUID) {
	w.T.Helper()
	p := w.User(anonName)
	h := 11
	if _, err := w.Prof.SaveCore(bg, p.User, profiles.CoreInput{
		Headline: "Заведующий секретной лабораторией", City: "Секретоград", RegionCode: "54", Degree: profiles.DegreeCandidate,
		DegreeSpecialty: "1.4.4", Specialties: []string{"1.4.4"}, HScopus: &h,
	}); err != nil {
		w.T.Fatal(err)
	}
	if _, err := w.Prof.SetPrivacy(bg, p.User, "anonymous", false); err != nil {
		w.T.Fatal(err)
	}
	own, err := w.Prof.Own(bg, p.User)
	if err != nil {
		w.T.Fatal(err)
	}
	return p, own.Profile.ID
}

const anonLabel = "Кандидат наук, 1.4.4, R2, h-index 11"

func notificationText(t *testing.T, to testkit.Person, kind string) string {
	t.Helper()
	var title, body string
	if err := testkit.Pool.QueryRow(bg, `SELECT title, body FROM notifications WHERE user_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1`, to.ID, kind).Scan(&title, &body); err != nil {
		t.Fatalf("notification %s: %v", kind, err)
	}
	return title + "\n" + body
}

// Анонима приглашать можно, но организация не узнаёт имя: ни в приглашении, ни в списке, ни в уведомлении, ни в журнале.
func TestAnonymousScientistCanBeInvitedWithoutLearningTheName(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonymousScientist()

	targets, err := w.svc.Targets(bg, w.tm.HR.User, profile)
	if err != nil || len(targets) != 1 {
		t.Fatalf("вакансии для приглашения анонима: %v %v", targets, err)
	}
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Приходите")
	if o.Scientist == nil || !o.Scientist.Masked || o.Scientist.Name != "" || o.Scientist.Label != anonLabel || o.Scientist.ProfileID != profile {
		t.Fatalf("ответ на приглашение раскрыл лишнее: %+v", o.Scientist)
	}
	sent, err := w.svc.Sent(bg, w.tm.Owner.User, SentFilter{})
	if err != nil || len(sent.Items) != 1 {
		t.Fatalf("список организации: %v %v", sent, err)
	}
	if s := sent.Items[0].Scientist; !s.Masked || s.Name != "" || s.Label != anonLabel {
		t.Errorf("в списке: %+v", s)
	}
	// «Не сейчас» имя не открывает; уведомление называет человека научным обликом.
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionDeclined, Note: "Не сейчас"}); err != nil {
		t.Fatal(err)
	}
	text := notificationText(t, w.tm.HR, "offer_declined")
	if strings.Contains(text, anonName) || !strings.Contains(text, anonLabel) {
		t.Errorf("уведомление об отказе: %q", text)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM profile_reveals`); n != 0 {
		t.Errorf("отказ раскрыл имя: %d строк", n)
	}
	if sent, _ = w.svc.Sent(bg, w.tm.HR.User, SentFilter{}); sent.Items[0].Scientist.Name != "" || !sent.Items[0].Scientist.Masked {
		t.Errorf("после отказа в списке: %+v", sent.Items[0].Scientist)
	}
	// Письмо сотруднику тоже без имени.
	var mails string
	rows, err := testkit.Pool.Query(bg, `SELECT subject || E'\n' || body FROM outbox WHERE to_email = $1`, w.tm.HR.Email)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m string
		_ = rows.Scan(&m)
		mails += m
	}
	rows.Close()
	if strings.Contains(mails, anonName) || strings.Contains(mails, "Секретоград") {
		t.Errorf("в письме организации есть имя: %q", mails)
	}
}

// «Интересно» открывает имя той организации, которая пригласила, и только ей.
func TestInterestedOpensTheNameToTheInvitingOrganizationOnly(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonymousScientist()
	other := w.Team()
	otherVacancy := other.Published(&other.UnitA.ID)

	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	otherOffer := w.mustInvite(other.HR, otherVacancy.ID, profile, "")
	if err := w.svc.Answer(bg, sci.User, o.ID, AnswerInput{Action: ActionInterested}); err != nil {
		t.Fatal(err)
	}

	sent, _ := w.svc.Sent(bg, w.tm.HR.User, SentFilter{})
	if s := sent.Items[0].Scientist; s.Masked || s.Name != anonName || s.Label != "" {
		t.Errorf("пригласившая организация: %+v", s)
	}
	if text := notificationText(t, w.tm.HR, "offer_interested"); !strings.Contains(text, anonName) {
		t.Errorf("в уведомлении об «Интересно» должно быть имя: %q", text)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM profile_reveals WHERE org_id = $1 AND reason = 'offer_interested'`, w.tm.OrgID); n != 1 {
		t.Errorf("строк раскрытия: %d", n)
	}
	// Другая организация по-прежнему видит аноним, в том числе в приглашении, которое ещё ждёт ответа.
	otherSent, _ := w.svc.Sent(bg, other.HR.User, SentFilter{})
	if len(otherSent.Items) != 1 || otherSent.Items[0].ID != otherOffer.ID || !otherSent.Items[0].Scientist.Masked || otherSent.Items[0].Scientist.Name != "" {
		t.Errorf("чужая организация: %+v", otherSent.Items)
	}
	// Страница профиля: пригласившей организации открыта целиком, чужой анонимно.
	if page, err := w.Prof.Get(bg, profile, &w.tm.HR.User); err != nil || page.Viewer.Masked || page.Profile.Name != anonName {
		t.Errorf("страница для пригласившей организации: %+v %v", page.Profile, err)
	}
	if page, err := w.Prof.Get(bg, profile, &other.HR.User); err != nil || !page.Viewer.Masked || page.Profile.Name != "" {
		t.Errorf("страница для чужой организации: %+v %v", page.Profile, err)
	}
}

// Профиль, скрытый от организации, ей приглашать нельзя в любом режиме; для других организаций ничего не меняется.
func TestProfileHiddenFromTheOrganizationCannotBeInvited(t *testing.T) {
	for _, mode := range []string{"anonymous", "orgs", "public"} {
		t.Run(mode, func(t *testing.T) {
			w := newWorld(t)
			sci, profile := w.scientist("Учёный", mode)
			other := w.Team()
			otherVacancy := other.Published(&other.UnitA.ID)
			if _, err := w.Prof.HideFrom(bg, sci.User, w.tm.Slug); err != nil {
				t.Fatal(err)
			}
			before := w.snap(w.vacancy.ID, sci, w.tm.HR)
			if _, err := w.invite(w.tm.HR, w.vacancy.ID, profile, ""); !errors.Is(err, ErrNotFound) {
				t.Fatalf("приглашение: %v", err)
			}
			if _, err := w.svc.Targets(bg, w.tm.Owner.User, profile); !errors.Is(err, ErrNotFound) {
				t.Errorf("вакансии для приглашения: %v", err)
			}
			if after := w.snap(w.vacancy.ID, sci, w.tm.HR); after != before {
				t.Errorf("отказ оставил следы: %+v → %+v", before, after)
			}
			// Другая организация приглашает как обычно.
			if _, err := w.invite(other.HR, otherVacancy.ID, profile, ""); err != nil {
				t.Errorf("другая организация: %v", err)
			}
			// Сняли скрытие: можно.
			if _, err := w.Prof.UnhideFrom(bg, sci.User, w.tm.Slug); err != nil {
				t.Fatal(err)
			}
			if _, err := w.invite(w.tm.HR, w.vacancy.ID, profile, ""); err != nil {
				t.Errorf("после снятия скрытия: %v", err)
			}
		})
	}
}

// Журнал действий организации не называет анонимного приглашённого; после раскрытия имени называет.
func TestAuditDoesNotNameAnAnonymousInvitee(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.anonymousScientist()
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	if err := w.svc.Cancel(bg, w.tm.HR.User, o.ID); err != nil {
		t.Fatal(err)
	}
	read := func() []audit.Entry {
		page, err := audit.List(bg, dbgen.New(testkit.Pool), w.tm.OrgID, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []audit.Entry
		for _, e := range page.Items {
			if strings.HasPrefix(string(e.Action), "offer.") {
				out = append(out, e)
			}
		}
		return out
	}
	entries := read()
	if len(entries) != 2 {
		t.Fatalf("записей об приглашениях: %d", len(entries))
	}
	for _, e := range entries {
		if e.Subject == nil || e.Subject.Name != audit.MaskedSubject || e.Subject.ID != uuid.Nil {
			t.Errorf("в записи %s: %+v", e.Action, e.Subject)
		}
	}
	// Открытое имя называется.
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO profile_reveals (profile_id, org_id, reason, created_at) VALUES ($1, $2, 'application', now())`, profile, w.tm.OrgID); err != nil {
		t.Fatal(err)
	}
	for _, e := range read() {
		if e.Subject == nil || e.Subject.Name != anonName || e.Subject.ID != sci.ID {
			t.Errorf("после раскрытия в записи %s: %+v", e.Action, e.Subject)
		}
	}
}

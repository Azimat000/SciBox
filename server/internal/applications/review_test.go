package applications

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// ---- помощники ----

func (w *world) when(d time.Duration) string {
	return w.clock.Now().Add(d).In(moscow).Format(time.RFC3339)
}

func (w *world) interview() InvitationInput {
	return InvitationInput{Kind: InvInterview, StartsAt: w.when(48 * time.Hour), PlaceKind: PlaceOnline, Place: "https://meet.example.org/room-1", Message: "Расскажем о проекте"}
}

func contactsIn() InvitationInput {
	return InvitationInput{Kind: InvContacts, ContactName: "Ольга Кузнецова", ContactEmail: "hr@example.ru", ContactPhone: "+7 913 000-00-00", Message: "Пишите в любое время"}
}

func requestIn() InvitationInput {
	return InvitationInput{Kind: InvRequest, Message: "Оставьте телефон и удобное время для звонка"}
}

func (w *world) mustInvite(by testkit.Person, id uuid.UUID, in InvitationInput) Invitation {
	w.T.Helper()
	inv, err := w.svc.Invite(bg, by.User, id, in)
	if err != nil {
		w.T.Fatalf("invite: %v", err)
	}
	return inv
}

func (w *world) statusOf(id uuid.UUID) string {
	w.T.Helper()
	var s string
	if err := testkit.Pool.QueryRow(bg, `SELECT status FROM applications WHERE id = $1`, id).Scan(&s); err != nil {
		w.T.Fatal(err)
	}
	return s
}

func (w *world) invStatus(id uuid.UUID) string {
	w.T.Helper()
	var s string
	if err := testkit.Pool.QueryRow(bg, `SELECT status FROM application_invitations WHERE id = $1`, id).Scan(&s); err != nil {
		w.T.Fatal(err)
	}
	return s
}

// invitationsOf — приглашения отклика глазами организации.
func (w *world) invitationsOf(id uuid.UUID) []Invitation {
	w.T.Helper()
	d, err := w.svc.Get(bg, w.tm.Owner.User, id)
	if err != nil {
		w.T.Fatal(err)
	}
	return d.Invitations
}

// invitation — приглашение отклика по номеру, глазами организации.
func (w *world) invitation(appID, id uuid.UUID) Invitation {
	w.T.Helper()
	for _, inv := range w.invitationsOf(appID) {
		if inv.ID == id {
			return inv
		}
	}
	w.T.Fatalf("invitation %s not found", id)
	return Invitation{}
}

func (w *world) noticeBody(who testkit.Person, kind string) string {
	w.T.Helper()
	var body string
	if err := testkit.Pool.QueryRow(bg, `SELECT body FROM notifications WHERE user_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1`, who.ID, kind).Scan(&body); err != nil {
		w.T.Fatalf("no %q notification for %s: %v", kind, who.Email, err)
	}
	return body
}

func (w *world) mailTo(who testkit.Person, subject string) int {
	w.T.Helper()
	return testkit.Count(w.T, `SELECT count(*) FROM outbox WHERE to_email = $1 AND subject = $2`, who.Email, subject)
}

// staffOfUnitA — те, кто вправе разбирать отклики на вакансию подразделения A.
func (w *world) staffOfUnitA() []testkit.Person {
	return []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA}
}

// viewedApp — отклик, который организация уже открывала.
func (w *world) viewedApp(me testkit.Person) Detail {
	w.T.Helper()
	d := w.mustApply(me, nil)
	if _, err := w.svc.Get(bg, w.tm.Owner.User, d.ID); err != nil {
		w.T.Fatal(err)
	}
	return d
}

func (w *world) makeStaff(p testkit.Person, role string) {
	w.T.Helper()
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: w.tm.OrgID, UserID: p.ID, Role: role, JoinedAt: time.Now().UTC()}); err != nil {
		w.T.Fatal(err)
	}
}

// ---- просмотр ----

func TestOpeningTheCardMarksTheApplicationViewedOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)

	// Сам соискатель, руководитель чужого подразделения и посторонний статус не двигают.
	if got, err := w.svc.Get(bg, me.User, d.ID); err != nil || got.Status != StatusSent {
		t.Fatalf("applicant opens: %v %v", err, got.Status)
	}
	for _, p := range []testkit.Person{w.tm.HeadB, w.tm.Out} {
		if _, err := w.svc.Get(bg, p.User, d.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s opens: %v", p.Name, err)
		}
	}
	if w.statusOf(d.ID) != StatusSent || notificationsOf(t, me, "application_viewed") != 0 {
		t.Fatal("the status moved without an entitled reader")
	}

	// Руководитель своего подразделения: статус «просмотрен» и уже в ответе.
	got, err := w.svc.Get(bg, w.tm.HeadA.User, d.ID)
	if err != nil || got.Status != StatusViewed || got.Viewer.Role != RoleStaff {
		t.Fatalf("staff opens: %v %v %v", err, got.Status, got.Viewer)
	}
	if w.statusOf(d.ID) != StatusViewed || !got.StatusChangedAt.Equal(w.clock.Now()) {
		t.Errorf("status in the database: %s, changed at %v", w.statusOf(d.ID), got.StatusChangedAt)
	}
	// Остальные открывают сколько угодно: соискатель получает одно уведомление и одно письмо.
	for _, p := range []testkit.Person{w.tm.HeadA, w.tm.Owner, w.tm.HR} {
		if _, err := w.svc.Get(bg, p.User, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n := notificationsOf(t, me, "application_viewed"); n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
	if n := w.mailTo(me, "Ваш отклик просмотрен"); n != 1 {
		t.Errorf("mails = %d, want 1", n)
	}
	if body := w.noticeBody(me, "application_viewed"); !strings.Contains(body, "«"+w.vacancy.Title+"»") {
		t.Errorf("body = %q", body)
	}
}

func TestOpeningOwnApplicationAsStaffDoesNotMarkItViewed(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	w.makeStaff(me, "hr")
	if _, err := w.svc.Get(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if w.statusOf(d.ID) != StatusSent {
		t.Error("the applicant, who is now staff, marked their own application as viewed")
	}
}

func TestSimultaneousOpeningsNotifyOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	var wg sync.WaitGroup
	for _, p := range w.staffOfUnitA() {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := w.svc.Get(bg, p.User, d.ID); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	if n := notificationsOf(t, me, "application_viewed"); n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
}

// ---- кто что может ----

// Операции организации: владелец, кадровик и руководитель своего подразделения могут; руководитель чужого подразделения,
// посторонний и сам автор отклика (в том числе ставший сотрудником) получают «нет такого», и ничего не меняется.
func TestWhoCanDoWhat(t *testing.T) {
	type op struct {
		name string
		// prep готовит отклик и возвращает действие.
		prep func(w *world, me testkit.Person) (d Detail, inv Invitation, do func(who testkit.Person) error)
	}
	ops := []op{
		{"reject", func(w *world, me testkit.Person) (Detail, Invitation, func(testkit.Person) error) {
			d := w.viewedApp(me)
			return d, Invitation{}, func(who testkit.Person) error { return w.svc.Decide(bg, who.User, d.ID, StatusRejected, "") }
		}},
		{"accept", func(w *world, me testkit.Person) (Detail, Invitation, func(testkit.Person) error) {
			d := w.viewedApp(me)
			return d, Invitation{}, func(who testkit.Person) error { return w.svc.Decide(bg, who.User, d.ID, StatusAccepted, "") }
		}},
		{"invite", func(w *world, me testkit.Person) (Detail, Invitation, func(testkit.Person) error) {
			d := w.viewedApp(me)
			return d, Invitation{}, func(who testkit.Person) error { _, err := w.svc.Invite(bg, who.User, d.ID, w.interview()); return err }
		}},
		{"cancel invitation", func(w *world, me testkit.Person) (Detail, Invitation, func(testkit.Person) error) {
			d := w.viewedApp(me)
			inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
			return d, inv, func(who testkit.Person) error { return w.svc.CancelInvitation(bg, who.User, d.ID, inv.ID) }
		}},
		{"accept proposal", func(w *world, me testkit.Person) (Detail, Invitation, func(testkit.Person) error) {
			d := w.viewedApp(me)
			inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
			if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerPropose, ProposedAt: w.when(96 * time.Hour)}); err != nil {
				t.Fatal(err)
			}
			return d, inv, func(who testkit.Person) error { return w.svc.AcceptProposal(bg, who.User, d.ID, inv.ID) }
		}},
	}
	for _, o := range ops {
		for _, who := range []string{"owner", "hr", "head of unit A", "head of unit B", "outsider", "the applicant", "the applicant turned hr"} {
			t.Run(o.name+"/"+who, func(t *testing.T) {
				w := newWorld(t)
				me := w.Applicant("Мария")
				d, inv, do := o.prep(w, me)
				person := map[string]testkit.Person{
					"owner": w.tm.Owner, "hr": w.tm.HR, "head of unit A": w.tm.HeadA, "head of unit B": w.tm.HeadB,
					"outsider": w.tm.Out, "the applicant": me, "the applicant turned hr": me,
				}[who]
				if who == "the applicant turned hr" {
					w.makeStaff(me, "hr")
				}
				allowed := who == "owner" || who == "hr" || who == "head of unit A"

				before := w.snapshot(d.ID, inv.ID)
				err := do(person)
				if allowed {
					if err != nil {
						t.Fatalf("must be allowed: %v", err)
					}
					if w.snapshot(d.ID, inv.ID) == before {
						t.Error("an allowed action changed nothing")
					}
					return
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				if w.snapshot(d.ID, inv.ID) != before {
					t.Error("a refused action left a trace")
				}
			})
		}
	}
}

// snapshot — всё, что может измениться от действий организации: статус, записка, приглашения, число уведомлений и писем.
func (w *world) snapshot(appID, invID uuid.UUID) string {
	w.T.Helper()
	var s string
	err := testkit.Pool.QueryRow(bg, `
SELECT a.status || '|' || a.decision_note || '|' ||
  COALESCE((SELECT string_agg(i.status || ':' || COALESCE(i.starts_at::text, ''), ',' ORDER BY i.id) FROM application_invitations i WHERE i.application_id = a.id), '') || '|' ||
  (SELECT count(*) FROM notifications) || '|' || (SELECT count(*) FROM outbox)
FROM applications a WHERE a.id = $1`, appID).Scan(&s)
	if err != nil {
		w.T.Fatal(err)
	}
	return s
}

// Отвечать на приглашение может только автор отклика.
func TestOnlyTheApplicantAnswers(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	for _, p := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA, w.tm.HeadB, w.tm.Out} {
		before := w.snapshot(d.ID, inv.ID)
		if err := w.svc.Answer(bg, p.User, d.ID, inv.ID, AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s answers: %v", p.Name, err)
		}
		if w.snapshot(d.ID, inv.ID) != before {
			t.Errorf("%s left a trace", p.Name)
		}
	}
	// Приглашение из другого отклика не находится.
	other := w.mustApply(w.Applicant("Пётр"), nil)
	if err := w.svc.Answer(bg, me.User, other.ID, inv.ID, AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign application: %v", err)
	}
	if err := w.svc.Answer(bg, me.User, d.ID, uuid.New(), AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown invitation: %v", err)
	}
	if err := w.svc.Answer(bg, me.User, uuid.New(), inv.ID, AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerConfirm}); err != nil {
		t.Errorf("applicant: %v", err)
	}
}

// Приглашение из чужого отклика организация тоже не отменит и не примет.
func TestInvitationsStayInsideTheirApplication(t *testing.T) {
	w := newWorld(t)
	a := w.viewedApp(w.Applicant("Мария"))
	b := w.viewedApp(w.Applicant("Пётр"))
	inv := w.mustInvite(w.tm.Owner, a.ID, w.interview())
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, b.ID, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cancel through another application: %v", err)
	}
	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, b.ID, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("accept through another application: %v", err)
	}
	if w.invStatus(inv.ID) != InvPending {
		t.Error("the invitation changed")
	}
}

// ---- решение ----

func TestDecide(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusSent, StatusRejected, true}, {StatusSent, StatusAccepted, false},
		{StatusViewed, StatusRejected, true}, {StatusViewed, StatusAccepted, true},
		{StatusInvited, StatusRejected, true}, {StatusInvited, StatusAccepted, true},
		{StatusRejected, StatusRejected, false}, {StatusRejected, StatusAccepted, false},
		{StatusAccepted, StatusRejected, false}, {StatusAccepted, StatusAccepted, false},
		{StatusWithdrawn, StatusRejected, false}, {StatusWithdrawn, StatusAccepted, false},
	}
	for _, c := range cases {
		t.Run(c.from+"→"+c.to, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			w.setStatus(d.ID, c.from)
			err := w.svc.Decide(bg, w.tm.HR.User, d.ID, c.to, "  Спасибо за отклик  ")
			if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrBadStatus)) {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
			if !c.ok {
				if w.statusOf(d.ID) != c.from || notificationsOf(t, me, "application_"+c.to) != 0 {
					t.Error("a refused decision left a trace")
				}
				return
			}
			got, _ := w.svc.Get(bg, me.User, d.ID)
			if got.Status != c.to || got.DecisionNote != "Спасибо за отклик" || got.Viewer.CanWithdraw || len(got.Viewer.Decisions) != 0 {
				t.Errorf("applicant sees %+v", got)
			}
			staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID)
			if len(staff.Viewer.Decisions) != 0 || staff.Viewer.CanInvite {
				t.Errorf("staff can still act: %+v", staff.Viewer)
			}
			var by uuid.UUID
			if err := testkit.Pool.QueryRow(bg, `SELECT decided_by FROM applications WHERE id = $1`, d.ID).Scan(&by); err != nil || by != w.tm.HR.ID {
				t.Errorf("decided_by = %v, %v", by, err)
			}
			if n := notificationsOf(t, me, "application_"+c.to); n != 1 {
				t.Errorf("notifications = %d", n)
			}
			if body := w.noticeBody(me, "application_"+c.to); !strings.Contains(body, "Сообщение организации: Спасибо за отклик") {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestDecideValidation(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	for _, to := range []string{StatusInvited, StatusViewed, StatusSent, StatusWithdrawn, "", "unknown"} {
		var verr *auth.ValidationError
		if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, to, ""); !errors.As(err, &verr) || verr.Fields["status"] == "" {
			t.Errorf("decide %q: %v", to, err)
		}
	}
	var verr *auth.ValidationError
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, strings.Repeat("я", 1001)); !errors.As(err, &verr) || verr.Fields["note"] == "" {
		t.Errorf("long note: %v", err)
	}
	if err := w.svc.Decide(bg, w.tm.Owner.User, uuid.New(), StatusRejected, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
	if w.statusOf(d.ID) != StatusViewed {
		t.Error("invalid decisions changed the status")
	}
	// Записка ровно в предел.
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, strings.Repeat("я", 1000)); err != nil {
		t.Errorf("note at the limit: %v", err)
	}
}

func TestRejectionClosesOpenInvitationsAcceptanceDoesNot(t *testing.T) {
	for _, to := range []string{StatusRejected, StatusAccepted} {
		t.Run(to, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария")
			d := w.viewedApp(me)
			pending := w.mustInvite(w.tm.Owner, d.ID, w.interview())
			confirmed := w.mustInvite(w.tm.Owner, d.ID, w.interview()) // заменяет первое
			if w.invStatus(pending.ID) != InvCancelled {
				t.Fatal("the new interview did not replace the old one")
			}
			if err := w.svc.Answer(bg, me.User, d.ID, confirmed.ID, AnswerInput{Action: AnswerConfirm}); err != nil {
				t.Fatal(err)
			}
			shared := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
			ask := w.mustInvite(w.tm.Owner, d.ID, requestIn())
			if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, to, ""); err != nil {
				t.Fatal(err)
			}
			want := map[uuid.UUID]string{confirmed.ID: InvConfirmed, shared.ID: InvShared, ask.ID: InvPending}
			if to == StatusRejected {
				want = map[uuid.UUID]string{confirmed.ID: InvCancelled, shared.ID: InvShared, ask.ID: InvCancelled}
			}
			for id, st := range want {
				if got := w.invStatus(id); got != st {
					t.Errorf("invitation status = %s, want %s", got, st)
				}
			}
		})
	}
}

func TestSimultaneousDecisionsPassOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	var ok, bad atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			to := []string{StatusRejected, StatusAccepted}[i%2]
			switch err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, to, ""); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrBadStatus):
				bad.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || bad.Load() != 5 {
		t.Errorf("passed %d, refused %d", ok.Load(), bad.Load())
	}
	if n := notificationsOf(t, me, "application_rejected") + notificationsOf(t, me, "application_accepted"); n != 1 {
		t.Errorf("notifications = %d", n)
	}
}

// ---- приглашения ----

func TestInviteInterview(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	in := w.interview()
	in.StartsAt = "2026-12-24T14:30:00+03:00"
	w.clock.Set(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC))
	inv := w.mustInvite(w.tm.HeadA, d.ID, in)
	if inv.Status != InvPending || inv.Kind != InvInterview || inv.PlaceKind != PlaceOnline || inv.Place != in.Place || !inv.CanCancel || inv.CanAnswer {
		t.Errorf("invitation = %+v", inv)
	}
	if inv.StartsAt == nil || !inv.StartsAt.Equal(time.Date(2026, 12, 24, 11, 30, 0, 0, time.UTC)) {
		t.Errorf("starts at %v", inv.StartsAt)
	}
	if w.statusOf(d.ID) != StatusInvited {
		t.Errorf("status = %s", w.statusOf(d.ID))
	}
	body := w.noticeBody(me, "invitation_interview")
	for _, want := range []string{"24 декабря 2026, 14:30 (МСК)", "Онлайн: " + in.Place, "Сообщение организации: Расскажем о проекте", w.vacancy.Title} {
		if !strings.Contains(body, want) {
			t.Errorf("notice %q lacks %q", body, want)
		}
	}
	if w.mailTo(me, "Приглашение на собеседование") != 1 {
		t.Error("no email to the applicant")
	}
	// Соискатель видит то же приглашение и может ответить.
	got, _ := w.svc.Get(bg, me.User, d.ID)
	if len(got.Invitations) != 1 || !got.Invitations[0].CanAnswer || got.Invitations[0].CanCancel || got.Status != StatusInvited {
		t.Errorf("applicant view: %+v", got.Invitations)
	}
	// И в «Моих откликах» видно, что приглашение ждёт ответа.
	list, _ := w.svc.Mine(bg, me.User, 20, 0)
	if list.Items[0].PendingInvitations != 1 {
		t.Errorf("pending = %d", list.Items[0].PendingInvitations)
	}
	// Сотрудникам решение «пригласить» не уведомление: им ничего не приходит.
	if notificationsOf(t, w.tm.Owner, "invitation_interview") != 0 {
		t.Error("staff got the applicant's notification")
	}
}

func TestInviteOtherKinds(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	c := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
	if c.Status != InvShared || c.ContactEmail != "hr@example.ru" || c.ContactPhone != "+7 913 000-00-00" || c.ContactName != "Ольга Кузнецова" || c.StartsAt != nil || c.PlaceKind != "" || c.CanCancel {
		t.Errorf("contacts = %+v", c)
	}
	body := w.noticeBody(me, "invitation_contacts")
	for _, want := range []string{"Контактное лицо: Ольга Кузнецова", "Почта: hr@example.ru", "Телефон: +7 913 000-00-00", "Пишите в любое время"} {
		if !strings.Contains(body, want) {
			t.Errorf("notice %q lacks %q", body, want)
		}
	}
	r := w.mustInvite(w.tm.Owner, d.ID, requestIn())
	if r.Status != InvPending || !r.CanCancel {
		t.Errorf("request = %+v", r)
	}
	if body := w.noticeBody(me, "invitation_request_contacts"); !strings.Contains(body, "Оставьте телефон") || !strings.Contains(body, "странице отклика") {
		t.Errorf("notice %q", body)
	}
	// Приглашения разных видов живут рядом, новое собеседование заменяет только прежнее собеседование.
	i1 := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	i2 := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	if w.invStatus(i1.ID) != InvCancelled || w.invStatus(i2.ID) != InvPending || w.invStatus(c.ID) != InvShared || w.invStatus(r.ID) != InvPending {
		t.Error("kinds got mixed up")
	}
	if w.statusOf(d.ID) != StatusInvited {
		t.Error("status")
	}
}

func TestInviteRules(t *testing.T) {
	// Статусы: приглашать можно из «отправлен», «просмотрен» и «приглашён».
	for _, st := range Statuses {
		t.Run(st, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			w.setStatus(d.ID, st)
			_, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, w.interview())
			want := CanInvite(st)
			if want != (err == nil) || (!want && !errors.Is(err, ErrBadStatus)) {
				t.Fatalf("err = %v, want ok=%v", err, want)
			}
			if !want && (testkit.Count(t, `SELECT count(*) FROM application_invitations WHERE application_id = $1`, d.ID) != 0 || w.statusOf(d.ID) != st) {
				t.Error("a refused invitation left a trace")
			}
		})
	}
	t.Run("validation", func(t *testing.T) {
		w := newWorld(t)
		d := w.viewedApp(w.Applicant("Мария"))
		var verr *auth.ValidationError
		if _, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, InvitationInput{Kind: InvInterview}); !errors.As(err, &verr) || verr.Fields["starts_at"] == "" || verr.Fields["place_kind"] == "" {
			t.Errorf("err = %v", err)
		}
		if w.statusOf(d.ID) != StatusViewed {
			t.Error("a refused invitation moved the status")
		}
		if _, err := w.svc.Invite(bg, w.tm.Owner.User, uuid.New(), w.interview()); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown application: %v", err)
		}
	})
	t.Run("limit", func(t *testing.T) {
		w := newWorld(t)
		d := w.viewedApp(w.Applicant("Мария"))
		for i := range maxInvitations {
			if _, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, contactsIn()); err != nil {
				t.Fatalf("invitation %d: %v", i+1, err)
			}
		}
		if _, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, requestIn()); !errors.Is(err, ErrTooManyInvitations) {
			t.Errorf("eleventh: %v", err)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM application_invitations WHERE application_id = $1`, d.ID); n != maxInvitations {
			t.Errorf("invitations = %d", n)
		}
	})
}

// Решение и приглашение одновременно: либо отказ без приглашения, либо приглашение и затем отказ, который его закрыл.
func TestInviteAndRejectRace(t *testing.T) {
	for range 4 {
		w := newWorld(t)
		d := w.viewedApp(w.Applicant("Мария"))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, "") }()
		go func() { defer wg.Done(); _, _ = w.svc.Invite(bg, w.tm.HR.User, d.ID, w.interview()) }()
		wg.Wait()
		if w.statusOf(d.ID) != StatusRejected {
			t.Fatalf("status = %s", w.statusOf(d.ID))
		}
		if n := testkit.Count(t, `SELECT count(*) FROM application_invitations WHERE application_id = $1 AND status IN ('pending','proposed','confirmed')`, d.ID); n != 0 {
			t.Fatalf("%d open invitations on a rejected application", n)
		}
	}
}

func TestCancelInvitation(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	if err := w.svc.CancelInvitation(bg, w.tm.HeadA.User, d.ID, inv.ID); err != nil {
		t.Fatal(err)
	}
	if w.invStatus(inv.ID) != InvCancelled || w.statusOf(d.ID) != StatusInvited {
		t.Error("state after cancel")
	}
	if body := w.noticeBody(me, "invitation_cancelled"); !strings.Contains(body, "собеседование") {
		t.Errorf("notice %q", body)
	}
	// Отменённое отменить и принять нельзя; ответить на него тоже.
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, inv.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("cancel twice: %v", err)
	}
	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, inv.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("accept cancelled: %v", err)
	}
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("answer cancelled: %v", err)
	}
	// Переданные контакты и ответ на просьбу отменять нельзя; неизвестное приглашение не находится.
	shared := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, shared.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("cancel shared contacts: %v", err)
	}
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, uuid.New(), inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
	// Подтверждённое собеседование отменить можно, отвеченную просьбу нельзя.
	i2 := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	_ = w.svc.Answer(bg, me.User, d.ID, i2.ID, AnswerInput{Action: AnswerConfirm})
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, i2.ID); err != nil {
		t.Errorf("cancel confirmed: %v", err)
	}
	ask := w.mustInvite(w.tm.Owner, d.ID, requestIn())
	_ = w.svc.Answer(bg, me.User, d.ID, ask.ID, AnswerInput{Action: AnswerReply, Contact: "x@y.ru"})
	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, ask.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("cancel answered request: %v", err)
	}
}

// ---- ответы соискателя ----

func TestAnswerConfirmProposeReply(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)

	// Подтверждение собеседования.
	i1 := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	if err := w.svc.Answer(bg, me.User, d.ID, i1.ID, AnswerInput{Action: AnswerConfirm, Note: "  Буду  "}); err != nil {
		t.Fatal(err)
	}
	for _, p := range w.staffOfUnitA() {
		if body := w.noticeBody(p, "invitation_confirmed"); !strings.Contains(body, "Кандидат: Мария") || !strings.Contains(body, "(МСК)") {
			t.Errorf("%s: %q", p.Name, body)
		}
	}
	for _, p := range []testkit.Person{w.tm.HeadB, w.tm.Out, me} {
		if notificationsOf(t, p, "invitation_confirmed") != 0 {
			t.Errorf("%s was notified", p.Name)
		}
	}
	got := w.invitation(d.ID, i1.ID)
	if got.Status != InvConfirmed || got.Answer == nil || got.Answer.Note != "Буду" || got.Answer.ProposedAt != nil || got.CanCancel != true || got.CanAnswer {
		t.Errorf("confirmed = %+v", got)
	}
	// Отвечать второй раз нельзя.
	for _, in := range []AnswerInput{{Action: AnswerConfirm}, {Action: AnswerPropose, ProposedAt: w.when(72 * time.Hour)}} {
		if err := w.svc.Answer(bg, me.User, d.ID, i1.ID, in); !errors.Is(err, ErrBadInvitation) {
			t.Errorf("second answer: %v", err)
		}
	}

	// Другое время.
	i2 := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	proposed := w.when(100 * time.Hour)
	if err := w.svc.Answer(bg, me.User, d.ID, i2.ID, AnswerInput{Action: AnswerPropose, ProposedAt: proposed, Note: "В этот день занят"}); err != nil {
		t.Fatal(err)
	}
	if w.invStatus(i2.ID) != InvProposed {
		t.Error("not proposed")
	}
	if body := w.noticeBody(w.tm.HR, "invitation_proposed"); !strings.Contains(body, "Предложено:") || !strings.Contains(body, "В этот день занят") {
		t.Errorf("notice %q", body)
	}
	forStaff := w.invitation(d.ID, i2.ID)
	if !forStaff.CanAcceptProposal || forStaff.Answer == nil || forStaff.Answer.ProposedAt == nil {
		t.Errorf("proposal = %+v", forStaff)
	}
	// Список организации показывает, что на предложение нужно ответить.
	list, _ := w.svc.Candidates(bg, w.tm.Owner.User, CandidateFilter{})
	if list.Items[0].ProposedInvitations != 1 || list.Items[0].PendingInvitations != 0 {
		t.Errorf("list: %+v", list.Items[0])
	}

	// Просьба оставить контакты. Контакты соискателя в уведомление не попадают.
	i3 := w.mustInvite(w.tm.Owner, d.ID, requestIn())
	if err := w.svc.Answer(bg, me.User, d.ID, i3.ID, AnswerInput{Action: AnswerReply, Contact: "+7 913 111-22-33", Time: "после 15:00"}); err != nil {
		t.Fatal(err)
	}
	body := w.noticeBody(w.tm.Owner, "invitation_answered")
	if strings.Contains(body, "913") || !strings.Contains(body, "карточке отклика") {
		t.Errorf("notice %q", body)
	}
	got = w.invitation(d.ID, i3.ID)
	if got.Status != InvAnswered || got.Answer.Contact != "+7 913 111-22-33" || got.Answer.Time != "после 15:00" {
		t.Errorf("reply = %+v", got)
	}
}

func TestAnswerValidationKeepsTheInvitationOpen(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	interview := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	contacts := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
	ask := w.mustInvite(w.tm.Owner, d.ID, requestIn())
	cases := []struct {
		name string
		inv  uuid.UUID
		in   AnswerInput
		want string
	}{
		{"propose without time", interview.ID, AnswerInput{Action: AnswerPropose}, "proposed_at"},
		{"reply to an interview", interview.ID, AnswerInput{Action: AnswerReply, Contact: "x"}, "action"},
		{"reply without contact", ask.ID, AnswerInput{Action: AnswerReply}, "contact"},
		{"confirm a request", ask.ID, AnswerInput{Action: AnswerConfirm}, "action"},
		{"answer shared contacts", contacts.ID, AnswerInput{Action: AnswerConfirm}, "action"},
	}
	for _, c := range cases {
		var verr *auth.ValidationError
		if err := w.svc.Answer(bg, me.User, d.ID, c.inv, c.in); !errors.As(err, &verr) || verr.Fields[c.want] == "" {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if w.invStatus(interview.ID) != InvPending || w.invStatus(ask.ID) != InvPending || w.invStatus(contacts.ID) != InvShared {
		t.Error("an invalid answer changed an invitation")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind LIKE 'invitation\_%'`, w.tm.Owner.ID); n != 0 {
		t.Errorf("%d notifications for staff", n)
	}
}

func TestSimultaneousAnswersPassOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	var ok, bad atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := AnswerInput{Action: AnswerConfirm}
			if i%2 == 1 {
				in = AnswerInput{Action: AnswerPropose, ProposedAt: w.when(80 * time.Hour)}
			}
			switch err := w.svc.Answer(bg, me.User, d.ID, inv.ID, in); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrBadInvitation):
				bad.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || bad.Load() != 5 {
		t.Errorf("passed %d, refused %d", ok.Load(), bad.Load())
	}
	if n := testkit.Count(t, `SELECT count(*) FROM notifications WHERE user_id = $1 AND kind IN ('invitation_confirmed', 'invitation_proposed')`, w.tm.Owner.ID); n != 1 {
		t.Errorf("owner notifications = %d", n)
	}
}

// ---- принять предложенное время ----

func TestAcceptProposal(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())

	// Пока предложения нет, принимать нечего.
	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, inv.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("accept before a proposal: %v", err)
	}
	proposed := w.when(100 * time.Hour)
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerPropose, ProposedAt: proposed}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.AcceptProposal(bg, w.tm.HeadA.User, d.ID, inv.ID); err != nil {
		t.Fatal(err)
	}
	got := w.invitation(d.ID, inv.ID)
	want, _ := time.Parse(time.RFC3339, proposed)
	if got.Status != InvConfirmed || got.StartsAt == nil || !got.StartsAt.Truncate(time.Minute).Equal(want.UTC().Truncate(time.Minute)) || got.CanAcceptProposal || !got.CanCancel {
		t.Errorf("after accept = %+v", got)
	}
	if body := w.noticeBody(me, "invitation_confirmed"); !strings.Contains(body, "Собеседование:") || !strings.Contains(body, "Онлайн:") {
		t.Errorf("notice %q", body)
	}
	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, inv.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("accept twice: %v", err)
	}
}

func TestAcceptProposalAfterTheProposedTimePassed(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerPropose, ProposedAt: w.when(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(4 * time.Hour)
	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, inv.ID); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("err = %v", err)
	}
	if w.invStatus(inv.ID) != InvProposed {
		t.Error("the invitation changed")
	}
}

// ---- отзыв закрывает приглашения ----

func TestWithdrawClosesOpenInvitations(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	open := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	shared := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
	if err := w.svc.Withdraw(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if w.invStatus(open.ID) != InvCancelled || w.invStatus(shared.ID) != InvShared {
		t.Error("invitations after withdraw")
	}
	if err := w.svc.Answer(bg, me.User, d.ID, open.ID, AnswerInput{Action: AnswerConfirm}); !errors.Is(err, ErrBadInvitation) {
		t.Errorf("answer after withdraw: %v", err)
	}
}

// ---- что видит каждая сторона ----

func TestViewerHints(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	// Соискатель: кнопок организации нет.
	got, _ := w.svc.Get(bg, me.User, d.ID)
	if got.Viewer.CanInvite || len(got.Viewer.Decisions) != 0 || got.Invitations == nil || len(got.Invitations) != 0 {
		t.Errorf("applicant: %+v / %v", got.Viewer, got.Invitations)
	}
	// Организация: из «просмотрен» можно приглашать, принять и отказать.
	staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID)
	if !staff.Viewer.CanInvite || len(staff.Viewer.Decisions) != 2 || staff.Viewer.Decisions[0] != StatusAccepted || staff.Viewer.CanWithdraw {
		t.Errorf("staff: %+v", staff.Viewer)
	}
}

// ---- список откликов организации ----

type listWorld struct {
	*world
	other   *testkit.Team // чужая организация
	vacA    vacancies.Detail
	vacB    vacancies.Detail
	vacOth  vacancies.Detail
	appA    []Detail
	appB    Detail
	appOth  Detail
	applics []testkit.Person
}

func newListWorld(t *testing.T) *listWorld {
	w := newWorld(t)
	l := &listWorld{world: w}
	l.vacA = w.vacancy
	l.vacB = w.tm.Published(&w.tm.UnitB.ID)
	l.other = w.Team()
	l.vacOth = l.other.Published(&l.other.UnitA.ID)
	for _, name := range []string{"Анна", "Борис", "Вера"} {
		p := w.Applicant(name)
		l.applics = append(l.applics, p)
		l.appA = append(l.appA, w.mustApply(p, nil))
		w.clock.Advance(time.Minute)
	}
	l.appB = w.mustApply(l.applics[0], func(i *Input) { i.VacancyID = l.vacB.ID })
	l.appOth = w.mustApply(l.applics[1], func(i *Input) { i.VacancyID = l.vacOth.ID })
	return l
}

func ids(l CandidateList) []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, c := range l.Items {
		out[i] = c.ID
	}
	return out
}

func TestCandidatesScopeByRole(t *testing.T) {
	l := newListWorld(t)
	has := func(list CandidateList, id uuid.UUID) bool {
		for _, got := range ids(list) {
			if got == id {
				return true
			}
		}
		return false
	}
	type row struct {
		who  testkit.Person
		name string
		a, b bool // видит отклики на вакансии подразделений A и B
	}
	for _, r := range []row{
		{l.tm.Owner, "owner", true, true}, {l.tm.HR, "hr", true, true},
		{l.tm.HeadA, "head A", true, false}, {l.tm.HeadB, "head B", false, true},
		{l.tm.Out, "outsider", false, false}, {l.applics[0], "applicant", false, false},
	} {
		list, err := l.svc.Candidates(bg, r.who.User, CandidateFilter{})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		for _, a := range l.appA {
			if has(list, a.ID) != r.a {
				t.Errorf("%s: sees unit A application = %v, want %v", r.name, !r.a, r.a)
			}
		}
		if has(list, l.appB.ID) != r.b {
			t.Errorf("%s: unit B application visibility", r.name)
		}
		if has(list, l.appOth.ID) {
			t.Errorf("%s sees an application of another organization", r.name)
		}
		want := 0
		if r.a {
			want += 3
		}
		if r.b {
			want++
		}
		if list.Total != want || len(list.Items) != want {
			t.Errorf("%s: total %d items %d, want %d", r.name, list.Total, len(list.Items), want)
		}
		vs, err := l.svc.CandidateVacancies(bg, r.who.User)
		if err != nil || len(vs) != btoi(r.a)+btoi(r.b) {
			t.Errorf("%s: vacancies %v %v", r.name, vs, err)
		}
	}
	// Чужой организации тоже ничего не видно с этой стороны; фильтр по чужой вакансии даёт пустоту, а не ошибку.
	list, err := l.svc.Candidates(bg, l.tm.Owner.User, CandidateFilter{VacancyID: l.vacOth.ID})
	if err != nil || list.Total != 0 || len(list.Items) != 0 {
		t.Errorf("foreign vacancy filter: %+v %v", list, err)
	}
	if list, _ := l.svc.Candidates(bg, l.other.Owner.User, CandidateFilter{}); list.Total != 1 || list.Items[0].ID != l.appOth.ID {
		t.Errorf("the other organization: %+v", list)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestCandidatesFiltersCountsAndPages(t *testing.T) {
	l := newListWorld(t)
	owner := l.tm.Owner.User
	// Порядок: новые сверху.
	all, _ := l.svc.Candidates(bg, owner, CandidateFilter{})
	if all.Total != 4 || all.Items[0].ID != l.appB.ID || all.Items[3].ID != l.appA[0].ID {
		t.Errorf("order: %v", ids(all))
	}
	if all.Counts[StatusSent] != 4 || all.Counts[StatusRejected] != 0 || len(all.Counts) != len(Statuses) {
		t.Errorf("counts = %v", all.Counts)
	}
	first := all.Items[3]
	if first.ApplicantName != "Анна" || first.Headline == "" || first.Vacancy.Title != l.vacA.Title || first.UnitName == "" || first.Vacancy.OrgName == "" {
		t.Errorf("row = %+v", first)
	}
	// Вакансия.
	byVac, _ := l.svc.Candidates(bg, owner, CandidateFilter{VacancyID: l.vacA.ID})
	if byVac.Total != 3 || byVac.Counts[StatusSent] != 3 {
		t.Errorf("by vacancy: %+v", byVac)
	}
	// Статус: счётчики остаются полными, список короче.
	l.setStatus(l.appA[0].ID, StatusInvited)
	l.setStatus(l.appA[1].ID, StatusWithdrawn)
	byStatus, _ := l.svc.Candidates(bg, owner, CandidateFilter{Status: StatusInvited})
	if byStatus.Total != 1 || byStatus.Items[0].ID != l.appA[0].ID || byStatus.Counts[StatusSent] != 2 || byStatus.Counts[StatusWithdrawn] != 1 {
		t.Errorf("by status: %+v", byStatus)
	}
	both, _ := l.svc.Candidates(bg, owner, CandidateFilter{Status: StatusSent, VacancyID: l.vacA.ID})
	if both.Total != 1 || both.Items[0].ID != l.appA[2].ID {
		t.Errorf("status and vacancy: %+v", both)
	}
	// Страницы: по умолчанию 20, предел 50, отрицательное смещение — с начала.
	page, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 3, Offset: 3})
	if page.Total != 4 || len(page.Items) != 1 {
		t.Errorf("page: %+v", page)
	}
	if beyond, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 3, Offset: 30}); len(beyond.Items) != 0 || beyond.Total != 4 {
		t.Errorf("beyond: %+v", beyond)
	}
	if odd, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 1000, Offset: -5}); len(odd.Items) != 4 {
		t.Errorf("odd paging: %+v", odd)
	}
	// Неизвестный статус.
	var verr *auth.ValidationError
	if _, err := l.svc.Candidates(bg, owner, CandidateFilter{Status: "done"}); !errors.As(err, &verr) || verr.Fields["status"] == "" {
		t.Errorf("unknown status: %v", err)
	}
}

func TestCandidateVacancies(t *testing.T) {
	l := newListWorld(t)
	vs, err := l.svc.CandidateVacancies(bg, l.tm.Owner.User)
	if err != nil || len(vs) != 2 {
		t.Fatalf("%v %v", vs, err)
	}
	byID := map[uuid.UUID]VacancyCount{}
	for _, v := range vs {
		byID[v.ID] = v
	}
	if a := byID[l.vacA.ID]; a.Total != 3 || a.New != 3 || a.Title != l.vacA.Title || a.OrgName == "" || a.OrgSlug != l.tm.Slug || a.Status != "published" {
		t.Errorf("A = %+v", a)
	}
	l.setStatus(l.appA[0].ID, StatusViewed)
	l.setStatus(l.appA[1].ID, StatusWithdrawn)
	vs, _ = l.svc.CandidateVacancies(bg, l.tm.Owner.User)
	for _, v := range vs {
		if v.ID == l.vacA.ID && (v.Total != 2 || v.New != 1) {
			t.Errorf("A after changes = %+v", v)
		}
	}
	// Вакансия без откликов в список не попадает; руководитель видит только свою.
	l.vacancy = l.tm.Published(&l.tm.UnitA.ID)
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.Owner.User); len(vs) != 2 {
		t.Errorf("a vacancy without applications is listed: %d", len(vs))
	}
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.HeadB.User); len(vs) != 1 || vs[0].ID != l.vacB.ID {
		t.Errorf("head B: %v", vs)
	}
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.Out.User); len(vs) != 0 {
		t.Errorf("outsider: %v", vs)
	}
}

func TestKindWord(t *testing.T) {
	for kind, want := range map[string]string{InvInterview: "собеседование", InvContacts: "контакты организации", InvRequest: "просьбу оставить контакты"} {
		if got := kindWord(kind); got != want {
			t.Errorf("kindWord(%q) = %q", kind, got)
		}
	}
}

// Автор отклика, ставший сотрудником, о своём же ответе уведомления не получает.
func TestAnswerDoesNotNotifyTheApplicantWhoIsStaff(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	inv := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	w.makeStaff(me, "hr")
	if err := w.svc.Answer(bg, me.User, d.ID, inv.ID, AnswerInput{Action: AnswerConfirm}); err != nil {
		t.Fatal(err)
	}
	if notificationsOf(t, me, "invitation_confirmed") != 0 || notificationsOf(t, w.tm.Owner, "invitation_confirmed") != 1 {
		t.Error("wrong recipients")
	}
}

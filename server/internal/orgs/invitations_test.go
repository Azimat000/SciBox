package orgs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/mail"
)

func TestInviteAndAccept(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван Петров")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	newbie := w.user("Анна Смирнова")

	inv, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "  " + strings.ToUpper(newbie.Email) + " ", Role: "unit_head", UnitID: &unit.ID})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Email != newbie.Email || inv.Role != "unit_head" || inv.UnitName == nil || *inv.UnitName != unit.Name || !inv.ExpiresAt.Equal(inv.CreatedAt.Add(7*24*time.Hour)) {
		t.Fatalf("invitation: %+v", inv)
	}

	m := w.lastMailTo(newbie.Email)
	for _, want := range []string{"Иван Петров", org.Name, "руководителя подразделения", unit.Name, "/invitations/accept?token=", "семь дней"} {
		if !strings.Contains(m.Body+m.Subject, want) {
			t.Errorf("mail lacks %q:\n%s\n%s", want, m.Subject, m.Body)
		}
	}
	token := tokenIn(t, m.Body)

	preview, err := w.svc.LookupInvitation(bg, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Organization.Slug != org.Slug || preview.Organization.Name != org.Name || preview.Role != "unit_head" || preview.Email != newbie.Email || preview.EmailMatches != nil {
		t.Errorf("anonymous preview: %+v", preview)
	}
	if preview, _ = w.svc.LookupInvitation(bg, token, &newbie.User); preview.EmailMatches == nil || !*preview.EmailMatches {
		t.Errorf("the right account must match: %+v", preview)
	}
	stranger := w.user("Чужой")
	if preview, _ = w.svc.LookupInvitation(bg, token, &stranger.User); preview.EmailMatches == nil || *preview.EmailMatches {
		t.Errorf("another account must not match: %+v", preview)
	}

	// Чужой аккаунт приглашение принять не может, и оно от этого не сгорает.
	if _, err := w.svc.AcceptInvitation(bg, stranger.User, token); !errors.Is(err, ErrWrongEmail) {
		t.Fatalf("err = %v, want ErrWrongEmail", err)
	}
	if _, err := w.svc.LookupInvitation(bg, token, nil); err != nil {
		t.Fatalf("a wrong-address attempt must not burn the invitation: %v", err)
	}
	if mine, _ := w.svc.MyOrganizations(bg, stranger.User); len(mine) != 0 {
		t.Fatal("a stranger became a member")
	}

	joined, err := w.svc.AcceptInvitation(bg, newbie.User, token)
	if err != nil {
		t.Fatal(err)
	}
	if joined.Slug != org.Slug || joined.Name != org.Name || joined.Role != "unit_head" {
		t.Errorf("joined: %+v", joined)
	}
	if v, _ := w.svc.GetUnit(bg, org.Slug, unit.ID, &newbie.User); v.Unit.HeadName == nil || *v.Unit.HeadName != "Анна Смирнова" || len(v.Viewer.EditableUnits) != 1 {
		t.Errorf("the new head must head the unit: %+v", v)
	}
	// Ссылка одноразовая.
	if _, err := w.svc.AcceptInvitation(bg, newbie.User, token); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("second use: %v", err)
	}
	if _, err := w.svc.LookupInvitation(bg, token, nil); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("lookup after use: %v", err)
	}
}

func TestAcceptKeepsExistingHead(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	first, second := w.user("Первый"), w.user("Второй")
	w.member(owner, org.Slug, first, "unit_head", &unit.ID)
	w.member(owner, org.Slug, second, "unit_head", &unit.ID) // место занято
	v, _ := w.svc.GetUnit(bg, org.Slug, unit.ID, &owner.User)
	if v.Unit.HeadUserID == nil || *v.Unit.HeadUserID != first.ID {
		t.Errorf("an accepted invitation must not take over an occupied place: %+v", v.Unit)
	}
	if roleOf(t, w, org.Slug, owner, second) != "unit_head" {
		t.Error("the second person must still become a member")
	}
}

func TestInviteValidation(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	otherOrg := w.org(owner)
	otherUnit := w.unit(owner, otherOrg.Slug)
	unit := w.unit(owner, org.Slug)
	member := w.user("Уже здесь")
	w.member(owner, org.Slug, member, "hr", nil)

	cases := []struct {
		name  string
		in    InviteInput
		field string
		msg   string
	}{
		{"bad email", InviteInput{Email: "not-an-email", Role: "hr"}, "email", ""},
		{"empty email", InviteInput{Email: "", Role: "hr"}, "email", ""},
		{"bad role", InviteInput{Email: "a@example.ru", Role: "boss"}, "role", msgRoleInvalid},
		{"unit for hr", InviteInput{Email: "a@example.ru", Role: "hr", UnitID: &unit.ID}, "unit_id", msgUnitForHeadsOn},
		{"unit for owner", InviteInput{Email: "a@example.ru", Role: "owner", UnitID: &unit.ID}, "unit_id", msgUnitForHeadsOn},
		{"unit of another organization", InviteInput{Email: "a@example.ru", Role: "unit_head", UnitID: &otherUnit.ID}, "unit_id", msgUnitUnknown},
		{"unit that does not exist", InviteInput{Email: "a@example.ru", Role: "unit_head", UnitID: ptr(uuid.New())}, "unit_id", msgUnitUnknown},
		{"already a member", InviteInput{Email: strings.ToUpper(member.Email), Role: "hr"}, "email", msgAlreadyMember},
	}
	for _, c := range cases {
		_, err := w.svc.Invite(bg, owner.User, org.Slug, c.in)
		got := fieldsOf(t, err)[c.field]
		if got == "" || (c.msg != "" && got != c.msg) {
			t.Errorf("%s: field %q = %q, want %q", c.name, c.field, got, c.msg)
		}
	}
	// Одно письмо при неверных данных не уходит.
	if n := len(w.mailsTo("a@example.ru")); n != 0 {
		t.Errorf("%d mails sent for invalid invitations", n)
	}
	// Роль неверная и подразделение указано: ошибка только про роль (про подразделение судить рано).
	_, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "a@example.ru", Role: "boss", UnitID: &unit.ID})
	if f := fieldsOf(t, err); f["unit_id"] != "" || f["role"] == "" {
		t.Errorf("fields = %v", f)
	}
	// Руководитель без подразделения — допустимо.
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "a@example.ru", Role: "unit_head"}); err != nil {
		t.Errorf("a head without a unit: %v", err)
	}
}

func TestInviteAgainReplacesEarlier(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	guest := w.user("Гость")

	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	first := tokenIn(t, w.lastMailTo(guest.Email).Body)
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	second := tokenIn(t, w.lastMailTo(guest.Email).Body)
	if first == second {
		t.Fatal("a new invitation needs a new link")
	}
	if _, err := w.svc.AcceptInvitation(bg, guest.User, first); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("the replaced link must stop working: %v", err)
	}
	v, _ := w.svc.Members(bg, owner.User, org.Slug)
	if len(v.Invitations) != 1 || v.Invitations[0].Role != "owner" {
		t.Errorf("only the newest invitation is pending: %+v", v.Invitations)
	}
	if _, err := w.svc.AcceptInvitation(bg, guest.User, second); err != nil {
		t.Fatal(err)
	}
	if got := roleOf(t, w, org.Slug, owner, guest); got != "owner" {
		t.Errorf("role = %q, want the newest invitation's role", got)
	}
}

func TestInvitationExpiresAndCanBeRevoked(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	guest := w.user("Гость")

	inv, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
	if err != nil {
		t.Fatal(err)
	}
	token := tokenIn(t, w.lastMailTo(guest.Email).Body)
	w.clock.Advance(7*24*time.Hour - time.Second)
	if _, err := w.svc.LookupInvitation(bg, token, nil); err != nil {
		t.Fatalf("one second before the end: %v", err)
	}
	w.clock.Advance(2 * time.Second)
	if _, err := w.svc.LookupInvitation(bg, token, nil); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("expired lookup: %v", err)
	}
	if _, err := w.svc.AcceptInvitation(bg, guest.User, token); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("expired accept: %v", err)
	}
	if v, _ := w.svc.Members(bg, owner.User, org.Slug); len(v.Invitations) != 0 {
		t.Errorf("an expired invitation is not pending: %+v", v.Invitations)
	}
	if mine, _ := w.svc.MyInvitations(bg, guest.User); len(mine) != 0 {
		t.Errorf("an expired invitation is not listed: %+v", mine)
	}

	// Отзыв.
	inv2, _ := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
	token2 := tokenIn(t, w.lastMailTo(guest.Email).Body)
	if err := w.svc.RevokeInvitation(bg, owner.User, org.Slug, inv2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AcceptInvitation(bg, guest.User, token2); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("revoked accept: %v", err)
	}
	if err := w.svc.RevokeInvitation(bg, owner.User, org.Slug, inv2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking twice: %v", err)
	}
	if err := w.svc.RevokeInvitation(bg, owner.User, org.Slug, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an expired-and-replaced invitation: %v", err)
	}
	// Приглашение другой организации по адресу этой не отозвать.
	other := w.org(owner)
	inv3, _ := w.svc.Invite(bg, owner.User, other.Slug, InviteInput{Email: guest.Email, Role: "hr"})
	if err := w.svc.RevokeInvitation(bg, owner.User, org.Slug, inv3.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking through the wrong organization: %v", err)
	}
	if err := w.svc.RevokeInvitation(bg, owner.User, "no-such-org", inv3.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing organization: %v", err)
	}
	if _, err := w.svc.LookupInvitation(bg, "garbage", nil); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("garbage token: %v", err)
	}
}

func TestAcceptByIDAndMyInvitations(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	guest := w.user("Гость")

	if mine, _ := w.svc.MyInvitations(bg, guest.User); len(mine) != 0 {
		t.Fatalf("no invitations yet: %+v", mine)
	}
	inv, _ := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "unit_head", UnitID: &unit.ID})
	mine, err := w.svc.MyInvitations(bg, guest.User)
	if err != nil || len(mine) != 1 {
		t.Fatalf("my invitations: %+v, %v", mine, err)
	}
	m := mine[0]
	if m.ID != inv.ID || m.Organization.Slug != org.Slug || m.Organization.Name != org.Name || m.Role != "unit_head" || m.UnitName == nil || *m.UnitName != unit.Name {
		t.Errorf("listed invitation: %+v", m)
	}
	stranger := w.user("Чужой")
	if other, _ := w.svc.MyInvitations(bg, stranger.User); len(other) != 0 {
		t.Errorf("someone else's invitation is listed: %+v", other)
	}

	if _, err := w.svc.AcceptInvitationByID(bg, stranger.User, inv.ID); !errors.Is(err, ErrWrongEmail) {
		t.Errorf("a stranger accepting by id: %v", err)
	}
	if _, err := w.svc.AcceptInvitationByID(bg, guest.User, uuid.New()); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("unknown id: %v", err)
	}
	joined, err := w.svc.AcceptInvitationByID(bg, guest.User, inv.ID)
	if err != nil || joined.Slug != org.Slug || joined.Role != "unit_head" {
		t.Fatalf("accept by id: %+v, %v", joined, err)
	}
	if _, err := w.svc.AcceptInvitationByID(bg, guest.User, inv.ID); !errors.Is(err, ErrInvalidInvitation) {
		t.Errorf("accepting twice: %v", err)
	}
	if mine, _ := w.svc.MyInvitations(bg, guest.User); len(mine) != 0 {
		t.Errorf("an accepted invitation is not listed: %+v", mine)
	}
}

// Человек уже вступил (владелец сам добавил его через другое приглашение): повторное принятие отказывает и не сжигает приглашение.
func TestAcceptRefusedForExistingMemberKeepsInvitationAlive(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	guest := w.user("Гость")
	// Два приглашения от разных «поколений»: второе ещё живо, пока первое принимают в обход замены.
	inv, _ := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
	token := tokenIn(t, w.lastMailTo(guest.Email).Body)
	if _, err := sharedPool.Exec(bg, "INSERT INTO org_members (org_id, user_id, role, joined_at) VALUES ($1, $2, 'hr', now())", org.ID, guest.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AcceptInvitation(bg, guest.User, token); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("err = %v, want ErrAlreadyMember", err)
	}
	if _, err := w.svc.AcceptInvitationByID(bg, guest.User, inv.ID); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("by id: %v", err)
	}
	if _, err := w.svc.LookupInvitation(bg, token, nil); err != nil {
		t.Errorf("a refused acceptance must not burn the invitation: %v", err)
	}
}

func TestInviteIsRateLimited(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	w.svc.cfg.Invite = Limit{Max: 3, Window: time.Hour}
	for i := 0; i < 3; i++ {
		if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: uniqueEmail(), Role: "hr"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "late@example.ru", Role: "hr"})
	var rl *auth.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > time.Hour {
		t.Fatalf("err = %v", err)
	}
	if n := len(w.mailsTo("late@example.ru")); n != 0 {
		t.Errorf("a limited invitation sent %d mails", n)
	}
	w.clock.Advance(61 * time.Minute)
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "late@example.ru", Role: "hr"}); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

func uniqueEmail() string { return strings.ToLower("x" + uuid.NewString()[:8] + "@example.ru") }

// Почтовый сервер упал: приглашение создано, письмо потеряно, сбой попал в журнал без адреса получателя.
func TestMailFailureIsLoggedWithoutAddress(t *testing.T) {
	w := newWorld(t)
	w.svc.mailer = failingMailer{}
	owner := w.user("Иван")
	org := w.org(owner)
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "secret.person@example.ru", Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	w.svc.Flush()
	logs := w.logs.String()
	if !strings.Contains(logs, "send mail") || strings.Contains(logs, "secret.person") {
		t.Errorf("logs:\n%s", logs)
	}
}

type failingMailer struct{}

func (failingMailer) Send(context.Context, mail.Message) error { return errors.New("smtp is down") }

package auth

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/testaudit"
)

func (e *env) accountActions(id uuid.UUID) []string {
	e.t.Helper()
	return testaudit.Actions(testaudit.Account(e.t, sharedPool, id))
}

// Всё, что важно знать о чужом доступе к аккаунту, остаётся в журнале аккаунта (без организации).
func TestAuditTrailOfTheAccount(t *testing.T) {
	e := newEnv(t)
	email := uniqueEmail()
	tok := e.register(email)
	meta := e.meta()
	u, s, err := e.svc.ConfirmEmail(bg, tok, meta)
	if err != nil {
		t.Fatal(err)
	}
	// Неудачные входы и ошибки проверки в журнал не попадают.
	if _, _, err := e.login(email, "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	loginMeta := e.meta()
	if _, _, err := e.svc.Login(bg, email, goodPassword, loginMeta); err != nil {
		t.Fatal(err)
	}
	p := e.freshPrincipal(s)
	if err := e.svc.ChangePassword(bg, p, "wrong current", "another secure phrase"); err == nil {
		t.Fatal("a wrong current password was accepted")
	}
	if err := e.svc.ChangePassword(bg, p, goodPassword, "violet candle orchard 77"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResetPassword(bg, tokenIn(t, e.lastMailTo(email)), "quiet harbour lantern 31"); err != nil {
		t.Fatal(err)
	}
	newEmail := uniqueEmail()
	_, s2, err := e.svc.Login(bg, email, "quiet harbour lantern 31", e.meta())
	if err != nil {
		t.Fatal(err)
	}
	p = e.freshPrincipal(s2)
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmEmailChange(bg, tokenIn(t, e.lastMailTo(newEmail))); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RevertEmailChange(bg, tokenIn(t, e.lastMailTo(email))); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"account.login",            // по ссылке из письма
		"account.login",            // пароль
		"account.password_changed", // смена в настройках
		"account.password_reset",
		"account.login",
		"account.email_change_asked",
		"account.email_changed",
		"account.email_reverted",
	}
	if got := e.accountActions(u.ID); !slices.Equal(got, want) {
		t.Fatalf("journal = %v\nwant      %v", got, want)
	}
	rows := testaudit.Account(t, sharedPool, u.ID)
	if rows[0].Details["via"] != "email_link" || *rows[0].IP != meta.IP || *rows[1].IP != loginMeta.IP {
		t.Errorf("login rows: %+v / %+v", rows[0], rows[1])
	}
	for _, r := range rows {
		if r.Org != nil || r.Subject != nil || *r.Actor != u.ID || r.TargetKind != "" {
			t.Errorf("%s: %+v", r.Action, r)
		}
		// Адреса почты (старый и новый) в записях нет.
		for _, v := range r.Details {
			if v == email || v == newEmail {
				t.Errorf("%s keeps an address", r.Action)
			}
		}
	}
	// В журнале организации этих записей нет.
	if n := countRows(t, `SELECT count(*) FROM audit_log WHERE org_id IS NOT NULL AND actor_id = $1`, u.ID); n != 0 {
		t.Errorf("%d account entries landed in organization journals", n)
	}
}

// Нет записи — нет входа и нет смены пароля или почты.
func TestAccountActionsFailWhenTheJournalIsDown(t *testing.T) {
	e := newEnv(t)
	u, s, email := e.confirmedUser()
	p := e.freshPrincipal(s)
	if err := e.svc.RequestPasswordReset(bg, email, e.meta()); err != nil {
		t.Fatal(err)
	}
	reset := tokenIn(t, e.lastMailTo(email))
	newEmail := uniqueEmail()
	if err := e.svc.RequestEmailChange(bg, p, newEmail, e.meta()); err != nil {
		t.Fatal(err)
	}
	change := tokenIn(t, e.lastMailTo(newEmail))
	tok2 := e.register(uniqueEmail())
	sessions := countRows(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, u.ID)
	before := countRows(t, `SELECT count(*) FROM audit_log`)

	testaudit.Break(t, sharedPool)

	if _, _, err := e.login(email, goodPassword); err == nil {
		t.Error("logged in without a journal entry")
	}
	if n := countRows(t, `SELECT count(*) FROM sessions WHERE user_id = $1`, u.ID); n != sessions {
		t.Errorf("sessions: %d, was %d", n, sessions)
	}
	if _, _, err := e.svc.ConfirmEmail(bg, tok2, e.meta()); err == nil {
		t.Error("confirmed an address without a journal entry")
	}
	if err := e.svc.ChangePassword(bg, p, goodPassword, "violet candle orchard 77"); err == nil {
		t.Error("changed the password without a journal entry")
	}
	if err := e.svc.ResetPassword(bg, reset, "quiet harbour lantern 31"); err == nil {
		t.Error("reset the password without a journal entry")
	}
	if _, err := e.svc.ConfirmEmailChange(bg, change); err == nil {
		t.Error("changed the address without a journal entry")
	}
	e.clock.Advance(5 * time.Minute) // пауза между письмами со ссылкой: иначе просьба молча ничего не сделает
	if err := e.svc.RequestEmailChange(bg, p, uniqueEmail(), e.meta()); err == nil {
		t.Error("asked for an address change without a journal entry")
	}
	if _, err := e.svc.RequestDeletion(bg, p); err == nil {
		t.Error("asked for deletion without a journal entry")
	}
	if n := countRows(t, `SELECT count(*) FROM audit_log`); n != before {
		t.Errorf("%d entries appeared", n-before)
	}
	// Ничего не изменилось: прежний пароль и прежняя почта на месте, удаление не назначено.
	if n := countRows(t, `SELECT count(*) FROM users WHERE id = $1 AND email = $2 AND deletion_due_at IS NULL`, u.ID, email); n != 1 {
		t.Error("the account changed although the journal refused")
	}
	if _, err := e.svc.Authenticate(bg, s.Token); err != nil {
		t.Errorf("the session ended: %v", err)
	}
}

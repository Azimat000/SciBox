package orgs

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/erasure"
	"scibox/server/internal/testaudit"
)

const testIP = "203.0.113.5"

// Всё, что меняет права в организации, остаётся в журнале: кто, кому, что было и что стало.
func TestAuditTrailOfMembers(t *testing.T) {
	w := newWorld(t)
	ctx := audit.WithSource(bg, testIP)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	hr, guest := w.user("Кадровик"), w.user("Гость")

	if _, err := w.svc.Invite(ctx, fresh(owner.User), org.Slug, InviteInput{Email: hr.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.AcceptInvitation(ctx, hr.User, tokenIn(t, w.lastMailTo(hr.Email).Body)); err != nil {
		t.Fatal(err)
	}
	steps := []func() error{
		func() error { return w.svc.ChangeRole(ctx, fresh(owner.User), org.Slug, hr.ID, "hr") },        // роль та же: записи нет
		func() error { return w.svc.ChangeRole(ctx, fresh(owner.User), org.Slug, hr.ID, "unit_head") }, // запись
		func() error { _, err := w.svc.SetUnitHead(ctx, owner.User, org.Slug, unit.ID, &hr.ID); return err },
		func() error { _, err := w.svc.SetUnitHead(ctx, owner.User, org.Slug, unit.ID, &hr.ID); return err }, // тот же: записи нет
		func() error { _, err := w.svc.SetUnitHead(ctx, owner.User, org.Slug, unit.ID, nil); return err },
		func() error { return w.svc.RemoveMember(ctx, owner.User, org.Slug, hr.ID) },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	inv, err := w.svc.Invite(ctx, fresh(owner.User), org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.RevokeInvitation(ctx, owner.User, org.Slug, inv.ID); err != nil {
		t.Fatal(err)
	}
	// Сотрудник уходит сам.
	w.member(owner, org.Slug, guest, "hr", nil)
	if err := w.svc.RemoveMember(ctx, guest.User, org.Slug, guest.ID); err != nil {
		t.Fatal(err)
	}

	rows := testaudit.Org(t, sharedPool, org.ID)
	want := []string{
		"invitation.sent", "member.joined", "member.role_changed", "unit.head_changed", "unit.head_changed", "member.removed",
		"invitation.sent", "invitation.revoked",
		"invitation.sent", "member.joined", "member.left", // w.member
	}
	if got := testaudit.Actions(rows); !slices.Equal(got, want) {
		t.Fatalf("journal = %v\nwant      %v", got, want)
	}
	for i, r := range rows {
		if r.Org == nil || *r.Org != org.ID || r.Actor == nil {
			t.Errorf("%s: org %v, actor %v", r.Action, r.Org, r.Actor)
		}
		// Адрес берётся из контекста запроса; вызовы w.member (записи 8 и 9) идут без запроса.
		if hasIP := r.IP != nil && *r.IP == testIP; hasIP == (i == 8 || i == 9) {
			t.Errorf("%s (#%d): ip %v", r.Action, i, r.IP)
		}
	}
	at := func(i int) testaudit.Row { return rows[i] }
	if r := at(0); *r.Actor != owner.ID || r.Details["role"] != "hr" || r.Details["email"] != audit.MaskEmail(hr.Email) || r.TargetKind != "invitation" || r.TargetID == nil {
		t.Errorf("invitation: %+v", r)
	}
	if r := at(1); *r.Actor != hr.ID || r.Details["role"] != "hr" || r.TargetID == nil || *r.TargetID != *at(0).TargetID {
		t.Errorf("joined: %+v (must point to the same invitation)", r)
	}
	if r := at(2); *r.Actor != owner.ID || *r.Subject != hr.ID || r.Details["from"] != "hr" || r.Details["to"] != "unit_head" {
		t.Errorf("role: %+v", r)
	}
	if r := at(3); *r.Subject != hr.ID || r.TargetKind != "unit" || *r.TargetID != unit.ID || r.TargetLabel != unit.Name {
		t.Errorf("head set: %+v", r)
	}
	if r := at(4); r.Subject != nil || r.Actor == nil {
		t.Errorf("head cleared: %+v", r)
	}
	if r := at(5); *r.Actor != owner.ID || *r.Subject != hr.ID || r.Details["role"] != "unit_head" {
		t.Errorf("removed: %+v", r)
	}
	if r := at(7); *r.TargetID != inv.ID || *r.Actor != owner.ID {
		t.Errorf("revoked: %+v", r)
	}
	if r := at(10); *r.Actor != guest.ID || *r.Subject != guest.ID || *r.IP != testIP {
		t.Errorf("left: %+v", r)
	}
	// Адрес письма в журнале только скрытый.
	for _, r := range rows {
		for _, v := range r.Details {
			if v == hr.Email || v == guest.Email {
				t.Errorf("%s keeps a full email address", r.Action)
			}
		}
	}
}

// Отказ в правах, чужая и несуществующая цель ничего не записывают.
func TestRefusedActionsLeaveNoTrace(t *testing.T) {
	w := newWorld(t)
	owner, hr, outsider := w.user("Иван"), w.user("Кадровик"), w.user("Посторонний")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	w.member(owner, org.Slug, hr, "hr", nil)
	before := len(testaudit.Org(t, sharedPool, org.ID))

	refused := []struct {
		name string
		do   func() error
	}{
		{"role by hr", func() error { return w.svc.ChangeRole(bg, fresh(hr.User), org.Slug, hr.ID, "owner") }},
		{"role by outsider", func() error { return w.svc.ChangeRole(bg, fresh(outsider.User), org.Slug, hr.ID, "owner") }},
		{"role of a stranger", func() error { return w.svc.ChangeRole(bg, fresh(owner.User), org.Slug, outsider.ID, "hr") }},
		{"bad role", func() error { return w.svc.ChangeRole(bg, fresh(owner.User), org.Slug, hr.ID, "boss") }},
		{"owner role needs a fresh password", func() error { return w.svc.ChangeRole(bg, stale(owner.User), org.Slug, hr.ID, "owner") }},
		{"remove by hr", func() error { return w.svc.RemoveMember(bg, hr.User, org.Slug, owner.ID) }},
		{"remove a stranger", func() error { return w.svc.RemoveMember(bg, owner.User, org.Slug, outsider.ID) }},
		{"remove the last owner", func() error { return w.svc.RemoveMember(bg, owner.User, org.Slug, owner.ID) }},
		{"invite by hr", func() error {
			_, err := w.svc.Invite(bg, fresh(hr.User), org.Slug, InviteInput{Email: "x@example.ru", Role: "hr"})
			return err
		}},
		{"invite a bad address", func() error {
			_, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: "not-an-address", Role: "hr"})
			return err
		}},
		{"revoke by hr", func() error { return w.svc.RevokeInvitation(bg, hr.User, org.Slug, uuid.New()) }},
		{"revoke unknown", func() error { return w.svc.RevokeInvitation(bg, owner.User, org.Slug, uuid.New()) }},
		{"head by hr", func() error { _, err := w.svc.SetUnitHead(bg, hr.User, org.Slug, unit.ID, &hr.ID); return err }},
		{"head of an unknown unit", func() error { _, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, uuid.New(), &hr.ID); return err }},
		{"head who is not a member", func() error { _, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, &outsider.ID); return err }},
		{"accept an unknown invitation", func() error { _, err := w.svc.AcceptInvitation(bg, outsider.User, "no-such-token"); return err }},
	}
	for _, tc := range refused {
		if err := tc.do(); err == nil {
			t.Errorf("%s: the action was not refused", tc.name)
		}
	}
	if after := len(testaudit.Org(t, sharedPool, org.ID)); after != before {
		t.Fatalf("refused actions wrote %d entries", after-before)
	}
}

// stale — человек, который давно не вводил пароль.
func stale(u auth.User) auth.Principal { return auth.Principal{User: u} }

// Нет записи — нет действия: если журнал не принимает запись, ничего не меняется.
func TestActionsFailWhenTheJournalIsDown(t *testing.T) {
	w := newWorld(t)
	owner, hr, guest, joiner := w.user("Иван"), w.user("Кадровик"), w.user("Гость"), w.user("Новичок")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	w.member(owner, org.Slug, hr, "hr", nil)
	pending, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: joiner.Email, Role: "hr"})
	if err != nil {
		t.Fatal(err)
	}
	token := tokenIn(t, w.lastMailTo(joiner.Email).Body)
	before := countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, org.ID)

	testaudit.Break(t, sharedPool)

	failing := []struct {
		name  string
		do    func() error
		state func() bool // состояние осталось прежним
	}{
		{"invite", func() error {
			_, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: guest.Email, Role: "hr"})
			return err
		}, func() bool {
			return countRows(t, `SELECT count(*) FROM org_invitations WHERE org_id = $1 AND email = $2`, org.ID, guest.Email) == 0
		}},
		{"revoke", func() error { return w.svc.RevokeInvitation(bg, owner.User, org.Slug, pending.ID) }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_invitations WHERE id = $1 AND revoked_at IS NULL`, pending.ID) == 1
		}},
		{"accept", func() error { _, err := w.svc.AcceptInvitation(bg, joiner.User, token); return err }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2`, org.ID, joiner.ID) == 0 &&
				countRows(t, `SELECT count(*) FROM org_invitations WHERE id = $1 AND accepted_at IS NULL`, pending.ID) == 1
		}},
		{"accept by id", func() error { _, err := w.svc.AcceptInvitationByID(bg, joiner.User, pending.ID); return err }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2`, org.ID, joiner.ID) == 0
		}},
		{"change role", func() error { return w.svc.ChangeRole(bg, fresh(owner.User), org.Slug, hr.ID, "unit_head") }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2 AND role = 'hr'`, org.ID, hr.ID) == 1
		}},
		{"set unit head", func() error { _, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, &hr.ID); return err }, func() bool {
			return countRows(t, `SELECT count(*) FROM units WHERE id = $1 AND head_user_id IS NULL`, unit.ID) == 1
		}},
		{"remove", func() error { return w.svc.RemoveMember(bg, owner.User, org.Slug, hr.ID) }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2`, org.ID, hr.ID) == 1
		}},
		{"leave", func() error { return w.svc.RemoveMember(bg, hr.User, org.Slug, hr.ID) }, func() bool {
			return countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = $2`, org.ID, hr.ID) == 1
		}},
	}
	for _, tc := range failing {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.do(); err == nil {
				t.Fatal("the action passed without a journal entry")
			}
			if !tc.state() {
				t.Fatal("the action changed the database although the journal refused it")
			}
		})
	}
	if after := countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, org.ID); after != before {
		t.Fatalf("%d entries appeared", after-before)
	}
}

// eraseUser стирает аккаунт единственного владельца так, как это делает фоновая уборка.
func (w *world) eraseUser(u person) error {
	w.t.Helper()
	tx, err := sharedPool.Begin(bg)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	q := dbgen.New(tx)
	row, err := q.LockUserRow(bg, u.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := erasure.Erase(bg, q, row, w.clock.Now()); err != nil {
		return err
	}
	return tx.Commit(bg)
}

// T-34: у организации без владельца нельзя принять приглашение сотрудником. Приглашение владельцем принять можно:
// оно возвращает организации хозяина.
func TestInvitationsToAnOrganizationWithoutOwner(t *testing.T) {
	w := newWorld(t)
	owner, hr, heir := w.user("Иван"), w.user("Кадровик"), w.user("Наследник")
	org := w.org(owner)
	if _, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: hr.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	hrToken := tokenIn(t, w.lastMailTo(hr.Email).Body)
	if _, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: heir.Email, Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	heirToken := tokenIn(t, w.lastMailTo(heir.Email).Body)

	if err := w.eraseUser(owner); err != nil {
		t.Fatalf("the only member must be erasable: %v", err)
	}
	if _, err := w.svc.AcceptInvitation(bg, hr.User, hrToken); !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("hr joining an ownerless organization: %v", err)
	}
	if countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1`, org.ID) != 0 {
		t.Fatal("the organization got a member although it has no owner")
	}
	if _, err := w.svc.AcceptInvitation(bg, heir.User, heirToken); err != nil {
		t.Fatalf("the owner invitation must still work: %v", err)
	}
	if countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1 AND role = 'owner'`, org.ID) != 1 {
		t.Fatal("the heir is not an owner")
	}
}

// T-34: приём приглашения и стирание аккаунта владельца идут по одной блокировке и не разминутся.
func TestAcceptingWaitsForTheErasureOfTheOwner(t *testing.T) {
	w := newWorld(t)
	owner, hr := w.user("Иван"), w.user("Кадровик")
	org := w.org(owner)
	if _, err := w.svc.Invite(bg, fresh(owner.User), org.Slug, InviteInput{Email: hr.Email, Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	token := tokenIn(t, w.lastMailTo(hr.Email).Body)

	// Стирание начато: организации владельца заблокированы, но ещё не завершено.
	tx, err := sharedPool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(bg) }()
	q := dbgen.New(tx)
	row, err := q.LockUserRow(bg, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.LockOrganizationsOwnedBy(bg, owner.ID); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := w.svc.AcceptInvitation(bg, hr.User, token)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the acceptance did not wait for the erasure: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := erasure.Erase(context.Background(), q, row, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(bg); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrInvalidInvitation) {
		t.Fatalf("acceptance after the erasure: %v", err)
	}
	if countRows(t, `SELECT count(*) FROM org_members WHERE org_id = $1`, org.ID) != 0 {
		t.Fatal("members without an owner")
	}
}

// Журнал читает только владелец своей организации; адрес в ответе не отдаётся.
func TestHTTPAuditLog(t *testing.T) {
	a := newAPI(t)
	owner, hr, head, other, outsider := a.user("Иван"), a.user("Кадровик"), a.user("Руководитель"), a.user("Владелец другой"), a.user("Посторонний")
	org := a.org(owner)
	unit := a.unit(owner, org.Slug)
	a.member(owner, org.Slug, hr, "hr", nil)
	a.member(owner, org.Slug, head, "unit_head", &unit.ID)
	otherOrg := a.org(other)
	path := "/api/organizations/" + org.Slug + "/audit"

	got := a.do(&owner, "GET", path, nil)
	if got.Code != 200 {
		t.Fatalf("owner: %d %s", got.Code, got.Raw)
	}
	items, _ := got.json(t)["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("items = %d: %s", len(items), got.Raw)
	}
	first, _ := items[0].(map[string]any)
	actor, _ := first["actor"].(map[string]any)
	if first["action"] != "member.joined" || actor["name"] != "Руководитель" {
		t.Errorf("newest entry = %v", first)
	}
	for _, banned := range []string{`"ip"`, hr.Email, head.Email, "127.0.0.1"} {
		if strings.Contains(got.Raw, banned) {
			t.Errorf("the response contains %q", banned)
		}
	}

	// Листание: по две записи, потом остаток.
	page1 := a.do(&owner, "GET", path+"?limit=2", nil).json(t)
	next, ok := page1["next"].(float64)
	if len(page1["items"].([]any)) != 2 || !ok {
		t.Fatalf("page 1 = %v", page1)
	}
	page2 := a.do(&owner, "GET", path+"?limit=2&before="+strconv.FormatInt(int64(next), 10), nil).json(t)
	if len(page2["items"].([]any)) != 2 || page2["next"] != nil {
		t.Fatalf("page 2 = %v", page2)
	}
	if bad := a.do(&owner, "GET", path+"?limit=abc&before=-7", nil); bad.Code != 200 || len(bad.json(t)["items"].([]any)) != 4 {
		t.Errorf("garbage parameters: %d %s", bad.Code, bad.Raw)
	}

	// Все остальные не видят журнал: организация публична, поэтому отказ, а не «нет такой».
	for _, tc := range []struct {
		name string
		who  *person
		path string
		code int
	}{
		{"guest", nil, path, 401},
		{"hr", &hr, path, 403},
		{"unit head", &head, path, 403},
		{"owner of another organization", &other, path, 403},
		{"outsider", &outsider, path, 403},
		{"unknown organization", &owner, "/api/organizations/no-such-org/audit", 404},
		{"owner reading another organization", &owner, "/api/organizations/" + otherOrg.Slug + "/audit", 403},
	} {
		if r := a.do(tc.who, "GET", tc.path, nil); r.Code != tc.code || strings.Contains(r.Raw, "member.joined") {
			t.Errorf("%s: %d %s", tc.name, r.Code, r.Raw)
		}
	}
}

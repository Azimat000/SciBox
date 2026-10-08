package erasure_test

import (
	"slices"
	"testing"

	"scibox/server/internal/audit"
	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/orgs"
	"scibox/server/internal/testaudit"
	"scibox/server/internal/testkit"
)

// Журнал — история организации, поэтому записи остаются, но адрес человека из них стирается вместе с аккаунтом;
// имя берётся из аккаунта при показе и у стёртого аккаунта становится «Аккаунт удалён».
func TestErasureKeepsTheJournalButDropsAddresses(t *testing.T) {
	w := newWorld(t)
	hr := w.User("Кадровик Иванова")
	ctx := audit.WithSource(bg, "203.0.113.40")
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: w.tm.OrgID, UserID: hr.ID, Role: "hr", JoinedAt: hr.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	// Действие сотрудника в организации и личные события аккаунта, все с адресом.
	if err := w.Orgs.ChangeRole(ctx, auth.Principal{User: hr.User}, w.tm.Slug, w.tm.HeadA.ID, "unit_head"); err == nil {
		t.Fatal("hr must not change roles")
	}
	if err := w.Orgs.RemoveMember(ctx, hr.User, w.tm.Slug, hr.ID); err != nil {
		t.Fatal(err)
	}
	p := fresh(t, w.World, hr.Email)
	if _, err := w.Auth.RequestDeletion(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Запись другого человека о том же сотруднике (его роль меняли) остаётся с его адресом.
	if _, err := w.Orgs.Invite(ctx, auth.Principal{User: w.tm.Owner.User, Fresh: true}, w.tm.Slug, orgs.InviteInput{Email: "future@example.ru", Role: "hr"}); err != nil {
		t.Fatal(err)
	}
	mine := testaudit.Rows(t, testkit.Pool, `actor_id = $1`, hr.ID)
	if len(mine) < 3 || !slices.ContainsFunc(mine, func(r testaudit.Row) bool { return r.IP != nil && *r.IP == "203.0.113.40" }) {
		t.Fatalf("before the erasure: %v", testaudit.Actions(mine))
	}
	others := len(testaudit.Rows(t, testkit.Pool, `actor_id = $1 AND ip IS NOT NULL`, w.tm.Owner.ID))
	if others == 0 {
		t.Fatal("the owner's entry has no address")
	}

	exec(t, `UPDATE users SET deletion_due_at = now() - interval '1 minute' WHERE id = $1`, hr.ID)
	if n, err := w.Auth.ProcessDueDeletions(bg); err != nil || n != 1 {
		t.Fatalf("erasure: %d %v", n, err)
	}

	after := testaudit.Rows(t, testkit.Pool, `actor_id = $1`, hr.ID)
	if len(after) != len(mine)+1 || after[len(after)-1].Action != "account.erased" {
		t.Fatalf("after the erasure: %v (the entries must stay and one is added)", testaudit.Actions(after))
	}
	for i, r := range after {
		if r.IP != nil {
			t.Errorf("entry %d (%s) keeps the address %s", i, r.Action, *r.IP)
		}
		if i < len(mine) && (r.Action != mine[i].Action || r.Details["role"] != mine[i].Details["role"] || r.ID != mine[i].ID) {
			t.Errorf("entry %d changed: %+v → %+v", i, mine[i], r)
		}
	}
	if kept := len(testaudit.Rows(t, testkit.Pool, `actor_id = $1 AND ip IS NOT NULL`, w.tm.Owner.ID)); kept != others {
		t.Errorf("another person's addresses: %d, was %d", kept, others)
	}
	// Владелец видит запись сотрудника под именем «Аккаунт удалён».
	page, err := w.Orgs.Audit(bg, w.tm.Owner.User, w.tm.Slug, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, e := range page.Items {
		if e.Actor != nil && e.Actor.ID == hr.ID {
			named = true
			if e.Actor.Name != "Аккаунт удалён" {
				t.Errorf("the erased person is shown as %q", e.Actor.Name)
			}
		}
	}
	if !named {
		t.Error("the journal of the organization lost the entries of the erased person")
	}
}

// Удаление и его отмена записываются в личный журнал; остановка из-за организации — тоже.
func TestAuditTrailOfDeletion(t *testing.T) {
	w := newWorld(t)
	who := w.User("Передумавший")
	p := fresh(t, w.World, who.Email)
	if _, err := w.Auth.RequestDeletion(bg, p); err != nil {
		t.Fatal(err)
	}
	if err := w.Auth.CancelDeletion(bg, fresh(t, w.World, who.Email)); err != nil {
		t.Fatal(err)
	}
	if err := w.Auth.CancelDeletion(bg, fresh(t, w.World, who.Email)); err != nil { // отменять нечего: записи нет
		t.Fatal(err)
	}
	got := testaudit.Actions(testaudit.Account(t, testkit.Pool, who.ID))
	var core []string
	for _, a := range got {
		if a != "account.login" {
			core = append(core, a)
		}
	}
	if !slices.Equal(core, []string{"account.deletion_requested", "account.deletion_canceled"}) {
		t.Fatalf("journal = %v", got)
	}
}

package vacancies

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/audit"
	"scibox/server/internal/testaudit"
)

// Каждая допустимая смена статуса имеет своё действие в журнале: добавить новую пару в таблицу и не решить,
// как она выглядит в журнале, нельзя.
func TestEveryTransitionHasAnAuditAction(t *testing.T) {
	seen := map[audit.Action]bool{}
	for from, nexts := range transitions {
		for _, to := range nexts {
			a, ok := auditAction[[2]string{from, to}]
			if !ok || !audit.OrgScoped(a) {
				t.Errorf("%s → %s has no journal action", from, to)
			}
			if seen[a] {
				t.Errorf("%s is used by two transitions", a)
			}
			seen[a] = true
		}
	}
	if len(auditAction) != len(seen) {
		t.Errorf("the journal table has %d actions for %d transitions", len(auditAction), len(seen))
	}
}

func TestAuditTrailOfStatusChanges(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	ctx := audit.WithSource(bg, "198.51.100.4")
	d := w.create(tm, tm.headA, withUnit(goodInput(), tm.unitA.ID))
	for _, step := range []struct {
		who person
		to  string
	}{
		{tm.headA, StatusPublished}, {tm.hr, StatusClosed}, {tm.owner, StatusPublished}, {tm.owner, StatusClosed},
	} {
		if _, err := w.svc.SetStatus(ctx, step.who.User, d.ID, step.to); err != nil {
			t.Fatalf("%s: %v", step.to, err)
		}
	}
	// team() уже назначил руководителей подразделений, эти записи здесь не нужны.
	rows := testaudit.Rows(t, sharedPool, `org_id = $1 AND action LIKE 'vacancy.%'`, tm.orgID)
	want := []string{"vacancy.published", "vacancy.closed", "vacancy.reopened", "vacancy.closed"}
	if got := testaudit.Actions(rows); !slices.Equal(got, want) {
		t.Fatalf("journal = %v, want %v", got, want)
	}
	wantActor := []person{tm.headA, tm.hr, tm.owner, tm.owner}
	for i, r := range rows {
		if *r.Actor != wantActor[i].ID || r.TargetKind != "vacancy" || *r.TargetID != d.ID || r.TargetLabel != d.Title || r.Subject != nil {
			t.Errorf("entry %d: %+v", i, r)
		}
		if r.IP == nil || *r.IP != "198.51.100.4" {
			t.Errorf("entry %d ip: %v", i, r.IP)
		}
	}
	if rows[2].Details["from"] != "closed" || rows[2].Details["to"] != "published" {
		t.Errorf("details = %v", rows[2].Details)
	}
}

func TestRefusedStatusChangesLeaveNoTrace(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	draft := w.create(tm, tm.owner, goodInput())
	pub := w.published(tm, goodInput())
	before := len(testaudit.Org(t, sharedPool, tm.orgID))
	for name, do := range map[string]func() error{
		"outsider publishes a draft":    func() error { _, err := w.svc.SetStatus(bg, tm.out.User, draft.ID, StatusPublished); return err },
		"head of another unit closes":   func() error { _, err := w.svc.SetStatus(bg, tm.headB.User, pub.ID, StatusClosed); return err },
		"impossible transition":         func() error { _, err := w.svc.SetStatus(bg, tm.owner.User, draft.ID, StatusClosed); return err },
		"unknown status":                func() error { _, err := w.svc.SetStatus(bg, tm.owner.User, draft.ID, "exploded"); return err },
		"incomplete vacancy is refused": func() error { return errors.New("replaced below") },
		"outsider closes a published":   func() error { _, err := w.svc.SetStatus(bg, tm.out.User, pub.ID, StatusClosed); return err },
		"publishing an unknown vacancy": func() error { _, err := w.svc.SetStatus(bg, tm.owner.User, uuid.UUID{1}, StatusPublished); return err },
	} {
		if name == "incomplete vacancy is refused" {
			in := goodInput()
			in.Summary = ""
			bare := w.create(tm, tm.owner, in)
			do = func() error { _, err := w.svc.SetStatus(bg, tm.owner.User, bare.ID, StatusPublished); return err }
		}
		if err := do(); err == nil {
			t.Errorf("%s: not refused", name)
		}
	}
	if after := len(testaudit.Org(t, sharedPool, tm.orgID)); after != before {
		t.Fatalf("refused changes wrote %d entries", after-before)
	}
}

// Нет записи — нет смены статуса.
func TestStatusChangeFailsWhenTheJournalIsDown(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	d := w.create(tm, tm.owner, goodInput())
	testaudit.Break(t, sharedPool)
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished); err == nil {
		t.Fatal("the vacancy was published without a journal entry")
	}
	if n := countRows(t, `SELECT count(*) FROM vacancies WHERE id = $1 AND status = 'draft'`, d.ID); n != 1 {
		t.Fatal("the status changed although the journal refused")
	}
}

func withUnit(in Input, unit uuid.UUID) Input {
	in.UnitID = &unit
	return in
}

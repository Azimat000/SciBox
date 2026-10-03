package orgs

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/access"
	"scibox/server/internal/dbgen"
)

// Кому сообщать об откликах: владелец и кадровики всегда, руководитель только своего подразделения, посторонние никогда.
func TestUsersWhoCanViewApplications(t *testing.T) {
	p := newPermWorld(t)
	// Руководителя подразделения нужно назначить подразделению: сотрудник с ролью руководителя без подразделения
	// видит отклики только на вакансии «своего» подразделения.
	if _, err := p.w.svc.SetUnitHead(bg, p.owner.User, p.slug, p.unitA.ID, &p.headA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.w.svc.SetUnitHead(bg, p.owner.User, p.slug, p.unitB.ID, &p.headB.ID); err != nil {
		t.Fatal(err)
	}
	org, _ := p.w.svc.GetOrganization(bg, p.slug, nil)
	q := dbgen.New(sharedPool)

	ids := func(unit uuid.UUID) map[uuid.UUID]bool {
		t.Helper()
		got, err := UsersWhoCan(bg, q, org.Organization.ID, access.ViewApplications, unit)
		if err != nil {
			t.Fatal(err)
		}
		m := map[uuid.UUID]bool{}
		for _, id := range got {
			m[id] = true
		}
		return m
	}
	cases := []struct {
		name string
		unit uuid.UUID
		want []person
		not  []person
	}{
		{"unit A", p.unitA.ID, []person{p.owner, p.hr, p.colleague, p.headA}, []person{p.headB, p.stranger}},
		{"unit B", p.unitB.ID, []person{p.owner, p.hr, p.colleague, p.headB}, []person{p.headA, p.stranger}},
		{"whole organization", access.NoUnit, []person{p.owner, p.hr, p.colleague}, []person{p.headA, p.headB, p.stranger}},
	}
	for _, c := range cases {
		got := ids(c.unit)
		for _, w := range c.want {
			if !got[w.ID] {
				t.Errorf("%s: %s must be told", c.name, w.Name)
			}
		}
		for _, n := range c.not {
			if got[n.ID] {
				t.Errorf("%s: %s must not be told", c.name, n.Name)
			}
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: %d people, want %d", c.name, len(got), len(c.want))
		}
	}
}

func TestUsersWhoCanReportsDatabaseFailures(t *testing.T) {
	p := newPermWorld(t)
	if _, err := p.w.svc.SetUnitHead(bg, p.owner.User, p.slug, p.unitA.ID, &p.headA.ID); err != nil {
		t.Fatal(err)
	}
	org, _ := p.w.svc.GetOrganization(bg, p.slug, nil)
	for n := int32(1); n <= 40; n++ {
		calls := &atomic.Int32{}
		db := &faultDB{DB: sharedPool, calls: calls, failAt: n}
		_, err := UsersWhoCan(bg, dbgen.New(db), org.Organization.ID, access.ViewApplications, p.unitA.ID)
		if calls.Load() < n {
			if err != nil {
				t.Fatalf("fault %d did not happen but err = %v", n, err)
			}
			return
		}
		if !errors.Is(err, errFault) {
			t.Fatalf("a database failure at call #%d was lost: err = %v", n, err)
		}
	}
	t.Fatal("too many database calls")
}

package access

import (
	"testing"

	"github.com/google/uuid"
)

var (
	ownUnit   = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	otherUnit = uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
)

func TestParseRole(t *testing.T) {
	for _, r := range Roles {
		got, ok := ParseRole(string(r))
		if !ok || got != r {
			t.Errorf("ParseRole(%q) = %q, %v", r, got, ok)
		}
	}
	for _, bad := range []string{"", "admin", "Owner", "OWNER", " owner", "unit-head", "member"} {
		if got, ok := ParseRole(bad); ok || got != "" {
			t.Errorf("ParseRole(%q) = %q, %v; want no role", bad, got, ok)
		}
	}
}

// Каждая пара «кто → что → в каком подразделении» проверена явно. Если права меняются, этот список меняется вместе с ними.
func TestCan(t *testing.T) {
	owner := Actor{Role: RoleOwner}
	hr := Actor{Role: RoleHR}
	head := Actor{Role: RoleUnitHead, HeadOf: []uuid.UUID{ownUnit}}
	headOfNothing := Actor{Role: RoleUnitHead}
	// Руководителю подразделений в базе не положено быть не руководителем, но кадровик с «чужими» подразделениями
	// в списке не должен получить от этого лишних прав: список учитывается только для роли руководителя.
	hrWithUnits := Actor{Role: RoleHR, HeadOf: []uuid.UUID{ownUnit}}
	stranger := Actor{}
	unknownRole := Actor{Role: "admin", HeadOf: []uuid.UUID{ownUnit}}

	type row struct {
		who  string
		a    Actor
		perm Permission
		unit uuid.UUID
		want bool
	}
	rows := []row{
		// Права на всю организацию: только владелец, в каком бы подразделении ни спрашивали.
		{"owner", owner, EditOrganization, NoUnit, true},
		{"owner", owner, ManageMembers, NoUnit, true},
		{"owner", owner, ManageUnits, NoUnit, true},
		{"owner", owner, ManageUnits, otherUnit, true},
		{"hr", hr, EditOrganization, NoUnit, false},
		{"hr", hr, ManageMembers, NoUnit, false},
		{"hr", hr, ManageUnits, NoUnit, false},
		{"hr with units", hrWithUnits, ManageUnits, ownUnit, false},
		{"head", head, EditOrganization, NoUnit, false},
		{"head", head, ManageMembers, NoUnit, false},
		{"head", head, ManageUnits, ownUnit, false},
		{"head of nothing", headOfNothing, ManageUnits, NoUnit, false},
		{"stranger", stranger, EditOrganization, NoUnit, false},
		{"stranger", stranger, ManageMembers, NoUnit, false},
		{"stranger", stranger, ManageUnits, ownUnit, false},
		{"unknown role", unknownRole, EditOrganization, NoUnit, false},
		{"unknown role", unknownRole, ManageMembers, NoUnit, false},
		{"unknown role", unknownRole, ManageUnits, ownUnit, false},

		// Изменить подразделение: владелец везде, руководитель только своё, кадровик никак.
		{"owner", owner, EditUnit, ownUnit, true},
		{"owner", owner, EditUnit, otherUnit, true},
		{"hr", hr, EditUnit, ownUnit, false},
		{"hr with units", hrWithUnits, EditUnit, ownUnit, false},
		{"head", head, EditUnit, ownUnit, true},
		{"head", head, EditUnit, otherUnit, false},
		{"head", head, EditUnit, NoUnit, false},
		{"head of nothing", headOfNothing, EditUnit, ownUnit, false},
		{"stranger", stranger, EditUnit, ownUnit, false},
		{"unknown role", unknownRole, EditUnit, ownUnit, false},

		// Вакансии и отклики: владелец и кадровик везде (и без подразделения), руководитель только в своём.
		{"owner", owner, ManageVacancies, ownUnit, true},
		{"owner", owner, ManageVacancies, NoUnit, true},
		{"owner", owner, ViewApplications, otherUnit, true},
		{"owner", owner, ViewApplications, NoUnit, true},
		{"hr", hr, ManageVacancies, ownUnit, true},
		{"hr", hr, ManageVacancies, otherUnit, true},
		{"hr", hr, ManageVacancies, NoUnit, true},
		{"hr", hr, ViewApplications, otherUnit, true},
		{"hr", hr, ViewApplications, NoUnit, true},
		{"head", head, ManageVacancies, ownUnit, true},
		{"head", head, ManageVacancies, otherUnit, false},
		{"head", head, ManageVacancies, NoUnit, false},
		{"head", head, ViewApplications, ownUnit, true},
		{"head", head, ViewApplications, otherUnit, false},
		{"head", head, ViewApplications, NoUnit, false},
		{"head of nothing", headOfNothing, ManageVacancies, ownUnit, false},
		{"head of nothing", headOfNothing, ViewApplications, NoUnit, false},
		{"stranger", stranger, ManageVacancies, ownUnit, false},
		{"stranger", stranger, ManageVacancies, NoUnit, false},
		{"stranger", stranger, ViewApplications, ownUnit, false},
		{"unknown role", unknownRole, ManageVacancies, ownUnit, false},
		{"unknown role", unknownRole, ViewApplications, ownUnit, false},

		// Несуществующее действие запрещено всем.
		{"owner", owner, Permission(0), ownUnit, false},
		{"owner", owner, Permission(99), ownUnit, false},
		{"hr", hr, Permission(0), NoUnit, false},
	}
	for _, r := range rows {
		if got := r.a.Can(r.perm, r.unit); got != r.want {
			t.Errorf("%s: Can(%d, unit %v) = %v, want %v", r.who, r.perm, r.unit, got, r.want)
		}
	}
}

// Руководитель нескольких подразделений: права на каждое из них, но не на соседнее.
func TestHeadOfSeveralUnits(t *testing.T) {
	third := uuid.MustParse("00000000-0000-0000-0000-0000000000c3")
	a := Actor{Role: RoleUnitHead, HeadOf: []uuid.UUID{ownUnit, third}}
	for _, u := range []uuid.UUID{ownUnit, third} {
		if !a.Can(ManageVacancies, u) || !a.Can(EditUnit, u) || !a.Can(ViewApplications, u) {
			t.Errorf("head must manage own unit %v", u)
		}
	}
	if a.Can(ManageVacancies, otherUnit) {
		t.Error("head must not manage someone else's unit")
	}
}

func TestIsMember(t *testing.T) {
	for _, r := range Roles {
		if !(Actor{Role: r}).IsMember() {
			t.Errorf("role %q must count as a member", r)
		}
	}
	for _, r := range []Role{"", "admin"} {
		if (Actor{Role: r}).IsMember() {
			t.Errorf("role %q must not count as a member", r)
		}
	}
}

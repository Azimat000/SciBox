package orgs

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// permWorld — организация со всеми ролями: владелец, кадровик, руководитель подразделения A, руководитель подразделения B,
// посторонний (вошёл, но не сотрудник) и рядовой коллега, над которым проверяются действия владельца.
type permWorld struct {
	w                                            *world
	slug                                         string
	unitA, unitB                                 Unit
	owner, hr, headA, headB, stranger, colleague person
	pendingInvitation                            uuid.UUID
}

func newPermWorld(t *testing.T) *permWorld {
	t.Helper()
	w := newWorld(t)
	p := &permWorld{w: w}
	p.owner = w.user("Владелец")
	p.hr = w.user("Кадровик")
	p.headA = w.user("Руководитель А")
	p.headB = w.user("Руководитель Б")
	p.stranger = w.user("Посторонний")
	p.colleague = w.user("Коллега")
	org := w.org(p.owner)
	p.slug = org.Slug
	p.unitA = w.unit(p.owner, p.slug)
	p.unitB = w.unit(p.owner, p.slug)
	w.member(p.owner, p.slug, p.hr, "hr", nil)
	w.member(p.owner, p.slug, p.headA, "unit_head", &p.unitA.ID)
	w.member(p.owner, p.slug, p.headB, "unit_head", &p.unitB.ID)
	w.member(p.owner, p.slug, p.colleague, "hr", nil)
	inv, err := w.svc.Invite(bg, p.owner.User, p.slug, InviteInput{Email: "pending@example.ru", Role: "hr"})
	if err != nil {
		t.Fatal(err)
	}
	p.pendingInvitation = inv.ID
	return p
}

// Каждая пара «кто → что» проверена явно: можно или нельзя. Это таблица прав на уровне сервиса (пакет access проверен отдельно).
func TestWhoCanDoWhat(t *testing.T) {
	type op struct {
		name string
		run  func(p *permWorld, who person) error
		// allowed — кому действие разрешено; всем остальным (включая постороннего) отказ ErrForbidden.
		allowed func(p *permWorld) []person
	}
	ownerOnly := func(p *permWorld) []person { return []person{p.owner} }
	ops := []op{
		{"edit organization", func(p *permWorld, who person) error {
			_, err := p.w.svc.UpdateOrganization(bg, who.User, p.slug, orgInput())
			return err
		}, ownerOnly},
		{"create a unit", func(p *permWorld, who person) error {
			_, err := p.w.svc.CreateUnit(bg, who.User, p.slug, unitInput())
			return err
		}, ownerOnly},
		{"edit unit A", func(p *permWorld, who person) error {
			_, err := p.w.svc.UpdateUnit(bg, who.User, p.slug, p.unitA.ID, unitInput())
			return err
		}, func(p *permWorld) []person { return []person{p.owner, p.headA} }},
		{"edit unit B", func(p *permWorld, who person) error {
			_, err := p.w.svc.UpdateUnit(bg, who.User, p.slug, p.unitB.ID, unitInput())
			return err
		}, func(p *permWorld) []person { return []person{p.owner, p.headB} }},
		{"assign a unit head", func(p *permWorld, who person) error {
			_, err := p.w.svc.SetUnitHead(bg, who.User, p.slug, p.unitA.ID, &p.colleague.ID)
			return err
		}, ownerOnly},
		{"delete a unit", func(p *permWorld, who person) error {
			u, err := p.w.svc.CreateUnit(bg, p.owner.User, p.slug, unitInput())
			if err != nil {
				return err
			}
			return p.w.svc.DeleteUnit(bg, who.User, p.slug, u.ID)
		}, ownerOnly},
		{"see members and their emails", func(p *permWorld, who person) error {
			_, err := p.w.svc.Members(bg, who.User, p.slug)
			return err
		}, ownerOnly},
		{"change a role", func(p *permWorld, who person) error {
			return p.w.svc.ChangeRole(bg, who.User, p.slug, p.colleague.ID, "unit_head")
		}, ownerOnly},
		{"remove a colleague", func(p *permWorld, who person) error {
			target := p.w.user("Жертва")
			p.w.member(p.owner, p.slug, target, "hr", nil)
			return p.w.svc.RemoveMember(bg, who.User, p.slug, target.ID)
		}, ownerOnly},
		{"invite", func(p *permWorld, who person) error {
			_, err := p.w.svc.Invite(bg, who.User, p.slug, InviteInput{Email: uniqueEmail(), Role: "hr"})
			return err
		}, ownerOnly},
		{"revoke an invitation", func(p *permWorld, who person) error {
			inv, err := p.w.svc.Invite(bg, p.owner.User, p.slug, InviteInput{Email: uniqueEmail(), Role: "hr"})
			if err != nil {
				return err
			}
			return p.w.svc.RevokeInvitation(bg, who.User, p.slug, inv.ID)
		}, ownerOnly},
	}
	for _, o := range ops {
		t.Run(o.name, func(t *testing.T) {
			p := newPermWorld(t)
			allowed := map[uuid.UUID]bool{}
			for _, a := range o.allowed(p) {
				allowed[a.ID] = true
			}
			for _, who := range []person{p.owner, p.hr, p.headA, p.headB, p.stranger} {
				err := o.run(p, who)
				switch {
				case allowed[who.ID] && err != nil:
					t.Errorf("%s must be allowed, got %v", who.Name, err)
				case !allowed[who.ID] && !errors.Is(err, ErrForbidden):
					t.Errorf("%s must be refused with ErrForbidden, got %v", who.Name, err)
				}
			}
		})
	}
}

// Отказ не оставляет следов: ни данных, ни писем.
func TestForbiddenActionsChangeNothing(t *testing.T) {
	p := newPermWorld(t)
	w := p.w
	for _, who := range []person{p.hr, p.headA, p.stranger} {
		_, _ = w.svc.UpdateOrganization(bg, who.User, p.slug, OrgInput{Name: "Захвачено", Kind: KindOther, City: "Москва"})
		_, _ = w.svc.CreateUnit(bg, who.User, p.slug, unitInput())
		_, _ = w.svc.Invite(bg, who.User, p.slug, InviteInput{Email: "intruder@example.ru", Role: "owner"})
		_ = w.svc.ChangeRole(bg, who.User, p.slug, p.owner.ID, "hr")
		_ = w.svc.RemoveMember(bg, who.User, p.slug, p.owner.ID)
		_ = w.svc.DeleteUnit(bg, who.User, p.slug, p.unitB.ID)
	}
	page, _ := w.svc.GetOrganization(bg, p.slug, nil)
	if page.Organization.Name == "Захвачено" || len(page.Units) != 2 {
		t.Errorf("a refused action changed data: %+v", page)
	}
	if got := roleOf(t, w, p.slug, p.owner, p.owner); got != "owner" {
		t.Errorf("owner's role = %q", got)
	}
	if n := len(w.mailsTo("intruder@example.ru")); n != 0 {
		t.Errorf("a refused invitation sent %d mails", n)
	}
	v, _ := w.svc.Members(bg, p.owner.User, p.slug)
	for _, i := range v.Invitations {
		if i.Email == "intruder@example.ru" {
			t.Error("a refused invitation was stored")
		}
	}
}

// Руководитель теряет права на подразделение, как только его сменили, и сразу получает их на новое.
func TestUnitHeadRightsFollowTheAssignment(t *testing.T) {
	p := newPermWorld(t)
	w := p.w
	if _, err := w.svc.UpdateUnit(bg, p.headA.User, p.slug, p.unitA.ID, unitInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.SetUnitHead(bg, p.owner.User, p.slug, p.unitA.ID, &p.headB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.UpdateUnit(bg, p.headA.User, p.slug, p.unitA.ID, unitInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("the replaced head must lose rights: %v", err)
	}
	if _, err := w.svc.UpdateUnit(bg, p.headB.User, p.slug, p.unitA.ID, unitInput()); err != nil {
		t.Errorf("the new head must gain rights: %v", err)
	}
	if _, err := w.svc.UpdateUnit(bg, p.headB.User, p.slug, p.unitB.ID, unitInput()); err != nil {
		t.Errorf("headB keeps unit B: %v", err)
	}
}

// Право руководить подразделением даёт роль unit_head вместе с назначением; кадровика назначение не расширяет.
func TestHrAssignedAsHeadStaysHr(t *testing.T) {
	p := newPermWorld(t)
	w := p.w
	if _, err := w.svc.SetUnitHead(bg, p.owner.User, p.slug, p.unitA.ID, &p.hr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.UpdateUnit(bg, p.hr.User, p.slug, p.unitA.ID, unitInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("hr is not a unit head by role: %v", err)
	}
	// Организация в другом slug: у руководителя из этой организации там нет прав.
	other := w.org(p.owner)
	if _, err := w.svc.UpdateUnit(bg, p.headA.User, other.Slug, p.unitA.ID, unitInput()); !errors.Is(err, ErrForbidden) {
		t.Errorf("rights do not cross organizations: %v", err)
	}
}

package orgs

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func roleOf(t *testing.T, w *world, slug string, owner person, who person) string {
	t.Helper()
	v, err := w.svc.Members(bg, owner.User, slug)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range v.Members {
		if m.UserID == who.ID {
			return m.Role
		}
	}
	return ""
}

func TestMembersList(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван Петров")
	org := w.org(owner)
	hr := w.user("Анна Кадрова")
	head := w.user("Борис Лабов")
	w.member(owner, org.Slug, head, "unit_head", nil)
	w.member(owner, org.Slug, hr, "hr", nil)
	if _, err := w.svc.Invite(bg, owner.User, org.Slug, InviteInput{Email: "future@example.ru", Role: "hr"}); err != nil {
		t.Fatal(err)
	}

	v, err := w.svc.Members(bg, owner.User, org.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Members) != 3 || v.Members[0].Role != "owner" || v.Members[1].Role != "hr" || v.Members[2].Role != "unit_head" {
		t.Fatalf("members must go owner, hr, unit_head: %+v", v.Members)
	}
	if v.Members[0].Email != owner.Email || v.Members[0].Name != "Иван Петров" {
		t.Errorf("owner row: %+v", v.Members[0])
	}
	if len(v.Invitations) != 1 || v.Invitations[0].Email != "future@example.ru" {
		t.Errorf("pending invitations: %+v", v.Invitations)
	}

	// Кадровик и руководитель список с почтами не видят.
	for _, who := range []person{hr, head} {
		if _, err := w.svc.Members(bg, who.User, org.Slug); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: %v, want ErrForbidden", who.Name, err)
		}
	}
	if _, err := w.svc.Members(bg, owner.User, "no-such-org"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing organization: %v", err)
	}
}

func TestChangeRole(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	other := w.user("Анна")
	w.member(owner, org.Slug, other, "hr", nil)

	if err := w.svc.ChangeRole(bg, owner.User, org.Slug, other.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if got := roleOf(t, w, org.Slug, owner, other); got != "owner" {
		t.Fatalf("role = %q, want owner", got)
	}
	// Теперь владельцев двое: первого можно понизить.
	if err := w.svc.ChangeRole(bg, other.User, org.Slug, owner.ID, "hr"); err != nil {
		t.Fatal(err)
	}
	if got := roleOf(t, w, org.Slug, other, owner); got != "hr" {
		t.Fatalf("role = %q, want hr", got)
	}
	// Остался один владелец (Анна): понизить себя она не может.
	if err := w.svc.ChangeRole(bg, other.User, org.Slug, other.ID, "unit_head"); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
	if got := roleOf(t, w, org.Slug, other, other); got != "owner" {
		t.Errorf("the last owner must stay an owner, got %q", got)
	}
	// Подтвердить владельца владельцем можно.
	if err := w.svc.ChangeRole(bg, other.User, org.Slug, other.ID, "owner"); err != nil {
		t.Errorf("owner -> owner: %v", err)
	}

	if err := w.svc.ChangeRole(bg, other.User, org.Slug, owner.ID, "boss"); fieldsOf(t, err)["role"] == "" {
		t.Errorf("unknown role: %v", err)
	}
	if err := w.svc.ChangeRole(bg, other.User, org.Slug, uuid.New(), "hr"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown person: %v", err)
	}
	// Не владелец роли не меняет.
	if err := w.svc.ChangeRole(bg, owner.User, org.Slug, other.ID, "hr"); !errors.Is(err, ErrForbidden) {
		t.Errorf("hr changing roles: %v", err)
	}
	if err := w.svc.ChangeRole(bg, other.User, "no-such-org", owner.ID, "hr"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing organization: %v", err)
	}
}

func TestRemoveMember(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	head := w.user("Руководитель")
	w.member(owner, org.Slug, head, "unit_head", &unit.ID)
	hr := w.user("Кадровик")
	w.member(owner, org.Slug, hr, "hr", nil)

	// Кадровик и руководитель других не убирают.
	if err := w.svc.RemoveMember(bg, hr.User, org.Slug, head.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("hr removing: %v", err)
	}
	if err := w.svc.RemoveMember(bg, head.User, org.Slug, hr.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("head removing: %v", err)
	}
	// Посторонний не убирает и не «уходит».
	stranger := w.user("Чужой")
	if err := w.svc.RemoveMember(bg, stranger.User, org.Slug, owner.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger removing: %v", err)
	}
	if err := w.svc.RemoveMember(bg, stranger.User, org.Slug, stranger.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger leaving: %v", err)
	}

	// Владелец убирает руководителя: подразделение остаётся без руководителя.
	if err := w.svc.RemoveMember(bg, owner.User, org.Slug, head.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := w.svc.GetUnit(bg, org.Slug, unit.ID, nil); v.Unit.HeadName != nil {
		t.Errorf("the removed head must stop heading the unit: %+v", v.Unit)
	}
	if mine, _ := w.svc.MyOrganizations(bg, head.User); len(mine) != 0 {
		t.Errorf("the removed person still has organizations: %+v", mine)
	}
	if err := w.svc.RemoveMember(bg, owner.User, org.Slug, head.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}

	// Кадровик уходит сам.
	if err := w.svc.RemoveMember(bg, hr.User, org.Slug, hr.ID); err != nil {
		t.Errorf("leaving: %v", err)
	}

	// Последний владелец уйти не может ни сам, ни чужими руками; со вторым владельцем может.
	if err := w.svc.RemoveMember(bg, owner.User, org.Slug, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Errorf("the last owner leaving: %v", err)
	}
	second := w.user("Второй владелец")
	w.member(owner, org.Slug, second, "owner", nil)
	if err := w.svc.RemoveMember(bg, second.User, org.Slug, owner.ID); err != nil {
		t.Errorf("removing an owner when another exists: %v", err)
	}
	if err := w.svc.RemoveMember(bg, second.User, org.Slug, second.ID); !errors.Is(err, ErrLastOwner) {
		t.Errorf("now the second is the last: %v", err)
	}
	if err := w.svc.RemoveMember(bg, second.User, "no-such-org", second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing organization: %v", err)
	}
}

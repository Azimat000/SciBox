package orgs

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestUnitLifecycle(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)

	in := UnitInput{Name: "  Лаборатория   квантовой оптики ", Kind: UnitLaboratory, Description: "Описание", Topics: []string{"Фотоны", "фотоны", "Кубиты"}}
	unit, err := w.svc.CreateUnit(bg, owner.User, org.Slug, in)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Name != "Лаборатория квантовой оптики" || len(unit.Topics) != 2 || unit.HeadName != nil {
		t.Fatalf("created unit: %+v", unit)
	}
	empty, err := w.svc.CreateUnit(bg, owner.User, org.Slug, UnitInput{Name: "Кафедра", Kind: UnitDepartment})
	if err != nil || empty.Topics == nil || len(empty.Topics) != 0 {
		t.Fatalf("topics of a new unit must be an empty list: %+v, %v", empty, err)
	}

	updated, err := w.svc.UpdateUnit(bg, owner.User, org.Slug, unit.ID, UnitInput{Name: "Лаборатория фотоники", Kind: UnitDivision, Topics: []string{"Свет"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != unit.ID || updated.Name != "Лаборатория фотоники" || updated.Kind != UnitDivision || updated.Description != "" || len(updated.Topics) != 1 {
		t.Fatalf("updated unit: %+v", updated)
	}
	if _, err := w.svc.UpdateUnit(bg, owner.User, org.Slug, unit.ID, UnitInput{}); fieldsOf(t, err)["name"] == "" {
		t.Error("invalid input must be rejected")
	}
	if _, err := w.svc.UpdateUnit(bg, owner.User, org.Slug, uuid.New(), unitInput()); !errors.Is(err, ErrNotFound) {
		t.Errorf("a unit that does not exist: %v", err)
	}
	if _, err := w.svc.CreateUnit(bg, owner.User, org.Slug, UnitInput{}); fieldsOf(t, err)["name"] == "" {
		t.Error("create: invalid input must be rejected")
	}
	if _, err := w.svc.CreateUnit(bg, owner.User, "no-such-org", unitInput()); !errors.Is(err, ErrNotFound) {
		t.Errorf("create in a missing organization: %v", err)
	}

	if err := w.svc.DeleteUnit(bg, owner.User, org.Slug, unit.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.DeleteUnit(bg, owner.User, org.Slug, unit.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
	page, _ := w.svc.GetOrganization(bg, org.Slug, nil)
	if len(page.Units) != 1 || page.Units[0].ID != empty.ID {
		t.Errorf("units after delete: %+v", page.Units)
	}
}

func TestGetUnit(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	head := w.user("Анна Смирнова")
	w.member(owner, org.Slug, head, "hr", nil)
	if _, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, &head.ID); err != nil {
		t.Fatal(err)
	}

	v, err := w.svc.GetUnit(bg, org.Slug, unit.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Organization.Slug != org.Slug || v.Organization.Name != org.Name || v.Organization.City != org.City || v.Organization.Kind != org.Kind {
		t.Errorf("organization reference: %+v", v.Organization)
	}
	if v.Viewer != nil || v.Unit.HeadUserID != nil || v.Unit.HeadName == nil || *v.Unit.HeadName != "Анна Смирнова" {
		t.Errorf("anonymous view: %+v", v)
	}
	v, _ = w.svc.GetUnit(bg, org.Slug, unit.ID, &owner.User)
	if v.Viewer == nil || v.Viewer.Role != "owner" || len(v.Viewer.EditableUnits) != 1 || v.Unit.HeadUserID == nil || *v.Unit.HeadUserID != head.ID {
		t.Errorf("owner view: %+v", v)
	}
	stranger := w.user("Чужой")
	v, _ = w.svc.GetUnit(bg, org.Slug, unit.ID, &stranger.User)
	if v.Viewer == nil || v.Viewer.Role != "" || v.Unit.HeadUserID != nil {
		t.Errorf("stranger view: %+v", v)
	}

	if _, err := w.svc.GetUnit(bg, "no-such-org", unit.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing organization: %v", err)
	}
	if _, err := w.svc.GetUnit(bg, org.Slug, uuid.New(), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing unit: %v", err)
	}
	// Подразделение другой организации по адресу этой не открывается.
	other := w.org(owner)
	if _, err := w.svc.GetUnit(bg, other.Slug, unit.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a unit of another organization must not open here: %v", err)
	}
}

func TestSetUnitHead(t *testing.T) {
	w := newWorld(t)
	owner := w.user("Иван")
	org := w.org(owner)
	unit := w.unit(owner, org.Slug)
	colleague := w.user("Коллега")
	w.member(owner, org.Slug, colleague, "unit_head", nil)
	stranger := w.user("Чужой")

	got, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, &colleague.ID)
	if err != nil || got.HeadUserID == nil || *got.HeadUserID != colleague.ID || got.HeadName == nil || *got.HeadName != "Коллега" {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Тот, кто не сотрудник, руководителем быть не может.
	_, err = w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, &stranger.ID)
	if fieldsOf(t, err)["user_id"] == "" {
		t.Errorf("a stranger as head: %v", err)
	}
	if got, _ := w.svc.GetUnit(bg, org.Slug, unit.ID, nil); got.Unit.HeadName == nil || *got.Unit.HeadName != "Коллега" {
		t.Error("a rejected assignment must not change the head")
	}
	if _, err := w.svc.SetUnitHead(bg, owner.User, org.Slug, uuid.New(), &colleague.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing unit: %v", err)
	}
	if _, err := w.svc.SetUnitHead(bg, owner.User, "no-such-org", unit.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing organization: %v", err)
	}
	got, err = w.svc.SetUnitHead(bg, owner.User, org.Slug, unit.ID, nil)
	if err != nil || got.HeadUserID != nil || got.HeadName != nil {
		t.Errorf("removing the head: %+v, %v", got, err)
	}
}

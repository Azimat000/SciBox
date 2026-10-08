package audit_test

import (
	"os"
	"strings"
	"testing"

	"scibox/server/internal/audit"
)

// Каждое действие организации должно иметь подпись на сайте: новое действие без подписи показывалось бы
// владельцу как «Действие «vacancy.exploded»». Тест читает словарь сайта и падает, пока подписи нет.
func TestEveryOrganizationActionHasALabelOnTheSite(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/i18n/ru.ts")
	if err != nil {
		t.Fatal(err)
	}
	dict := string(raw)
	_, block, ok := strings.Cut(dict, "audit: {")
	if !ok {
		t.Fatal("the audit section is missing from the site dictionary")
	}
	block, _, _ = strings.Cut(block, "invitationKinds")
	checked := 0
	for _, a := range audit.Actions() {
		if !audit.OrgScoped(a) {
			continue
		}
		checked++
		if !strings.Contains(block, "'"+string(a)+"':") {
			t.Errorf("action %s has no label in web/src/i18n/ru.ts (orgs.audit.actions)", a)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d organization actions were checked", checked)
	}
	// Особая фраза: руководителя подразделения снимают, и человека в записи нет.
	if !strings.Contains(block, "'unit.head_cleared':") {
		t.Error("the label for a cleared unit head is missing")
	}
}

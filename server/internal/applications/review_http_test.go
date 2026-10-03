package applications

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHTTPReviewRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id, inv := uuid.NewString(), uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/my/candidates"}, {"GET", "/api/my/candidate-vacancies"},
		{"POST", "/api/applications/" + id + "/status"}, {"POST", "/api/applications/" + id + "/invitations"},
		{"DELETE", "/api/applications/" + id + "/invitations/" + inv}, {"POST", "/api/applications/" + id + "/invitations/" + inv + "/answer"},
		{"POST", "/api/applications/" + id + "/invitations/" + inv + "/accept-proposal"},
	} {
		if r := a.api.Do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPReviewFlow(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария Смирнова")
	d := a.mustApply(me, nil)
	app := "/api/applications/" + d.ID.String()

	// Список и вакансии организации.
	r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates?vacancy="+a.vacancy.ID.String()+"&status=sent&limit=5&offset=0", nil)
	if r.Code != 200 || r.JSON(t)["total"] != float64(1) || r.JSON(t)["counts"].(map[string]any)["sent"] != float64(1) {
		t.Fatalf("candidates: %d %s", r.Code, r.Raw)
	}
	item := r.JSON(t)["items"].([]any)[0].(map[string]any)
	if item["applicant_name"] != "Мария Смирнова" || item["status"] != "sent" {
		t.Errorf("item = %v", item)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates?limit=x", nil); r.Code != 200 {
		t.Errorf("bad paging: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates?vacancy=nope", nil); r.Code != 422 || r.Fields(t)["vacancy"] == nil {
		t.Errorf("bad vacancy: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates?status=done", nil); r.Code != 422 || r.Fields(t)["status"] == nil {
		t.Errorf("bad status: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.Out, "GET", "/api/my/candidates", nil); r.Code != 200 || r.JSON(t)["total"] != float64(0) {
		t.Errorf("outsider: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidate-vacancies", nil); r.Code != 200 || len(r.JSON(t)["items"].([]any)) != 1 {
		t.Errorf("vacancies: %d %s", r.Code, r.Raw)
	}

	// Открытие карточки отмечает «просмотрен».
	if r := a.api.Do(&a.tm.HR, "GET", app, nil); r.Code != 200 || r.JSON(t)["application"].(map[string]any)["status"] != "viewed" {
		t.Fatalf("open: %d %s", r.Code, r.Raw)
	}

	// Приглашение.
	bad := a.api.Do(&a.tm.HR, "POST", app+"/invitations", map[string]any{"kind": "interview"})
	if bad.Code != 422 || bad.Fields(t)["starts_at"] == nil {
		t.Errorf("bad invitation: %d %s", bad.Code, bad.Raw)
	}
	r = a.api.Do(&a.tm.HR, "POST", app+"/invitations", map[string]any{
		"kind": "interview", "starts_at": a.when(48 * time.Hour), "place_kind": "onsite", "place": "Новосибирск, пр. Лаврентьева, 5",
	})
	if r.Code != 201 {
		t.Fatalf("invite: %d %s", r.Code, r.Raw)
	}
	inv := r.JSON(t)["invitation"].(map[string]any)["id"].(string)
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations", "{broken"); r.Code != 400 {
		t.Errorf("broken json: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations", map[string]any{"kind": "interview", "surprise": 1}); r.Code != 400 {
		t.Errorf("unknown field: %d", r.Code)
	}
	if r := a.api.Do(&me, "POST", app+"/invitations", map[string]any{"kind": "request_contacts"}); r.Code != 404 {
		t.Errorf("applicant invites: %d", r.Code)
	}

	// Ответ соискателя.
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations/"+inv+"/answer", map[string]any{"action": "confirm"}); r.Code != 404 {
		t.Errorf("staff answers: %d", r.Code)
	}
	if r := a.api.Do(&me, "POST", app+"/invitations/"+inv+"/answer", map[string]any{"action": "reply"}); r.Code != 422 || r.Fields(t)["action"] == nil {
		t.Errorf("bad answer: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", app+"/invitations/"+inv+"/answer", map[string]any{"action": "propose", "proposed_at": a.when(72 * time.Hour)}); r.Code != 204 {
		t.Fatalf("answer: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", app+"/invitations/"+inv+"/answer", map[string]any{"action": "confirm"}); r.Code != 409 || r.ErrCode(t) != CodeInvalidInvite {
		t.Errorf("answer twice: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", app+"/invitations/"+inv+"/answer", "{broken"); r.Code != 400 {
		t.Errorf("broken answer: %d", r.Code)
	}

	// Принять предложенное время, затем отменить.
	if r := a.api.Do(&a.tm.HeadB, "POST", app+"/invitations/"+inv+"/accept-proposal", nil); r.Code != 404 {
		t.Errorf("head of another unit accepts: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations/"+inv+"/accept-proposal", nil); r.Code != 204 {
		t.Fatalf("accept proposal: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations/"+inv+"/accept-proposal", nil); r.Code != 409 || r.ErrCode(t) != CodeInvalidInvite {
		t.Errorf("accept twice: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "DELETE", app+"/invitations/"+inv, nil); r.Code != 204 {
		t.Errorf("cancel: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "DELETE", app+"/invitations/"+inv, nil); r.Code != 409 {
		t.Errorf("cancel twice: %d", r.Code)
	}

	// Решение.
	if r := a.api.Do(&a.tm.HR, "POST", app+"/status", map[string]any{"status": "invited"}); r.Code != 422 || r.Fields(t)["status"] == nil {
		t.Errorf("bad decision: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/status", "{broken"); r.Code != 400 {
		t.Errorf("broken decision: %d", r.Code)
	}
	if r := a.api.Do(&me, "POST", app+"/status", map[string]any{"status": "accepted"}); r.Code != 404 {
		t.Errorf("applicant decides: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/status", map[string]any{"status": "accepted", "note": "Ждём вас"}); r.Code != 204 {
		t.Fatalf("decide: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/status", map[string]any{"status": "rejected"}); r.Code != 409 || r.ErrCode(t) != CodeInvalidStatusCh {
		t.Errorf("decide twice: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "POST", app+"/invitations", map[string]any{"kind": "request_contacts"}); r.Code != 409 || r.ErrCode(t) != CodeInvalidStatusCh {
		t.Errorf("invite after the decision: %d %s", r.Code, r.Raw)
	}
	got := a.api.Do(&me, "GET", app, nil).JSON(t)["application"].(map[string]any)
	if got["decision_note"] != "Ждём вас" || got["status"] != "accepted" || len(got["invitations"].([]any)) != 1 {
		t.Errorf("applicant sees %v", got)
	}

	// Плохие номера.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/applications/x/status"}, {"POST", "/api/applications/x/invitations"},
		{"DELETE", "/api/applications/x/invitations/y"}, {"POST", "/api/applications/x/invitations/y/answer"},
		{"POST", "/api/applications/x/invitations/y/accept-proposal"}, {"DELETE", app + "/invitations/y"},
		{"POST", app + "/invitations/y/answer"}, {"POST", app + "/invitations/y/accept-proposal"},
	} {
		if r := a.api.Do(&a.tm.HR, c.method, c.path, map[string]any{}); r.Code != 404 {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPInviteLimit(t *testing.T) {
	a := newAPI(t)
	d := a.viewedApp(a.Applicant("Мария"))
	for range maxInvitations {
		if r := a.api.Do(&a.tm.Owner, "POST", "/api/applications/"+d.ID.String()+"/invitations", map[string]any{"kind": "request_contacts"}); r.Code != 201 {
			t.Fatalf("%d %s", r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&a.tm.Owner, "POST", "/api/applications/"+d.ID.String()+"/invitations", map[string]any{"kind": "request_contacts"}); r.Code != 409 || r.ErrCode(t) != CodeTooManyInvites {
		t.Errorf("over the limit: %d %s", r.Code, r.Raw)
	}
}

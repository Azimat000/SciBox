package applications

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHTTPReviewRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/my/candidates"}, {"GET", "/api/my/candidate-vacancies"}, {"POST", "/api/applications/" + id + "/status"},
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
	got := a.api.Do(&me, "GET", app, nil).JSON(t)["application"].(map[string]any)
	if _, has := got["invitations"]; got["decision_note"] != "Ждём вас" || got["status"] != "accepted" || has {
		t.Errorf("applicant sees %v", got)
	}
	// Приглашений внутри отклика нет (D-256): адресов тоже нет.
	for _, c := range []struct{ method, path string }{
		{"POST", app + "/invitations"}, {"DELETE", app + "/invitations/" + uuid.NewString()},
		{"POST", app + "/invitations/" + uuid.NewString() + "/answer"}, {"POST", app + "/invitations/" + uuid.NewString() + "/accept-proposal"},
	} {
		if r := a.api.Do(&a.tm.HR, c.method, c.path, map[string]any{}); r.Code != http.StatusNotFound && r.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}

	// Плохие номера.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/applications/x/status"},
	} {
		if r := a.api.Do(&a.tm.HR, c.method, c.path, map[string]any{}); r.Code != 404 {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

// Выгрузка в файл по HTTP: вход обязателен, заголовки файла, отборы те же, что у списка, ошибки полей.
func TestHTTPExportCandidates(t *testing.T) {
	a := newAPI(t)
	a.svc.cfg.PublicURL = "https://scibox.example.ru"
	d := a.mustApply(a.Applicant("Мария Смирнова"), nil)
	if r := a.api.Do(nil, "GET", "/api/my/candidates/export", nil); r.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", r.Code)
	}
	r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates/export?vacancy="+a.vacancy.ID.String()+"&status=sent", nil)
	if r.Code != 200 || r.Header.Get("Content-Type") != "text/csv; charset=utf-8" || r.Header.Get("Cache-Control") != "no-store" ||
		!strings.HasPrefix(r.Header.Get("Content-Disposition"), `attachment; filename="otkliki-`) || r.Header.Get("X-Export-Truncated") != "" {
		t.Fatalf("export: %d %v", r.Code, r.Header)
	}
	if !strings.HasPrefix(r.Raw, "\uFEFF") || !strings.Contains(r.Raw, "Мария Смирнова") || !strings.Contains(r.Raw, "https://scibox.example.ru/candidates/"+d.ID.String()) {
		t.Errorf("body: %q", r.Raw)
	}
	// Другой статус: в файле только заголовок.
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates/export?status=rejected", nil); r.Code != 200 || strings.Count(r.Raw, "\r\n") != 1 {
		t.Errorf("empty export: %d %q", r.Code, r.Raw)
	}
	// Посторонний получает пустой файл, а не чужие отклики.
	if r := a.api.Do(&a.tm.Out, "GET", "/api/my/candidates/export", nil); r.Code != 200 || strings.Contains(r.Raw, "Смирнова") {
		t.Errorf("outsider: %d %q", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates/export?vacancy=nope", nil); r.Code != 422 || r.Fields(t)["vacancy"] == nil {
		t.Errorf("bad vacancy: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates/export?status=done", nil); r.Code != 422 || r.Fields(t)["status"] == nil {
		t.Errorf("bad status: %d %s", r.Code, r.Raw)
	}
	// Предел: заголовок сообщает, что файл неполный.
	defer func(old int) { exportLimit = old }(exportLimit)
	exportLimit = 0
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/candidates/export", nil); r.Code != 200 || r.Header.Get("X-Export-Truncated") != "1" {
		t.Errorf("truncated: %d %v", r.Code, r.Header)
	}
}

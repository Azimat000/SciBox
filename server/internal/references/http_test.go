package references

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/testkit"
)

type apiWorld struct {
	*world
	api *testkit.API
}

func newAPI(t *testing.T) *apiWorld {
	w := newWorld(t)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(w.svc, w.Log, requireUser).Mount(r)
	})
	return &apiWorld{world: w, api: api}
}

func TestHTTPApplicantRoutes(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария Смирнова")
	other := a.Applicant("Другая")
	app := a.application(me)
	path := "/api/applications/" + app.String() + "/references"

	// Без входа.
	for _, c := range []struct{ method, p string }{{"POST", path}, {"POST", path + "/" + uuid.NewString() + "/resend"}, {"DELETE", path + "/" + uuid.NewString()}} {
		if r := a.api.Do(nil, c.method, c.p, referee()); r.Code != 401 {
			t.Errorf("%s %s anonymous: %d", c.method, c.p, r.Code)
		}
	}
	// Добавить.
	in := referee()
	r := a.api.Do(&me, "POST", path, in)
	if r.Code != 201 {
		t.Fatalf("add: %d %s", r.Code, r.Raw)
	}
	ref := r.JSON(t)["reference"].(map[string]any)
	if ref["name"] != in.Name || ref["status"] != "pending" || ref["can_resend"] != false {
		t.Errorf("reference = %v", ref)
	}
	refID := ref["id"].(string)
	if _, leaked := ref["letter"]; leaked {
		t.Error("applicant view contains a letter field")
	}
	// Ошибки формы.
	if r := a.api.Do(&me, "POST", path, RefereeInput{Name: "", Email: "x"}); r.Code != 422 || r.Fields(t)["name"] == nil || r.Fields(t)["email"] == nil {
		t.Errorf("validation: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", path, "not json"); r.Code != 400 {
		t.Errorf("broken body: %d", r.Code)
	}
	// Чужой и несуществующий отклик одинаково «нет».
	for _, who := range []*testkit.Person{&other, &a.tm.Owner} {
		if r := a.api.Do(who, "POST", path, referee()); r.Code != 404 || r.ErrCode(t) != "not_found" {
			t.Errorf("foreign add: %d %s", r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&me, "POST", "/api/applications/not-a-uuid/references", referee()); r.Code != 404 {
		t.Errorf("bad id: %d", r.Code)
	}
	if r := a.api.Do(&me, "POST", path+"/not-a-uuid/resend", nil); r.Code != 404 {
		t.Errorf("bad ref id: %d", r.Code)
	}
	if r := a.api.Do(&me, "DELETE", path+"/not-a-uuid", nil); r.Code != 404 {
		t.Errorf("bad ref id on cancel: %d", r.Code)
	}
	// Повторить слишком рано: 429 с паузой.
	r = a.api.Do(&me, "POST", path+"/"+refID+"/resend", nil)
	if r.Code != 429 || r.ErrCode(t) != CodeResendTooSoon || r.Header.Get("Retry-After") == "" {
		t.Errorf("resend too soon: %d %s %v", r.Code, r.Raw, r.Header)
	}
	a.clock.Advance(25 * time.Hour)
	if r := a.api.Do(&me, "POST", path+"/"+refID+"/resend", nil); r.Code != 200 || r.JSON(t)["reference"] == nil {
		t.Errorf("resend: %d %s", r.Code, r.Raw)
	}
	// Отменить.
	if r := a.api.Do(&me, "DELETE", path+"/"+refID, nil); r.Code != 204 {
		t.Errorf("cancel: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "DELETE", path+"/"+refID, nil); r.Code != 404 {
		t.Errorf("cancel twice: %d", r.Code)
	}
	// Лимит в три рекомендателя.
	for range MaxPerApplication {
		if r := a.api.Do(&me, "POST", path, referee()); r.Code != 201 {
			t.Fatalf("add: %d %s", r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&me, "POST", path, referee()); r.Code != 409 || r.ErrCode(t) != CodeTooMany {
		t.Errorf("fourth: %d %s", r.Code, r.Raw)
	}
	// Закрытый отклик.
	a.setStatus(app, "rejected")
	if r := a.api.Do(&me, "POST", path, referee()); r.Code != 409 {
		t.Errorf("closed: %d", r.Code)
	}
}

func TestHTTPApplicantErrorCodes(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	app := a.application(me)
	path := "/api/applications/" + app.String() + "/references"
	req, token := a.add(me, app)
	if err := a.svc.Submit(bg, token, LetterInput{Text: "письмо"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, p, code string }{
		{"POST", path + "/" + req.ID.String() + "/resend", CodeNotPending},
		{"DELETE", path + "/" + req.ID.String(), CodeNotPending},
	} {
		if r := a.api.Do(&me, c.method, c.p, nil); r.Code != 409 || r.ErrCode(t) != c.code {
			t.Errorf("%s: %d %s", c.method, r.Code, r.Raw)
		}
	}
	a.setStatus(app, "accepted")
	if r := a.api.Do(&me, "POST", path, referee()); r.Code != 409 || r.ErrCode(t) != CodeClosed {
		t.Errorf("closed: %d %s", r.Code, r.Raw)
	}
	// Лимит просьб в сутки.
	a.svc.cfg.Requests = Limit{Max: 1, Window: time.Hour}
	other := a.Applicant("Вторая")
	app2 := a.application(other)
	p2 := "/api/applications/" + app2.String() + "/references"
	if r := a.api.Do(&other, "POST", p2, referee()); r.Code != 201 {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&other, "POST", p2, referee()); r.Code != 429 || r.ErrCode(t) != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Errorf("rate limit: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPRefereeFlow(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария Смирнова")
	app := a.application(me)
	_, token := a.add(me, app)

	// Рекомендатель не входит в аккаунт.
	r := a.api.Do(nil, "POST", "/api/recommendations/lookup", map[string]string{"token": token})
	if r.Code != 200 {
		t.Fatalf("lookup: %d %s", r.Code, r.Raw)
	}
	info := r.JSON(t)["request"].(map[string]any)
	if info["applicant_name"] != "Мария Смирнова" || info["status"] != "pending" {
		t.Errorf("info = %v", info)
	}
	if strings.Contains(r.Raw, app.String()) || strings.Contains(r.Raw, me.Email) {
		t.Errorf("the referee page leaks ids or emails: %s", r.Raw)
	}
	// Файл не PDF.
	r = a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token}, testkit.FilePart{Name: "a.pdf", Data: []byte("MZ")})
	if r.Code != 422 || r.Fields(t)["file"] == nil {
		t.Errorf("not a pdf: %d %s", r.Code, r.Raw)
	}
	// Пустое письмо.
	r = a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token, "text": " "})
	if r.Code != 422 || r.Fields(t)["text"] == nil {
		t.Errorf("empty: %d %s", r.Code, r.Raw)
	}
	// Два файла.
	r = a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token, "text": "x"},
		testkit.FilePart{Name: "a.pdf", Data: pdfBytes()}, testkit.FilePart{Name: "b.pdf", Data: pdfBytes()})
	if r.Code != 422 || r.Fields(t)["file"] == nil {
		t.Errorf("two files: %d %s", r.Code, r.Raw)
	}
	// Не multipart.
	if r := a.api.Do(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token}); r.Code != 400 {
		t.Errorf("json instead of form: %d %s", r.Code, r.Raw)
	}
	// Хорошее письмо с PDF.
	r = a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token, "text": "Рекомендую"}, testkit.FilePart{Name: "Письмо.pdf", Data: pdfBytes()})
	if r.Code != 200 || r.JSON(t)["status"] != "received" {
		t.Fatalf("submit: %d %s", r.Code, r.Raw)
	}
	r = a.api.DoForm(nil, "POST", "/api/recommendations/submit", map[string]string{"token": token, "text": "ещё раз"})
	if r.Code != 409 || r.ErrCode(t) != CodeAlreadyAnswered {
		t.Errorf("second submit: %d %s", r.Code, r.Raw)
	}
	r = a.api.Do(nil, "POST", "/api/recommendations/decline", map[string]string{"token": token})
	if r.Code != 409 || r.ErrCode(t) != CodeAlreadyAnswered {
		t.Errorf("decline after answer: %d %s", r.Code, r.Raw)
	}
	// Отказ тоже принимается без входа.
	other := a.Applicant("Вторая")
	_, token2 := a.add(other, a.application(other))
	if r := a.api.Do(nil, "POST", "/api/recommendations/decline", map[string]string{"token": token2}); r.Code != 200 || r.JSON(t)["status"] != "declined" {
		t.Errorf("decline: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPRefereeDeadLinks(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	app := a.application(me)
	_, token := a.add(me, app)
	post := func(p string, body any) testkit.Reply { return a.api.Do(nil, "POST", "/api/recommendations/"+p, body) }

	if r := post("lookup", map[string]string{"token": "нет"}); r.Code != 404 || r.ErrCode(t) != CodeInvalidLink {
		t.Errorf("unknown: %d %s", r.Code, r.Raw)
	}
	if r := post("lookup", map[string]string{"token": token, "extra": "x"}); r.Code != 400 {
		t.Errorf("unknown field: %d", r.Code)
	}
	if r := post("decline", map[string]string{"token": "нет"}); r.Code != 404 {
		t.Errorf("decline unknown: %d", r.Code)
	}
	if r := post("decline", "not json"); r.Code != 400 {
		t.Errorf("decline broken body: %d", r.Code)
	}
	var soon error = &TooSoonError{RetryAfter: time.Minute}
	if soon.Error() == "" {
		t.Error("empty error text")
	}
	a.setStatus(app, "withdrawn")
	if r := post("lookup", map[string]string{"token": token}); r.Code != 410 || r.ErrCode(t) != CodeWithdrawn {
		t.Errorf("withdrawn: %d %s", r.Code, r.Raw)
	}
	a.setStatus(app, "sent")
	a.clock.Advance(a.svc.cfg.TTL + time.Hour)
	if r := post("lookup", map[string]string{"token": token}); r.Code != 410 || r.ErrCode(t) != CodeLinkExpired {
		t.Errorf("expired: %d %s", r.Code, r.Raw)
	}
	// Запрос с чужого сайта отвергается до разбора.
	req := httptest.NewRequest("POST", "/api/recommendations/decline", bytes.NewReader([]byte(`{"token":"`+token+`"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	a.api.H.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("cross-site: %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPInternalErrorsAreHidden(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	app := a.application(me)
	_, token := a.add(me, app)
	db, _ := testkit.Faulty(testkit.Pool, 1)
	broken := testkit.NewAPI(a.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(svcOn(a.world, db), a.Log, requireUser).Mount(r)
	})
	r := broken.Do(nil, "POST", "/api/recommendations/lookup", map[string]string{"token": token})
	if r.Code != 500 || r.ErrCode(t) != "internal" || len(r.Raw) > 300 {
		t.Errorf("%d %s", r.Code, r.Raw)
	}
}

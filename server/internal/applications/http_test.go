package applications

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/references"
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
		references.NewHandler(w.refs, w.Log, requireUser).Mount(r)
	})
	return &apiWorld{world: w, api: api}
}

func (a *apiWorld) form(who *testkit.Person, in Input, files ...testkit.FilePart) testkit.Reply {
	return a.api.DoForm(who, "POST", "/api/applications", in, files...)
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/applications"}, {"GET", "/api/applications"}, {"GET", "/api/applications/for-vacancy/" + id},
		{"GET", "/api/applications/" + id}, {"GET", "/api/applications/" + id + "/files/" + id}, {"POST", "/api/applications/" + id + "/withdraw"},
	} {
		if r := a.api.Do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPApplyAndRead(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария Смирнова")
	in := a.input(me)
	ref := refereeIn()
	in.Referees = []references.RefereeInput{ref}
	r := a.form(&me, in, testkit.FilePart{Name: "Список публикаций.pdf", Data: pdfBytes()})
	if r.Code != 201 {
		t.Fatalf("apply: %d %s", r.Code, r.Raw)
	}
	app := r.JSON(t)["application"].(map[string]any)
	id := app["id"].(string)
	if app["status"] != "sent" || app["viewer"].(map[string]any)["role"] != "applicant" || len(app["files"].([]any)) != 1 {
		t.Errorf("application = %v", app)
	}
	if app["cv"] == nil {
		t.Error("no cv in the answer")
	}

	// Тот же отклик ещё раз.
	if r := a.form(&me, in); r.Code != 409 || r.ErrCode(t) != CodeAlreadyApplied {
		t.Errorf("second: %d %s", r.Code, r.Raw)
	}
	// Карточка, список и состояние на странице вакансии.
	if r := a.api.Do(&me, "GET", "/api/applications/"+id, nil); r.Code != 200 || r.JSON(t)["application"].(map[string]any)["id"] != id {
		t.Errorf("get: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "GET", "/api/applications?limit=5", nil); r.Code != 200 || r.JSON(t)["total"] != float64(1) {
		t.Errorf("mine: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "GET", "/api/applications?limit=abc&offset=zz", nil); r.Code != 200 {
		t.Errorf("mine with bad paging: %d", r.Code)
	}
	if r := a.api.Do(&me, "GET", "/api/applications/for-vacancy/"+a.vacancy.ID.String(), nil); r.Code != 200 || r.JSON(t)["reason"] != "applied" {
		t.Errorf("for-vacancy: %d %s", r.Code, r.Raw)
	}
	// Организация открывает ту же карточку: роль «сотрудник».
	if r := a.api.Do(&a.tm.Owner, "GET", "/api/applications/"+id, nil); r.Code != 200 || r.JSON(t)["application"].(map[string]any)["viewer"].(map[string]any)["role"] != "staff" {
		t.Errorf("staff get: %d %s", r.Code, r.Raw)
	}
	// Посторонние не отличают чужой отклик от несуществующего.
	for name, who := range map[string]*testkit.Person{"outsider": &a.tm.Out, "head of another unit": &a.tm.HeadB} {
		r := a.api.Do(who, "GET", "/api/applications/"+id, nil)
		same := a.api.Do(who, "GET", "/api/applications/"+uuid.NewString(), nil)
		if r.Code != 404 || r.Raw != same.Raw {
			t.Errorf("%s: %d %s vs %d %s", name, r.Code, r.Raw, same.Code, same.Raw)
		}
	}
	if r := a.api.Do(&me, "GET", "/api/applications/not-a-uuid", nil); r.Code != 404 {
		t.Errorf("bad id: %d", r.Code)
	}
	// Отзыв.
	if r := a.api.Do(&a.tm.Owner, "POST", "/api/applications/"+id+"/withdraw", nil); r.Code != 404 {
		t.Errorf("withdraw by the organization: %d", r.Code)
	}
	if r := a.api.Do(&me, "POST", "/api/applications/"+id+"/withdraw", nil); r.Code != 204 {
		t.Errorf("withdraw: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", "/api/applications/"+id+"/withdraw", nil); r.Code != 409 || r.ErrCode(t) != CodeInvalidStatusCh {
		t.Errorf("withdraw twice: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", "/api/applications/not-a-uuid/withdraw", nil); r.Code != 404 {
		t.Errorf("withdraw bad id: %d", r.Code)
	}
}

func TestHTTPApplyErrors(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	in := a.input(me)
	ok := testkit.FilePart{Name: "a.pdf", Data: pdfBytes()}

	if r := a.form(&me, Input{VacancyID: in.VacancyID, ContactEmail: "x", CoverLetter: "коротко"}); r.Code != 422 || r.Fields(t)["contact_email"] == nil || r.Fields(t)["cover_letter"] == nil {
		t.Errorf("validation: %d %s", r.Code, r.Raw)
	}
	if r := a.form(&me, in, testkit.FilePart{Name: "evil.pdf", Data: []byte("MZ")}); r.Code != 422 || r.Fields(t)["files"] == nil {
		t.Errorf("not a pdf: %d %s", r.Code, r.Raw)
	}
	big := testkit.FilePart{Name: "big.pdf", Data: append([]byte("%PDF-"), make([]byte, 11<<20)...)}
	if r := a.form(&me, in, big); r.Code != 422 || !strings.Contains(r.Raw, "big.pdf") {
		t.Errorf("big file: %d %.200s", r.Code, r.Raw)
	}
	if r := a.api.Do(&me, "POST", "/api/applications", in); r.Code != 400 {
		t.Errorf("JSON instead of a form: %d %s", r.Code, r.Raw)
	}
	other := in
	other.VacancyID = uuid.New()
	if r := a.form(&me, other); r.Code != 404 {
		t.Errorf("unknown vacancy: %d", r.Code)
	}
	if r := a.form(&a.tm.Owner, a.input(a.tm.Owner)); r.Code != 403 && r.Code != 422 {
		t.Errorf("own vacancy: %d %s", r.Code, r.Raw)
	}
	a.FillProfile(a.tm.Owner)
	if r := a.form(&a.tm.Owner, a.input(a.tm.Owner)); r.Code != 403 || r.ErrCode(t) != CodeOwnVacancy {
		t.Errorf("own vacancy: %d %s", r.Code, r.Raw)
	}
	empty := a.User("Пустой")
	if r := a.form(&empty, a.input(empty)); r.Code != 422 || r.Fields(t)["profile"] == nil {
		t.Errorf("empty profile: %d %s", r.Code, r.Raw)
	}
	_ = ok
}

func TestHTTPVacancyStatesAndRateLimit(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	closed := a.tm.Published(&a.tm.UnitA.ID)
	a.tm.Status(closed.ID, "closed")
	in := a.input(me)
	in.VacancyID = closed.ID
	if r := a.form(&me, in); r.Code != 409 || r.ErrCode(t) != CodeVacancyClosed {
		t.Errorf("closed: %d %s", r.Code, r.Raw)
	}
	a.clock.Advance(366 * 24 * 60 * 60 * 1e9)
	in.VacancyID = a.vacancy.ID
	if r := a.form(&me, in); r.Code != 409 || r.ErrCode(t) != CodeDeadlinePassed {
		t.Errorf("deadline: %d %s", r.Code, r.Raw)
	}
	a.clock.Advance(-366 * 24 * 60 * 60 * 1e9)
	if r := a.api.Do(&me, "GET", "/api/applications/for-vacancy/not-a-uuid", nil); r.Code != 404 {
		t.Errorf("for-vacancy bad id: %d", r.Code)
	}
	if r := a.api.Do(&me, "GET", "/api/applications/for-vacancy/"+uuid.NewString(), nil); r.Code != 404 {
		t.Errorf("for-vacancy unknown: %d", r.Code)
	}
	// Лимит откликов.
	a.svc.cfg.Apply = Limit{Max: 1, Window: 3600 * 1e9}
	if r := a.form(&me, in); r.Code != 201 {
		t.Fatalf("first: %d %s", r.Code, r.Raw)
	}
	v2 := a.tm.Published(&a.tm.UnitA.ID)
	in.VacancyID = v2.ID
	if r := a.form(&me, in); r.Code != 429 || r.ErrCode(t) != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Errorf("rate limit: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPFileDownload(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	ref := refereeIn()
	in := a.input(me)
	in.Referees = []references.RefereeInput{ref}
	d := a.mustApply(me, func(i *Input) { *i = in }, pdf("Список публикаций.pdf"))
	if err := a.refs.Submit(bg, a.tokenFor(ref.Email), references.LetterInput{}, &pdfUpload); err != nil {
		t.Fatal(err)
	}
	staff, _ := a.svc.Get(bg, a.tm.Owner.User, d.ID)
	letter := staff.References.([]references.StaffRequest)[0].Letter.File

	path := func(f uuid.UUID) string { return "/api/applications/" + d.ID.String() + "/files/" + f.String() }
	r := a.api.Do(&me, "GET", path(d.Files[0].ID), nil)
	if r.Code != 200 || r.Header.Get("Content-Type") != "application/pdf" || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment;") || !bytes.HasPrefix([]byte(r.Raw), []byte("%PDF-")) {
		t.Errorf("download: %d %v", r.Code, r.Header)
	}
	if r := a.api.Do(&a.tm.HR, "GET", path(letter.ID), nil); r.Code != 200 {
		t.Errorf("hr downloads the letter: %d", r.Code)
	}
	// Соискатель письмо получить не может, и ответ тот же, что про несуществующий файл.
	hidden := a.api.Do(&me, "GET", path(letter.ID), nil)
	unknown := a.api.Do(&me, "GET", path(uuid.New()), nil)
	if hidden.Code != 404 || hidden.Raw != unknown.Raw || strings.Contains(hidden.Raw, "%PDF") {
		t.Errorf("letter for the applicant: %d %s vs %s", hidden.Code, hidden.Raw, unknown.Raw)
	}
	if r := a.api.Do(&a.tm.Out, "GET", path(d.CV.ID), nil); r.Code != 404 {
		t.Errorf("outsider: %d", r.Code)
	}
	if r := a.api.Do(&me, "GET", "/api/applications/"+d.ID.String()+"/files/not-a-uuid", nil); r.Code != 404 {
		t.Errorf("bad file id: %d", r.Code)
	}
}

var pdfUpload = pdf("Письмо.pdf")

func TestHTTPInternalErrorsAreHidden(t *testing.T) {
	a := newAPI(t)
	me := a.Applicant("Мария")
	d := a.mustApply(me, nil)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/applications"},
		{"GET", "/api/applications/" + d.ID.String()},
		{"GET", "/api/applications/for-vacancy/" + a.vacancy.ID.String()},
		{"POST", "/api/applications/" + d.ID.String() + "/withdraw"},
	} {
		db, _ := testkit.Faulty(testkit.Pool, 1)
		broken := testkit.NewAPI(a.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
			NewHandler(svcOn(a.world, db), a.Log, requireUser).Mount(r)
		})
		r := broken.Do(&me, c.method, c.path, map[string]any{})
		if r.Code != 500 || r.ErrCode(t) != "internal" || len(r.Raw) > 300 {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	// Запрос с чужого сайта отвергается.
	req := httptest.NewRequest("POST", "/api/applications/"+d.ID.String()+"/withdraw", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	a.api.H.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("cross-site: %d", rec.Code)
	}
}

func TestNewServiceStartsRealClock(t *testing.T) {
	w := newWorld(t)
	s := NewService(testkit.Pool, w.Prof, w.refs, w.notes, DefaultConfig())
	if s.cfg.Apply.Max != 20 || s.now().IsZero() {
		t.Errorf("cfg=%+v", s.cfg)
	}
}

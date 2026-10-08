package offers

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
)

type apiWorld struct {
	*world
	api *testkit.API
}

func newAPI(t *testing.T) *apiWorld {
	w := newWorld(t)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		profiles.NewHandler(w.Prof, w.Log, requireUser).Mount(r)
		NewHandler(w.svc, w.Log, requireUser).Mount(r)
	})
	return &apiWorld{world: w, api: api}
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/offers"}, {"GET", "/api/offers"}, {"GET", "/api/offers/" + id}, {"POST", "/api/offers/" + id + "/answer"},
		{"POST", "/api/offers/" + id + "/cancel"}, {"GET", "/api/my/sent-offers"}, {"GET", "/api/scientists/" + id + "/offer-targets"},
	} {
		if r := a.api.Do(nil, c.method, c.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", c.method, c.path, r.Code)
		}
	}
}

func TestHTTPFlow(t *testing.T) {
	a := newAPI(t)
	sci, profile := a.scientist("Анна Ильина", "public")

	// Куда можно пригласить.
	r := a.api.Do(&a.tm.HR, "GET", "/api/scientists/"+profile.String()+"/offer-targets", nil)
	if r.Code != 200 {
		t.Fatalf("targets: %d %s", r.Code, r.Raw)
	}
	found := false
	for _, it := range r.JSON(t)["items"].([]any) {
		if it.(map[string]any)["id"] == a.vacancy.ID.String() {
			found = true
		}
	}
	if !found {
		t.Errorf("вакансии нет среди возможных: %s", r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/scientists/nope/offer-targets", nil); r.Code != 404 {
		t.Errorf("targets bad id: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/scientists/"+uuid.NewString()+"/offer-targets", nil); r.Code != 404 {
		t.Errorf("targets unknown: %d", r.Code)
	}

	// Приглашение.
	body := map[string]any{"vacancy_id": a.vacancy.ID, "profile_id": profile, "message": "Приглашаем вас в лабораторию"}
	r = a.api.Do(&a.tm.HR, "POST", "/api/offers", body)
	if r.Code != 201 {
		t.Fatalf("invite: %d %s", r.Code, r.Raw)
	}
	offer := r.JSON(t)["offer"].(map[string]any)
	id := offer["id"].(string)
	if offer["status"] != "pending" || offer["scientist"].(map[string]any)["name"] != "Анна Ильина" || offer["can_cancel"] != true {
		t.Errorf("offer: %v", offer)
	}
	for _, tc := range []struct {
		name string
		body any
		code int
		err  string
	}{
		{"повтор", body, 409, "already_offered"},
		{"неверный JSON", "{broken", 400, ""},
		{"лишнее поле", map[string]any{"vacancy_id": a.vacancy.ID, "profile_id": profile, "surprise": 1}, 400, ""},
		{"несуществующие вакансия и профиль", map[string]any{"vacancy_id": uuid.NewString(), "profile_id": uuid.NewString(), "message": ""}, 404, "not_found"},
	} {
		if r := a.api.Do(&a.tm.Owner, "POST", "/api/offers", tc.body); r.Code != tc.code || (tc.err != "" && r.ErrCode(t) != tc.err) {
			t.Errorf("%s: %d %s", tc.name, r.Code, r.Raw)
		}
	}
	long := map[string]any{"vacancy_id": a.vacancy.ID, "profile_id": profile, "message": strings.Repeat("я", 1001)}
	if r := a.api.Do(&a.tm.Owner, "POST", "/api/offers", long); r.Code != 422 || r.Fields(t)["message"] == nil {
		t.Errorf("длинное сообщение: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.Out, "POST", "/api/offers", body); r.Code != 404 {
		t.Errorf("посторонний: %d", r.Code)
	}

	// Список и карточка учёного.
	r = a.api.Do(&sci, "GET", "/api/offers?status=pending&limit=5&offset=0", nil)
	if r.Code != 200 || r.JSON(t)["total"] != float64(1) || r.JSON(t)["pending"] != float64(1) {
		t.Fatalf("mine: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&sci, "GET", "/api/offers?status=done", nil); r.Code != 422 || r.Fields(t)["status"] == nil {
		t.Errorf("bad status: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&sci, "GET", "/api/offers?limit=x", nil); r.Code != 200 {
		t.Errorf("bad paging: %d", r.Code)
	}
	r = a.api.Do(&sci, "GET", "/api/offers/"+id, nil)
	if r.Code != 200 {
		t.Fatalf("get: %d %s", r.Code, r.Raw)
	}
	got := r.JSON(t)["offer"].(map[string]any)
	if got["message"] != "Приглашаем вас в лабораторию" || got["can_answer"] != true || got["scientist"] != nil {
		t.Errorf("get: %v", got)
	}
	if r := a.api.Do(&a.tm.Out, "GET", "/api/offers/"+id, nil); r.Code != 404 {
		t.Errorf("чужое: %d", r.Code)
	}
	if r := a.api.Do(&sci, "GET", "/api/offers/nope", nil); r.Code != 404 {
		t.Errorf("bad id: %d", r.Code)
	}

	// Список организации.
	r = a.api.Do(&a.tm.HR, "GET", "/api/my/sent-offers?vacancy="+a.vacancy.ID.String()+"&status=pending&limit=5&offset=0", nil)
	if r.Code != 200 || r.JSON(t)["total"] != float64(1) || r.JSON(t)["counts"].(map[string]any)["pending"] != float64(1) {
		t.Fatalf("sent: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/sent-offers?vacancy=nope", nil); r.Code != 422 || r.Fields(t)["vacancy"] == nil {
		t.Errorf("bad vacancy: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.HR, "GET", "/api/my/sent-offers?status=done", nil); r.Code != 422 || r.Fields(t)["status"] == nil {
		t.Errorf("bad status: %d %s", r.Code, r.Raw)
	}
	if r := a.api.Do(&a.tm.Out, "GET", "/api/my/sent-offers", nil); r.Code != 200 || r.JSON(t)["total"] != float64(0) {
		t.Errorf("посторонний: %d %s", r.Code, r.Raw)
	}

	// Ответ.
	for _, tc := range []struct {
		name string
		who  testkit.Person
		path string
		body any
		code int
		err  string
	}{
		{"чужой отвечает", a.tm.Out, "/answer", map[string]any{"action": "interested"}, 404, ""},
		{"нет действия", sci, "/answer", map[string]any{}, 422, ""},
		{"неверный JSON", sci, "/answer", "{broken", 400, ""},
		{"ответ", sci, "/answer", map[string]any{"action": "interested", "note": "Интересно, напишите"}, 204, ""},
		{"второй ответ", sci, "/answer", map[string]any{"action": "declined"}, 409, "invalid_offer_state"},
		{"отзыв после ответа", a.tm.HR, "/cancel", nil, 409, "invalid_offer_state"},
	} {
		if r := a.api.Do(&tc.who, "POST", "/api/offers/"+id+tc.path, tc.body); r.Code != tc.code || (tc.err != "" && r.ErrCode(t) != tc.err) {
			t.Errorf("%s: %d %s", tc.name, r.Code, r.Raw)
		}
	}
	if r := a.api.Do(&sci, "POST", "/api/offers/nope/answer", map[string]any{"action": "interested"}); r.Code != 404 {
		t.Errorf("answer bad id: %d", r.Code)
	}
	if r := a.api.Do(&sci, "POST", "/api/offers/nope/cancel", nil); r.Code != 404 {
		t.Errorf("cancel bad id: %d", r.Code)
	}

	// Отзыв.
	_, profile2 := a.scientist("Борис Орлов", "orgs")
	r = a.api.Do(&a.tm.HR, "POST", "/api/offers", map[string]any{"vacancy_id": a.vacancy.ID, "profile_id": profile2})
	id2 := r.JSON(t)["offer"].(map[string]any)["id"].(string)
	if r := a.api.Do(&a.tm.Out, "POST", "/api/offers/"+id2+"/cancel", nil); r.Code != 404 {
		t.Errorf("cancel чужим: %d", r.Code)
	}
	if r := a.api.Do(&a.tm.HR, "POST", "/api/offers/"+id2+"/cancel", nil); r.Code != 204 {
		t.Errorf("cancel: %d %s", r.Code, r.Raw)
	}
}

// Каждая ошибка правил приглашения имеет свой код ответа.
func TestHTTPErrorCodes(t *testing.T) {
	a := newAPI(t)
	invite := func(vacancy uuid.UUID, profile uuid.UUID) testkit.Reply {
		return a.api.Do(&a.tm.HR, "POST", "/api/offers", map[string]any{"vacancy_id": vacancy, "profile_id": profile})
	}
	sci, profile := a.scientist("Учёный", "public")

	closed := a.tm.Published(&a.tm.UnitA.ID)
	a.tm.Status(closed.ID, "closed")
	if r := invite(closed.ID, profile); r.Code != 409 || r.ErrCode(t) != "vacancy_closed" {
		t.Errorf("закрытая: %d %s", r.Code, r.Raw)
	}
	a.apply(sci, a.vacancy.ID, "sent")
	if r := invite(a.vacancy.ID, profile); r.Code != 409 || r.ErrCode(t) != "already_applied" {
		t.Errorf("откликнулся: %d %s", r.Code, r.Raw)
	}
	a.FillProfile(a.tm.Owner)
	if _, err := a.Prof.SetPrivacy(bg, a.tm.Owner.User, "public", false); err != nil {
		t.Fatal(err)
	}
	own, _ := a.Prof.Own(bg, a.tm.Owner.User)
	if r := invite(a.vacancy.ID, own.Profile.ID); r.Code != 409 || r.ErrCode(t) != "invitee_is_staff" {
		t.Errorf("сотрудник: %d %s", r.Code, r.Raw)
	}
	deadline, _ := time.Parse("2006-01-02", a.vacancy.Deadline)
	a.clock.Set(deadline.Add(48 * time.Hour))
	_, profile2 := a.scientist("Другой", "public")
	if r := invite(a.vacancy.ID, profile2); r.Code != 409 || r.ErrCode(t) != "deadline_passed" {
		t.Errorf("срок: %d %s", r.Code, r.Raw)
	}
	a.clock.Set(time.Now().UTC().Truncate(time.Microsecond))
	if r := invite(a.vacancy.ID, profile2); r.Code != 201 {
		t.Fatalf("приглашение: %d %s", r.Code, r.Raw)
	}
	if r := invite(a.vacancy.ID, profile2); r.Code != 409 || r.ErrCode(t) != "already_offered" {
		t.Errorf("повтор: %d %s", r.Code, r.Raw)
	}
}

func TestHTTPRateLimit(t *testing.T) {
	a := newAPI(t)
	a.svc.cfg = Config{Invite: Limit{Max: 1, Window: time.Hour}}
	_, p1 := a.scientist("Первый", "public")
	_, p2 := a.scientist("Второй", "public")
	invite := func(p uuid.UUID) testkit.Reply {
		return a.api.Do(&a.tm.HR, "POST", "/api/offers", map[string]any{"vacancy_id": a.vacancy.ID, "profile_id": p})
	}
	if r := invite(p1); r.Code != 201 {
		t.Fatalf("первое: %d %s", r.Code, r.Raw)
	}
	r := invite(p2)
	if r.Code != 429 || r.ErrCode(t) != "rate_limited" || r.Header.Get("Retry-After") == "" {
		t.Errorf("второе: %d %s", r.Code, r.Raw)
	}
}

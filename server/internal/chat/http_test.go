package chat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
	t.Helper()
	w := newWorld(t)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(w.svc, w.Log, requireUser).Mount(r)
	})
	return &apiWorld{world: w, api: api}
}

func (a *apiWorld) do(who *testkit.Person, method, path string, body any) testkit.Reply {
	a.T.Helper()
	return a.api.Do(who, method, path, body)
}

// expect проверяет код ответа и его форму по файлу контракта; для ошибок file — errors, а code — машинный код.
func (a *apiWorld) expect(r testkit.Reply, status int, file string) {
	a.T.Helper()
	if r.Code != status {
		a.T.Fatalf("%s: status %d, want %d: %s", file, r.Code, status, r.Raw)
	}
	f := loadContract(a.T, file)
	if errs := f.responseErrors(r.Raw); len(errs) > 0 {
		a.T.Errorf("%s does not match the contract: %v\n%s", file, errs, r.Raw)
	}
	allowed := false
	for _, e := range f.Examples {
		if e.Status == status {
			allowed = true
		}
	}
	if !allowed {
		a.T.Errorf("%s has no example with status %d", file, status)
	}
}

func (a *apiWorld) expectError(r testkit.Reply, status int, code string) {
	a.T.Helper()
	if r.Code != status || r.ErrCode(a.T) != code {
		a.T.Fatalf("status %d code %q, want %d %q: %s", r.Code, r.ErrCode(a.T), status, code, r.Raw)
	}
	f := loadContract(a.T, "errors")
	if errs := f.responseErrors(r.Raw); len(errs) > 0 {
		a.T.Errorf("error does not match errors.json: %v\n%s", errs, r.Raw)
	}
	// Тот же код в файле контракта есть у такого статуса.
	found := false
	for _, e := range f.Examples {
		if e.Status == status {
			if m, ok := e.Response.(map[string]any)["error"].(map[string]any); ok && m["code"] == code {
				found = true
			}
		}
	}
	if !found {
		a.T.Errorf("errors.json has no example %d %s", status, code)
	}
}

func TestHTTPRequiresSignIn(t *testing.T) {
	a := newAPI(t)
	id := uuid.NewString()
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/chats?side=org"}, {"GET", "/api/chats/unread-count"}, {"GET", "/api/chats/vacancies"}, {"GET", "/api/chats/" + id},
		{"GET", "/api/chats/" + id + "/messages"}, {"POST", "/api/chats/" + id + "/messages"}, {"PATCH", "/api/chats/" + id + "/messages/1"},
		{"DELETE", "/api/chats/" + id + "/messages/1"}, {"POST", "/api/chats/" + id + "/read"}, {"POST", "/api/chats/" + id + "/archive"},
		{"POST", "/api/chats/" + id + "/unarchive"}, {"POST", "/api/chats/" + id + "/typing"},
	} {
		r := a.do(nil, c.method, c.path, map[string]any{})
		a.expectError(r, http.StatusUnauthorized, "unauthorized")
		if c.method == "" {
			t.Fatal("unreachable")
		}
	}
}

// contractLines — строки о событиях, как они записаны в примерах контракта: данные берутся оттуда же.
func contractLines(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	f := loadContract(t, "messages")
	out := map[string]json.RawMessage{}
	for _, e := range f.Examples {
		resp, _ := e.Response.(map[string]any)
		items, _ := resp["items"].([]any)
		for _, it := range items {
			ev, _ := it.(map[string]any)["event"].(map[string]any)
			if ev == nil {
				continue
			}
			raw, _ := json.Marshal(ev["data"])
			out[ev["type"].(string)] = raw
		}
	}
	if len(out) != len(EventTypes()) {
		t.Fatalf("the contract examples have %d event types, the server knows %d", len(out), len(EventTypes()))
	}
	return out
}

func TestHTTPFollowsTheContract(t *testing.T) {
	a := newAPI(t)
	sci := a.Applicant("Дмитрий Королёв")
	anon := a.Applicant("Скрытый Учёный")
	a.privacy(anon, "anon")

	// Переписка учёного: отклик с приглашением, все виды строк о событиях.
	chat := a.chatOf(sci, a.vacancy.ID, "invited")
	app := uuidOfApplication(t, chat)
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO application_invitations (application_id, kind, status, message, starts_at, place_kind, place, created_at, updated_at)
		VALUES ($1, 'interview', 'pending', 'Приходите', now() + interval '2 days', 'online', 'https://meet.example.org/r', now(), now())`, app); err != nil {
		t.Fatal(err)
	}
	lines := contractLines(t)
	types := EventTypes()
	slices.Sort(types)
	for _, ev := range types {
		a.clock.Advance(time.Second)
		if _, err := AppendSystem(bg, a.q(), chat, ev, json.RawMessage(lines[ev]), a.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	a.clock.Advance(time.Second)
	a.send(a.tm.HR, chat, "Добрый день! Пришлите, пожалуйста, ссылку на статью.")

	// Приглашение на вакансию анонимному учёному и закрытая переписка.
	offer := a.offer(anon, a.other.ID, "pending")
	anonChat := a.open(anon, a.other.ID, nil, &offer)
	a.clock.Advance(time.Second)
	a.system(anonChat, EventOfferCreated, json.RawMessage(lines[EventOfferCreated]))
	a.send(anon, anonChat, "Расскажите подробнее.")
	closed := a.chatOf(a.Applicant("Закрытый Отклик"), a.vacancy.ID, "rejected")
	a.system(closed, EventApplicationDecided, json.RawMessage(lines[EventApplicationDecided]))

	scientistPath := "/api/chats/" + chat.String()
	t.Run("lists", func(t *testing.T) {
		a.expect(a.do(&sci, "GET", "/api/chats?side=scientist", nil), 200, "list")
		a.expect(a.do(&sci, "GET", "/api/chats?side=scientist&box=unread", nil), 200, "list")
		a.expect(a.do(&sci, "GET", "/api/chats?side=scientist&box=archive", nil), 200, "list")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats?side=org&limit=2", nil), 200, "list")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats?side=org&q=Расскажите&vacancy="+a.other.ID.String(), nil), 200, "list")
		a.expect(a.do(&a.tm.Out, "GET", "/api/chats?side=org", nil), 200, "list")
		a.expect(a.do(&sci, "GET", "/api/chats/unread-count", nil), 200, "unread-count")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats/unread-count", nil), 200, "unread-count")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats/vacancies", nil), 200, "vacancies")
		a.expect(a.do(&a.tm.Out, "GET", "/api/chats/vacancies", nil), 200, "vacancies")
	})
	t.Run("headers", func(t *testing.T) {
		a.expect(a.do(&sci, "GET", scientistPath, nil), 200, "chat")
		a.expect(a.do(&a.tm.HR, "GET", scientistPath, nil), 200, "chat")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats/"+anonChat.String(), nil), 200, "chat")
		a.expect(a.do(&anon, "GET", "/api/chats/"+anonChat.String(), nil), 200, "chat")
		a.expect(a.do(&sci, "GET", "/api/chats/"+closed.String(), nil), 404, "errors")
		closedOwner := a.do(&a.tm.Owner, "GET", "/api/chats/"+closed.String(), nil)
		a.expect(closedOwner, 200, "chat")
		if !strings.Contains(closedOwner.Raw, `"closed_reason":"rejected"`) || !strings.Contains(closedOwner.Raw, `"can_write":false`) {
			t.Errorf("closed header: %s", closedOwner.Raw)
		}
	})
	t.Run("messages", func(t *testing.T) {
		r := a.do(&sci, "GET", scientistPath+"/messages", nil)
		a.expect(r, 200, "messages")
		a.expect(a.do(&a.tm.Owner, "GET", scientistPath+"/messages?limit=5", nil), 200, "messages")
		a.expect(a.do(&sci, "GET", scientistPath+"/messages?before=5&limit=3", nil), 200, "messages")
		a.expect(a.do(&sci, "GET", scientistPath+"/messages?after=12", nil), 200, "messages")
		a.expect(a.do(&a.tm.Owner, "GET", "/api/chats/"+anonChat.String()+"/messages", nil), 200, "messages")
	})
	t.Run("send edit delete", func(t *testing.T) {
		body := map[string]any{"text": "Спасибо, 22-го в 11:00 мне удобно.", "client_id": uuid.NewString()}
		if errs := loadContract(t, "send").requestErrors(body); len(errs) > 0 {
			t.Fatalf("request body does not match send.json: %v", errs)
		}
		a.expect(a.do(&sci, "POST", scientistPath+"/messages", body), 201, "send")
		a.expect(a.do(&sci, "POST", scientistPath+"/messages", body), 200, "send") // повтор
		var made struct{ Message Message }
		r := a.do(&sci, "POST", scientistPath+"/messages", map[string]any{"text": "Вторая", "client_id": uuid.NewString()})
		a.expect(r, 201, "send")
		if err := json.Unmarshal([]byte(r.Raw), &made); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("%s/messages/%d", scientistPath, made.Message.Seq)
		a.expect(a.do(&sci, "PATCH", path, map[string]any{"text": "Вторая, исправленная"}), 200, "edit")
		a.expect(a.do(&sci, "DELETE", path, nil), 204, "delete")
		a.expect(a.do(&sci, "DELETE", path, nil), 204, "delete") // повтор
	})
	t.Run("small actions", func(t *testing.T) {
		a.expect(a.do(&sci, "POST", scientistPath+"/read", map[string]any{"up_to": 6}), 204, "read")
		a.expect(a.do(&sci, "POST", scientistPath+"/archive", nil), 204, "archive")
		a.expect(a.do(&sci, "POST", scientistPath+"/unarchive", nil), 204, "unarchive")
		a.expect(a.do(&sci, "POST", scientistPath+"/typing", nil), 204, "typing")
	})
	t.Run("errors", func(t *testing.T) {
		missing := "/api/chats/" + uuid.NewString()
		a.expectError(a.do(&sci, "GET", missing, nil), 404, "not_found")
		a.expectError(a.do(&sci, "GET", "/api/chats/not-a-uuid", nil), 404, "not_found")
		a.expectError(a.do(&a.tm.Out, "GET", scientistPath, nil), 404, "not_found")
		// Чужая и несуществующая отвечают одинаково до байта.
		foreign, absent := a.do(&a.tm.Out, "GET", scientistPath+"/messages", nil), a.do(&a.tm.Out, "GET", missing+"/messages", nil)
		if foreign.Code != absent.Code || foreign.Raw != absent.Raw {
			t.Errorf("foreign %d %s / absent %d %s", foreign.Code, foreign.Raw, absent.Code, absent.Raw)
		}
		a.expectError(a.do(&sci, "GET", "/api/chats?side=nobody", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "GET", "/api/chats", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "GET", "/api/chats?side=org&limit=0", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "GET", "/api/chats?side=org&limit=abc", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "GET", scientistPath+"/messages?before=x", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "GET", scientistPath+"/messages?before=3&after=2", nil), 400, "bad_request")
		a.expectError(a.do(&sci, "POST", scientistPath+"/read", map[string]any{}), 400, "bad_request")
		a.expectError(a.do(&sci, "POST", scientistPath+"/read", map[string]any{"up_to": -4}), 400, "bad_request")
		a.expectError(a.do(&sci, "POST", scientistPath+"/messages", `{"text": "а", "client_id": "`+uuid.NewString()+`", "extra": 1}`), 400, "bad_request")
		r := a.do(&sci, "POST", scientistPath+"/messages", map[string]any{"text": " ", "client_id": uuid.NewString()})
		a.expectError(r, 422, "validation_failed")
		if f := r.Fields(t); f["text"] == nil {
			t.Errorf("fields = %v", f)
		}
		a.expectError(a.do(&sci, "POST", scientistPath+"/messages", map[string]any{"text": "Привет"}), 422, "validation_failed")
		// Строка о событии, чужое сообщение и закрытая переписка.
		a.expectError(a.do(&sci, "PATCH", scientistPath+"/messages/1", map[string]any{"text": "Событие"}), 409, "system_message")
		a.expectError(a.do(&sci, "DELETE", scientistPath+"/messages/1", nil), 409, "system_message")
		orgSeq := a.lastTextSeq(chat, a.tm.HR)
		a.expectError(a.do(&sci, "PATCH", fmt.Sprintf("%s/messages/%d", scientistPath, orgSeq), map[string]any{"text": "Чужое"}), 403, "not_author")
		a.expectError(a.do(&a.tm.Owner, "DELETE", fmt.Sprintf("%s/messages/%d", scientistPath, orgSeq), nil), 403, "not_author")
		a.expectError(a.do(&sci, "PATCH", scientistPath+"/messages/9999", map[string]any{"text": "Нет"}), 404, "not_found")
		a.expectError(a.do(&sci, "PATCH", scientistPath+"/messages/abc", map[string]any{"text": "Нет"}), 404, "not_found")
		a.expectError(a.do(&a.tm.Out, "PATCH", scientistPath+"/messages/abc", map[string]any{"text": "Нет"}), 404, "not_found")
		own := a.send(sci, chat, "Скоро будет поздно")
		a.clock.Advance(EditWindow + time.Minute)
		a.expectError(a.do(&sci, "PATCH", fmt.Sprintf("%s/messages/%d", scientistPath, own.Seq), map[string]any{"text": "Поздно"}), 409, "edit_window_closed")
		a.expectError(a.do(&sci, "DELETE", fmt.Sprintf("%s/messages/%d", scientistPath, own.Seq), nil), 409, "edit_window_closed")
		closedPath := "/api/chats/" + closed.String()
		a.expectError(a.do(&a.tm.Owner, "POST", closedPath+"/messages", map[string]any{"text": "Ещё слово", "client_id": uuid.NewString()}), 409, "chat_closed")
		a.expectError(a.do(&a.tm.Owner, "POST", closedPath+"/typing", nil), 409, "chat_closed")
	})
}

// lastTextSeq — номер последнего текста этого человека в переписке.
func (a *apiWorld) lastTextSeq(chat uuid.UUID, who testkit.Person) int {
	a.T.Helper()
	var seq int
	if err := testkit.Pool.QueryRow(bg, `SELECT max(seq) FROM chat_messages WHERE chat_id = $1 AND author_user_id = $2`, chat, who.ID).Scan(&seq); err != nil {
		a.T.Fatal(err)
	}
	return seq
}

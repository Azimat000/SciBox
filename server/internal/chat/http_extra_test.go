package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scibox/server/internal/testkit"
)

// Негодный номер переписки или сообщения даёт тот же 404, что и чужая переписка.
func TestHTTPBadIdentifiers(t *testing.T) {
	a := newAPI(t)
	sci := a.Applicant("Анна Ильина")
	chat := a.chatOf(sci, a.vacancy.ID, "sent")
	body := map[string]any{"text": "Привет", "client_id": uuid.NewString(), "up_to": 1}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/chats/12345"}, {"GET", "/api/chats/12345/messages"}, {"POST", "/api/chats/12345/messages"},
		{"PATCH", "/api/chats/12345/messages/1"}, {"DELETE", "/api/chats/12345/messages/1"}, {"POST", "/api/chats/12345/read"},
		{"POST", "/api/chats/12345/archive"}, {"POST", "/api/chats/12345/unarchive"}, {"POST", "/api/chats/12345/typing"},
	} {
		a.expectError(a.do(&sci, c.method, c.path, body), 404, "not_found")
	}
	own := "/api/chats/" + chat.String() + "/messages/"
	for _, seq := range []string{"0", "-1", "abc", "1.5", "99999999999999999999"} {
		a.expectError(a.do(&sci, "PATCH", own+seq, map[string]any{"text": "Правка"}), 404, "not_found")
		a.expectError(a.do(&sci, "DELETE", own+seq, nil), 404, "not_found")
	}
	// Параметры, которых нет в списке и ленте.
	a.expectError(a.do(&sci, "GET", "/api/chats?side=scientist&vacancy=нет", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats?side=scientist&box=trash", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats?side=scientist&cursor=not-a-cursor!", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats/"+chat.String()+"/messages?limit=0", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats/"+chat.String()+"/messages?limit=abc", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats/"+chat.String()+"/messages?limit=101", nil), 400, "bad_request")
	a.expectError(a.do(&sci, "GET", "/api/chats/"+chat.String()+"/messages?after=x", nil), 400, "bad_request")
	if e := (&BadParamError{Param: "side"}); e.Error() == "" {
		t.Error("empty error text")
	}
	// Тело, которое не JSON.
	a.expectError(a.do(&sci, "PATCH", own+"1", "{не json"), 400, "bad_request")
	a.expectError(a.do(&sci, "POST", "/api/chats/"+chat.String()+"/read", "{не json"), 400, "bad_request")
	// Плохой текст в правке: сначала права, потом проверка.
	a.expectError(a.do(&a.tm.Out, "PATCH", own+"1", map[string]any{"text": " "}), 404, "not_found")
	a.expectError(a.do(&sci, "PATCH", own+"1", map[string]any{"text": " "}), 422, "validation_failed")
}

// Сбой базы доходит до человека как 500, а не как «нет такой переписки».
func TestHTTPDatabaseDown(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	pool, err := pgxpool.New(context.Background(), testkit.Pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close() // закрытый пул отвечает ошибкой на любое обращение
	svc := NewService(pool)
	api := testkit.NewAPI(w.World, func(r chi.Router, requireUser func(http.Handler) http.Handler) {
		NewHandler(svc, w.Log, requireUser).Mount(r)
	})
	id := "/api/chats/" + chat.String()
	send := map[string]any{"text": "Привет", "client_id": uuid.NewString()}
	edit := map[string]any{"text": "Правка"}
	read := map[string]any{"up_to": 1}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/chats?side=scientist", nil}, {"GET", "/api/chats/unread-count", nil}, {"GET", "/api/chats/vacancies", nil},
		{"GET", id, nil}, {"GET", id + "/messages", nil}, {"POST", id + "/messages", send}, {"PATCH", id + "/messages/1", edit},
		{"DELETE", id + "/messages/1", nil}, {"POST", id + "/read", read}, {"POST", id + "/archive", nil},
		{"POST", id + "/unarchive", nil}, {"POST", id + "/typing", nil}, {"PATCH", id + "/messages/abc", edit},
	} {
		r := api.Do(&sci, c.method, c.path, c.body)
		if r.Code < 500 {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
}

// Вакансия на всю организацию (без подразделения) и без срока подачи.
func TestChatOfAVacancyWithoutUnitAndDeadline(t *testing.T) {
	w := newWorld(t)
	whole := w.tm.Published(nil)
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET deadline = NULL WHERE id = $1`, whole.ID); err != nil {
		t.Fatal(err)
	}
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, whole.ID, "sent")
	for _, who := range []testkit.Person{w.tm.Owner, w.tm.HR, sci} {
		hdr, err := w.svc.Get(bg, who.User, chat)
		if err != nil {
			t.Fatalf("%s: %v", who.Name, err)
		}
		if hdr.Vacancy.Deadline != nil || hdr.Vacancy.UnitName != "" || hdr.Vacancy.Status != "published" {
			t.Errorf("header = %+v", hdr.Vacancy)
		}
	}
	// Руководитель подразделения вакансию на всю организацию не ведёт.
	for _, who := range []testkit.Person{w.tm.HeadA, w.tm.HeadB} {
		if _, err := w.svc.Get(bg, who.User, chat); !errors.Is(err, ErrNotFound) {
			t.Errorf("head: %v", err)
		}
	}
	// Права на правку шаблонов: владелец и кадровик, не руководитель.
	owner, _ := w.svc.Get(bg, w.tm.Owner.User, chat)
	hr, _ := w.svc.Get(bg, w.tm.HR.User, chat)
	if !owner.Viewer.CanEditTemplates || !hr.Viewer.CanEditTemplates || !owner.Viewer.CanUseTemplates || owner.Viewer.OrgID == nil || *owner.Viewer.OrgID != w.tm.OrgID {
		t.Errorf("viewer of the staff = %+v / %+v", owner.Viewer, hr.Viewer)
	}
	head, err := w.svc.Get(bg, w.tm.HeadA.User, w.chatOf(w.Applicant("Борис"), w.vacancy.ID, "sent"))
	if err != nil || head.Viewer.CanEditTemplates || !head.Viewer.CanUseTemplates || head.Viewer.CanBlock || head.Viewer.CanComplain {
		t.Errorf("viewer of the head = %+v, %v", head.Viewer, err)
	}
	mine, _ := w.svc.Get(bg, sci.User, chat)
	if !mine.Viewer.CanBlock || !mine.Viewer.CanComplain || mine.Viewer.CanUseTemplates || mine.Viewer.CanEditTemplates || mine.Viewer.OrgID != nil || mine.Viewer.Side != SideScientist {
		t.Errorf("viewer of the scientist = %+v", mine.Viewer)
	}
}

func TestAppendSystemRefusesUnserializableData(t *testing.T) {
	w := newWorld(t)
	chat := w.chatOf(w.Applicant("Анна Ильина"), w.vacancy.ID, "sent")
	if _, err := AppendSystem(bg, w.q(), chat, EventApplicationSent, make(chan int), w.clock.Now()); err == nil {
		t.Fatal("a channel was accepted as event data")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, chat); n != 0 {
		t.Errorf("messages = %d", n)
	}
}

// К переписке привязали отклик, пока отправка ждала замок: замки берутся заново и состояние читается по новому отклику.
func TestLockStateFollowsARelink(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	offer := w.offer(sci, w.vacancy.ID, "pending")
	chat := w.open(sci, w.vacancy.ID, nil, &offer)
	rejected := w.application(sci, w.vacancy.ID, "rejected")

	db, _ := testkit.Faulty(testkit.Pool, 0)
	relinked := false
	db.Hook = func(sql string) {
		if !relinked && hasAll(sql, "FROM chats WHERE id", "FOR UPDATE") {
			relinked = true
			if _, err := testkit.Pool.Exec(bg, `UPDATE chats SET application_id = $2 WHERE id = $1`, chat, rejected); err != nil {
				t.Error(err)
			}
		}
	}
	svc := svcOn(w, db)
	_, _, err := svc.Send(bg, sci.User, chat, SendInput{Text: "Привет", ClientID: uuid.NewString()})
	if !relinked {
		t.Fatal("the hook did not fire")
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("Send = %v, want ErrClosed: the chat follows the rejected application now", err)
	}
}

func hasAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

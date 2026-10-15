package erasure_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/chat"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

// Переписки учёного стираются вместе с откликами, у сотрудника остаются сообщения без автора, личные отметки стираются
// (T-103, срез 37). Чужие переписки не трогаются.
func TestEraseHandlesChats(t *testing.T) {
	w := newWorld(t)
	svc := chat.NewService(testkit.Pool)
	who := w.Applicant("Удаляемый Учёный")
	other := w.Applicant("Другой Учёный")
	q := dbgen.New(testkit.Pool)
	now := time.Now().UTC()

	open := func(p testkit.Person) uuid.UUID {
		t.Helper()
		app := w.apply(p, w.vac.ID)
		id, err := chat.Open(bg, q, chat.OpenInput{VacancyID: w.vac.ID, UserID: p.ID, ApplicationID: &app.ID}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := chat.AppendSystem(bg, q, id, chat.EventApplicationSent, map[string]any{"application_id": app.ID}, now); err != nil {
			t.Fatal(err)
		}
		return id
	}
	say := func(p testkit.Person, c uuid.UUID, text string) {
		t.Helper()
		if _, _, err := svc.Send(bg, p.User, c, chat.SendInput{Text: text, ClientID: uuid.NewString()}); err != nil {
			t.Fatal(err)
		}
	}
	mine, theirs := open(who), open(other)
	for _, c := range []uuid.UUID{mine, theirs} {
		say(w.tm.HR, c, "Здравствуйте, это кадровик")
	}
	say(who, mine, "Ответ учёного")
	if err := svc.MarkRead(bg, who.User, mine, 3); err != nil {
		t.Fatal(err)
	}
	for _, c := range []uuid.UUID{mine, theirs} {
		if err := svc.MarkRead(bg, w.tm.HR.User, c, 2); err != nil {
			t.Fatal(err)
		}
		if err := svc.Archive(bg, w.tm.Owner.User, c, true); err != nil {
			t.Fatal(err)
		}
	}

	// Уходит сотрудник: его сообщения остаются без автора, его отметки стёрты, отметки коллеги целы.
	if err := erase(t, w.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []uuid.UUID{mine, theirs} {
		if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND kind = 'text' AND author_side = 'org' AND author_user_id IS NULL`, c); n != 1 {
			t.Errorf("messages of the erased employee without an author = %d", n)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM chat_participants WHERE chat_id = $1 AND user_id = $2`, c, w.tm.HR.ID); n != 0 {
			t.Errorf("read marks of the erased employee = %d", n)
		}
		if n := testkit.Count(t, `SELECT count(*) FROM chat_participants WHERE chat_id = $1 AND user_id = $2 AND archived`, c, w.tm.Owner.ID); n != 1 {
			t.Errorf("the archive of a colleague = %d", n)
		}
	}
	page, err := svc.Messages(bg, who.User, mine, chat.MessagesQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, m := range page.Items {
		if m.Kind == chat.KindText && m.Author != nil && m.Author.Side == chat.SideOrg {
			seen = true
			if m.Author.Name != "" || !m.Author.Former {
				t.Errorf("author of the erased employee = %+v", m.Author)
			}
		}
	}
	if !seen {
		t.Error("the message of the erased employee is gone")
	}

	// Уходит учёный: его переписка стёрта целиком, чужая цела.
	if err := erase(t, who.ID); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE id = $1`, mine); n != 0 {
		t.Errorf("the chat of the erased scientist = %d", n)
	}
	for _, table := range []string{"chat_messages", "chat_participants"} {
		if n := testkit.Count(t, `SELECT count(*) FROM `+table+` WHERE chat_id = $1`, mine); n != 0 {
			t.Errorf("%s of the erased chat = %d", table, n)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, theirs); n != 2 {
		t.Errorf("messages of another chat = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_participants WHERE chat_id = $1 AND user_id = $2`, theirs, w.tm.Owner.ID); n != 1 {
		t.Errorf("marks of another chat = %d", n)
	}
	// Текст сообщений в базе не хранит ни имени, ни почты стёртого: тело писал сам человек, оно ушло вместе с перепиской.
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE body LIKE '%Ответ учёного%'`); n != 0 {
		t.Errorf("text of the erased scientist = %d", n)
	}
}

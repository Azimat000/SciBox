package erasure_test

import (
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/chat"
	"scibox/server/internal/testkit"
)

// Переписки учёного стираются вместе с откликами, у сотрудника остаются сообщения без автора, личные отметки стираются
// (T-103, срез 37). Чужие переписки не трогаются.
func TestEraseHandlesChats(t *testing.T) {
	w := newWorld(t)
	svc := chat.NewService(testkit.Pool)
	who := w.Applicant("Удаляемый Учёный")
	other := w.Applicant("Другой Учёный")
	open := func(p testkit.Person) uuid.UUID {
		t.Helper()
		// Настоящий отклик сам заводит переписку и пишет в неё строку «отклик отправлен» (срез 38).
		app := w.apply(p, w.vac.ID)
		var id uuid.UUID
		if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, app.ID).Scan(&id); err != nil {
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
	// Журнал живого потока (срез 39) не хранит номер стёртого ни как учёного переписки, ни как читавшего.
	if n := testkit.Count(t, `SELECT count(*) FROM chat_events WHERE user_id = $1 OR actor_id = $1`, who.ID); n != 0 {
		t.Errorf("journal rows of the erased scientist = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_events WHERE chat_id = $1`, theirs); n == 0 {
		t.Error("journal rows of another chat must stay")
	}
	// Текст сообщений в базе не хранит ни имени, ни почты стёртого: тело писал сам человек, оно ушло вместе с перепиской.
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE body LIKE '%Ответ учёного%'`); n != 0 {
		t.Errorf("text of the erased scientist = %d", n)
	}
}

// Блокировки и жалобы человека уходят с аккаунтом; автор шаблона очищается, сам шаблон остаётся организации (T-103).
func TestEraseHandlesChatTools(t *testing.T) {
	w := newWorld(t)
	svc := chat.NewService(testkit.Pool)
	who := w.Applicant("Жалобщик")
	app := w.apply(who, w.vac.ID)
	var c uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, app.ID).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complain(bg, who.User, c, chat.ComplaintInput{Reason: "spam", Note: "Пишут не по делу"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Block(bg, who.User, c); err != nil {
		t.Fatal(err)
	}
	// Блокировка и жалоба другого человека не затрагиваются.
	bystander := w.Applicant("Сторонний")
	other := w.apply(bystander, w.vac.ID)
	var oc uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, other.ID).Scan(&oc); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Block(bg, bystander.User, oc); err != nil {
		t.Fatal(err)
	}
	tpl, err := svc.CreateTemplate(bg, w.tm.HR.User, w.tm.OrgID, chat.TemplateInput{Title: "Шаблон кадровика", Text: "Добрый день!"})
	if err != nil {
		t.Fatal(err)
	}

	if err := erase(t, who.ID); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_blocks WHERE user_id = $1`, who.ID); n != 0 {
		t.Errorf("blocks of the erased person = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_complaints WHERE user_id = $1`, who.ID); n != 0 {
		t.Errorf("complaints of the erased person = %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_blocks WHERE user_id = $1`, bystander.ID); n != 1 {
		t.Errorf("block of another person = %d", n)
	}
	if err := erase(t, w.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	var author *uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT created_by FROM chat_templates WHERE id = $1`, tpl.ID).Scan(&author); err != nil {
		t.Fatalf("the template went with its author: %v", err)
	}
	if author != nil {
		t.Errorf("author of the template = %v", author)
	}
}

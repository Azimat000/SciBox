package chat

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
)

// snapshot — всё, что действия в переписке могут изменить.
type snapshot struct{ messages, participants, lastSeq, archived, scientistRead, orgRead int }

func (w *world) snap(chat uuid.UUID) snapshot {
	w.T.Helper()
	s := snapshot{
		messages:     testkit.Count(w.T, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, chat),
		participants: testkit.Count(w.T, `SELECT count(*) FROM chat_participants WHERE chat_id = $1`, chat),
		archived:     testkit.Count(w.T, `SELECT count(*) FROM chat_participants WHERE chat_id = $1 AND archived`, chat),
	}
	err := testkit.Pool.QueryRow(bg, `SELECT last_seq, scientist_read, org_read FROM chats WHERE id = $1`, chat).Scan(&s.lastSeq, &s.scientistRead, &s.orgRead)
	if err != nil {
		w.T.Fatal(err)
	}
	return s
}

// Кто какое действие в переписке вправе делать: учёный и сотрудники, которые ведут отклики этой вакансии, да; остальные
// получают «нет такой переписки» и ничего не меняют (T-95, T-96).
func TestWhoCanDoWhat(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	stranger := w.Applicant("Чужой Учёный")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	w.system(chat, EventApplicationSent, map[string]any{"application_id": uuid.New(), "cover_letter": "Письмо"})

	ops := []struct {
		name string
		do   func(who testkit.Person) error
	}{
		{"get", func(who testkit.Person) error { _, err := w.svc.Get(bg, who.User, chat); return err }},
		{"messages", func(who testkit.Person) error {
			_, err := w.svc.Messages(bg, who.User, chat, MessagesQuery{})
			return err
		}},
		{"send", func(who testkit.Person) error {
			_, _, err := w.svc.Send(bg, who.User, chat, SendInput{Text: "Здравствуйте", ClientID: uuid.NewString()})
			return err
		}},
		{"read", func(who testkit.Person) error { return w.svc.MarkRead(bg, who.User, chat, 1) }},
		{"archive", func(who testkit.Person) error { return w.svc.Archive(bg, who.User, chat, true) }},
		{"unarchive", func(who testkit.Person) error { return w.svc.Archive(bg, who.User, chat, false) }},
		{"typing", func(who testkit.Person) error { return w.svc.Typing(bg, who.User, chat) }},
		{"edit", func(who testkit.Person) error {
			_, err := w.svc.Edit(bg, who.User, chat, 2, EditInput{Text: "Исправлено"})
			return err
		}},
		{"delete", func(who testkit.Person) error { return w.svc.Delete(bg, who.User, chat, 2) }},
	}
	type person struct {
		name    string
		who     testkit.Person
		allowed bool
	}
	crowd := []person{
		{"учёный", sci, true}, {"владелец", w.tm.Owner, true}, {"кадровик", w.tm.HR, true}, {"руководитель А", w.tm.HeadA, true},
		{"руководитель Б", w.tm.HeadB, false}, {"посторонний", w.tm.Out, false}, {"чужой учёный", stranger, false},
	}
	for _, op := range ops {
		for _, p := range crowd {
			t.Run(op.name+"/"+p.name, func(t *testing.T) {
				// Сообщение 2 пишет кто-нибудь заново: правка и удаление проверяются на чужом тексте.
				w.send(sci, chat, "Сообщение учёного")
				before := w.snap(chat)
				err := op.do(p.who)
				switch {
				case p.allowed && (op.name == "edit" || op.name == "delete") && p.name != "учёный":
					// Участник, но не автор: отдельный отказ, а не «нет такой переписки».
					if !errors.Is(err, ErrNotAuthor) {
						t.Errorf("err = %v, want ErrNotAuthor", err)
					}
				case p.allowed:
					if err != nil {
						t.Errorf("err = %v", err)
					}
				default:
					if !errors.Is(err, ErrNotFound) {
						t.Errorf("err = %v, want ErrNotFound", err)
					}
					if after := w.snap(chat); after != before {
						t.Errorf("a refused action changed the chat: %+v -> %+v", before, after)
					}
				}
			})
		}
	}
}

// Чужая и несуществующая переписка неотличимы (T-95).
func TestMissingChatLooksLikeForeign(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	for _, who := range []testkit.Person{w.tm.Out, w.tm.HeadB} {
		_, errForeign := w.svc.Get(bg, who.User, chat)
		_, errMissing := w.svc.Get(bg, who.User, uuid.New())
		if !errors.Is(errForeign, ErrNotFound) || errForeign.Error() != errMissing.Error() {
			t.Errorf("foreign: %v, missing: %v", errForeign, errMissing)
		}
	}
}

// Потерял роль, потерял переписку: права проверяются при каждом запросе (T-96).
func TestLosingTheRoleLosesTheChat(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	if _, err := w.svc.Get(bg, w.tm.HR.User, chat); err != nil {
		t.Fatal(err)
	}
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, w.tm.OrgID, w.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, w.tm.HR.User, chat); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after leaving = %v", err)
	}
	if _, _, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "Ещё я", ClientID: uuid.NewString()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Send after leaving = %v", err)
	}
	// Руководитель подразделения теряет доступ, когда перестаёт руководить им.
	if _, err := w.svc.Get(bg, w.tm.HeadA.User, chat); err != nil {
		t.Fatal(err)
	}
	if _, err := w.tm.W.Orgs.SetUnitHead(bg, w.tm.Owner.User, w.tm.Slug, w.tm.UnitA.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, w.tm.HeadA.User, chat); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after the head was removed = %v", err)
	}
}

// Отправка: подпись, обе стороны видят одно и то же, счётчики адресатов (T-101).
func TestSendAndRepeat(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	client := uuid.NewString()

	m, created, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "  Добрый день!\n", ClientID: client})
	if err != nil || !created {
		t.Fatalf("Send = %v, created %v", err, created)
	}
	if m.Seq != 1 || m.Text != "Добрый день!" || m.Kind != KindText || m.Author == nil ||
		m.Author.Side != SideOrg || !m.Author.IsMe || m.Author.Name != w.tm.HR.Name || m.Author.Former || m.Author.Anonymous {
		t.Errorf("message = %+v author %+v", m, m.Author)
	}
	if m.EditableUntil == nil || !m.EditableUntil.Equal(m.CreatedAt.Add(EditWindow)) || m.ClientID == nil || m.ClientID.String() != client {
		t.Errorf("editable until %v client %v", m.EditableUntil, m.ClientID)
	}
	if m.ID != messageID(chat, 1) {
		t.Errorf("id = %v", m.ID)
	}

	// Повтор: то же сообщение, ничего нового.
	again, created, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "Другой текст", ClientID: client})
	if err != nil || created || again.Seq != 1 || again.Text != "Добрый день!" {
		t.Errorf("repeat = %+v, created %v, err %v", again, created, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, chat); n != 1 {
		t.Errorf("messages = %d", n)
	}

	// Тот же номер у другого человека или в другой переписке — не повтор.
	other := w.chatOf(w.Applicant("Борис"), w.vacancy.ID, "sent")
	if m2, created, err := w.svc.Send(bg, w.tm.HR.User, other, SendInput{Text: "Здравствуйте", ClientID: client}); err != nil || !created || m2.Seq != 1 {
		t.Errorf("same client id in another chat = %+v, created %v, err %v", m2, created, err)
	}
	if m3, created, err := w.svc.Send(bg, sci.User, chat, SendInput{Text: "Ответ", ClientID: client}); err != nil || !created || m3.Seq != 2 {
		t.Errorf("same client id from another person = %+v, created %v, err %v", m3, created, err)
	}

	// Собеседник видит чужое сообщение без номера отправки и без срока правки.
	page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("messages = %+v, %v", page, err)
	}
	first := page.Items[0]
	if first.Author.IsMe || first.ClientID != nil || first.EditableUntil != nil || first.Author.Name != w.tm.HR.Name {
		t.Errorf("seen by the scientist: %+v author %+v", first, first.Author)
	}
	// Коллега автора тоже не «я» и править не может.
	colleague, err := w.svc.Messages(bg, w.tm.Owner.User, chat, MessagesQuery{})
	if err != nil || colleague.Items[0].Author.IsMe || colleague.Items[0].EditableUntil != nil || colleague.Items[0].ClientID != nil {
		t.Errorf("seen by the colleague: %+v, %v", colleague.Items[0], err)
	}
}

func TestSendValidation(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	cases := []struct {
		name  string
		in    SendInput
		field string
	}{
		{"empty text", SendInput{Text: "  ", ClientID: uuid.NewString()}, "text"},
		{"too long", SendInput{Text: strings.Repeat("я", MaxText+1), ClientID: uuid.NewString()}, "text"},
		{"NUL", SendInput{Text: "а\x00б", ClientID: uuid.NewString()}, "text"},
		{"no client id", SendInput{Text: "Привет"}, "client_id"},
		{"bad client id", SendInput{Text: "Привет", ClientID: "не номер"}, "client_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := w.snap(chat)
			_, _, err := w.svc.Send(bg, sci.User, chat, c.in)
			verr, ok := errors.AsType[*auth.ValidationError](err)
			if !ok || verr.Fields[c.field] == "" {
				t.Fatalf("err = %v", err)
			}
			if w.snap(chat) != before {
				t.Error("an invalid message changed the chat")
			}
		})
	}
	// Предел ровно в 4000 знаков проходит.
	if _, _, err := w.svc.Send(bg, sci.User, chat, SendInput{Text: strings.Repeat("я", MaxText), ClientID: uuid.NewString()}); err != nil {
		t.Errorf("4000 characters: %v", err)
	}
	// Чужой не узнаёт о переписке по проверке текста.
	_, _, err := w.svc.Send(bg, w.tm.Out.User, chat, SendInput{Text: "", ClientID: ""})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("validation before rights: %v", err)
	}
}

// Закрытие следует из состояния отклика и приглашения: писать, править, удалять и печатать нельзя, история видна (T-106).
func TestClosedChatsRefuseWriting(t *testing.T) {
	cases := []struct {
		name         string
		app, offer   string
		closed       bool
		reason       string
		sourceStatus string
	}{
		{name: "application sent", app: "sent"},
		{name: "application viewed", app: "viewed"},
		{name: "application invited", app: "invited"},
		{name: "application accepted", app: "accepted"},
		{name: "application rejected", app: "rejected", closed: true, reason: ClosedRejected},
		{name: "application withdrawn", app: "withdrawn", closed: true, reason: ClosedWithdrawn},
		{name: "offer pending", offer: "pending"},
		{name: "offer interested", offer: "interested"},
		{name: "offer declined", offer: "declined", closed: true, reason: ClosedOfferDeclined},
		{name: "offer cancelled", offer: "cancelled", closed: true, reason: ClosedOfferCancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			sci := w.Applicant("Анна Ильина")
			var app, offer *uuid.UUID
			if c.app != "" {
				id := w.application(sci, w.vacancy.ID, "sent")
				app = &id
			}
			if c.offer != "" {
				id := w.offer(sci, w.vacancy.ID, "pending")
				offer = &id
			}
			chat := w.open(sci, w.vacancy.ID, app, offer)
			// Сообщение написано, пока переписка была открыта.
			mine := w.send(sci, chat, "Ещё открыто")
			clientRepeat := uuid.NewString()
			first, _, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "Первое", ClientID: clientRepeat})
			if err != nil {
				t.Fatal(err)
			}
			if app != nil {
				w.setApplication(*app, c.app)
			} else {
				w.setOffer(*offer, c.offer)
			}

			// История видна и в закрытой переписке.
			if got := w.texts(sci, chat); len(got) != 2 || got[0] != "Ещё открыто" || got[1] != "Первое" {
				t.Errorf("history = %v", got)
			}
			hdr, err := w.svc.Get(bg, sci.User, chat)
			if err != nil {
				t.Fatal(err)
			}
			if hdr.CanWrite == c.closed || (hdr.ClosedReason != nil) != c.closed || (c.closed && *hdr.ClosedReason != c.reason) {
				t.Errorf("header can_write %v reason %v", hdr.CanWrite, hdr.ClosedReason)
			}
			before := w.snap(chat)
			sendErr := func(who testkit.Person) error {
				_, _, err := w.svc.Send(bg, who.User, chat, SendInput{Text: "Новое", ClientID: uuid.NewString()})
				return err
			}
			editErr := func() error {
				_, err := w.svc.Edit(bg, sci.User, chat, int(mine.Seq), EditInput{Text: "Поправка"})
				return err
			}
			deleteErr := func() error { return w.svc.Delete(bg, sci.User, chat, int(mine.Seq)) }
			typingErr := func() error { return w.svc.Typing(bg, w.tm.Owner.User, chat) }
			for name, got := range map[string]error{"send by the scientist": sendErr(sci), "send by the organization": sendErr(w.tm.HR),
				"edit": editErr(), "delete": deleteErr(), "typing": typingErr()} {
				if c.closed != errors.Is(got, ErrClosed) || (!c.closed && got != nil) {
					t.Errorf("%s: err = %v (closed: %v)", name, got, c.closed)
				}
			}
			// Повтор отправки уже отправленного отдаёт то же сообщение и в закрытой переписке.
			rep, created, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "Первое", ClientID: clientRepeat})
			if err != nil || created || rep.Seq != first.Seq {
				t.Errorf("repeat in the chat: %+v, created %v, err %v", rep, created, err)
			}
			if c.closed {
				if after := w.snap(chat); after != before {
					t.Errorf("a refused action changed the closed chat: %+v -> %+v", before, after)
				}
			}
			// Отметка и архив работают и в закрытой переписке.
			if err := w.svc.MarkRead(bg, sci.User, chat, 1); err != nil {
				t.Errorf("read: %v", err)
			}
			if err := w.svc.Archive(bg, sci.User, chat, true); err != nil {
				t.Errorf("archive: %v", err)
			}
		})
	}
}

// Правка и удаление: срок ровно сутки, чужое, строка о событии (T-101).
func TestEditAndDelete(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	sys := w.system(chat, EventApplicationViewed, nil)
	orgMsg := w.send(w.tm.HR, chat, "Текст организации")
	mine := w.send(sci, chat, "Мой текст")

	// Правка: новый текст, пометка, время.
	w.clock.Advance(time.Minute)
	edited, err := w.svc.Edit(bg, sci.User, chat, mine.Seq, EditInput{Text: "  Мой исправленный текст "})
	if err != nil || edited.Text != "Мой исправленный текст" || edited.EditedAt == nil || !edited.EditedAt.Equal(w.clock.Now()) {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	if !edited.CreatedAt.Equal(mine.CreatedAt) || edited.ClientID == nil {
		t.Errorf("edit changed the original: %+v", edited)
	}
	// Тот же текст не отмечается «изменено» заново.
	w.clock.Advance(time.Minute)
	same, err := w.svc.Edit(bg, sci.User, chat, mine.Seq, EditInput{Text: "Мой исправленный текст"})
	if err != nil || same.EditedAt == nil || !same.EditedAt.Equal(edited.EditedAt.UTC()) {
		t.Errorf("repeat edit = %+v, %v", same, err)
	}

	// Не своё, строка о событии, несуществующее.
	if _, err := w.svc.Edit(bg, sci.User, chat, orgMsg.Seq, EditInput{Text: "Чужое"}); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("edit of a foreign message = %v", err)
	}
	if _, err := w.svc.Edit(bg, w.tm.Owner.User, chat, orgMsg.Seq, EditInput{Text: "Коллеги"}); !errors.Is(err, ErrNotAuthor) {
		t.Errorf("edit of a colleague's message = %v", err)
	}
	if _, err := w.svc.Edit(bg, sci.User, chat, sys, EditInput{Text: "Событие"}); !errors.Is(err, ErrSystemMessage) {
		t.Errorf("edit of a system line = %v", err)
	}
	if err := w.svc.Delete(bg, sci.User, chat, sys); !errors.Is(err, ErrSystemMessage) {
		t.Errorf("delete of a system line = %v", err)
	}
	if _, err := w.svc.Edit(bg, sci.User, chat, 99, EditInput{Text: "Нет такого"}); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("edit of a missing message = %v", err)
	}
	if err := w.svc.Delete(bg, sci.User, chat, 99); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("delete of a missing message = %v", err)
	}
	if _, err := w.svc.Edit(bg, sci.User, chat, mine.Seq, EditInput{Text: "   "}); err == nil {
		t.Error("edit to empty text must fail")
	}

	// Граница суток: за секунду до срока можно, секундой позже нельзя.
	w.clock.Advance(EditWindow - 2*time.Minute - time.Second)
	if now := w.clock.Now(); now.After(mine.CreatedAt.Add(EditWindow)) {
		t.Fatal("test clock is already past the window")
	}
	if _, err := w.svc.Edit(bg, sci.User, chat, mine.Seq, EditInput{Text: "Ещё успел"}); err != nil {
		t.Errorf("edit just before the end of the window: %v", err)
	}
	w.clock.Advance(2 * time.Second)
	if _, err := w.svc.Edit(bg, sci.User, chat, mine.Seq, EditInput{Text: "Поздно"}); !errors.Is(err, ErrEditWindowClosed) {
		t.Errorf("edit after the window = %v", err)
	}
	if err := w.svc.Delete(bg, sci.User, chat, mine.Seq); !errors.Is(err, ErrEditWindowClosed) {
		t.Errorf("delete after the window = %v", err)
	}

	// Удаление: текст стёрт совсем, пометка остаётся у обеих сторон, повтор ничего не меняет.
	fresh := w.send(sci, chat, "Секретный пароль 12345")
	if err := w.svc.Delete(bg, sci.User, chat, fresh.Seq); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Delete(bg, sci.User, chat, fresh.Seq); err != nil {
		t.Errorf("second delete = %v", err)
	}
	for _, who := range []testkit.Person{sci, w.tm.HR} {
		page, err := w.svc.Messages(bg, who.User, chat, MessagesQuery{After: new(fresh.Seq - 1)})
		if err != nil || len(page.Items) != 1 || !page.Items[0].Deleted || page.Items[0].Text != "" || page.Items[0].EditableUntil != nil || page.Items[0].ClientID != nil {
			t.Errorf("deleted message seen by %s: %+v, %v", who.Name, page.Items, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND body LIKE '%12345%'`, chat); n != 0 {
		t.Error("the text of a deleted message is still in the database")
	}
	if _, err := w.svc.Edit(bg, sci.User, chat, fresh.Seq, EditInput{Text: "Воскресить"}); !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("edit of a deleted message = %v", err)
	}
}

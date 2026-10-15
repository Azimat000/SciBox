package chat

import (
	"errors"
	"testing"
	"time"

	"scibox/server/internal/testkit"
)

// Сценарий: строки о событии адресованы тому, кто их не совершал; текст адресован противоположной стороне.
func (w *world) conversation(sci testkit.Person) (chat string, last int) {
	w.T.Helper()
	id := w.chatOf(sci, w.vacancy.ID, "sent")
	w.system(id, EventApplicationSent, map[string]any{"cover_letter": "Письмо"}) // 1: организации
	w.clock.Advance(time.Minute)
	w.system(id, EventApplicationViewed, nil) // 2: учёному
	w.clock.Advance(time.Minute)
	w.send(w.tm.HR, id, "Добрый день") // 3: учёному
	w.clock.Advance(time.Minute)
	w.send(sci, id, "Здравствуйте") // 4: организации
	return id.String(), 4
}

func TestReadMarksAndUnread(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	cs, last := w.conversation(sci)
	chat := uuidOf(t, cs)

	unread := func(who testkit.Person, side Side) int {
		t.Helper()
		l, err := w.svc.List(bg, who.User, ListQuery{Side: string(side)})
		if err != nil || len(l.Items) != 1 {
			t.Fatalf("list of %s: %+v, %v", who.Name, l, err)
		}
		return l.Items[0].Unread
	}
	read := func(who testkit.Person) Read {
		t.Helper()
		c, err := w.svc.Get(bg, who.User, chat)
		if err != nil {
			t.Fatal(err)
		}
		return c.Read
	}

	if got := unread(sci, SideScientist); got != 2 {
		t.Errorf("scientist unread = %d, want 2 (viewed line and the text of the organization)", got)
	}
	for _, who := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA} {
		if got := unread(who, SideOrg); got != 2 {
			t.Errorf("%s unread = %d, want 2 (the application line and the text of the scientist)", who.Name, got)
		}
	}
	if r := read(sci); r != (Read{}) {
		t.Errorf("fresh read marks = %+v", r)
	}

	// Учёный прочитал всё: его число обнулилось, у организации «прочитано» растёт.
	if err := w.svc.MarkRead(bg, sci.User, chat, last); err != nil {
		t.Fatal(err)
	}
	if got := unread(sci, SideScientist); got != 0 {
		t.Errorf("scientist unread after reading = %d", got)
	}
	if r := read(w.tm.Owner); r.Mine != 0 || r.Theirs != last {
		t.Errorf("owner marks after the scientist read = %+v", r)
	}
	if r := read(sci); r.Mine != last || r.Theirs != 0 {
		t.Errorf("scientist marks = %+v", r)
	}

	// Сторона организации прочитала, если хоть один сотрудник открыл; у коллег число своё.
	if err := w.svc.MarkRead(bg, w.tm.Owner.User, chat, last); err != nil {
		t.Fatal(err)
	}
	if r := read(sci); r.Theirs != last {
		t.Errorf("scientist sees the organization read up to %d, want %d", r.Theirs, last)
	}
	if got := unread(w.tm.Owner, SideOrg); got != 0 {
		t.Errorf("owner unread = %d", got)
	}
	if got := unread(w.tm.HR, SideOrg); got != 2 {
		t.Errorf("a colleague's unread must stay = %d, want 2", got)
	}
	if r := read(w.tm.HR); r.Mine != 0 || r.Theirs != last {
		t.Errorf("colleague marks = %+v", r)
	}

	// Отметка только растёт; за пределы переписки не уходит; отрицательное не принимается.
	if err := w.svc.MarkRead(bg, sci.User, chat, 1); err != nil {
		t.Fatal(err)
	}
	if r := read(sci); r.Mine != last {
		t.Errorf("a smaller up_to lowered the mark: %+v", r)
	}
	if err := w.svc.MarkRead(bg, w.tm.HR.User, chat, 999); err != nil {
		t.Fatal(err)
	}
	if r := read(w.tm.HR); r.Mine != last {
		t.Errorf("up_to beyond the last message = %+v", r)
	}
	if err := w.svc.MarkRead(bg, w.tm.HR.User, chat, -1); err == nil {
		t.Error("negative up_to accepted")
	} else if bad, ok := errors.AsType[*BadParamError](err); !ok || bad.Param != "up_to" {
		t.Errorf("negative up_to = %v", err)
	}

	// Новое сообщение снова даёт непрочитанное той стороне, которой адресовано.
	w.clock.Advance(time.Minute)
	w.send(w.tm.HR, chat, "Ещё вопрос")
	if got := unread(sci, SideScientist); got != 1 {
		t.Errorf("scientist unread after a new message = %d", got)
	}
	if got := unread(w.tm.Owner, SideOrg); got != 0 {
		t.Errorf("a message of the organization must not be unread for its colleague: %d", got)
	}
}

// Прочитанное не считает своё и не считает неадресованное: число в шапке и вкладка «Непрочитанные» (T-96).
func TestUnreadCountAndBoxes(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	second := w.chatOf(w.Applicant("Борис Сомов"), w.other.ID, "sent")

	count := func(who testkit.Person) UnreadCount {
		t.Helper()
		c, err := w.svc.UnreadCount(bg, who.User)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if c := count(sci); c != (UnreadCount{}) {
		t.Errorf("empty = %+v", c)
	}
	w.clock.Advance(time.Minute)
	w.send(w.tm.HR, chat, "Здравствуйте")
	w.clock.Advance(time.Minute)
	w.send(w.tm.HR, chat, "Ещё одно")
	if c := count(sci); c != (UnreadCount{Total: 1, Scientist: 1}) {
		t.Errorf("scientist = %+v: two messages in one chat are one chat", c)
	}
	if c := count(w.tm.Owner); c != (UnreadCount{}) {
		t.Errorf("owner = %+v: messages of the organization are not unread for it", c)
	}
	w.clock.Advance(time.Minute)
	w.send(sci, chat, "Ответ")
	w.clock.Advance(time.Minute)
	w.system(second, EventApplicationSent, nil)
	if c := count(w.tm.Owner); c != (UnreadCount{Total: 2, Org: 2}) {
		t.Errorf("owner = %+v", c)
	}
	// Руководитель подразделения А видит одну из двух, руководитель Б другую.
	if c := count(w.tm.HeadA); c != (UnreadCount{Total: 1, Org: 1}) {
		t.Errorf("head of A = %+v", c)
	}
	if c := count(w.tm.HeadB); c != (UnreadCount{Total: 1, Org: 1}) {
		t.Errorf("head of B = %+v", c)
	}
	if c := count(w.tm.Out); c != (UnreadCount{}) {
		t.Errorf("outsider = %+v", c)
	}

	// Вкладки: «Непрочитанные» и число «В архиве» не зависят от поиска и отбора.
	l, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Box: BoxUnread})
	if err != nil || len(l.Items) != 2 || l.Counts != (Counts{Unread: 2}) {
		t.Fatalf("unread box = %+v, %v", l, err)
	}
	if err := w.svc.Archive(bg, w.tm.Owner.User, chat, true); err != nil {
		t.Fatal(err)
	}
	l, err = w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Box: BoxUnread})
	if err != nil || len(l.Items) != 1 || l.Counts != (Counts{Unread: 1, Archive: 1}) {
		t.Fatalf("unread box with an archived chat = %+v, %v", l, err)
	}
	if c := count(w.tm.Owner); c != (UnreadCount{Total: 1, Org: 1}) {
		t.Errorf("archived chats are not counted: %+v", c)
	}
	// Поиск и отбор не меняют числа вкладок.
	l, err = w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Box: BoxAll, Search: "ничего подобного"})
	if err != nil || len(l.Items) != 0 || l.Counts != (Counts{Unread: 1, Archive: 1}) {
		t.Errorf("search leaves the counts: %+v, %v", l, err)
	}
}

// Архив личный; новое сообщение возвращает переписку у всех участников.
func TestArchiveIsPersonal(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	w.system(chat, EventApplicationSent, nil)

	boxes := func(who testkit.Person, side Side) (all, archive int) {
		t.Helper()
		a, err := w.svc.List(bg, who.User, ListQuery{Side: string(side), Box: BoxAll})
		if err != nil {
			t.Fatal(err)
		}
		b, err := w.svc.List(bg, who.User, ListQuery{Side: string(side), Box: BoxArchive})
		if err != nil {
			t.Fatal(err)
		}
		return len(a.Items), len(b.Items)
	}
	if err := w.svc.Archive(bg, w.tm.HR.User, chat, true); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Archive(bg, w.tm.HR.User, chat, true); err != nil {
		t.Errorf("a repeat of the archive changes nothing and is not an error: %v", err)
	}
	if all, arch := boxes(w.tm.HR, SideOrg); all != 0 || arch != 1 {
		t.Errorf("hr: all %d archive %d", all, arch)
	}
	if all, arch := boxes(w.tm.Owner, SideOrg); all != 1 || arch != 0 {
		t.Errorf("a colleague keeps the chat: all %d archive %d", all, arch)
	}
	if all, arch := boxes(sci, SideScientist); all != 1 || arch != 0 {
		t.Errorf("the scientist keeps the chat: all %d archive %d", all, arch)
	}
	hdr, err := w.svc.Get(bg, w.tm.HR.User, chat)
	if err != nil || !hdr.Archived {
		t.Errorf("header archived = %v, %v", hdr.Archived, err)
	}
	if hdr, _ = w.svc.Get(bg, w.tm.Owner.User, chat); hdr.Archived {
		t.Error("archived for a colleague")
	}

	// Новое сообщение возвращает переписку из архива.
	if err := w.svc.Archive(bg, sci.User, chat, true); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(time.Minute)
	w.send(w.tm.Owner, chat, "Есть вопрос")
	if all, arch := boxes(w.tm.HR, SideOrg); all != 1 || arch != 0 {
		t.Errorf("hr after a new message: all %d archive %d", all, arch)
	}
	if all, arch := boxes(sci, SideScientist); all != 1 || arch != 0 {
		t.Errorf("scientist after a new message: all %d archive %d", all, arch)
	}
	// Строка о событии возвращает так же.
	if err := w.svc.Archive(bg, sci.User, chat, true); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(time.Minute)
	w.system(chat, EventApplicationViewed, nil)
	if _, arch := boxes(sci, SideScientist); arch != 0 {
		t.Error("a system line left the chat in the archive")
	}
	// Вернуть вручную.
	if err := w.svc.Archive(bg, w.tm.Owner.User, chat, true); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Archive(bg, w.tm.Owner.User, chat, false); err != nil {
		t.Fatal(err)
	}
	if all, _ := boxes(w.tm.Owner, SideOrg); all != 1 {
		t.Error("unarchive did not return the chat")
	}
}

// Лента: последние сообщения, листание назад и вперёд, границы.
func TestMessagesPaging(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	for i := range 25 {
		w.clock.Advance(time.Second)
		if i%2 == 0 {
			w.send(sci, chat, "Учёный "+string(rune('а'+i)))
		} else {
			w.send(w.tm.HR, chat, "Организация "+string(rune('а'+i)))
		}
	}
	seqs := func(p Page) []int {
		out := make([]int, len(p.Items))
		for i, m := range p.Items {
			out[i] = m.Seq
		}
		return out
	}
	page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{Limit: 10})
	if err != nil || !page.HasMore || len(page.Items) != 10 || page.Items[0].Seq != 16 || page.Items[9].Seq != 25 {
		t.Fatalf("latest = %v more %v, %v", seqs(page), page.HasMore, err)
	}
	before := 16
	older, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{Before: &before, Limit: 10})
	if err != nil || !older.HasMore || older.Items[0].Seq != 6 || older.Items[9].Seq != 15 {
		t.Fatalf("before 16 = %v more %v, %v", seqs(older), older.HasMore, err)
	}
	before = 6
	oldest, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{Before: &before, Limit: 10})
	if err != nil || oldest.HasMore || len(oldest.Items) != 5 || oldest.Items[0].Seq != 1 {
		t.Fatalf("before 6 = %v more %v, %v", seqs(oldest), oldest.HasMore, err)
	}
	after := 22
	newer, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{After: &after})
	if err != nil || newer.HasMore || len(newer.Items) != 3 || newer.Items[0].Seq != 23 {
		t.Fatalf("after 22 = %v more %v, %v", seqs(newer), newer.HasMore, err)
	}
	after = 25
	none, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{After: &after})
	if err != nil || none.HasMore || len(none.Items) != 0 || none.Items == nil {
		t.Fatalf("after the last = %+v, %v", none, err)
	}
	after = 20
	capped, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{After: &after, Limit: 2})
	if err != nil || !capped.HasMore || len(capped.Items) != 2 {
		t.Fatalf("after 20 limit 2 = %v more %v, %v", seqs(capped), capped.HasMore, err)
	}

	zero, minus := 0, -1
	for name, q := range map[string]MessagesQuery{
		"limit 101": {Limit: 101}, "negative limit": {Limit: -1}, "both directions": {Before: &before, After: &after},
		"before zero": {Before: &zero}, "after negative": {After: &minus},
	} {
		if _, err := w.svc.Messages(bg, sci.User, chat, q); err == nil {
			t.Errorf("%s accepted", name)
		} else if _, ok := errors.AsType[*BadParamError](err); !ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

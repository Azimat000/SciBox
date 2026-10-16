package chat

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
)

// Пределы против спама и злоупотреблений (срез 40, T-99, D-248, I-121).

func (w *world) trySend(who testkit.Person, chat uuid.UUID, text string) error {
	_, _, err := w.svc.Send(bg, who.User, chat, SendInput{Text: text, ClientID: uuid.NewString()})
	return err
}

func (w *world) messageCount(chat uuid.UUID) int {
	return testkit.Count(w.T, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND kind = 'text'`, chat)
}

// Подряд без ответа: пять текстов, шестой отвергается; ответ собеседника сбрасывает счёт; счёт общий для стороны.
func TestUnansweredStreak(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(DefaultLimits())
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")

	// Сопроводительное письмо и другие строки о событиях — не тексты и в счёт не идут.
	var last Message
	for i := range DefaultUnanswered {
		last = w.send(w.tm.HR, chat, fmt.Sprintf("Сообщение %d", i))
	}
	if err := w.trySend(w.tm.HR, chat, "Шестое"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("sixth text = %v, want ErrAwaitingReply", err)
	}
	// Счёт общий для стороны: коллега кадровика не обходит предел.
	if err := w.trySend(w.tm.Owner, chat, "От коллеги"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("a colleague = %v, want ErrAwaitingReply", err)
	}
	if n := w.messageCount(chat); n != DefaultUnanswered {
		t.Fatalf("refused texts were stored: %d", n)
	}
	// Удалить и написать снова нельзя: удалённое считается.
	if err := w.svc.Delete(bg, w.tm.HR.User, chat, last.Seq); err != nil {
		t.Fatal(err)
	}
	if err := w.trySend(w.tm.HR, chat, "После удаления"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("after deleting = %v, want ErrAwaitingReply", err)
	}
	// Повтор уже принятого сообщения предела не касается (иначе ответ «не дошло» на дошедшее сообщение).
	m, _, err := w.svc.Send(bg, w.tm.HR.User, chat, SendInput{Text: "Сообщение 0", ClientID: (*messageClient(t, chat, 1)).String()})
	if err != nil || m.Seq != 1 {
		t.Errorf("repeat of an accepted message = %+v, %v", m, err)
	}
	// Строка о событии ответом не считается.
	w.system(chat, "application.viewed", map[string]any{})
	if err := w.trySend(w.tm.HR, chat, "После строки"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("after a system line = %v, want ErrAwaitingReply", err)
	}
	// Учёный отвечает, и сторона организации снова может писать; у учёного свой счёт.
	w.send(sci, chat, "Здравствуйте")
	for i := range DefaultUnanswered {
		w.send(w.tm.HR, chat, fmt.Sprintf("Ещё %d", i))
	}
	if err := w.trySend(w.tm.HR, chat, "Снова шестое"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("after the answer, sixth = %v", err)
	}
	for i := range DefaultUnanswered {
		w.send(sci, chat, fmt.Sprintf("Учёный %d", i))
	}
	if err := w.trySend(sci, chat, "Лишнее"); !errors.Is(err, ErrAwaitingReply) {
		t.Fatalf("the scientist's sixth in a row = %v", err)
	}
	w.send(w.tm.Owner, chat, "Ответ организации")
	w.send(sci, chat, "Теперь можно")
}

// messageClient — client_id сообщения (для проверки повтора).
func messageClient(t *testing.T, chat uuid.UUID, seq int) *uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT client_id FROM chat_messages WHERE chat_id = $1 AND seq = $2`, chat, seq).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &id
}

// rateLimited проверяет, что отказ — «слишком часто», и возвращает, через сколько можно повторить.
func rateLimited(t *testing.T, err error) time.Duration {
	t.Helper()
	rl, ok := errors.AsType[*auth.RateLimitedError](err)
	if !ok {
		t.Fatalf("err = %v, want RateLimitedError", err)
	}
	return rl.RetryAfter
}

// Сутки на человека: считаются сообщения во всех его переписках; через сутки предел освобождается.
func TestSendLimitPerPerson(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(Limits{PersonPerDay: 3})
	var chats []uuid.UUID
	for i := range 4 {
		chats = append(chats, w.chatOf(w.Applicant(fmt.Sprintf("Учёный %d", i)), w.vacancy.ID, "sent"))
	}
	for _, c := range chats[:3] {
		w.send(w.tm.HR, c, "Здравствуйте")
	}
	retry := rateLimited(t, w.trySend(w.tm.HR, chats[3], "Четвёртое"))
	if retry < 23*time.Hour || retry > 24*time.Hour {
		t.Errorf("retry after = %v", retry)
	}
	if n := w.messageCount(chats[3]); n != 0 {
		t.Errorf("a refused message was stored: %d", n)
	}
	// Другой человек организации не затронут, а учёный пишет по своему счёту.
	w.send(w.tm.Owner, chats[3], "От владельца")
	// Повтор принятого сообщения предел не расходует.
	if _, created, err := w.svc.Send(bg, w.tm.HR.User, chats[0], SendInput{Text: "Здравствуйте", ClientID: messageClient(t, chats[0], 1).String()}); err != nil || created {
		t.Errorf("repeat = created %v, err %v", created, err)
	}
	w.clock.Advance(day + time.Minute)
	w.send(w.tm.HR, chats[3], "Через сутки")
}

// Сутки на организацию: все сотрудники вместе; учёные и чужие организации не затронуты.
func TestSendLimitPerOrganization(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(Limits{OrgPerDay: 3})
	var chats []uuid.UUID
	var people []testkit.Person
	for i := range 4 {
		p := w.Applicant(fmt.Sprintf("Учёный %d", i))
		people = append(people, p)
		chats = append(chats, w.chatOf(p, w.vacancy.ID, "sent"))
	}
	w.send(w.tm.HR, chats[0], "Раз")
	w.send(w.tm.Owner, chats[1], "Два")
	w.send(w.tm.HR, chats[2], "Три")
	_ = rateLimited(t, w.trySend(w.tm.Owner, chats[3], "Четыре"))
	_ = rateLimited(t, w.trySend(w.tm.HeadA, chats[3], "От руководителя"))

	// Учёные пишут сколько нужно: предел организации их не касается.
	for range 5 {
		w.send(people[3], chats[3], "Реплика учёного")
	}
	// Чужая организация считается отдельно.
	foreign := w.Team()
	fv := foreign.Published(&foreign.UnitA.ID)
	for i := range 3 {
		w.send(foreign.Owner, w.chatOf(w.Applicant(fmt.Sprintf("Чужой %d", i)), fv.ID, "sent"), "Из другой организации")
	}
	// Через сутки предел освобождается.
	w.clock.Advance(day + time.Minute)
	w.send(w.tm.Owner, chats[3], "Через сутки")
}

// Пачка одновременных запросов не проходит предел с одним и тем же нулём (I-80).
func TestSendLimitsHoldUnderConcurrency(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(Limits{PersonPerDay: 5, OrgPerDay: 7})
	const n = 14
	var chats []uuid.UUID
	for i := range n {
		chats = append(chats, w.chatOf(w.Applicant(fmt.Sprintf("Учёный %d", i)), w.vacancy.ID, "sent"))
	}
	run := func(who func(i int) testkit.Person) (ok int) {
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := w.trySend(who(i), chats[i], "Параллельно")
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				default:
					if _, isRL := errors.AsType[*auth.RateLimitedError](err); !isRL {
						t.Errorf("unexpected error: %v", err)
					}
				}
			}()
		}
		wg.Wait()
		return ok
	}
	if ok := run(func(int) testkit.Person { return w.tm.HR }); ok != 5 {
		t.Errorf("one person got %d messages through, want 5", ok)
	}
	// Второй сотрудник: свои пять, но у организации осталось только два места из семи.
	people := []testkit.Person{w.tm.Owner, w.tm.HeadA}
	left := 7 - 5
	var ok int
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 5; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w.trySend(people[i%2], chats[i], "Параллельно") == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != left {
		t.Errorf("the organization got %d messages through, want %d", ok, left)
	}
}

// Жалобы: три в сутки, одна на переписку за неделю, пачка запросов проходит один раз.
func TestComplaintLimits(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(DefaultLimits())
	sci := w.Applicant("Анна Ильина")
	var chats []uuid.UUID
	for range 4 {
		chats = append(chats, w.chatOf(sci, w.tm.Published(&w.tm.UnitA.ID).ID, "sent"))
	}
	in := ComplaintInput{Reason: "spam", Note: "Рассылка"}
	for _, c := range chats[:3] {
		if _, err := w.svc.Complain(bg, sci.User, c, in); err != nil {
			t.Fatal(err)
		}
	}
	retry := rateLimited(t, errOf(w.svc.Complain(bg, sci.User, chats[3], in)))
	if retry < 23*time.Hour {
		t.Errorf("retry after = %v", retry)
	}
	// Повтор на ту же переписку отвергается раньше, чем считаются сутки.
	if _, err := w.svc.Complain(bg, sci.User, chats[0], in); !errors.Is(err, ErrAlreadyComplained) {
		t.Errorf("same chat = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_complaints WHERE user_id = $1`, sci.ID); n != 3 {
		t.Fatalf("complaints stored = %d", n)
	}
	w.clock.Advance(day + time.Minute)
	if _, err := w.svc.Complain(bg, sci.User, chats[3], in); err != nil {
		t.Errorf("next day: %v", err)
	}
	if _, err := w.svc.Complain(bg, sci.User, chats[0], in); !errors.Is(err, ErrAlreadyComplained) {
		t.Errorf("same chat the next day = %v", err)
	}
	w.clock.Advance(7 * day)
	if _, err := w.svc.Complain(bg, sci.User, chats[0], in); err != nil {
		t.Errorf("a week later: %v", err)
	}
}

func errOf[T any](_ T, err error) error { return err }

// Пачка одновременных жалоб на одну переписку даёт одну жалобу.
func TestComplaintsHoldUnderConcurrency(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(DefaultLimits())
	sci := w.Applicant("Анна Ильина")
	c := w.chatOf(sci, w.vacancy.ID, "sent")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.svc.Complain(bg, sci.User, c, ComplaintInput{Reason: "spam"}); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Errorf("%d complaints passed, want 1", ok)
	}
}

// Тот же номер отправки с другим текстом — ошибка, а не молчаливый возврат старого сообщения; исправленное и удалённое
// сообщение при повторе отдаётся как есть.
func TestClientIDReuse(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	c := w.chatOf(sci, w.vacancy.ID, "sent")
	client := uuid.NewString()
	if _, _, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Первый текст", ClientID: client}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Другой текст", ClientID: client}); !errors.Is(err, ErrClientIDReused) {
		t.Fatalf("another text = %v", err)
	}
	if _, created, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Первый текст", ClientID: client}); err != nil || created {
		t.Fatalf("same text = created %v, %v", created, err)
	}
	// После правки запоздалый повтор исходного текста отдаёт исправленное сообщение.
	if _, err := w.svc.Edit(bg, sci.User, c, 1, EditInput{Text: "Исправлено"}); err != nil {
		t.Fatal(err)
	}
	if m, created, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Первый текст", ClientID: client}); err != nil || created || m.Text != "Исправлено" {
		t.Fatalf("repeat after edit = %+v created %v, %v", m, created, err)
	}
	// И после удаления.
	if err := w.svc.Delete(bg, sci.User, c, 1); err != nil {
		t.Fatal(err)
	}
	if m, created, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Первый текст", ClientID: client}); err != nil || created || !m.Deleted {
		t.Fatalf("repeat after delete = %+v created %v, %v", m, created, err)
	}
}

// Повторное «в архив» и «из архива» ничего не рассылает.
func TestArchiveRepeatSendsNoEvents(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	c := w.chatOf(sci, w.vacancy.ID, "sent")
	events := func() int { return testkit.Count(t, `SELECT count(*) FROM chat_events WHERE chat_id = $1`, c) }
	if err := w.svc.Archive(bg, sci.User, c, true); err != nil {
		t.Fatal(err)
	}
	once := events()
	for range 3 {
		if err := w.svc.Archive(bg, sci.User, c, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := events(); got != once {
		t.Errorf("repeated archiving wrote %d events", got-once)
	}
	if err := w.svc.Archive(bg, sci.User, c, false); err != nil {
		t.Fatal(err)
	}
	if got := events(); got != once+1 {
		t.Errorf("unarchiving wrote %d events, want 1", got-once)
	}
}

// Письма о непрочитанном: не больше MailPerDay в сутки одному получателю; чужой текст в письме коротко и в одну строку.
func TestUnreadMailCapPerRecipient(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	w.svc.WithLimits(Limits{MailPerDay: 2})
	sci := w.Applicant("Анна Ильина")
	for i := range 4 {
		v := w.tm.Published(&w.tm.UnitA.ID)
		c := w.chatOf(sci, v.ID, "sent")
		w.send(w.tm.HR, c, fmt.Sprintf("Текст %d", i))
	}
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != 2 {
		t.Fatalf("mails = %d, want 2 (the daily cap)", n)
	}
	if got := outboxFor(t, sci); len(got) != 2 {
		t.Fatalf("outbox of the scientist: %d", len(got))
	}
	// На следующий день предел свободен, но непрочитанное старше срока писем не вызывает только через 48 часов.
	w.clock.Advance(day + time.Minute)
	if n := w.sendMail(); n != 0 {
		t.Errorf("the suppressed chats must not mail later: %d", n)
	}
}

// Правка текста: не больше EditsPerDay настоящих исправлений в сутки; та же правка повторно и отказ ничего не тратят.
func TestEditLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.WithLimits(Limits{EditsPerDay: 3})
	sci := w.Applicant("Анна Ильина")
	c := w.chatOf(sci, w.vacancy.ID, "sent")
	w.send(sci, c, "Исходный текст")
	for i := range 3 {
		if _, err := w.svc.Edit(bg, sci.User, c, 1, EditInput{Text: fmt.Sprintf("Правка %d", i)}); err != nil {
			t.Fatalf("edit %d: %v", i, err)
		}
	}
	// Тот же текст — не исправление: события нет, предел не тратится.
	if _, err := w.svc.Edit(bg, sci.User, c, 1, EditInput{Text: "Правка 2"}); err != nil {
		t.Errorf("an edit without a change: %v", err)
	}
	_, err := w.svc.Edit(bg, sci.User, c, 1, EditInput{Text: "Четвёртая"})
	_ = rateLimited(t, err)
	if got := w.texts(sci, c); len(got) != 1 || got[0] != "Правка 2" {
		t.Errorf("a refused edit changed the text: %q", got)
	}
	w.clock.Advance(day + time.Minute)
	if _, err := w.svc.Edit(bg, sci.User, c, 1, EditInput{Text: "Следующие сутки"}); !errors.Is(err, ErrEditWindowClosed) {
		t.Errorf("after a day the edit window is closed: %v", err)
	}
}

func TestMailSafe(t *testing.T) {
	long := strings.Repeat("я", 300)
	for name, c := range map[string]struct {
		in   string
		max  int
		want string
	}{
		"plain":    {"Анна Ильина", 100, "Анна Ильина"},
		"lines":    {"Анна\n\nИльина\r\n", 100, "Анна Ильина"},
		"bidi":     {"Анна‮аниьлИ", 100, "Анна аниьлИ"},
		"control":  {"A\x00B\x07C", 100, "A B C"},
		"too long": {long, 10, strings.Repeat("я", 9) + "…"},
		"exact":    {strings.Repeat("я", 10), 10, strings.Repeat("я", 10)},
		"empty":    {"  \n ", 10, ""},
	} {
		if got := mailSafe(c.in, c.max); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestLimitsNormalized(t *testing.T) {
	if got := (Limits{}).normalized(); got != DefaultLimits() {
		t.Errorf("zero limits = %+v, want the defaults", got)
	}
	if got := (Limits{PersonPerDay: -1, Unanswered: -5, ComplaintPerChat: -time.Hour}).normalized(); got != DefaultLimits() {
		t.Errorf("negative limits = %+v, want the defaults", got)
	}
	got := (Limits{PersonPerDay: 7}).normalized()
	if got.PersonPerDay != 7 || got.OrgPerDay != DefaultOrgPerDay {
		t.Errorf("partial limits = %+v", got)
	}
	d := DefaultLimits()
	if d.PersonPerDay != 600 || d.OrgPerDay != 2000 || d.Unanswered != 5 || d.ComplaintsPerDay != 3 || d.ComplaintPerChat != 7*24*time.Hour {
		t.Errorf("defaults are not the chosen ones (D-248): %+v", d)
	}
}

// Те же пределы через HTTP: коды, заголовок Retry-After и совпадение с contracts/chat/errors.json.
func TestHTTPLimitErrors(t *testing.T) {
	a := newAPI(t)
	a.svc.WithLimits(Limits{PersonPerDay: 5, Unanswered: 2, ComplaintsPerDay: 1})
	sci := a.Applicant("Анна Ильина")
	c1 := a.chatOf(sci, a.vacancy.ID, "sent")
	path := "/api/chats/" + c1.String()
	body := func(text, client string) map[string]any { return map[string]any{"text": text, "client_id": client} }

	a.expect(a.do(&a.tm.HR, "POST", path+"/messages", body("Раз", uuid.NewString())), 201, "send")
	repeat := uuid.NewString()
	a.expect(a.do(&a.tm.HR, "POST", path+"/messages", body("Два", repeat)), 201, "send")
	a.expectError(a.do(&a.tm.HR, "POST", path+"/messages", body("Три", uuid.NewString())), 409, "awaiting_reply")
	// Повтор принятого сообщения проходит даже при упёртом пределе, а тот же номер с другим текстом — ошибка.
	a.expect(a.do(&a.tm.HR, "POST", path+"/messages", body("Два", repeat)), 200, "send")
	a.expectError(a.do(&a.tm.HR, "POST", path+"/messages", body("Совсем другое", repeat)), 409, "client_id_reused")

	// Суточный предел человека: ответ 429 с Retry-After.
	for i := range 5 {
		c := a.chatOf(a.Applicant(fmt.Sprintf("Учёный %d", i)), a.vacancy.ID, "sent")
		r := a.do(&a.tm.HR, "POST", "/api/chats/"+c.String()+"/messages", body("Привет", uuid.NewString()))
		if i < 3 {
			a.expect(r, 201, "send")
			continue
		}
		a.expectError(r, 429, "rate_limited")
		if r.Header.Get("Retry-After") == "" {
			t.Errorf("no Retry-After: %v", r.Header)
		}
		break
	}

	// Жалобы: одна в сутки, повтор на ту же переписку.
	a.expect(a.do(&sci, "POST", path+"/complaints", map[string]any{"reason": "spam", "note": ""}), 201, "complaint")
	a.expectError(a.do(&sci, "POST", path+"/complaints", map[string]any{"reason": "spam", "note": ""}), 409, "already_complained")
	c2 := a.chatOf(sci, a.other.ID, "sent")
	a.expectError(a.do(&sci, "POST", "/api/chats/"+c2.String()+"/complaints", map[string]any{"reason": "spam", "note": ""}), 429, "rate_limited")
}

// Переписка заводится только вместе с откликом или приглашением на вакансию (CHAT.md): писать «в пустоту» нельзя, а предел
// приглашений нельзя обойти через чат (T-99). Записать переписку вправе только applications и offers через chat.Record;
// заводит её только пакет chat.
func TestChatsAreOpenedOnlyByApplicationsAndOffers(t *testing.T) {
	allowed := map[string]bool{"applications": true, "offers": true}
	root := ".."
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		pkg := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if pkg == "chat" || pkg == "dbgen" || pkg == "testkit" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		for _, call := range []string{"chat.Open(", "OpenChat(", "chat.Record(", "chat.AppendSystem("} {
			if !strings.Contains(text, call) {
				continue
			}
			if !allowed[pkg] || call == "chat.Open(" || call == "OpenChat(" || call == "chat.AppendSystem(" {
				t.Errorf("%s calls %s: a chat may be opened only by applications and offers through chat.Record", rel, call)
			}
			found[pkg] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found["applications"] || !found["offers"] {
		t.Errorf("the scan did not find the two callers: %v", found)
	}
}

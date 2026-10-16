package chat

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

// Переписка пары одна: повторное открытие находит её, приглашение и отклик собираются в ней.
func TestOpenIsOnePerPair(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	offer := w.offer(sci, w.vacancy.ID, "pending")
	first := w.open(sci, w.vacancy.ID, nil, &offer)
	if again := w.open(sci, w.vacancy.ID, nil, &offer); again != first {
		t.Errorf("the second open made a new chat: %v / %v", again, first)
	}
	app := w.application(sci, w.vacancy.ID, "sent")
	withApp := w.open(sci, w.vacancy.ID, &app, nil)
	if withApp != first {
		t.Fatalf("an application made another chat: %v / %v", withApp, first)
	}
	// Ссылка на приглашение сохранилась, отклик добавился; пустая ссылка ничего не стирает.
	var gotApp, gotOffer *uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT application_id, offer_id FROM chats WHERE id = $1`, first).Scan(&gotApp, &gotOffer); err != nil {
		t.Fatal(err)
	}
	if gotApp == nil || *gotApp != app || gotOffer == nil || *gotOffer != offer {
		t.Errorf("links = %v / %v", gotApp, gotOffer)
	}
	w.open(sci, w.vacancy.ID, nil, nil)
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE vacancy_id = $1 AND user_id = $2`, w.vacancy.ID, sci.ID); n != 1 {
		t.Errorf("chats of the pair = %d", n)
	}
	// Состояние идёт по отклику, а приглашение «Не сейчас» его уже не закрывает.
	w.setOffer(offer, "declined")
	hdr, err := w.svc.Get(bg, w.tm.Owner.User, first)
	if err != nil || !hdr.CanWrite || hdr.Status != (Status{Source: "application", Value: "sent"}) || hdr.ApplicationID == nil || hdr.OfferID == nil {
		t.Errorf("header = %+v, %v", hdr, err)
	}
	// Переписки других пар не мешают.
	if w.chatOf(sci, w.other.ID, "sent") == first {
		t.Error("another vacancy reused the chat")
	}
}

// Откат события откатывает и переписку со строкой: Open и AppendSystem живут в чужой транзакции.
func TestOpenAndAppendRollBackWithTheTransaction(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	app := w.application(sci, w.vacancy.ID, "sent")
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	q := dbgen.New(tx)
	id, err := Open(bg, q, OpenInput{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app}, w.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AppendSystem(bg, q, id, EventApplicationSent, map[string]any{"cover_letter": "Письмо"}, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(bg); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE user_id = $1`, sci.ID); n != 0 {
		t.Errorf("a chat survived the rollback: %d", n)
	}
}

func TestAppendSystem(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	if _, err := AppendSystem(bg, w.q(), chat, "application.exploded", nil, w.clock.Now()); !errors.Is(err, ErrUnknownEvent) {
		t.Errorf("unknown event = %v", err)
	}
	data := map[string]any{"application_id": uuid.New(), "cover_letter": "Письмо"}
	seq := w.system(chat, EventApplicationSent, data)
	if seq != 1 {
		t.Errorf("seq = %d", seq)
	}
	w.clock.Advance(time.Minute)
	if w.system(chat, EventApplicationViewed, nil) != 2 {
		t.Error("second seq")
	}
	page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("messages = %+v, %v", page, err)
	}
	first := page.Items[0]
	var got map[string]any
	if first.Kind != KindSystem || first.Author != nil || first.Text != "" || first.Event == nil || first.Event.Type != EventApplicationSent ||
		first.EditableUntil != nil || first.ClientID != nil || first.Deleted {
		t.Errorf("system line = %+v", first)
	}
	if err := json.Unmarshal(first.Event.Data, &got); err != nil || got["cover_letter"] != "Письмо" {
		t.Errorf("event data = %s, %v", first.Event.Data, err)
	}
	if string(page.Items[1].Event.Data) != "{}" {
		t.Errorf("empty data = %s", page.Items[1].Event.Data)
	}
	// Счётчики: событие адресовано стороне, которая его не совершала.
	var forScientist, forOrg int
	if err := testkit.Pool.QueryRow(bg, `SELECT last_for_scientist, last_for_org FROM chats WHERE id = $1`, chat).Scan(&forScientist, &forOrg); err != nil {
		t.Fatal(err)
	}
	if forScientist != 2 || forOrg != 1 {
		t.Errorf("addressees = scientist %d, org %d", forScientist, forOrg)
	}
}

// T-106: решение организации и отправка выстраиваются в очередь. Сообщение после решения «Отказ» в переписку не попадает,
// сколько бы людей ни писало в этот момент.
func TestSendRacesWithTheDecision(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	app := w.application(sci, w.vacancy.ID, "viewed")
	chat := w.open(sci, w.vacancy.ID, &app, nil)

	// Решение: отклик обновлён, строка в переписке ещё не добавлена. Отправка ждёт, а не пролезает между шагами.
	tx, err := testkit.Pool.Begin(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(bg, `UPDATE applications SET status = 'rejected' WHERE id = $1`, app); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := w.svc.Send(bg, sci.User, chat, SendInput{Text: "Успею?", ClientID: uuid.NewString()})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Send did not wait for the decision: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := AppendSystem(bg, dbgen.New(tx), chat, EventApplicationDecided, map[string]any{"decision": "rejected"}, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(bg); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Errorf("Send after the decision = %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND kind = 'text'`, chat); n != 0 {
		t.Errorf("a message got into the closed chat: %d", n)
	}
}

func TestManySendersAndTheDecision(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	app := w.application(sci, w.vacancy.ID, "viewed")
	chat := w.open(sci, w.vacancy.ID, &app, nil)
	// Сервис на часах «сейчас», чтобы время строк шло само.
	svc := relaxed(NewService(testkit.Pool))

	var wg sync.WaitGroup
	results := make(chan error, 60)
	for i := range 30 {
		who := sci
		if i%2 == 1 {
			who = w.tm.HR
		}
		wg.Go(func() {
			for range 2 {
				_, _, err := svc.Send(bg, who.User, chat, SendInput{Text: "Сообщение", ClientID: uuid.NewString()})
				results <- err
			}
		})
	}
	wg.Go(func() {
		time.Sleep(15 * time.Millisecond)
		tx, err := testkit.Pool.Begin(bg)
		if err != nil {
			results <- err
			return
		}
		defer func() { _ = tx.Rollback(bg) }()
		if _, err := tx.Exec(bg, `UPDATE applications SET status = 'rejected' WHERE id = $1`, app); err != nil {
			results <- err
			return
		}
		if _, err := AppendSystem(bg, dbgen.New(tx), chat, EventApplicationDecided, map[string]any{"decision": "rejected"}, time.Now().UTC()); err != nil {
			results <- err
			return
		}
		if err := tx.Commit(bg); err != nil {
			results <- err
		}
	})
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, ErrClosed) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	var decided int
	if err := testkit.Pool.QueryRow(bg, `SELECT seq FROM chat_messages WHERE chat_id = $1 AND event_type = 'application.decided'`, chat).Scan(&decided); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1 AND kind = 'text' AND seq > $2`, chat, decided); n != 0 {
		t.Errorf("%d messages came after the decision", n)
	}
	// Номера идут подряд без дыр и повторов.
	var last, rows int
	if err := testkit.Pool.QueryRow(bg, `SELECT last_seq FROM chats WHERE id = $1`, chat).Scan(&last); err != nil {
		t.Fatal(err)
	}
	rows = testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, chat)
	if last != rows {
		t.Errorf("last_seq %d, messages %d", last, rows)
	}
}

// Два одинаковых запроса одновременно дают одно сообщение (T-101).
func TestParallelRepeatsMakeOneMessage(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	client := uuid.NewString()
	svc := relaxed(NewService(testkit.Pool))
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for range 8 {
		wg.Go(func() {
			_, ok, err := svc.Send(bg, sci.User, chat, SendInput{Text: "Один раз", ClientID: client})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("Send: %v", err)
			}
			if ok {
				created++
			}
		})
	}
	wg.Wait()
	if created != 1 {
		t.Errorf("created %d times", created)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_messages WHERE chat_id = $1`, chat); n != 1 {
		t.Errorf("messages = %d", n)
	}
}

// Чтение, архив и отправка в одну переписку одновременно не ждут друг друга по кругу: порядок замков один (переписка,
// потом личные отметки). Взаимная блокировка PostgreSQL (40P01) здесь была бы ошибкой 500 у человека.
func TestReadArchiveAndSendDoNotDeadlock(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	svc := relaxed(NewService(testkit.Pool))
	people := []testkit.Person{sci, w.tm.Owner, w.tm.HR, w.tm.HeadA}
	// У каждого уже есть личная строка в архиве: отправка будет её менять, чтение и архив тоже.
	for _, p := range people {
		if err := svc.Archive(bg, p.User, chat, true); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(people)*15*3)
	for i, p := range people {
		wg.Go(func() {
			for n := range 15 {
				_, _, err := svc.Send(bg, p.User, chat, SendInput{Text: "Сообщение", ClientID: uuid.NewString()})
				errs <- err
				errs <- svc.MarkRead(bg, p.User, chat, n+i)
				errs <- svc.Archive(bg, p.User, chat, n%2 == 0)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("%v", err)
		}
	}
}

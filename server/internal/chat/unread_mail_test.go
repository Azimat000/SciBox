package chat

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/testkit"
)

// Письма о непрочитанном (срез 39, D-245, T-102): одно на переписку и сторону, без текста сообщения, без дублей при
// нескольких копиях, с учётом выключателя и анонимности.

type mailRow struct{ to, subject, body string }

// outboxFor — письма очереди, адресованные людям.
func outboxFor(t *testing.T, people ...testkit.Person) []mailRow {
	t.Helper()
	var out []mailRow
	for _, p := range people {
		rows, err := testkit.Pool.Query(bg, `SELECT to_email, subject, body FROM outbox WHERE to_email = $1 ORDER BY id`, p.Email)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var m mailRow
			if err := rows.Scan(&m.to, &m.subject, &m.body); err != nil {
				t.Fatal(err)
			}
			out = append(out, m)
		}
		rows.Close()
	}
	return out
}

// withMail включает письма сервиса и очищает очередь писем о непрочитанном: она общая для всей базы пакета, а задача
// берёт все строки, которым пора, в том числе оставленные другими тестами.
func (w *world) withMail() {
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM chat_unread_mail`); err != nil {
		w.T.Fatal(err)
	}
	w.svc.WithMail(MailConfig{ProductName: "SciBox", PublicURL: "https://scibox.example.ru/"})
}

// sendMail запускает задачу и возвращает число поставленных писем.
func (w *world) sendMail() int {
	w.T.Helper()
	n, err := w.svc.SendUnreadMail(bg)
	if err != nil {
		w.T.Fatalf("SendUnreadMail: %v", err)
	}
	return n
}

func TestUnreadMailToTheOrganization(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	w.send(sci, chat, "Секретный текст сообщения номер один")

	// Рано: пятнадцати минут ещё нет.
	w.clock.Advance(UnreadMailAfter - time.Minute)
	if n := w.sendMail(); n != 0 {
		t.Fatalf("mails before the time: %d", n)
	}
	w.clock.Advance(2 * time.Minute)
	staff := []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA}
	before := len(outboxFor(t, staff...))
	if n := w.sendMail(); n != len(staff) {
		t.Fatalf("mails: %d, want one for each who handles applications of the vacancy", n)
	}
	// Руководитель другого подразделения и посторонний писем не получают; сам учёный тоже.
	if got := outboxFor(t, w.tm.HeadB, w.tm.Out, sci); len(got) != 0 {
		t.Errorf("mails to people who must not get them: %+v", got)
	}
	got := outboxFor(t, staff...)
	if len(got) != before+len(staff) {
		t.Fatalf("outbox rows: %d", len(got))
	}
	m := got[len(got)-1]
	for _, bad := range []string{"Секретный текст"} {
		if strings.Contains(m.body, bad) || strings.Contains(m.subject, bad) {
			t.Errorf("the message text is in the mail: %q / %q", m.subject, m.body)
		}
	}
	if !strings.Contains(m.subject, w.vacancy.Title) || !strings.Contains(m.body, "От: Анна Ильина") ||
		!strings.Contains(m.body, "https://scibox.example.ru/chats/"+chat.String()) || strings.Contains(m.body, "//chats") {
		t.Errorf("mail: %q / %q", m.subject, m.body)
	}

	// Одно письмо на переписку, пока её не открыли: повтор и новые сообщения письма не дают.
	if n := w.sendMail(); n != 0 {
		t.Errorf("repeat: %d", n)
	}
	w.send(sci, chat, "Ещё одно")
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != 0 {
		t.Errorf("a new message before the chat was opened: %d", n)
	}

	// Кадровик открыл переписку: письма снова возможны, но только про новые сообщения.
	if err := w.svc.MarkRead(bg, w.tm.HR.User, chat, 99); err != nil {
		t.Fatal(err)
	}
	if n := w.sendMail(); n != 0 {
		t.Errorf("after reading: %d", n)
	}
	w.send(sci, chat, "Уже после прочтения")
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != len(staff) {
		t.Errorf("a new unread message after the chat was opened: %d", n)
	}
}

func TestUnreadMailToTheScientist(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	w.send(w.tm.Owner, chat, "Назначаем собеседование, подробности в письме.")
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != 1 {
		t.Fatalf("mails: %d", n)
	}
	got := outboxFor(t, sci)
	if len(got) == 0 {
		t.Fatal("no mail to the scientist")
	}
	var orgName string
	if err := testkit.Pool.QueryRow(bg, `SELECT name FROM organizations WHERE id = $1`, w.tm.OrgID).Scan(&orgName); err != nil {
		t.Fatal(err)
	}
	m := got[len(got)-1]
	if strings.Contains(m.body, "собеседование") || !strings.Contains(m.body, "От: "+orgName) {
		t.Errorf("mail: %q", m.body)
	}
}

func TestUnreadMailRespectsTheSwitchAndReading(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")

	// Выключатель: письма не будет, строка отмечена и больше не разбирается.
	if _, err := testkit.Pool.Exec(bg, `INSERT INTO notification_settings (user_id, email_new_vacancies, email_deadlines, email_messages, updated_at)
		VALUES ($1, true, true, false, now())`, sci.ID); err != nil {
		t.Fatal(err)
	}
	w.send(w.tm.Owner, chat, "Для того, кто выключил письма")
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != 0 {
		t.Errorf("mails with the switch off: %d", n)
	}
	if got := testkit.Count(t, `SELECT count(*) FROM chat_unread_mail WHERE chat_id = $1 AND notified_at IS NOT NULL`, chat); got != 1 {
		t.Errorf("the row must be marked: %d", got)
	}

	// Прочитано вовремя: письма нет, строка ушла.
	w2 := newWorld(t)
	w2.withMail()
	sci2 := w2.Applicant("Борис Орлов")
	chat2 := w2.chatOf(sci2, w2.vacancy.ID, "sent")
	w2.send(w2.tm.Owner, chat2, "Прочитано вовремя")
	if err := w2.svc.MarkRead(bg, sci2.User, chat2, 99); err != nil {
		t.Fatal(err)
	}
	w2.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w2.sendMail(); n != 0 {
		t.Errorf("mails after reading: %d", n)
	}
	if got := testkit.Count(t, `SELECT count(*) FROM chat_unread_mail WHERE chat_id = $1`, chat2); got != 0 {
		t.Errorf("the row must be gone: %d", got)
	}
}

// Удалённое сообщение писем не вызывает; старые непрочитанные тоже.
func TestUnreadMailSkipsDeletedAndOld(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	m := w.send(sci, chat, "Передумал")
	if err := w.svc.Delete(bg, sci.User, chat, m.Seq); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n != 0 {
		t.Errorf("mails about a deleted message: %d", n)
	}
	if got := testkit.Count(t, `SELECT count(*) FROM chat_unread_mail WHERE chat_id = $1`, chat); got != 0 {
		t.Errorf("the row must be gone: %d", got)
	}

	// Непрочитанному больше срока писем нет, а строка убирается.
	w.send(sci, chat, "Давно")
	w.clock.Advance(UnreadMailMaxAge + time.Hour)
	if n := w.sendMail(); n != 0 {
		t.Errorf("mails about old unread: %d", n)
	}
	if got := testkit.Count(t, `SELECT count(*) FROM chat_unread_mail WHERE chat_id = $1`, chat); got != 0 {
		t.Errorf("stale rows: %d", got)
	}
}

// Анонимному учёному письмо организации не называет его (T-97).
func TestUnreadMailKeepsAnonymity(t *testing.T) {
	const name = "Кирилл Заречный"
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant(name)
	w.privacy(sci, "anon")
	offer := w.offer(sci, w.vacancy.ID, "pending")
	chat := w.open(sci, w.vacancy.ID, nil, &offer)
	w.send(sci, chat, "Здравствуйте")
	w.clock.Advance(UnreadMailAfter + time.Minute)
	if n := w.sendMail(); n == 0 {
		t.Fatal("no mails")
	}
	for _, m := range outboxFor(t, w.tm.Owner, w.tm.HR, w.tm.HeadA) {
		if strings.Contains(m.subject, name) || strings.Contains(m.body, name) || strings.Contains(m.body, "Заречн") {
			t.Errorf("the name leaks: %q / %q", m.subject, m.body)
		}
		if strings.Contains(m.subject, "Новое сообщение") && !strings.Contains(m.body, "От: Анонимный соискатель") {
			t.Errorf("body: %q", m.body)
		}
	}
}

// Несколько копий и повторы не двоят письма (T-102).
func TestUnreadMailNeverDoubles(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	var chats []uuid.UUID
	for i := range 6 {
		sci := w.Applicant("Учёный " + string(rune('А'+i)))
		c := w.chatOf(sci, w.vacancy.ID, "sent")
		w.send(sci, c, "Сообщение")
		chats = append(chats, c)
	}
	w.clock.Advance(UnreadMailAfter + time.Minute)

	staff := []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA}
	before := len(outboxFor(t, staff...))
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 4 { // четыре «копии» задачи одновременно, по две попытки каждая
		wg.Go(func() {
			svc := relaxed(NewService(testkit.Pool))
			svc.now = w.clock.Now
			svc.WithMail(MailConfig{ProductName: "SciBox", PublicURL: "https://scibox.example.ru"})
			for range 2 {
				n, err := svc.SendUnreadMail(bg)
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				total += n
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	want := len(chats) * len(staff)
	if total != want {
		t.Errorf("queued %d mails, want exactly %d", total, want)
	}
	if got := len(outboxFor(t, staff...)) - before; got != want {
		t.Errorf("outbox grew by %d, want %d", got, want)
	}
}

func TestUnreadMailNeedsPublicURL(t *testing.T) {
	w := newWorld(t)
	if _, err := w.svc.SendUnreadMail(bg); err == nil {
		t.Error("the job must refuse to run without the site address")
	}
}

func TestUnreadMailStatsAndPrune(t *testing.T) {
	w := newWorld(t)
	w.withMail()
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	st, err := w.svc.UnreadMailStats(bg)
	if err != nil {
		t.Fatal(err)
	}
	base := st.Waiting
	w.send(sci, chat, "Привет")
	w.clock.Advance(10 * time.Minute)
	st, err = w.svc.UnreadMailStats(bg)
	if err != nil {
		t.Fatal(err)
	}
	if st.Waiting != base+1 || st.OldestWaiting < 10*time.Minute {
		t.Errorf("stats: %+v (before %d)", st, base)
	}

	// Журнал событий чистится по сроку.
	events := testkit.Count(t, `SELECT count(*) FROM chat_events`)
	if events == 0 {
		t.Fatal("no events were published")
	}
	if n, err := w.svc.PruneEvents(bg, time.Hour, 10, 5); err != nil || n != 0 {
		t.Errorf("pruned fresh events: %d %v", n, err)
	}
	w.clock.Advance(2 * time.Hour)
	n, err := w.svc.PruneEvents(bg, time.Hour, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 || testkit.Count(t, `SELECT count(*) FROM chat_events WHERE created_at < $1`, w.clock.Now().Add(-time.Hour)) != 0 {
		t.Errorf("pruned %d", n)
	}
}

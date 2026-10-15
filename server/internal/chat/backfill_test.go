package chat

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
)

// Срез 38 (D-240): перенос прежних откликов и приглашений в переписки.

func (w *world) legacyInvitation(app uuid.UUID, kind, status string, at time.Time, answerAt *time.Time, answeredAt *time.Time, contact string) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	var place *string
	if kind == "interview" {
		place = str("online")
	}
	err := testkit.Pool.QueryRow(bg, `INSERT INTO application_invitations
		(application_id, kind, status, message, starts_at, place_kind, place, answer_at, answer_contact, answered_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'Приглашаем', CASE WHEN $2 = 'interview' THEN $4::timestamptz END, $5, CASE WHEN $2 = 'interview' THEN 'https://example.org/room' ELSE '' END, $6, $7, $8, $4, $4) RETURNING id`,
		app, kind, status, at, place, answerAt, contact, answeredAt).Scan(&id)
	if err != nil {
		w.T.Fatal(err)
	}
	return id
}

func (w *world) setApplicationTimes(id uuid.UUID, status string, created, changed time.Time, note string) {
	w.T.Helper()
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET status = $2, created_at = $3, status_changed_at = $4, updated_at = $4, decision_note = $5 WHERE id = $1`,
		id, status, created, changed, note); err != nil {
		w.T.Fatal(err)
	}
}

type backfilled struct {
	Type string
	At   time.Time
}

func (w *world) backfilledLines(c uuid.UUID) []backfilled {
	w.T.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT event_type, created_at FROM chat_messages WHERE chat_id = $1 ORDER BY seq`, c)
	if err != nil {
		w.T.Fatal(err)
	}
	defer rows.Close()
	var out []backfilled
	for rows.Next() {
		var l backfilled
		if err := rows.Scan(&l.Type, &l.At); err != nil {
			w.T.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func typesOf(ls []backfilled) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Type)
	}
	return out
}

func (w *world) chatByApplication(app uuid.UUID) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, app).Scan(&id); err != nil {
		w.T.Fatal(err)
	}
	return id
}

func TestBackfillBuildsChatsFromLegacyRecords(t *testing.T) {
	w := newWorld(t)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-72 * time.Hour)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }

	// 1. Отклик с собеседованием, которое предложили перенести и приняли, контактами и отменённым запросом; принят.
	rich := w.Applicant("Богатый Отклик")
	richApp := w.application(rich, w.vacancy.ID, "accepted")
	w.setApplicationTimes(richApp, "accepted", at(0), at(30), "Рады видеть вас")
	proposed := at(60)
	answered := at(5)
	w.legacyInvitation(richApp, "interview", "confirmed", at(2), &proposed, &answered, "")
	w.legacyInvitation(richApp, "contacts", "shared", at(10), nil, nil, "")
	reqAnswer := at(12)
	w.legacyInvitation(richApp, "request_contacts", "answered", at(11), nil, &reqAnswer, "+7 913 555-66-77")
	w.legacyInvitation(richApp, "interview", "cancelled", at(20), nil, nil, "")

	// 2. Простой отклик: просмотрен.
	viewed := w.Applicant("Просмотренный")
	viewedApp := w.application(viewed, w.vacancy.ID, "viewed")
	w.setApplicationTimes(viewedApp, "viewed", at(1), at(3), "")

	// 3. Отказ и отзыв.
	rejected := w.Applicant("Отказанный")
	rejectedApp := w.application(rejected, w.vacancy.ID, "rejected")
	w.setApplicationTimes(rejectedApp, "rejected", at(1), at(8), "Выбрали другого")
	withdrawn := w.Applicant("Отозвавший")
	withdrawnApp := w.application(withdrawn, w.vacancy.ID, "withdrawn")
	w.setApplicationTimes(withdrawnApp, "withdrawn", at(1), at(9), "")

	// 4. Отозванный отклик и новый живой отклик той же пары: переписка одна, следует за живым.
	again := w.Applicant("Дважды")
	oldApp := w.application(again, w.vacancy.ID, "withdrawn")
	w.setApplicationTimes(oldApp, "withdrawn", at(1), at(2), "")
	newApp := w.application(again, w.vacancy.ID, "sent")
	w.setApplicationTimes(newApp, "sent", at(4), at(4), "")

	// 5. Приглашение на вакансию, ответ, и приглашение, которое отозвали.
	invited := w.Applicant("Приглашённый")
	answeredOffer := w.offer(invited, w.vacancy.ID, "interested")
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancy_offers SET created_at = $2, answered_at = $3, updated_at = $3, answer_note = 'Интересно' WHERE id = $1`, answeredOffer, at(1), at(6)); err != nil {
		t.Fatal(err)
	}
	cancelled := w.Applicant("Отозванный")
	cancelledOffer := w.offer(cancelled, w.other.ID, "cancelled")
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancy_offers SET created_at = $2, updated_at = $3 WHERE id = $1`, cancelledOffer, at(1), at(7)); err != nil {
		t.Fatal(err)
	}

	// 6. Предложение, потом отклик: одна переписка с обеими связями.
	both := w.Applicant("И то и другое")
	bothOffer := w.offer(both, w.vacancy.ID, "interested")
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancy_offers SET created_at = $2, answered_at = $3, updated_at = $3 WHERE id = $1`, bothOffer, at(1), at(2)); err != nil {
		t.Fatal(err)
	}
	bothApp := w.application(both, w.vacancy.ID, "sent")
	w.setApplicationTimes(bothApp, "sent", at(3), at(3), "")

	// 7. Стёртый аккаунт: отклик обезличен, переписки не будет.
	erased := w.Applicant("Стёртый")
	erasedApp := w.application(erased, w.vacancy.ID, "sent")
	if _, err := testkit.Pool.Exec(bg, `UPDATE applications SET anonymized_at = now() WHERE id = $1`, erasedApp); err != nil {
		t.Fatal(err)
	}

	res, err := Backfill(bg, testkit.Pool)
	if err != nil {
		t.Fatal(err)
	}
	// Чужие пары из общей тестовой базы тоже переносятся; нужные строки проверяем по своим данным.
	if res.Pairs < 8 || res.Lines < 20 {
		t.Errorf("result = %+v", res)
	}

	check := func(name string, c uuid.UUID, want ...string) {
		t.Helper()
		got := w.backfilledLines(c)
		if !slices.Equal(typesOf(got), want) {
			t.Errorf("%s: lines = %v, want %v", name, typesOf(got), want)
		}
		if !slices.IsSortedFunc(got, func(a, b backfilled) int { return a.At.Compare(b.At) }) {
			t.Errorf("%s: lines are not in order of time: %+v", name, got)
		}
	}
	check("rich", w.chatByApplication(richApp),
		EventApplicationSent, EventInvitationCreated, EventInvitationAnswered, EventInvitationAccepted, EventInvitationCreated,
		EventInvitationCreated, EventInvitationAnswered, EventInvitationCreated, EventInvitationCancelled, EventApplicationDecided)
	check("viewed", w.chatByApplication(viewedApp), EventApplicationSent, EventApplicationViewed)
	check("rejected", w.chatByApplication(rejectedApp), EventApplicationSent, EventApplicationDecided)
	check("withdrawn", w.chatByApplication(withdrawnApp), EventApplicationSent, EventApplicationWithdrawn)
	check("two applications", w.chatByApplication(newApp), EventApplicationSent, EventApplicationWithdrawn, EventApplicationSent)
	check("offer answered", w.chatOfOffer(answeredOffer), EventOfferCreated, EventOfferAnswered)
	check("offer cancelled", w.chatOfOffer(cancelledOffer), EventOfferCreated, EventOfferCancelled)
	check("offer and application", w.chatByApplication(bothApp), EventOfferCreated, EventOfferAnswered, EventApplicationSent)
	if w.chatOfOffer(bothOffer) != w.chatByApplication(bothApp) {
		t.Error("the offer and the application of a pair must share one chat")
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE user_id = $1`, erased.ID); n != 0 {
		t.Errorf("an erased application got a chat: %d", n)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE application_id = $1`, oldApp); n != 0 {
		t.Errorf("the withdrawn application is linked while a live one exists: %d", n)
	}

	// Состояние переписки то же, что у отклика; перенесённое прочитано у обеих сторон.
	for _, c := range []struct {
		who  testkit.Person
		chat uuid.UUID
		open bool
	}{{rich, w.chatByApplication(richApp), true}, {rejected, w.chatByApplication(rejectedApp), false}, {withdrawn, w.chatByApplication(withdrawnApp), false}} {
		h := w.header(c.who, c.chat)
		if h.CanWrite != c.open {
			t.Errorf("%s: can_write = %v", c.who.Name, h.CanWrite)
		}
		if h.Read.Mine == 0 || h.Read.Mine != h.Read.Theirs {
			t.Errorf("%s: read = %+v", c.who.Name, h.Read)
		}
	}
	list, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Counts.Unread != 0 {
		t.Errorf("unread of the organization after the transfer: %d", list.Counts.Unread)
	}
	// Время последнего сообщения в списке: время самого позднего события, а не время переноса.
	richChat := w.chatByApplication(richApp)
	var updated time.Time
	if err := testkit.Pool.QueryRow(bg, `SELECT updated_at FROM chats WHERE id = $1`, richChat).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if !updated.Equal(at(30)) {
		t.Errorf("updated_at of the chat = %v, want the time of the decision %v", updated, at(30))
	}

	// Повторный перенос ничего не меняет.
	again2, err := Backfill(bg, testkit.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if again2.Pairs != 0 || again2.Lines != 0 {
		t.Errorf("second run = %+v", again2)
	}
	if got := w.backfilledLines(w.chatByApplication(richApp)); len(got) != 10 {
		t.Errorf("lines after the second run: %d", len(got))
	}
}

func (w *world) chatOfOffer(offer uuid.UUID) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE offer_id = $1`, offer).Scan(&id); err != nil {
		w.T.Fatal(err)
	}
	return id
}

// Вид ответа выводится из того, что человек ответил.
func TestAnswerOf(t *testing.T) {
	at := time.Now().UTC()
	for name, c := range map[string]struct {
		inv  dbgen.ApplicationInvitation
		want string
	}{
		"proposed time":      {dbgen.ApplicationInvitation{Kind: "interview", AnswerAt: &at}, "proposed"},
		"contact":            {dbgen.ApplicationInvitation{Kind: "request_contacts", AnswerContact: "+7 900"}, "answered"},
		"only a time":        {dbgen.ApplicationInvitation{Kind: "request_contacts", AnswerTime: "после 15:00"}, "answered"},
		"empty reply":        {dbgen.ApplicationInvitation{Kind: "request_contacts"}, "answered"},
		"plain confirmation": {dbgen.ApplicationInvitation{Kind: "interview"}, "confirmed"},
	} {
		if got := answerOf(c.inv); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

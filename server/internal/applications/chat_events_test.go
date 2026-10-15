package applications

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/chat"
	"scibox/server/internal/testkit"
)

// Срез 38 (T-97): каждое событие отклика пишет в переписку ровно одну строку в той же транзакции.

type chatLine struct {
	Type string
	Data map[string]any
	At   time.Time
}

// linesOf — строки о событиях переписки отклика по порядку.
func linesOf(t *testing.T, app uuid.UUID) []chatLine {
	t.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT m.event_type, m.event_data, m.created_at FROM chat_messages m
		JOIN chats c ON c.id = m.chat_id WHERE c.application_id = $1 AND m.kind = 'system' ORDER BY m.seq`, app)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []chatLine
	for rows.Next() {
		var l chatLine
		var raw []byte
		if err := rows.Scan(&l.Type, &raw, &l.At); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &l.Data); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func typesOf(lines []chatLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Type)
	}
	return out
}

func wantLines(t *testing.T, app uuid.UUID, want ...string) []chatLine {
	t.Helper()
	got := linesOf(t, app)
	if !slices.Equal(typesOf(got), want) {
		t.Fatalf("lines of the chat = %v, want %v", typesOf(got), want)
	}
	return got
}

// Отклик, просмотр, приглашения, ответы, принятое время, замена собеседования, отмена, решение: по строке на событие,
// повторное открытие карточки строк не добавляет.
func TestEveryApplicationEventWritesOneLine(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)

	got := wantLines(t, d.ID, chat.EventApplicationSent)
	if got[0].Data["application_id"] != d.ID.String() || got[0].Data["cover_letter"] == "" {
		t.Errorf("data of the sent line = %v", got[0].Data)
	}
	// Отклик связан с переписки пары «вакансия × учёный», и она видна обеим сторонам в ответах отклика.
	if d.Chat == nil || !d.Chat.CanWrite || d.Chat.Started {
		t.Errorf("link of a new application = %+v", d.Chat)
	}

	for range 3 {
		if _, err := w.svc.Get(bg, w.tm.Owner.User, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed)

	interview := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	got = wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed, chat.EventInvitationCreated)
	inv, _ := got[2].Data["invitation"].(map[string]any)
	if inv["id"] != interview.ID.String() || inv["kind"] != "interview" || inv["status"] != "pending" || inv["can_answer"] != false {
		t.Errorf("snapshot of the invitation = %v", inv)
	}
	if _, has := inv["answer"]; !has || inv["answer"] != nil {
		t.Errorf("a new invitation has no answer: %v", inv["answer"])
	}

	if err := w.svc.Answer(bg, me.User, d.ID, interview.ID, AnswerInput{Action: AnswerPropose, ProposedAt: w.when(96 * time.Hour), Note: "Удобнее в четверг"}); err != nil {
		t.Fatal(err)
	}
	got = wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed, chat.EventInvitationCreated, chat.EventInvitationAnswered)
	if a := got[3].Data; a["answer"] != "proposed" || a["note"] != "Удобнее в четверг" || a["proposed_at"] == nil || a["invitation_id"] != interview.ID.String() || a["kind"] != "interview" {
		t.Errorf("data of the answer = %v", a)
	}

	if err := w.svc.AcceptProposal(bg, w.tm.Owner.User, d.ID, interview.ID); err != nil {
		t.Fatal(err)
	}
	got = wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed, chat.EventInvitationCreated, chat.EventInvitationAnswered, chat.EventInvitationAccepted)
	if got[4].Data["starts_at"] == nil || got[4].Data["invitation_id"] != interview.ID.String() {
		t.Errorf("data of the accepted time = %v", got[4].Data)
	}

	// Новое собеседование заменяет прежнее открытое: прежнее закрывается своей строкой, потом новое.
	second := w.mustInvite(w.tm.Owner, d.ID, w.interview())
	got = wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed, chat.EventInvitationCreated, chat.EventInvitationAnswered,
		chat.EventInvitationAccepted, chat.EventInvitationCancelled, chat.EventInvitationCreated)
	if got[5].Data["invitation_id"] != interview.ID.String() || got[5].Data["kind"] != "interview" {
		t.Errorf("replaced interview: %v", got[5].Data)
	}

	if err := w.svc.CancelInvitation(bg, w.tm.Owner.User, d.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	request := w.mustInvite(w.tm.Owner, d.ID, requestIn())
	if err := w.svc.Answer(bg, me.User, d.ID, request.ID, AnswerInput{Action: AnswerReply, Contact: "+7 913 555-66-77", Time: "после 15:00"}); err != nil {
		t.Fatal(err)
	}
	got = linesOf(t, d.ID)
	last := got[len(got)-1]
	if last.Type != chat.EventInvitationAnswered || last.Data["answer"] != "answered" || last.Data["contact"] != "+7 913 555-66-77" || last.Data["time"] != "после 15:00" {
		t.Errorf("answer with contacts: %+v", last)
	}

	contacts := w.mustInvite(w.tm.Owner, d.ID, contactsIn())
	got = linesOf(t, d.ID)
	if c, _ := got[len(got)-1].Data["invitation"].(map[string]any); got[len(got)-1].Type != chat.EventInvitationCreated || c["status"] != "shared" || c["id"] != contacts.ID.String() {
		t.Errorf("contacts line: %+v", got[len(got)-1])
	}

	before := len(got)
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, "Выбрали другого кандидата"); err != nil {
		t.Fatal(err)
	}
	got = linesOf(t, d.ID)
	// Решение пишет одну строку; открытых приглашений к этому моменту нет (интервью отменено, запрос отвечен, контакты не открыты).
	if len(got) != before+1 || got[before].Type != chat.EventApplicationDecided {
		t.Fatalf("after the decision: %v", typesOf(got[before:]))
	}
	if got[before].Data["decision"] != "rejected" || got[before].Data["note"] != "Выбрали другого кандидата" {
		t.Errorf("data of the decision: %v", got[before].Data)
	}
}

// Отказ закрывает открытые приглашения: о каждом строка «отменено»; то же при отзыве отклика.
func TestDecisionAndWithdrawalCloseInvitationsWithLines(t *testing.T) {
	w := newWorld(t)

	rejected := w.mustApply(w.Applicant("Первая"), nil)
	open := w.mustInvite(w.tm.Owner, rejected.ID, w.interview())
	if err := w.svc.Decide(bg, w.tm.Owner.User, rejected.ID, StatusRejected, ""); err != nil {
		t.Fatal(err)
	}
	got := wantLines(t, rejected.ID, chat.EventApplicationSent, chat.EventInvitationCreated,
		chat.EventApplicationDecided, chat.EventInvitationCancelled)
	if got[3].Data["invitation_id"] != open.ID.String() {
		t.Errorf("cancelled invitation: %v", got[3].Data)
	}

	me := w.Applicant("Вторая")
	withdrawn := w.mustApply(me, nil)
	w.mustInvite(w.tm.Owner, withdrawn.ID, w.interview())
	if err := w.svc.Withdraw(bg, me.User, withdrawn.ID); err != nil {
		t.Fatal(err)
	}
	wantLines(t, withdrawn.ID, chat.EventApplicationSent, chat.EventInvitationCreated,
		chat.EventApplicationWithdrawn, chat.EventInvitationCancelled)

	// Отказ с неверным решением, повторная отмена, ответ на закрытое: строк не добавляют.
	for _, run := range []func() error{
		func() error { return w.svc.Decide(bg, w.tm.Owner.User, rejected.ID, StatusRejected, "") },
		func() error { return w.svc.Withdraw(bg, me.User, withdrawn.ID) },
		func() error { return w.svc.CancelInvitation(bg, w.tm.Owner.User, rejected.ID, open.ID) },
		func() error {
			return w.svc.Answer(bg, me.User, withdrawn.ID, uuid.New(), AnswerInput{Action: AnswerConfirm})
		},
	} {
		if err := run(); err == nil {
			t.Error("a repeated action succeeded")
		}
	}
	wantLines(t, rejected.ID, chat.EventApplicationSent, chat.EventInvitationCreated,
		chat.EventApplicationDecided, chat.EventInvitationCancelled)
	wantLines(t, withdrawn.ID, chat.EventApplicationSent, chat.EventInvitationCreated,
		chat.EventApplicationWithdrawn, chat.EventInvitationCancelled)
}

// Одновременный просмотр двумя сотрудниками даёт одну строку «просмотрен».
func TestViewedIsWrittenOnce(t *testing.T) {
	w := newWorld(t)
	d := w.mustApply(w.Applicant("Мария"), nil)
	done := make(chan error, 2)
	for _, who := range []testkit.Person{w.tm.Owner, w.tm.HeadA} {
		go func() { _, err := w.svc.Get(bg, who.User, d.ID); done <- err }()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	wantLines(t, d.ID, chat.EventApplicationSent, chat.EventApplicationViewed)
}

// Откат события откатывает переписку и строку: при сбое базы на любом обращении переписка есть тогда и только тогда,
// когда есть отклик, и в ней ровно строка об отправке.
func TestFailedApplyLeavesNoChatBehind(t *testing.T) {
	for n := int32(1); n <= 60; n++ {
		w := newWorld(t)
		me := w.Applicant("Мария")
		db, calls := testkit.Faulty(testkit.Pool, n)
		s := svcOn(w, db)
		_, err := s.Apply(bg, me.User, w.input(me), nil)
		apps := testkit.Count(t, `SELECT count(*) FROM applications WHERE user_id = $1`, me.ID)
		chats := testkit.Count(t, `SELECT count(*) FROM chats WHERE user_id = $1`, me.ID)
		lines := testkit.Count(t, `SELECT count(*) FROM chat_messages m JOIN chats c ON c.id = m.chat_id WHERE c.user_id = $1`, me.ID)
		if apps != chats || apps != lines {
			t.Fatalf("fault #%d (err = %v): applications %d, chats %d, lines %d", n, err, apps, chats, lines)
		}
		if calls.Load() < n {
			return
		}
	}
	t.Fatal("too many database calls")
}

// Решение, поставленное при сбое, не оставляет строки: статус и строка меняются вместе.
func TestFailedDecisionLeavesNoLineBehind(t *testing.T) {
	for n := int32(1); n <= 60; n++ {
		w := newWorld(t)
		d := w.mustApply(w.Applicant("Мария"), nil)
		db, calls := testkit.Faulty(testkit.Pool, n)
		err := svcOn(w, db).Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, "Нет")
		decided := w.statusOf(d.ID) == StatusRejected
		lines := len(linesOf(t, d.ID))
		if decided != (lines == 2) { // «отклик отправлен» и «решение»
			t.Fatalf("fault #%d (err = %v): status rejected = %v, lines = %d", n, err, decided, lines)
		}
		if calls.Load() < n {
			return
		}
	}
	t.Fatal("too many database calls")
}

// Приглашения нельзя посылать человеку, заблокировавшему организацию (T-105); решение по отклику ставить можно.
func TestBlockedOrganizationCannotInvite(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	chats := chat.NewService(testkit.Pool)
	var chatID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, d.ID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.Block(bg, me.User, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Invite(bg, w.tm.Owner.User, d.ID, w.interview()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("invite = %v, want ErrBlocked", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM application_invitations WHERE application_id = $1`, d.ID); n != 0 {
		t.Errorf("invitations = %d", n)
	}
	// Закрытая блокировкой переписка видна в ответах отклика: писать нельзя.
	got, err := w.svc.Get(bg, me.User, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Chat == nil || got.Chat.CanWrite {
		t.Errorf("link of a blocked chat = %+v", got.Chat)
	}
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, ""); err != nil {
		t.Errorf("a decision stays possible: %v", err)
	}
	if err := chats.Unblock(bg, me.User, uuid.Nil); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unblock of nothing = %v", err)
	}
}

// Ссылки на переписку в списках: «Мои отклики» и «Отклики» организации.
func TestListsCarryTheChatLink(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	var chatID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE application_id = $1`, d.ID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	mine, err := w.svc.Mine(bg, me.User, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Items) != 1 || mine.Items[0].Chat == nil || mine.Items[0].Chat.ID != chatID || mine.Items[0].Chat.Unread != 0 {
		t.Fatalf("my applications: %+v", mine.Items)
	}
	cands, err := w.svc.Candidates(bg, w.tm.Owner.User, CandidateFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands.Items) != 1 || cands.Items[0].Chat == nil || cands.Items[0].Chat.ID != chatID {
		t.Fatalf("candidates: %+v", cands.Items)
	}
	// Для организации строка «отклик отправлен» — непрочитанное, для соискателя нет.
	if cands.Items[0].Chat.Unread != 1 || cands.Items[0].Chat.Started {
		t.Errorf("link for the organization: %+v", cands.Items[0].Chat)
	}
	if _, _, err := chatsSend(t, me, chatID, "Здравствуйте"); err != nil {
		t.Fatal(err)
	}
	mine, _ = w.svc.Mine(bg, me.User, 20, 0)
	if !mine.Items[0].Chat.Started {
		t.Error("a chat with a message is started")
	}
	var raw []byte
	raw, _ = json.Marshal(mine.Items[0])
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if _, ok := back["chat"].(map[string]any); !ok {
		t.Errorf("the field chat of the response: %s", raw)
	}
	if err := w.svc.Withdraw(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	mine, _ = w.svc.Mine(bg, me.User, 20, 0)
	if mine.Items[0].Chat == nil || mine.Items[0].Chat.CanWrite {
		t.Errorf("a withdrawn application closes the chat: %+v", mine.Items[0].Chat)
	}
}

func chatsSend(t *testing.T, who testkit.Person, id uuid.UUID, text string) (chat.Message, bool, error) {
	t.Helper()
	return chat.NewService(testkit.Pool).Send(bg, who.User, id, chat.SendInput{Text: text, ClientID: uuid.NewString()})
}

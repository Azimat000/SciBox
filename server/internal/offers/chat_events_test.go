package offers

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/chat"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
)

// Срез 38 (T-97, T-105): строки о приглашениях на вакансию в переписке, анонимность на каждой её части, блокировка.

func (w *world) chatOf(offer uuid.UUID) uuid.UUID {
	w.T.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM chats WHERE offer_id = $1`, offer).Scan(&id); err != nil {
		w.T.Fatal(err)
	}
	return id
}

func (w *world) lines(chatID uuid.UUID) []string {
	w.T.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT event_type FROM chat_messages WHERE chat_id = $1 AND kind = 'system' ORDER BY seq`, chatID)
	if err != nil {
		w.T.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			w.T.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// Приглашение, ответ и отзыв: по строке на событие; повторный ответ и повторный отзыв строк не добавляют.
func TestOfferEventsWriteOneLineEach(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Игорь Лебедев", "public")
	answered := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Присоединяйтесь")
	cancelled := w.mustInvite(w.tm.HR, w.tm.Published(&w.tm.UnitA.ID).ID, profile, "")

	if answered.Chat == nil || !answered.Chat.CanWrite || answered.Chat.Started {
		t.Errorf("link in the answer of the invitation = %+v", answered.Chat)
	}
	c := w.chatOf(answered.ID)
	if got := w.lines(c); !slices.Equal(got, []string{chat.EventOfferCreated}) {
		t.Fatalf("after the invitation: %v", got)
	}
	var data []byte
	if err := testkit.Pool.QueryRow(bg, `SELECT event_data FROM chat_messages WHERE chat_id = $1 AND seq = 1`, c).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	if d["offer_id"] != answered.ID.String() || d["message"] != "Присоединяйтесь" || d["status"] != "pending" || d["can_answer"] != false {
		t.Errorf("data of the invitation line = %v", d)
	}

	if err := w.svc.Answer(bg, sci.User, answered.ID, AnswerInput{Action: ActionInterested, Note: "Интересно"}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Answer(bg, sci.User, answered.ID, AnswerInput{Action: ActionDeclined}); !errors.Is(err, ErrBadState) {
		t.Fatalf("second answer = %v", err)
	}
	if got := w.lines(c); !slices.Equal(got, []string{chat.EventOfferCreated, chat.EventOfferAnswered}) {
		t.Fatalf("after the answer: %v", got)
	}
	if err := testkit.Pool.QueryRow(bg, `SELECT event_data FROM chat_messages WHERE chat_id = $1 AND seq = 2`, c).Scan(&data); err != nil {
		t.Fatal(err)
	}
	d = nil
	_ = json.Unmarshal(data, &d)
	if d["answer"] != "interested" || d["note"] != "Интересно" || d["offer_id"] != answered.ID.String() {
		t.Errorf("data of the answer line = %v", d)
	}

	if err := w.svc.Cancel(bg, w.tm.HR.User, cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Cancel(bg, w.tm.HR.User, cancelled.ID); !errors.Is(err, ErrBadState) {
		t.Fatalf("second cancel = %v", err)
	}
	c2 := w.chatOf(cancelled.ID)
	if got := w.lines(c2); !slices.Equal(got, []string{chat.EventOfferCreated, chat.EventOfferCancelled}) {
		t.Fatalf("after the cancel: %v", got)
	}
	// Отозванное приглашение закрывает переписку для обеих сторон.
	h, err := chat.NewService(testkit.Pool).Get(bg, w.tm.HR.User, c2)
	if err != nil {
		t.Fatal(err)
	}
	if h.CanWrite || h.ClosedReason == nil || *h.ClosedReason != chat.ClosedOfferCancelled {
		t.Errorf("header of the cancelled offer: %+v", h)
	}
}

// Приглашение откликнувшемуся ранее человеку (отзыв отклика, потом приглашение) открывает ту же переписку: она следует
// за приглашением, а не остаётся закрытой отзывом.
func TestOfferAfterWithdrawnApplicationReopensTheChat(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Игорь Лебедев", "public")
	w.apply(sci, w.vacancy.ID, "withdrawn")
	var appID uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT id FROM applications WHERE user_id = $1`, sci.ID).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	if _, err := chat.Open(bg, dbgen.New(testkit.Pool), chat.OpenInput{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &appID}, w.clock.Now()); err != nil {
		t.Fatal(err)
	}
	o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Вернитесь к нам")
	c := w.chatOf(o.ID)
	h, err := chat.NewService(testkit.Pool).Get(bg, sci.User, c)
	if err != nil {
		t.Fatal(err)
	}
	if !h.CanWrite || h.Status.Source != "offer" || !h.NeedsAnswer {
		t.Errorf("header after the new invitation: %+v", h)
	}
	if h.ApplicationID != nil {
		t.Errorf("the withdrawn application stays linked: %v", h.ApplicationID)
	}
}

// Заблокировавшему организацию учёному она приглашений не шлёт, остальным шлёт.
func TestBlockedOrganizationCannotInviteOnAVacancy(t *testing.T) {
	w := newWorld(t)
	sci, profile := w.scientist("Игорь Лебедев", "public")
	first := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "")
	chats := chat.NewService(testkit.Pool)
	if _, err := chats.Block(bg, sci.User, w.chatOf(first.ID)); err != nil {
		t.Fatal(err)
	}
	other := w.tm.Published(&w.tm.UnitA.ID)
	before := w.snap(other.ID, sci, w.tm.HR)
	if _, err := w.invite(w.tm.HR, other.ID, profile, "Ещё одна вакансия"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("invite after the block = %v", err)
	}
	if after := w.snap(other.ID, sci, w.tm.HR); after != before {
		t.Errorf("a refusal changed the state: %+v → %+v", before, after)
	}
	// Блокировка организации не трогает чужую: её сотрудник по-прежнему приглашает.
	foreign := testkit.NewWorld(t).Team()
	fv := foreign.Published(&foreign.UnitA.ID)
	if _, err := w.svc.Invite(bg, foreign.HR.User, InviteInput{VacancyID: fv.ID, ProfileID: profile}); err != nil {
		t.Errorf("another organization is refused: %v", err)
	}
	if err := chats.Unblock(bg, sci.User, w.tm.OrgID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.invite(w.tm.HR, other.ID, profile, "Ещё одна вакансия"); err != nil {
		t.Errorf("invite after the unblock = %v", err)
	}
}

// Анонимность на каждой части переписки (T-97): шапка, список, поиск по имени, лента, строки о событиях, число и отбор
// вакансий, список приглашений организации, уведомления и письма сотрудникам. Пока человек не открылся, ни в одной нет
// ни имени, ни почты; после «Интересно» имя видно в шапке, списке и поиске, но почты по-прежнему нет нигде.
func TestAnonymousChatLeaksNothingToTheOrganization(t *testing.T) {
	type stage struct {
		name     string
		revealed bool
		run      func(t *testing.T, w *world, sci testkit.Person, offer uuid.UUID, c uuid.UUID)
	}
	chats := chat.NewService(testkit.Pool)
	say := func(t *testing.T, who testkit.Person, c uuid.UUID, text string) {
		t.Helper()
		if _, _, err := chats.Send(bg, who.User, c, chat.SendInput{Text: text, ClientID: uuid.NewString()}); err != nil {
			t.Fatal(err)
		}
	}
	stages := []stage{
		{"invited", false, func(t *testing.T, w *world, sci testkit.Person, offer, c uuid.UUID) {}},
		{"conversation", false, func(t *testing.T, w *world, sci testkit.Person, offer, c uuid.UUID) {
			say(t, w.tm.HR, c, "Расскажите, чем вы занимаетесь?")
			say(t, sci, c, "Занимаюсь катализом, подробности позже.")
		}},
		{"declined", false, func(t *testing.T, w *world, sci testkit.Person, offer, c uuid.UUID) {
			if err := w.svc.Answer(bg, sci.User, offer, AnswerInput{Action: ActionDeclined, Note: "Не сейчас, спасибо"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"interested", true, func(t *testing.T, w *world, sci testkit.Person, offer, c uuid.UUID) {
			if err := w.svc.Answer(bg, sci.User, offer, AnswerInput{Action: ActionInterested}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			w := newWorld(t)
			sci, profile := w.anonScientist()
			o := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Приглашаем вас")
			c := w.chatOf(o.ID)
			st.run(t, w, sci, o.ID, c)

			secrets := []string{"Седова", "Анна", "anna.sedova", sci.Email, "Редкая"}
			leaks := func(part string, v any) {
				t.Helper()
				raw, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range secrets {
					// После «Интересно» имя организации принадлежит: оно в шапке и в списке, но не почта.
					if st.revealed && s != sci.Email && s != "anna.sedova" {
						continue
					}
					if strings.Contains(string(raw), s) {
						t.Errorf("%s: %q is in %s", part, s, raw)
					}
				}
			}
			for _, staff := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA} {
				header, err := chats.Get(bg, staff.User, c)
				if err != nil {
					t.Fatal(err)
				}
				if st.revealed == header.Counterpart.Anonymous {
					t.Errorf("header: anonymous = %v at stage %s", header.Counterpart.Anonymous, st.name)
				}
				if !st.revealed && header.Counterpart.Name != "" {
					t.Errorf("header name = %q", header.Counterpart.Name)
				}
				leaks("header", header)

				list, err := chats.List(bg, staff.User, chat.ListQuery{Side: "org"})
				if err != nil {
					t.Fatal(err)
				}
				if len(list.Items) != 1 {
					t.Fatalf("list of %s: %d items", staff.Name, len(list.Items))
				}
				leaks("list", list)

				// Поиск по имени находит человека только тогда, когда имя организации показывается.
				found, err := chats.List(bg, staff.User, chat.ListQuery{Side: "org", Search: "Седова"})
				if err != nil {
					t.Fatal(err)
				}
				if st.revealed != (len(found.Items) == 1) {
					t.Errorf("search by the name found %d chats at stage %s", len(found.Items), st.name)
				}
				byEmail, err := chats.List(bg, staff.User, chat.ListQuery{Side: "org", Search: "anna.sedova"})
				if err != nil {
					t.Fatal(err)
				}
				if len(byEmail.Items) != 0 {
					t.Errorf("search by the email found %d chats", len(byEmail.Items))
				}

				page, err := chats.Messages(bg, staff.User, c, chat.MessagesQuery{Limit: 100})
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range page.Items {
					if m.Kind == chat.KindSystem && m.Author != nil {
						t.Errorf("a system line has an author: %+v", m)
					}
					if m.Author != nil && m.Author.Side == chat.SideScientist && !st.revealed && (m.Author.Name != "" || !m.Author.Anonymous) {
						t.Errorf("author of a message: %+v", m.Author)
					}
				}
				leaks("messages", page)

				vacs, err := chats.Vacancies(bg, staff.User)
				if err != nil {
					t.Fatal(err)
				}
				leaks("vacancies", vacs)
				count, err := chats.UnreadCount(bg, staff.User)
				if err != nil {
					t.Fatal(err)
				}
				leaks("unread", count)

				inbox, err := notifications.NewService(testkit.Pool, notifications.Config{ProductName: "SciBox", PublicURL: "http://localhost:5173"}).List(bg, staff.User, 50, 0, false)
				if err != nil {
					t.Fatal(err)
				}
				leaks("notifications", inbox)
				rows, err := testkit.Pool.Query(bg, `SELECT COALESCE(subject, '') || ' ' || COALESCE(body, '') FROM outbox WHERE lower(to_email) = lower($1)`, staff.Email)
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					var mail string
					if err := rows.Scan(&mail); err != nil {
						t.Fatal(err)
					}
					leaks("mail", mail)
				}
				rows.Close()
			}
			sent := w.sent()
			leaks("sent offers", sent)
			// Сама строка о событии в базе: без имени и почты на любом шаге.
			rows, err := testkit.Pool.Query(bg, `SELECT event_data::text FROM chat_messages WHERE chat_id = $1 AND kind = 'system'`, c)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var raw string
				if err := rows.Scan(&raw); err != nil {
					t.Fatal(err)
				}
				for _, s := range secrets {
					if strings.Contains(raw, s) {
						t.Errorf("stored event data holds %q: %s", s, raw)
					}
				}
			}
		})
	}
}

// Приглашение и блокировка в гонке: если приглашение прошло, его строка стоит в переписке раньше строки о блокировке;
// приглашение после блокировки не проходит (T-105).
func TestInviteRacesWithBlock(t *testing.T) {
	chats := chat.NewService(testkit.Pool)
	for range 12 {
		w := newWorld(t)
		sci, profile := w.scientist("Игорь Лебедев", "public")
		first := w.mustInvite(w.tm.HR, w.vacancy.ID, profile, "Первое")
		c := w.chatOf(first.ID)
		second := w.tm.Published(&w.tm.UnitA.ID)
		done := make(chan error, 2)
		go func() { _, err := w.invite(w.tm.HR, second.ID, profile, "Второе"); done <- err }()
		go func() { _, err := chats.Block(bg, sci.User, c); done <- err }()
		for range 2 {
			if err := <-done; err != nil && !errors.Is(err, ErrBlocked) {
				t.Fatalf("race: %v", err)
			}
		}
		var offers, created, blocked int
		if err := testkit.Pool.QueryRow(bg, `SELECT count(*) FROM vacancy_offers WHERE user_id = $1 AND vacancy_id = $2`, sci.ID, second.ID).Scan(&offers); err != nil {
			t.Fatal(err)
		}
		if offers == 0 {
			continue
		}
		err := testkit.Pool.QueryRow(bg, `SELECT c.id FROM chats c JOIN vacancy_offers o ON o.id = c.offer_id WHERE o.vacancy_id = $1 AND o.user_id = $2`, second.ID, sci.ID).Scan(new(uuid.UUID))
		if err != nil {
			t.Fatal(err)
		}
		// Приглашение прошло: строка о нём перед строкой о блокировке в своей переписке.
		if err := testkit.Pool.QueryRow(bg, `SELECT m.seq FROM chat_messages m JOIN chats c ON c.id = m.chat_id JOIN vacancy_offers o ON o.id = c.offer_id
			WHERE o.vacancy_id = $1 AND o.user_id = $2 AND m.event_type = 'offer.created'`, second.ID, sci.ID).Scan(&created); err != nil {
			t.Fatal(err)
		}
		if err := testkit.Pool.QueryRow(bg, `SELECT COALESCE(min(m.seq), 0) FROM chat_messages m JOIN chats c ON c.id = m.chat_id JOIN vacancy_offers o ON o.id = c.offer_id
			WHERE o.vacancy_id = $1 AND o.user_id = $2 AND m.event_type = 'chat.blocked'`, second.ID, sci.ID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked != 0 && blocked < created {
			t.Fatalf("an invitation (line %d) passed after the block (line %d)", created, blocked)
		}
	}
}

package chat

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"scibox/server/internal/testkit"
)

// Анонимному учёному организация видит «Анонимный соискатель»: имя не уходит ни через шапку, ни через список, ни через
// ленту, ни через поиск по имени (T-97, серверная часть срезов 37–38). Имя открывается откликом или ответом «Интересно».
func TestAnonymousScientistIsNeverNamed(t *testing.T) {
	const name = "Кирилл Заречный"
	cases := []struct {
		name     string
		prepare  func(w *world, p testkit.Person)
		viaOffer bool
		named    bool
	}{
		{"anonymous mode, invited only", func(w *world, p testkit.Person) { w.privacy(p, "anon") }, true, false},
		// T-117: перейти из «анонимно» в «скрыт» значит стать менее заметным, а не назвать себя организации.
		{"anonymous mode, then hidden: still anonymous", func(w *world, p testkit.Person) {
			w.privacy(p, "anon")
			w.privacy(p, "hidden")
		}, true, false},
		{"hidden mode, invited only", func(w *world, p testkit.Person) { w.privacy(p, "hidden") }, true, false},
		{"hidden but applied: the name is open (mode)", func(w *world, p testkit.Person) { w.privacy(p, "hidden") }, false, true},
		{"hidden from the organization, invited only", func(w *world, p testkit.Person) {
			w.privacy(p, "public")
			w.hideFrom(p, w.tm.OrgID)
		}, true, false},
		{"public profile is not anonymous", func(w *world, p testkit.Person) { w.privacy(p, "public") }, true, true},
		{"organizations see the name", func(w *world, p testkit.Person) { w.privacy(p, "orgs") }, true, true},
		{"anonymous but applied: the name is open", func(w *world, p testkit.Person) { w.privacy(p, "anon") }, false, true},
		{"hidden but applied: the name is open", func(w *world, p testkit.Person) {
			w.privacy(p, "public")
			w.hideFrom(p, w.tm.OrgID)
		}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			sci := w.Applicant(name)
			c.prepare(w, sci)
			var chat uuid.UUID
			if c.viaOffer {
				offer := w.offer(sci, w.vacancy.ID, "pending")
				chat = w.open(sci, w.vacancy.ID, nil, &offer)
			} else {
				chat = w.chatOf(sci, w.vacancy.ID, "sent")
			}
			w.system(chat, EventOfferCreated, map[string]any{"message": "Приходите"})
			w.send(sci, chat, "Здравствуйте, я хотел бы узнать подробности.")

			for _, who := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA} {
				hdr, err := w.svc.Get(bg, who.User, chat)
				if err != nil {
					t.Fatal(err)
				}
				cp := hdr.Counterpart
				if c.named {
					if cp.Anonymous || cp.Name != name {
						t.Errorf("header: %+v", cp)
					}
				} else if !cp.Anonymous || cp.Name != "" || cp.ProfileID != nil {
					t.Errorf("header leaks: %+v", cp)
				}
				page, err := w.svc.Messages(bg, who.User, chat, MessagesQuery{})
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range page.Items {
					if m.Author == nil || m.Author.Side != SideScientist {
						continue
					}
					if c.named != (m.Author.Name == name) || c.named == m.Author.Anonymous {
						t.Errorf("author of a message: %+v", m.Author)
					}
				}
				l, err := w.svc.List(bg, who.User, ListQuery{Side: "org", Search: "Заречн"})
				if err != nil {
					t.Fatal(err)
				}
				if c.named != (len(l.Items) == 1) {
					t.Errorf("search by the name found %d chats", len(l.Items))
				}
				all, _ := w.svc.List(bg, who.User, ListQuery{Side: "org"})
				item := all.Items[0]
				if !c.named && (item.Counterpart.Name != "" || !item.Counterpart.Anonymous ||
					(item.LastMessage != nil && item.LastMessage.Author != nil && (item.LastMessage.Author.Name != "" || !item.LastMessage.Author.Anonymous))) {
					t.Errorf("list leaks: %+v / %+v", item.Counterpart, item.LastMessage)
				}
				// Ни в каком ответе организации нет имени, пока оно скрыто.
				if !c.named {
					raw := w.rawText(t, hdr, page, all)
					if strings.Contains(raw, name) || strings.Contains(raw, "Заречн") {
						t.Errorf("the name is somewhere in the answers: %s", raw)
					}
				}
			}
			// Сам учёный видит своё имя всегда.
			page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{})
			if err != nil {
				t.Fatal(err)
			}
			last := page.Items[len(page.Items)-1]
			if last.Author == nil || last.Author.Name != name || last.Author.Anonymous || !last.Author.IsMe {
				t.Errorf("the scientist sees: %+v", last.Author)
			}
		})
	}
}

// Ответ «Интересно» открывает имя той организации, чьё это приглашение, и не открывает другим (D-199).
func TestInterestedOpensTheNameOnlyForThatOrganization(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Кирилл Заречный")
	w.privacy(sci, "anon")
	offer := w.offer(sci, w.vacancy.ID, "pending")
	chat := w.open(sci, w.vacancy.ID, nil, &offer)
	hdr, _ := w.svc.Get(bg, w.tm.Owner.User, chat)
	if !hdr.Counterpart.Anonymous {
		t.Fatal("not anonymous before the answer")
	}
	w.setOffer(offer, "interested")
	hdr, _ = w.svc.Get(bg, w.tm.Owner.User, chat)
	if hdr.Counterpart.Anonymous || hdr.Counterpart.Name != "Кирилл Заречный" {
		t.Errorf("after «Интересно»: %+v", hdr.Counterpart)
	}
	// Другая организация, где учёный не открывался, по-прежнему имени не видит.
	other := w.Team()
	vac := other.Published(&other.UnitA.ID)
	otherOffer := w.offer(sci, vac.ID, "pending")
	otherChat := w.open(sci, vac.ID, nil, &otherOffer)
	hdr, err := w.svc.Get(bg, other.Owner.User, otherChat)
	if err != nil || !hdr.Counterpart.Anonymous || hdr.Counterpart.Name != "" {
		t.Errorf("another organization: %+v, %v", hdr.Counterpart, err)
	}
}

// Автор организации: имя сотрудника; ушёл из организации или удалён — «Сотрудник организации» без имени.
func TestFormerStaffAuthor(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "sent")
	w.send(w.tm.HR, chat, "От кадровика")
	w.send(w.tm.Owner, chat, "От владельца")
	authors := func() (hr, owner *Author) {
		t.Helper()
		page, err := w.svc.Messages(bg, sci.User, chat, MessagesQuery{})
		if err != nil || len(page.Items) != 2 {
			t.Fatalf("messages: %+v, %v", page, err)
		}
		return page.Items[0].Author, page.Items[1].Author
	}
	hr, owner := authors()
	if hr.Name != w.tm.HR.Name || hr.Former || owner.Name != w.tm.Owner.Name {
		t.Errorf("authors = %+v / %+v", hr, owner)
	}
	if _, err := testkit.Pool.Exec(bg, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, w.tm.OrgID, w.tm.HR.ID); err != nil {
		t.Fatal(err)
	}
	hr, owner = authors()
	if hr.Name != "" || !hr.Former || hr.IsMe || owner.Former {
		t.Errorf("after leaving = %+v / %+v", hr, owner)
	}
	// Аккаунт удалён: автор очищен, подпись та же.
	if _, err := testkit.Pool.Exec(bg, `UPDATE chat_messages SET author_user_id = NULL WHERE chat_id = $1 AND author_user_id = $2`, chat, w.tm.Owner.ID); err != nil {
		t.Fatal(err)
	}
	_, owner = authors()
	if owner.Name != "" || !owner.Former {
		t.Errorf("after the account is erased = %+v", owner)
	}
}

func (w *world) privacy(p testkit.Person, visibility string) {
	w.T.Helper()
	if _, err := w.Prof.SetPrivacy(bg, p.User, visibility); err != nil {
		w.T.Fatalf("privacy: %v", err)
	}
}

func (w *world) hideFrom(p testkit.Person, org uuid.UUID) {
	w.T.Helper()
	if _, err := w.Prof.SetHiddenOrgs(bg, p.User, []uuid.UUID{org}); err != nil {
		w.T.Fatalf("hide: %v", err)
	}
}

// rawText — все ответы одной строкой: так ищется строка, которой в них быть не должно.
func (w *world) rawText(t *testing.T, parts ...any) string {
	t.Helper()
	var b strings.Builder
	for _, p := range parts {
		raw, err := jsonString(p)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(raw)
	}
	return b.String()
}

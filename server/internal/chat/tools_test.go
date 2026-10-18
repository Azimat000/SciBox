package chat

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
)

// Срез 38: блокировка организации учёным, жалобы, шаблоны ответов (T-105, T-107, D-237…D-239).

func (w *world) eventTypes(c uuid.UUID) []string {
	w.T.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT event_type FROM chat_messages WHERE chat_id = $1 AND kind = 'system' ORDER BY seq`, c)
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

func (w *world) header(who testkit.Person, c uuid.UUID) Chat {
	w.T.Helper()
	h, err := w.svc.Get(bg, who.User, c)
	if err != nil {
		w.T.Fatal(err)
	}
	return h
}

// Блокировка закрывает переписки с организацией (но не с другой и не уже закрытые), дописывает строку, снимается снятием.
func TestBlockClosesOnlyTheChatsOfThatOrganization(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Учёный")
	open := w.chatOf(sci, w.vacancy.ID, "viewed")
	second := w.chatOf(sci, w.other.ID, "viewed")
	third := w.Published3()
	rejected := w.chatOf(sci, third, "rejected")
	foreign := w.Team()
	fv := foreign.Published(&foreign.UnitA.ID)
	elsewhere := w.chatOf(sci, fv.ID, "viewed")

	if _, err := w.svc.Block(bg, w.tm.Owner.User, open); !errors.Is(err, ErrForbidden) {
		t.Fatalf("block by the organization = %v", err)
	}
	if _, err := w.svc.Block(bg, w.tm.Out.User, open); !errors.Is(err, ErrNotFound) {
		t.Fatalf("block by a stranger = %v", err)
	}
	b, err := w.svc.Block(bg, sci.User, open)
	if err != nil {
		t.Fatal(err)
	}
	if b.OrgID != w.tm.OrgID || b.OrgName == "" || b.OrgSlug == "" {
		t.Errorf("block = %+v", b)
	}
	if _, err := w.svc.Block(bg, sci.User, second); !errors.Is(err, ErrAlreadyBlocked) {
		t.Fatalf("second block = %v", err)
	}

	for _, c := range []uuid.UUID{open, second} {
		for _, who := range []testkit.Person{sci, w.tm.Owner} {
			h := w.header(who, c)
			if h.CanWrite || h.ClosedReason == nil || *h.ClosedReason != ClosedBlocked {
				t.Errorf("header of %s: %+v", who.Name, h)
			}
		}
		if got := w.eventTypes(c); !slices.Contains(got, EventChatBlocked) {
			t.Errorf("lines of a blocked chat: %v", got)
		}
		if _, _, err := w.svc.Send(bg, sci.User, c, SendInput{Text: "Привет", ClientID: uuid.NewString()}); !errors.Is(err, ErrClosed) {
			t.Errorf("send by the scientist = %v", err)
		}
		if _, _, err := w.svc.Send(bg, w.tm.Owner.User, c, SendInput{Text: "Привет", ClientID: uuid.NewString()}); !errors.Is(err, ErrClosed) {
			t.Errorf("send by the organization = %v", err)
		}
		if err := w.svc.Typing(bg, sci.User, c); !errors.Is(err, ErrClosed) {
			t.Errorf("typing = %v", err)
		}
	}
	if got := w.eventTypes(rejected); slices.Contains(got, EventChatBlocked) {
		t.Errorf("a chat closed by a decision gets no line: %v", got)
	}
	if h := w.header(sci, rejected); h.ClosedReason == nil || *h.ClosedReason != ClosedRejected {
		t.Errorf("the reason of a rejected chat: %+v", h.ClosedReason)
	}
	if h := w.header(sci, elsewhere); !h.CanWrite || slices.Contains(w.eventTypes(elsewhere), EventChatBlocked) {
		t.Errorf("another organization is affected: %+v", h)
	}
	// Организации блокировка видна строкой, непрочитанной для неё; учёному своя строка не «новая».
	list, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Counts.Unread == 0 {
		t.Error("the line about the block is unread for the organization")
	}

	blocks, err := w.svc.Blocks(bg, sci.User)
	if err != nil || len(blocks) != 1 || blocks[0].OrgID != w.tm.OrgID {
		t.Fatalf("blocks = %+v, %v", blocks, err)
	}
	if other, _ := w.svc.Blocks(bg, w.tm.Owner.User); len(other) != 0 {
		t.Errorf("blocks of another person: %+v", other)
	}

	if err := w.svc.Unblock(bg, w.tm.Owner.User, w.tm.OrgID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unblock by someone who blocked nothing = %v", err)
	}
	if err := w.svc.Unblock(bg, sci.User, w.tm.OrgID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Unblock(bg, sci.User, w.tm.OrgID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second unblock = %v", err)
	}
	for _, c := range []uuid.UUID{open, second} {
		if h := w.header(sci, c); !h.CanWrite || h.ClosedReason != nil {
			t.Errorf("header after the unblock: %+v", h)
		}
		got := w.eventTypes(c)
		if got[len(got)-1] != EventChatUnblocked {
			t.Errorf("lines after the unblock: %v", got)
		}
	}
	if got := w.eventTypes(rejected); slices.Contains(got, EventChatUnblocked) {
		t.Errorf("a rejected chat gets no line about the unblock: %v", got)
	}
	if h := w.header(sci, rejected); h.CanWrite {
		t.Error("the unblock must not reopen a rejected chat")
	}
	w.send(sci, open, "Снова на связи")
}

// Published3 — ещё одна опубликованная вакансия подразделения A.
func (w *world) Published3() uuid.UUID { return w.tm.Published(&w.tm.UnitA.ID).ID }

// Отправка и блокировка в гонке: ни одного текста после строки о блокировке (T-105, T-106).
func TestBlockRacesWithSending(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Учёный")
	c := w.chatOf(sci, w.vacancy.ID, "viewed")
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			who := w.tm.Owner
			if i%2 == 1 {
				who = sci
			}
			_, _, err := w.svc.Send(bg, who.User, c, SendInput{Text: "Сообщение", ClientID: uuid.NewString()})
			if err != nil && !errors.Is(err, ErrClosed) {
				t.Errorf("send: %v", err)
			}
		})
		if i == 8 {
			wg.Go(func() {
				if _, err := w.svc.Block(bg, sci.User, c); err != nil {
					t.Errorf("block: %v", err)
				}
			})
		}
	}
	wg.Wait()
	var blockSeq, lastText int
	if err := testkit.Pool.QueryRow(bg, `SELECT seq FROM chat_messages WHERE chat_id = $1 AND event_type = 'chat.blocked'`, c).Scan(&blockSeq); err != nil {
		t.Fatal(err)
	}
	if err := testkit.Pool.QueryRow(bg, `SELECT COALESCE(max(seq), 0) FROM chat_messages WHERE chat_id = $1 AND kind = 'text'`, c).Scan(&lastText); err != nil {
		t.Fatal(err)
	}
	if lastText > blockSeq {
		t.Errorf("a message (seq %d) came after the block line (seq %d)", lastText, blockSeq)
	}
}

// Жалоба: права, проверка полей, хранение; переписку она не закрывает и ничего не пишет в неё.
func TestComplaint(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Учёный")
	c := w.chatOf(sci, w.vacancy.ID, "viewed")
	before := w.eventTypes(c)

	if _, err := w.svc.Complain(bg, w.tm.Owner.User, c, ComplaintInput{Reason: "spam"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("complaint by the organization = %v", err)
	}
	if _, err := w.svc.Complain(bg, w.tm.Out.User, c, ComplaintInput{Reason: "spam"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("complaint by a stranger = %v", err)
	}
	tooLong := strings.Repeat("я", MaxComplaintNote+1)
	for name, in := range map[string]struct {
		in    ComplaintInput
		field string
	}{
		"no reason":      {ComplaintInput{Reason: ""}, "reason"},
		"unknown reason": {ComplaintInput{Reason: "rudeness"}, "reason"},
		"other needs it": {ComplaintInput{Reason: "other", Note: "   "}, "note"},
		"too long":       {ComplaintInput{Reason: "spam", Note: tooLong}, "note"},
		"control":        {ComplaintInput{Reason: "spam", Note: "a\x07b"}, "note"},
		"bidi":           {ComplaintInput{Reason: "spam", Note: "a\u202eb"}, "note"},
		"nul":            {ComplaintInput{Reason: "spam", Note: "a\x00b"}, "note"},
	} {
		_, err := w.svc.Complain(bg, sci.User, c, in.in)
		verr, ok := errors.AsType[*auth.ValidationError](err)
		if !ok || verr.Fields[in.field] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chat_complaints WHERE user_id = $1`, sci.ID); n != 0 {
		t.Fatalf("refused complaints were stored: %d", n)
	}
	got, err := w.svc.Complain(bg, sci.User, c, ComplaintInput{Reason: "spam", Note: "  Пишут про платные курсы\r\nк вакансии это не относится  "})
	if err != nil {
		t.Fatal(err)
	}
	var note, reason string
	var org, vac uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT note, reason, org_id, vacancy_id FROM chat_complaints WHERE id = $1`, got.ID).Scan(&note, &reason, &org, &vac); err != nil {
		t.Fatal(err)
	}
	if note != "Пишут про платные курсы\nк вакансии это не относится" || reason != "spam" || org != w.tm.OrgID || vac != w.vacancy.ID {
		t.Errorf("stored complaint: %q %q %v %v", note, reason, org, vac)
	}
	// Вторая жалоба на ту же переписку в ту же неделю не принимается, через неделю принимается (limits_test.go).
	if _, err := w.svc.Complain(bg, sci.User, c, ComplaintInput{Reason: "other", Note: "Оскорбления"}); !errors.Is(err, ErrAlreadyComplained) {
		t.Errorf("a second complaint: %v", err)
	}
	w.clock.Advance(8 * 24 * time.Hour)
	if _, err := w.svc.Complain(bg, sci.User, c, ComplaintInput{Reason: "other", Note: "Оскорбления"}); err != nil {
		t.Errorf("a complaint a week later: %v", err)
	}
	if h := w.header(sci, c); !h.CanWrite {
		t.Error("a complaint must not close the chat")
	}
	if after := w.eventTypes(c); !slices.Equal(after, before) {
		t.Errorf("a complaint wrote lines: %v → %v", before, after)
	}
}

// Права на шаблоны: читают все, кто ведёт отклики; правят владелец и кадровик; организация чужому «не существует».
func TestTemplateRights(t *testing.T) {
	w := newWorld(t)
	foreign := w.Team()
	in := TemplateInput{Title: "Приглашение", Text: "Добрый день! Приглашаем на собеседование."}
	created, err := w.svc.CreateTemplate(bg, w.tm.Owner.User, w.tm.OrgID, in)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		who      testkit.Person
		org      uuid.UUID
		read     error
		canEdit  bool
		editErr  error
		missing  error
		readable bool
	}{
		{"owner", w.tm.Owner, w.tm.OrgID, nil, true, nil, nil, true},
		{"hr", w.tm.HR, w.tm.OrgID, nil, true, nil, nil, true},
		{"head of a unit", w.tm.HeadA, w.tm.OrgID, nil, false, ErrForbidden, nil, true},
		{"head of another unit", w.tm.HeadB, w.tm.OrgID, nil, false, ErrForbidden, nil, true},
		{"stranger", w.tm.Out, w.tm.OrgID, ErrNotFound, false, ErrNotFound, ErrNotFound, false},
		{"scientist", w.Applicant("Учёный"), w.tm.OrgID, ErrNotFound, false, ErrNotFound, ErrNotFound, false},
		{"owner of another organization", foreign.Owner, w.tm.OrgID, ErrNotFound, false, ErrNotFound, ErrNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			list, err := w.svc.Templates(bg, c.who.User, c.org)
			if !errors.Is(err, c.read) {
				t.Fatalf("list = %v, want %v", err, c.read)
			}
			if c.readable {
				if !slices.ContainsFunc(list.Items, func(x Template) bool { return x.ID == created.ID }) || list.CanEdit != c.canEdit || list.Limit != MaxTemplates {
					t.Errorf("list = %+v", list)
				}
			}
			_, err = w.svc.CreateTemplate(bg, c.who.User, c.org, TemplateInput{Title: "Новый", Text: "Текст"})
			if c.name == "owner" || c.name == "hr" {
				if err != nil {
					t.Errorf("create = %v", err)
				}
			} else if !errors.Is(err, c.editErr) {
				t.Errorf("create = %v, want %v", err, c.editErr)
			}
			_, err = w.svc.UpdateTemplate(bg, c.who.User, c.org, created.ID, TemplateInput{Title: "Правка", Text: "Правка"})
			if c.canEdit {
				if err != nil {
					t.Errorf("update = %v", err)
				}
			} else if !errors.Is(err, c.editErr) {
				t.Errorf("update = %v, want %v", err, c.editErr)
			}
			if !c.canEdit {
				if err := w.svc.DeleteTemplate(bg, c.who.User, c.org, created.ID); !errors.Is(err, c.editErr) {
					t.Errorf("delete = %v, want %v", err, c.editErr)
				}
			}
		})
	}
	// Шаблон чужой организации по своему номеру организации не достать, и наоборот.
	if _, err := w.svc.UpdateTemplate(bg, foreign.Owner.User, foreign.OrgID, created.ID, in); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("update of a template of another organization = %v", err)
	}
	if err := w.svc.DeleteTemplate(bg, foreign.Owner.User, foreign.OrgID, created.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("delete of a template of another organization = %v", err)
	}
	if list, _ := w.svc.Templates(bg, foreign.Owner.User, foreign.OrgID); len(list.Items) != 0 {
		t.Errorf("list of another organization: %+v", list)
	}
}

// Проверка полей, предел в 50 штук, правка и удаление, порядок по названию.
func TestTemplateLifecycle(t *testing.T) {
	w := newWorld(t)
	owner := w.tm.Owner.User
	for name, in := range map[string]struct {
		in    TemplateInput
		field string
	}{
		"no title":      {TemplateInput{Title: " ", Text: "Текст"}, "title"},
		"long title":    {TemplateInput{Title: strings.Repeat("н", MaxTemplateTitle+1), Text: "Текст"}, "title"},
		"title newline": {TemplateInput{Title: "а\nб", Text: "Текст"}, "title"},
		"no text":       {TemplateInput{Title: "Название", Text: "  "}, "text"},
		"long text":     {TemplateInput{Title: "Название", Text: strings.Repeat("я", MaxText+1)}, "text"},
		"bad text":      {TemplateInput{Title: "Название", Text: "а\u202eб"}, "text"},
	} {
		_, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, in.in)
		verr, ok := errors.AsType[*auth.ValidationError](err)
		if !ok || verr.Fields[in.field] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Граница: 120 знаков названия и 4000 текста годятся.
	edge, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: strings.Repeat("я", MaxTemplateTitle), Text: strings.Repeat("я", MaxText)})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.DeleteTemplate(bg, owner, w.tm.OrgID, edge.ID); err != nil {
		t.Fatal(err)
	}
	a, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: "Б шаблон", Text: "  Текст\r\nв две строки  "})
	if err != nil {
		t.Fatal(err)
	}
	if a.Text != "Текст\nв две строки" {
		t.Errorf("text = %q", a.Text)
	}
	if _, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: "а шаблон", Text: "Первый"}); err != nil {
		t.Fatal(err)
	}
	list, _ := w.svc.Templates(bg, owner, w.tm.OrgID)
	if len(list.Items) != 2 || list.Items[0].Title != "а шаблон" {
		t.Errorf("order by title: %+v", list.Items)
	}
	upd, err := w.svc.UpdateTemplate(bg, owner, w.tm.OrgID, a.ID, TemplateInput{Title: "Я шаблон", Text: "Новый"})
	if err != nil || upd.Title != "Я шаблон" || upd.Text != "Новый" || upd.UpdatedAt.Before(a.UpdatedAt) {
		t.Errorf("update = %+v, %v", upd, err)
	}
	if _, err := w.svc.UpdateTemplate(bg, owner, w.tm.OrgID, uuid.New(), TemplateInput{Title: "Я", Text: "Я"}); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("update of nothing = %v", err)
	}
	if err := w.svc.DeleteTemplate(bg, owner, w.tm.OrgID, uuid.New()); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("delete of nothing = %v", err)
	}
	for i := 2; i < MaxTemplates; i++ {
		if _, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: "Шаблон", Text: "Текст"}); err != nil {
			t.Fatalf("template %d: %v", i, err)
		}
	}
	if _, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: "Лишний", Text: "Текст"}); !errors.Is(err, ErrTemplateLimit) {
		t.Fatalf("template over the limit = %v", err)
	}
	if err := w.svc.DeleteTemplate(bg, owner, w.tm.OrgID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.CreateTemplate(bg, owner, w.tm.OrgID, TemplateInput{Title: "Теперь можно", Text: "Текст"}); err != nil {
		t.Errorf("template after a deletion = %v", err)
	}
}

// Параллельные добавления не пробивают предел.
func TestTemplateLimitHoldsUnderRace(t *testing.T) {
	w := newWorld(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var created, refused int
	for range MaxTemplates + 10 {
		wg.Go(func() {
			_, err := w.svc.CreateTemplate(bg, w.tm.Owner.User, w.tm.OrgID, TemplateInput{Title: "Шаблон", Text: "Текст"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrTemplateLimit):
				refused++
			default:
				t.Errorf("create: %v", err)
			}
		})
	}
	wg.Wait()
	if created != MaxTemplates || refused != 10 {
		t.Errorf("created %d, refused %d", created, refused)
	}
}

// Ссылки отклика и приглашения на переписку: пустые, пока переписки нет; состояние и непрочитанное по стороне.
func TestLinksOf(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Учёный")
	app := w.application(sci, w.vacancy.ID, "viewed")
	none, err := LinksOf(bg, w.q(), SideScientist, sci.ID, []uuid.UUID{app}, nil)
	if err != nil || none.Application(app) != nil {
		t.Fatalf("links before the chat: %+v, %v", none, err)
	}
	if l, err := LinksOf(bg, w.q(), SideOrg, sci.ID, nil, nil); err != nil || l.Application(app) != nil || l.Offer(app) != nil {
		t.Fatalf("empty request: %+v, %v", l, err)
	}
	c := w.open(sci, w.vacancy.ID, &app, nil)
	w.system(c, EventApplicationSent, ApplicationSentData{ApplicationID: app, CoverLetter: "Письмо"})
	forScientist, _ := LinksOf(bg, w.q(), SideScientist, sci.ID, []uuid.UUID{app}, nil)
	forOrg, _ := LinksOf(bg, w.q(), SideOrg, w.tm.Owner.ID, []uuid.UUID{app}, nil)
	if l := forScientist.Application(app); l == nil || l.ID != c || l.Unread != 0 || !l.CanWrite || l.Started {
		t.Errorf("link of the scientist: %+v", l)
	}
	if l := forOrg.Application(app); l == nil || l.Unread != 1 || l.Started {
		t.Errorf("link of the organization: %+v", l)
	}
	w.send(w.tm.Owner, c, "Здравствуйте")
	forScientist, _ = LinksOf(bg, w.q(), SideScientist, sci.ID, []uuid.UUID{app}, nil)
	if l := forScientist.Application(app); l.Unread != 1 || !l.Started {
		t.Errorf("link after a message: %+v", l)
	}
	w.setApplication(app, "rejected")
	forScientist, _ = LinksOf(bg, w.q(), SideScientist, sci.ID, []uuid.UUID{app}, nil)
	if l := forScientist.Application(app); l.CanWrite {
		t.Errorf("link of a closed chat: %+v", l)
	}
	// Приглашение на вакансию той же пары: та же переписка находится и по нему.
	offer := w.offer(sci, w.other.ID, "pending")
	oc := w.open(sci, w.other.ID, nil, &offer)
	byOffer, _ := LinksOf(bg, w.q(), SideScientist, sci.ID, nil, []uuid.UUID{offer})
	if l := byOffer.Offer(offer); l == nil || l.ID != oc {
		t.Errorf("link by the offer: %+v", l)
	}
}

// Record сам находит переписку по связи, заводит её, если её нет, и не принимает неизвестное.
func TestRecord(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Учёный")
	app := w.application(sci, w.vacancy.ID, "sent")
	at := w.clock.Now()
	entry := Entry{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app, Type: EventApplicationSent, At: at,
		Data: ApplicationSentData{ApplicationID: app, CoverLetter: "Письмо"}}
	seq, err := Record(bg, w.q(), entry)
	if err != nil || seq != 1 {
		t.Fatalf("first record = %d, %v", seq, err)
	}
	entry.Type, entry.Data = EventApplicationViewed, ApplicationData{ApplicationID: app}
	if seq, err = Record(bg, w.q(), entry); err != nil || seq != 2 {
		t.Fatalf("second record = %d, %v", seq, err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE user_id = $1`, sci.ID); n != 1 {
		t.Errorf("chats = %d", n)
	}
	// Приглашение того же человека на ту же вакансию попадает в ту же переписку.
	offer := w.offer(sci, w.vacancy.ID, "pending")
	if _, err := Record(bg, w.q(), Entry{VacancyID: w.vacancy.ID, UserID: sci.ID, OfferID: &offer, Type: EventOfferCreated, At: at,
		Data: OfferCreatedData{OfferID: offer, Message: "Приглашаем", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM chats WHERE user_id = $1 AND application_id = $2 AND offer_id = $3`, sci.ID, app, offer); n != 1 {
		t.Errorf("the offer and the application share one chat: %d", n)
	}
	for name, bad := range map[string]Entry{
		"no link":       {VacancyID: w.vacancy.ID, UserID: sci.ID, Type: EventApplicationViewed, At: at},
		"unknown event": {VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app, Type: "application.exploded", At: at},
	} {
		if _, err := Record(bg, w.q(), bad); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := Record(bg, w.q(), Entry{VacancyID: w.vacancy.ID, UserID: sci.ID, ApplicationID: &app, Type: EventApplicationViewed, At: at, Data: make(chan int)}); err == nil {
		t.Error("data that cannot be encoded is accepted")
	}
}

// Данные каждой строки о событии, как их собирает сервер, совпадают с формой в contracts/chat/messages.json. Строки о
// приглашениях внутри отклика сервер больше не пишет (D-256), но старые лежат в переписках: их форма та же, что была.
func TestEventDataFollowsTheContract(t *testing.T) {
	id := uuid.New()
	now := w0().Format(time.RFC3339)
	samples := map[string]any{
		EventApplicationSent:   ApplicationSentData{ApplicationID: id, CoverLetter: "Письмо"},
		EventApplicationViewed: map[string]any{},
		EventInvitationCreated: map[string]any{"application_id": id, "invitation": map[string]any{
			"id": id, "kind": "interview", "status": "pending", "message": "Приглашаем", "starts_at": now, "place_kind": "online",
			"place": "https://example.org/room", "contact_name": "", "contact_email": "", "contact_phone": "", "answer": nil,
			"created_at": now, "can_answer": false, "can_cancel": false, "can_accept_proposal": false,
		}},
		EventInvitationAnswered: map[string]any{"application_id": id, "invitation_id": id, "kind": "interview", "answer": "proposed",
			"proposed_at": now, "note": "Удобнее позже", "contact": "", "time": ""},
		EventInvitationAccepted:   map[string]any{"application_id": id, "invitation_id": id, "starts_at": now},
		EventInvitationCancelled:  map[string]any{"application_id": id, "invitation_id": id, "kind": "contacts"},
		EventApplicationDecided:   ApplicationDecidedData{ApplicationID: id, Decision: "rejected", Note: "Спасибо"},
		EventApplicationWithdrawn: ApplicationData{ApplicationID: id},
		EventOfferCreated:         OfferCreatedData{OfferID: id, Message: "Приглашаем", Status: "pending"},
		EventOfferAnswered:        OfferAnsweredData{OfferID: id, Answer: "interested", Note: ""},
		EventOfferCancelled:       OfferData{OfferID: id},
		EventChatBlocked:          map[string]any{},
		EventChatUnblocked:        map[string]any{},
	}
	if len(samples) != len(EventTypes()) {
		t.Fatalf("samples cover %d of %d event types", len(samples), len(EventTypes()))
	}
	f := loadContract(t, "messages")
	for typ, data := range samples {
		raw, err := jsonString(map[string]any{"items": []any{map[string]any{
			"id": id, "seq": 1, "kind": "system", "author": nil, "text": "", "event": map[string]any{"type": typ, "data": data},
			"created_at": now, "edited_at": nil, "deleted": false, "editable_until": nil, "client_id": nil,
		}}, "has_more": false})
		if err != nil {
			t.Fatal(err)
		}
		if errs := f.responseErrors(raw); len(errs) > 0 {
			t.Errorf("%s does not match the contract: %v\n%s", typ, errs, raw)
		}
	}
}

// Пока блокировка не снята, она не даёт организации писать и через HTTP; ответы настоящего сервера совпадают с контрактом.
func TestToolsHTTPFollowTheContract(t *testing.T) {
	a := newAPI(t)
	sci := a.Applicant("Учёный")
	c := a.chatOf(sci, a.vacancy.ID, "viewed")
	chatPath := "/api/chats/" + c.String()
	orgPath := "/api/orgs/" + a.tm.OrgID.String() + "/chat-templates"

	// Шаблоны.
	a.expect(a.do(&a.tm.Owner, "GET", orgPath, nil), http.StatusOK, "templates-list")
	created := a.do(&a.tm.Owner, "POST", orgPath, map[string]any{"title": "Приглашение", "text": "Добрый день!"})
	a.expect(created, http.StatusCreated, "templates-create")
	id := created.JSON(t)["template"].(map[string]any)["id"].(string)
	a.expect(a.do(&a.tm.HR, "PATCH", orgPath+"/"+id, map[string]any{"title": "Новое", "text": "Текст"}), http.StatusOK, "templates-update")
	a.expect(a.do(&a.tm.HeadA, "GET", orgPath, nil), http.StatusOK, "templates-list")
	a.expectError(a.do(&a.tm.HeadA, "POST", orgPath, map[string]any{"title": "А", "text": "Б"}), http.StatusForbidden, "forbidden")
	a.expectError(a.do(&a.tm.Out, "GET", orgPath, nil), http.StatusNotFound, "not_found")
	a.expectError(a.do(&a.tm.Owner, "POST", orgPath, map[string]any{"title": "", "text": ""}), http.StatusUnprocessableEntity, "validation_failed")
	a.expectError(a.do(&a.tm.Owner, "PATCH", orgPath+"/"+uuid.NewString(), map[string]any{"title": "А", "text": "Б"}), http.StatusNotFound, "not_found")
	a.expectError(a.do(&a.tm.Owner, "PATCH", orgPath+"/bad-id", map[string]any{"title": "А", "text": "Б"}), http.StatusNotFound, "not_found")
	a.expectError(a.do(&a.tm.Out, "DELETE", orgPath+"/bad-id", nil), http.StatusNotFound, "not_found")
	a.expectError(a.do(&a.tm.Owner, "GET", "/api/orgs/not-a-uuid/chat-templates", nil), http.StatusNotFound, "not_found")
	a.expect(a.do(&a.tm.Owner, "DELETE", orgPath+"/"+id, nil), http.StatusNoContent, "templates-delete")
	for range MaxTemplates {
		a.expect(a.do(&a.tm.Owner, "POST", orgPath, map[string]any{"title": "Ш", "text": "Т"}), http.StatusCreated, "templates-create")
	}
	a.expectError(a.do(&a.tm.Owner, "POST", orgPath, map[string]any{"title": "Ш", "text": "Т"}), http.StatusConflict, "limit_reached")

	// Блокировка и жалоба.
	a.expectError(a.do(&a.tm.Owner, "POST", chatPath+"/block", nil), http.StatusForbidden, "forbidden")
	a.expectError(a.do(&a.tm.Owner, "POST", chatPath+"/complaints", map[string]any{"reason": "spam", "note": ""}), http.StatusForbidden, "forbidden")
	a.expectError(a.do(&sci, "POST", chatPath+"/complaints", map[string]any{"reason": "other", "note": ""}), http.StatusUnprocessableEntity, "validation_failed")
	a.expect(a.do(&sci, "POST", chatPath+"/complaints", map[string]any{"reason": "spam", "note": "Пишут не по делу"}), http.StatusCreated, "complaint")
	a.expect(a.do(&sci, "GET", "/api/chat-blocks", nil), http.StatusOK, "blocks-list")
	a.expect(a.do(&sci, "POST", chatPath+"/block", nil), http.StatusCreated, "block")
	a.expectError(a.do(&sci, "POST", chatPath+"/block", nil), http.StatusConflict, "already_blocked")
	a.expect(a.do(&sci, "GET", "/api/chat-blocks", nil), http.StatusOK, "blocks-list")
	a.expectError(a.do(&a.tm.Owner, "POST", chatPath+"/messages", map[string]any{"text": "Здравствуйте", "client_id": uuid.NewString()}), http.StatusConflict, "chat_closed")
	a.expect(a.do(&a.tm.Owner, "GET", chatPath, nil), http.StatusOK, "chat")
	a.expect(a.do(&sci, "DELETE", "/api/chat-blocks/"+a.tm.OrgID.String(), nil), http.StatusNoContent, "unblock")
	a.expectError(a.do(&sci, "DELETE", "/api/chat-blocks/"+a.tm.OrgID.String(), nil), http.StatusNotFound, "not_found")
	a.expectError(a.do(&sci, "DELETE", "/api/chat-blocks/not-a-uuid", nil), http.StatusNotFound, "not_found")
}

func w0() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// invitationRow — строка приглашения отклика для проверки форм данных.
// Негодные номера и тела на адресах инструментов: тот же 404, что у чужого, и 400 на неразборчивое тело; без входа 401.
func TestToolsHTTPRefuseBadInput(t *testing.T) {
	a := newAPI(t)
	sci := a.Applicant("Учёный")
	c := a.chatOf(sci, a.vacancy.ID, "viewed")
	for name, call := range map[string]struct{ method, path string }{
		"block, bad id":       {"POST", "/api/chats/nope/block"},
		"complaint, bad id":   {"POST", "/api/chats/nope/complaints"},
		"unblock, bad id":     {"DELETE", "/api/chat-blocks/nope"},
		"templates, bad org":  {"GET", "/api/orgs/nope/chat-templates"},
		"create, bad org":     {"POST", "/api/orgs/nope/chat-templates"},
		"update, bad org":     {"PATCH", "/api/orgs/nope/chat-templates/" + uuid.NewString()},
		"delete, bad org":     {"DELETE", "/api/orgs/nope/chat-templates/" + uuid.NewString()},
		"update, bad ids":     {"PATCH", "/api/orgs/nope/chat-templates/nope"},
		"complaint, no chat":  {"POST", "/api/chats/" + uuid.NewString() + "/complaints"},
		"block, unknown chat": {"POST", "/api/chats/" + uuid.NewString() + "/block"},
	} {
		body := map[string]any{"title": "а", "text": "б"}
		if strings.HasSuffix(call.path, "/complaints") {
			body = map[string]any{"reason": "spam", "note": ""}
		}
		r := a.do(&sci, call.method, call.path, body)
		if r.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, r.Code, r.Raw)
		}
	}
	chatPath := "/api/chats/" + c.String()
	orgPath := "/api/orgs/" + a.tm.OrgID.String() + "/chat-templates"
	for name, call := range map[string]struct {
		who          *testkit.Person
		method, path string
	}{
		"complaint": {&sci, "POST", chatPath + "/complaints"},
		"create":    {&a.tm.Owner, "POST", orgPath},
		"update":    {&a.tm.Owner, "PATCH", orgPath + "/" + uuid.NewString()},
	} {
		r := a.do(call.who, call.method, call.path, "{not json")
		if r.Code != http.StatusBadRequest && r.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s with a broken body: %d %s", name, r.Code, r.Raw)
		}
	}
	for _, call := range []struct{ method, path string }{
		{"POST", chatPath + "/block"}, {"POST", chatPath + "/complaints"}, {"GET", "/api/chat-blocks"},
		{"DELETE", "/api/chat-blocks/" + a.tm.OrgID.String()}, {"GET", orgPath}, {"POST", orgPath},
	} {
		if r := a.do(nil, call.method, call.path, map[string]any{}); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", call.method, call.path, r.Code)
		}
	}
}

package chat

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/testkit"
)

func ids(l List) []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, it := range l.Items {
		out[i] = it.ID
	}
	return out
}

// Порядок: новое сообщение поднимает переписку; листание по ключу не теряет и не повторяет, в том числе при равном времени.
func TestListOrderAndCursor(t *testing.T) {
	w := newWorld(t)
	var chats []uuid.UUID
	for i := range 7 {
		sci := w.Applicant("Учёный " + string(rune('А'+i)))
		c := w.chatOf(sci, w.vacancy.ID, "sent")
		w.clock.Advance(time.Minute)
		w.system(c, EventApplicationSent, nil)
		chats = append(chats, c)
	}
	// Две переписки с одинаковым временем: порядок определяет номер.
	same := w.clock.Now().Add(time.Hour)
	if _, err := testkit.Pool.Exec(bg, `UPDATE chats SET updated_at = $1 WHERE id = ANY($2)`, same, []uuid.UUID{chats[1], chats[2]}); err != nil {
		t.Fatal(err)
	}

	full, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Limit: 50})
	if err != nil || len(full.Items) != 7 || full.NextCursor != nil {
		t.Fatalf("full list = %d items, cursor %v, %v", len(full.Items), full.NextCursor, err)
	}
	for i := 1; i < len(full.Items); i++ {
		a, b := full.Items[i-1], full.Items[i]
		if a.UpdatedAt.Before(b.UpdatedAt) || (a.UpdatedAt.Equal(b.UpdatedAt) && a.ID.String() < b.ID.String()) {
			t.Fatalf("order is broken at %d: %v %v / %v %v", i, a.UpdatedAt, a.ID, b.UpdatedAt, b.ID)
		}
	}
	// Постранично по 3 даёт тот же список.
	var got []uuid.UUID
	cursor := ""
	for page := 0; page < 5; page++ {
		l, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(l)...)
		if l.NextCursor == nil {
			break
		}
		cursor = *l.NextCursor
	}
	if !slices.Equal(got, ids(full)) {
		t.Errorf("pages differ from the full list:\n got %v\nwant %v", got, ids(full))
	}
	// Ровно «страница в страницу»: курсора после последней нет.
	l, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Limit: 7})
	if err != nil || l.NextCursor != nil {
		t.Errorf("exact page has a cursor: %v, %v", l.NextCursor, err)
	}
	// Новое сообщение поднимает переписку наверх.
	w.clock.Advance(2 * time.Hour)
	w.send(w.tm.HR, chats[0], "Поднимите")
	l, err = w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org"})
	if err != nil || l.Items[0].ID != chats[0] {
		t.Errorf("a new message does not raise the chat: %v, %v", ids(l)[:1], err)
	}
}

func TestListValidation(t *testing.T) {
	w := newWorld(t)
	vac := w.vacancy.ID
	cases := map[string]ListQuery{
		"no side":                        {},
		"unknown side":                   {Side: "admin"},
		"unknown box":                    {Side: "org", Box: "trash"},
		"limit 51":                       {Side: "org", Limit: 51},
		"negative limit":                 {Side: "org", Limit: -3},
		"bad cursor":                     {Side: "org", Cursor: "не курсор"},
		"long search":                    {Side: "org", Search: strings.Repeat("я", 101)},
		"vacancy for the scientist side": {Side: "scientist", VacancyID: &vac},
	}
	for name, q := range cases {
		_, err := w.svc.List(bg, w.tm.Owner.User, q)
		if _, ok := errors.AsType[*BadParamError](err); !ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", Search: strings.Repeat("я", 100)}); err != nil {
		t.Errorf("search of 100 characters: %v", err)
	}
}

// Права по ролям: кто какие переписки видит в списке стороны организации; учёный видит только свои.
func TestListScopeByRole(t *testing.T) {
	w := newWorld(t)
	a := w.chatOf(w.Applicant("Учёный А"), w.vacancy.ID, "sent")
	b := w.chatOf(w.Applicant("Учёный Б"), w.other.ID, "sent")
	whole := w.tm.Published(nil) // вакансия на всю организацию
	c := w.chatOf(w.Applicant("Учёный В"), whole.ID, "sent")

	want := map[string][]uuid.UUID{
		"владелец":       {a, b, c},
		"кадровик":       {a, b, c},
		"руководитель А": {a},
		"руководитель Б": {b},
		"посторонний":    nil,
	}
	for _, p := range people(w) {
		l, err := w.svc.List(bg, p.who.User, ListQuery{Side: "org", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		got := ids(l)
		slices.SortFunc(got, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		exp := slices.Clone(want[p.name])
		slices.SortFunc(exp, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		if !slices.Equal(got, exp) {
			t.Errorf("%s sees %v, want %v", p.name, got, exp)
		}
		if l.Items == nil {
			t.Errorf("%s: items must be a list, not null", p.name)
		}
		// Сторона учёного у сотрудников пуста: они не откликались.
		own, err := w.svc.List(bg, p.who.User, ListQuery{Side: "scientist"})
		if err != nil || len(own.Items) != 0 {
			t.Errorf("%s as a scientist: %+v, %v", p.name, own, err)
		}
		vs, err := w.svc.Vacancies(bg, p.who.User)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != map[string]int{"владелец": 3, "кадровик": 3, "руководитель А": 1, "руководитель Б": 1, "посторонний": 0}[p.name] || vs == nil {
			t.Errorf("%s: vacancies = %+v", p.name, vs)
		}
	}
}

// Человек бывает и учёным, и сотрудником: стороны не смешиваются, и свой отклик не попадает в список организации.
func TestOneAccountTwoSides(t *testing.T) {
	w := newWorld(t)
	// Кадровик откликается на вакансию другой организации.
	foreign := testkitTeam(t, w)
	vac := foreign.Published(&foreign.UnitA.ID)
	mine := w.chatOf(w.tm.HR, vac.ID, "sent")
	own := w.chatOf(w.Applicant("Учёный А"), w.vacancy.ID, "sent")

	asScientist, err := w.svc.List(bg, w.tm.HR.User, ListQuery{Side: "scientist"})
	if err != nil || len(asScientist.Items) != 1 || asScientist.Items[0].ID != mine || asScientist.Items[0].Side != SideScientist {
		t.Fatalf("as a scientist: %+v, %v", asScientist, err)
	}
	asOrg, err := w.svc.List(bg, w.tm.HR.User, ListQuery{Side: "org"})
	if err != nil || len(asOrg.Items) != 1 || asOrg.Items[0].ID != own || asOrg.Items[0].Side != SideOrg {
		t.Fatalf("as an organization: %+v, %v", asOrg, err)
	}
	// Организация, в которую он откликнулся, не видит его чужого имени как своего сотрудника: он там только учёный.
	hdr, err := w.svc.Get(bg, w.tm.HR.User, mine)
	if err != nil || hdr.Side != SideScientist || hdr.Viewer.CanUseTemplates || hdr.Viewer.OrgID != nil || !hdr.Viewer.CanBlock {
		t.Errorf("header of the own application: %+v, %v", hdr.Viewer, err)
	}
	// Для шапки на свою же переписку кадровик «как организация» невозможен.
	if _, err := w.svc.Get(bg, foreign.Owner.User, mine); err != nil {
		t.Errorf("owner of the foreign organization: %v", err)
	}
	// Число в шапке считает обе стороны.
	w.system(mine, EventApplicationViewed, nil)
	w.system(own, EventApplicationSent, nil)
	c, err := w.svc.UnreadCount(bg, w.tm.HR.User)
	if err != nil || c != (UnreadCount{Total: 2, Scientist: 1, Org: 1}) {
		t.Errorf("unread = %+v, %v", c, err)
	}
}

func TestSearchAndVacancyFilter(t *testing.T) {
	w := newWorld(t)
	petrov := w.Applicant("Пётр Ковалёв")
	other := w.Applicant("Мария Лисицына")
	a := w.chatOf(petrov, w.vacancy.ID, "sent")
	b := w.chatOf(other, w.other.ID, "sent")
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET title = 'Лаборатория 100% чистоты' WHERE id = $1`, w.vacancy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET title = 'Аспирант по спинтронике' WHERE id = $1`, w.other.ID); err != nil {
		t.Fatal(err)
	}
	// Оба учёных открыли имя откликом, поэтому имена организации видны.
	find := func(who testkit.Person, side Side, q string) []uuid.UUID {
		t.Helper()
		l, err := w.svc.List(bg, who.User, ListQuery{Side: string(side), Search: q})
		if err != nil {
			t.Fatal(err)
		}
		return ids(l)
	}
	if got := find(w.tm.Owner, SideOrg, "ковалёв"); !slices.Equal(got, []uuid.UUID{a}) {
		t.Errorf("by the name (case-insensitive): %v", got)
	}
	if got := find(w.tm.Owner, SideOrg, "СПИНТРОНИКЕ"); !slices.Equal(got, []uuid.UUID{b}) {
		t.Errorf("by the vacancy title: %v", got)
	}
	// Знаки LIKE ищутся как буквы.
	if got := find(w.tm.Owner, SideOrg, "100%"); !slices.Equal(got, []uuid.UUID{a}) {
		t.Errorf("percent sign: %v", got)
	}
	if got := find(w.tm.Owner, SideOrg, "%"); len(got) != 1 {
		t.Errorf("a lone percent sign must match only a literal percent: %v", got)
	}
	if got := find(w.tm.Owner, SideOrg, "_"); len(got) != 0 {
		t.Errorf("underscore: %v", got)
	}
	if got := find(w.tm.Owner, SideOrg, `\`); len(got) != 0 {
		t.Errorf("backslash: %v", got)
	}
	// Учёный ищет по организации и вакансии, но не по имени организации сотрудника и не по чужому.
	if got := find(petrov, SideScientist, "Институт набора"); !slices.Equal(got, []uuid.UUID{a}) {
		t.Errorf("by the organization name: %v", got)
	}
	if got := find(petrov, SideScientist, "Лисицына"); len(got) != 0 {
		t.Errorf("a foreign scientist name found: %v", got)
	}

	l, err := w.svc.List(bg, w.tm.Owner.User, ListQuery{Side: "org", VacancyID: &w.other.ID})
	if err != nil || !slices.Equal(ids(l), []uuid.UUID{b}) {
		t.Errorf("by the vacancy: %v, %v", ids(l), err)
	}
	// Отбор по чужой вакансии не даёт ничего и не выдаёт, что она есть.
	l, err = w.svc.List(bg, w.tm.HeadA.User, ListQuery{Side: "org", VacancyID: &w.other.ID})
	if err != nil || len(l.Items) != 0 {
		t.Errorf("by a vacancy out of the scope: %v, %v", ids(l), err)
	}
}

// Строка списка: собеседник, последнее сообщение, ожидание ответа, закрытие.
func TestListItemContent(t *testing.T) {
	w := newWorld(t)
	sci := w.Applicant("Анна Ильина")
	chat := w.chatOf(sci, w.vacancy.ID, "viewed")
	app := uuidOfApplication(t, chat)
	w.system(chat, EventApplicationViewed, map[string]any{"application_id": app})
	l, err := w.svc.List(bg, sci.User, ListQuery{Side: "scientist"})
	if err != nil || len(l.Items) != 1 {
		t.Fatalf("list: %+v, %v", l, err)
	}
	it := l.Items[0]
	// Отклик ответа учёного не ждёт: приглашений внутри отклика нет (D-256).
	if it.NeedsAnswer || it.Status != (Status{Source: "application", Value: "viewed"}) || !it.CanWrite || it.ClosedReason != nil ||
		it.Counterpart.Kind != SideOrg || it.Counterpart.Name == "" || it.Counterpart.Anonymous || it.Counterpart.ProfileID != nil {
		t.Errorf("item = %+v", it)
	}
	if it.LastMessage == nil || it.LastMessage.Kind != KindSystem || it.LastMessage.Author != nil || it.LastMessage.EventType == nil ||
		*it.LastMessage.EventType != EventApplicationViewed || it.LastMessage.Text != "" || it.LastMessage.Seq != 1 {
		t.Errorf("last message = %+v", it.LastMessage)
	}
	if it.Vacancy.Title != w.vacancy.Title || it.Vacancy.OrgSlug != w.tm.Slug || it.Vacancy.UnitName != w.tm.UnitA.Name {
		t.Errorf("vacancy = %+v", it.Vacancy)
	}

	// Текст последним: автор, текст, удалённое без текста.
	w.clock.Advance(time.Minute)
	m := w.send(w.tm.HR, chat, "Ждём вас")
	l, _ = w.svc.List(bg, sci.User, ListQuery{Side: "scientist"})
	last := l.Items[0].LastMessage
	if last == nil || last.Text != "Ждём вас" || last.Author == nil || last.Author.IsMe || last.Author.Name != w.tm.HR.Name || last.EventType != nil {
		t.Errorf("last text = %+v", last)
	}
	l, _ = w.svc.List(bg, w.tm.HR.User, ListQuery{Side: "org"})
	if l.Items[0].LastMessage.Author == nil || !l.Items[0].LastMessage.Author.IsMe {
		t.Errorf("the author sees the message as its own: %+v", l.Items[0].LastMessage)
	}
	if err := w.svc.Delete(bg, w.tm.HR.User, chat, m.Seq); err != nil {
		t.Fatal(err)
	}
	l, _ = w.svc.List(bg, sci.User, ListQuery{Side: "scientist"})
	if last := l.Items[0].LastMessage; !last.Deleted || last.Text != "" {
		t.Errorf("deleted last message = %+v", last)
	}

	// Отказ закрывает: ждать ответа уже не надо.
	w.setApplication(app, "rejected")
	l, _ = w.svc.List(bg, sci.User, ListQuery{Side: "scientist"})
	if it := l.Items[0]; it.NeedsAnswer || it.CanWrite || it.ClosedReason == nil || *it.ClosedReason != ClosedRejected {
		t.Errorf("closed item = %+v", it)
	}
}

// uuidOfApplication — отклик, на котором стоит переписка.
func uuidOfApplication(t *testing.T, chat uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT application_id FROM chats WHERE id = $1`, chat).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func testkitTeam(t *testing.T, w *world) *testkit.Team {
	t.Helper()
	return w.Team()
}

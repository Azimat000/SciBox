package vacancies

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/orgs"
)

func isValidation(err error, field string) bool {
	var verr *auth.ValidationError
	return errors.As(err, &verr) && verr.Fields[field] != ""
}

func TestLifecycle(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	d := w.create(tm, tm.owner, goodInput())
	if d.Status != StatusDraft || d.PublishedAt != nil || d.Organization.Slug != tm.slug {
		t.Fatalf("new vacancy: %+v", d)
	}
	if d.Position.Type != TypeResearch || d.Position.Name != "Старший научный сотрудник" || d.Region == nil || d.Region.Name != "Новосибирская область" {
		t.Errorf("names from the reference tables: %+v %+v", d.Position, d.Region)
	}
	if len(d.Specialties) != 1 || d.Specialties[0].Code != "1.4.4" || d.Deadline != "2026-12-01" {
		t.Errorf("specialties/deadline: %+v %q", d.Specialties, d.Deadline)
	}
	if !d.Viewer.CanManage || len(d.Viewer.Transitions) != 1 || d.Viewer.Transitions[0] != StatusPublished {
		t.Errorf("viewer: %+v", d.Viewer)
	}

	step := func(to string, want string) Detail {
		t.Helper()
		got, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, to)
		if err != nil {
			t.Fatalf("→ %s: %v", to, err)
		}
		if got.Status != want {
			t.Fatalf("→ %s: status %s", to, got.Status)
		}
		return got
	}
	p := step(StatusPublished, StatusPublished)
	if p.PublishedAt == nil || !p.PublishedAt.Equal(testNow) {
		t.Errorf("published_at: %v", p.PublishedAt)
	}
	w.clock.Advance(time.Hour)
	step(StatusClosed, StatusClosed)
	re := step(StatusPublished, StatusPublished) // повторное открытие
	if !re.PublishedAt.Equal(testNow) {
		t.Errorf("published_at must keep the first publication time, got %v", re.PublishedAt)
	}
	step(StatusClosed, StatusClosed)
	step(StatusArchived, StatusArchived)
	step(StatusClosed, StatusClosed) // возврат из архива
	if got := countRows(t, `SELECT count(*) FROM vacancies WHERE id = $1 AND closed_at IS NOT NULL AND archived_at IS NOT NULL`, d.ID); got != 1 {
		t.Errorf("closed_at and archived_at must be recorded, got %d", got)
	}
}

// Любая смена статуса, которой нет в таблице, отвечает ErrBadTransition и ничего не меняет.
func TestForbiddenTransitionsChangeNothing(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	paths := map[string][]string{
		StatusDraft:     {},
		StatusPublished: {StatusPublished},
		StatusClosed:    {StatusPublished, StatusClosed},
		StatusArchived:  {StatusPublished, StatusClosed, StatusArchived},
	}
	for from, steps := range paths {
		d := w.create(tm, tm.owner, goodInput())
		for _, to := range steps {
			var err error
			if _, err = w.svc.SetStatus(bg, tm.owner.User, d.ID, to); err != nil {
				t.Fatalf("setup %s: %v", to, err)
			}
		}
		for _, to := range Statuses {
			if CanTransition(from, to) {
				continue
			}
			if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, to); !errors.Is(err, ErrBadTransition) {
				t.Errorf("%s → %s: %v", from, to, err)
			}
		}
		if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, "nonsense"); !errors.Is(err, ErrBadTransition) {
			t.Errorf("%s → nonsense: %v", from, err)
		}
		got, _ := w.svc.Get(bg, d.ID, &tm.owner.User)
		if got.Status != from {
			t.Errorf("status changed from %s to %s", from, got.Status)
		}
	}
}

func TestPublishChecksReadiness(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	// Черновик с названием и должностью сохраняется.
	d := w.create(tm, tm.owner, Input{Title: "Будущая вакансия", PositionCode: "researcher"})
	_, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished)
	for _, f := range []string{"summary", "description", "career_level", "work_format", "rate_percent", "contract_type", "specialties", "city", "region_code"} {
		if !isValidation(err, f) {
			t.Errorf("publishing an empty draft must complain about %s: %v", f, err)
		}
	}
	if got, _ := w.svc.Get(bg, d.ID, &tm.owner.User); got.Status != StatusDraft {
		t.Errorf("failed publication changed status to %s", got.Status)
	}
	// Срок подачи прошёл: публикации нет; после правки есть.
	in := goodInput()
	in.Deadline = "2026-10-02"
	d = w.create(tm, tm.owner, in)
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished); !isValidation(err, "deadline") {
		t.Errorf("past deadline: %v", err)
	}
	in.Deadline = "2026-10-03" // сегодня (по Москве) ещё можно
	if _, err := w.svc.Update(bg, tm.owner.User, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished); err != nil {
		t.Errorf("deadline today must be accepted: %v", err)
	}
	// «Сегодня» считается по Москве: в 22:30 по UTC в Москве уже 4 октября, 3 октября поздно.
	w.clock.Advance(10*time.Hour + 30*time.Minute)
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusPublished); !isValidation(err, "deadline") {
		t.Errorf("reopening after the deadline (Moscow date already moved on): %v", err)
	}
}

// Опубликованную вакансию нельзя испортить правкой: она обязана остаться готовой к показу; черновик можно сохранять неполным.
func TestUpdateRules(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	pub := w.published(tm, goodInput())
	broken := goodInput()
	broken.Summary = ""
	if _, err := w.svc.Update(bg, tm.owner.User, pub.ID, broken); !isValidation(err, "summary") {
		t.Errorf("a published vacancy must stay complete: %v", err)
	}
	ok := goodInput()
	ok.Title = "Новое название вакансии"
	ok.Specialties = []string{"1.4.1", "1.4.4"}
	ok.UnitID = &tm.unitA.ID
	got, err := w.svc.Update(bg, tm.owner.User, pub.ID, ok)
	if err != nil || got.Title != ok.Title || len(got.Specialties) != 2 || got.Unit == nil || got.Unit.ID != tm.unitA.ID || got.Status != StatusPublished {
		t.Fatalf("update: %+v %v", got, err)
	}
	ok.Specialties = []string{"1.4.4"}
	if got, _ = w.svc.Update(bg, tm.owner.User, pub.ID, ok); len(got.Specialties) != 1 {
		t.Errorf("specialties must be replaced, got %v", got.Specialties)
	}
	// Правка опубликованной вакансии с давно прошедшим сроком разрешена (её не публикуют заново).
	ok.Deadline = "2026-01-01"
	if _, err := w.svc.Update(bg, tm.owner.User, pub.ID, ok); err != nil {
		t.Errorf("editing a published vacancy with a past deadline: %v", err)
	}
	// Архивную вакансию править можно (иначе её не перенести в другое подразделение).
	for _, to := range []string{StatusClosed, StatusArchived} {
		if _, err := w.svc.SetStatus(bg, tm.owner.User, pub.ID, to); err != nil {
			t.Fatal(err)
		}
	}
	ok.UnitID = nil
	if _, err := w.svc.Update(bg, tm.owner.User, pub.ID, ok); err != nil {
		t.Errorf("editing an archived vacancy: %v", err)
	}
	// Черновик сохраняется неполным.
	dr := w.create(tm, tm.owner, goodInput())
	if _, err := w.svc.Update(bg, tm.owner.User, dr.ID, Input{Title: "Только название", PositionCode: "researcher"}); err != nil {
		t.Errorf("draft with a title only: %v", err)
	}
	if _, err := w.svc.Update(bg, tm.owner.User, uuid.New(), ok); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown vacancy: %v", err)
	}
}

func TestReferencesAreChecked(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	cases := []struct {
		name   string
		mutate func(in *Input)
		field  string
	}{
		{"unknown position", func(in *Input) { in.PositionCode = "wizard" }, "position_code"},
		{"unknown region", func(in *Input) { in.RegionCode = "00" }, "region_code"},
		{"unknown specialty", func(in *Input) { in.Specialties = []string{"1.4.4", "9.9.9"} }, "specialties"},
		{"repealed specialty", func(in *Input) { in.Specialties = []string{"1.3.14"} }, "specialties"},
		{"unit of another organization", func(in *Input) { in.UnitID = ptr(uuid.New()) }, "unit_id"},
	}
	for _, c := range cases {
		in := goodInput()
		c.mutate(&in)
		if _, err := w.svc.Create(bg, tm.owner.User, tm.slug, in); !isValidation(err, c.field) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// Подразделение из чужой организации не подходит.
	other := w.team()
	in := goodInput()
	in.UnitID = &other.unitA.ID
	if _, err := w.svc.Create(bg, tm.owner.User, tm.slug, in); !isValidation(err, "unit_id") {
		t.Errorf("a foreign unit: %v", err)
	}
	if _, err := w.svc.Create(bg, tm.owner.User, "no-such-organization", goodInput()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown organization: %v", err)
	}
}

// Таблица прав «кто → что». Подразделение A принадлежит руководителю A; B — руководителю B.
func TestWhoCanDoWhat(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	people := map[string]person{"owner": tm.owner, "hr": tm.hr, "headA": tm.headA, "headB": tm.headB, "stranger": tm.out}
	type op struct {
		name string
		// run выполняет действие; vacancy — вакансия в подразделении A (черновик), orgWide — вакансия без подразделения.
		run     func(who person, unitA, orgWide Detail) error
		allowed map[string]bool
	}
	unitIn := func(u *uuid.UUID) Input { in := goodInput(); in.UnitID = u; return in }
	ops := []op{
		{"create in unit A", func(who person, a, _ Detail) error {
			_, err := w.svc.Create(bg, who.User, tm.slug, unitIn(&tm.unitA.ID))
			return err
		}, map[string]bool{"owner": true, "hr": true, "headA": true}},
		{"create in unit B", func(who person, _, _ Detail) error {
			_, err := w.svc.Create(bg, who.User, tm.slug, unitIn(&tm.unitB.ID))
			return err
		}, map[string]bool{"owner": true, "hr": true, "headB": true}},
		{"create without unit", func(who person, _, _ Detail) error {
			_, err := w.svc.Create(bg, who.User, tm.slug, unitIn(nil))
			return err
		}, map[string]bool{"owner": true, "hr": true}},
		{"update unit A vacancy", func(who person, a, _ Detail) error {
			_, err := w.svc.Update(bg, who.User, a.ID, unitIn(&tm.unitA.ID))
			return err
		}, map[string]bool{"owner": true, "hr": true, "headA": true}},
		{"update org-wide vacancy", func(who person, _, o Detail) error {
			_, err := w.svc.Update(bg, who.User, o.ID, unitIn(nil))
			return err
		}, map[string]bool{"owner": true, "hr": true}},
		{"move unit A vacancy to unit B", func(who person, a, _ Detail) error {
			_, err := w.svc.Update(bg, who.User, a.ID, unitIn(&tm.unitB.ID))
			return err
		}, map[string]bool{"owner": true, "hr": true}},
		{"move unit A vacancy out of any unit", func(who person, a, _ Detail) error {
			_, err := w.svc.Update(bg, who.User, a.ID, unitIn(nil))
			return err
		}, map[string]bool{"owner": true, "hr": true}},
		{"publish unit A vacancy", func(who person, a, _ Detail) error {
			_, err := w.svc.SetStatus(bg, who.User, a.ID, StatusPublished)
			return err
		}, map[string]bool{"owner": true, "hr": true, "headA": true}},
		{"publish org-wide vacancy", func(who person, _, o Detail) error {
			_, err := w.svc.SetStatus(bg, who.User, o.ID, StatusPublished)
			return err
		}, map[string]bool{"owner": true, "hr": true}},
		{"delete unit A draft", func(who person, a, _ Detail) error { return w.svc.Delete(bg, who.User, a.ID) },
			map[string]bool{"owner": true, "hr": true, "headA": true}},
		{"delete org-wide draft", func(who person, _, o Detail) error { return w.svc.Delete(bg, who.User, o.ID) },
			map[string]bool{"owner": true, "hr": true}},
	}
	for _, o := range ops {
		for name, who := range people {
			a := w.create(tm, tm.owner, unitIn(&tm.unitA.ID))
			org := w.create(tm, tm.owner, unitIn(nil))
			err := o.run(who, a, org)
			if o.allowed[name] {
				if err != nil {
					t.Errorf("%s by %s must be allowed: %v", o.name, name, err)
				}
				continue
			}
			// Черновик для того, кто его вести не может, «не существует»; создание — отказ.
			if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
				t.Errorf("%s by %s must be refused, got %v", o.name, name, err)
			}
			// Отказ ничего не меняет.
			for _, d := range []Detail{a, org} {
				got, gerr := w.svc.Get(bg, d.ID, &tm.owner.User)
				if gerr != nil || got.Status != StatusDraft || got.Title != d.Title || (got.Unit == nil) != (d.Unit == nil) {
					t.Errorf("%s by %s left traces: %+v %v", o.name, name, got, gerr)
				}
			}
		}
	}
	// Не вошедшие в организацию не могут ничего и после публикации (там отказ, а не «нет такой»).
	pub := w.published(tm, unitIn(&tm.unitA.ID))
	for _, name := range []string{"headB", "stranger"} {
		who := people[name]
		if _, err := w.svc.Update(bg, who.User, pub.ID, unitIn(&tm.unitA.ID)); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s updating a published vacancy: %v", name, err)
		}
		if _, err := w.svc.SetStatus(bg, who.User, pub.ID, StatusClosed); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s closing a published vacancy: %v", name, err)
		}
		if err := w.svc.Delete(bg, who.User, pub.ID); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s deleting a published vacancy: %v", name, err)
		}
	}
	// Уйдёт ли руководитель с поста: права пропадают сразу.
	if _, err := w.orgs.SetUnitHead(bg, tm.owner.User, tm.slug, tm.unitA.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Update(bg, tm.headA.User, pub.ID, unitIn(&tm.unitA.ID)); !errors.Is(err, ErrForbidden) {
		t.Errorf("a former unit head keeps access: %v", err)
	}
}

// Кто какую вакансию видит: вошедшие и нет, по каждому статусу.
func TestWhoSeesWhat(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.UnitID = &tm.unitA.ID
	byStatus := map[string]Detail{}
	d := w.create(tm, tm.owner, in)
	byStatus[StatusDraft] = d
	for _, to := range []string{StatusPublished} {
		byStatus[to] = d
	}
	pub := w.published(tm, in)
	byStatus[StatusPublished] = pub
	closed := w.published(tm, in)
	if _, err := w.svc.SetStatus(bg, tm.owner.User, closed.ID, StatusClosed); err != nil {
		t.Fatal(err)
	}
	byStatus[StatusClosed] = closed
	arch := w.published(tm, in)
	for _, to := range []string{StatusClosed, StatusArchived} {
		if _, err := w.svc.SetStatus(bg, tm.owner.User, arch.ID, to); err != nil {
			t.Fatal(err)
		}
	}
	byStatus[StatusArchived] = arch

	viewers := []struct {
		name string
		who  *person
		mgr  bool
	}{
		{"anonymous", nil, false}, {"stranger", &tm.out, false}, {"headB", &tm.headB, false},
		{"headA", &tm.headA, true}, {"hr", &tm.hr, true}, {"owner", &tm.owner, true},
	}
	for status, d := range byStatus {
		for _, v := range viewers {
			var u *auth.User
			if v.who != nil {
				u = &v.who.User
			}
			got, err := w.svc.Get(bg, d.ID, u)
			visible := v.mgr || PubliclyVisible(status)
			if visible && err != nil {
				t.Errorf("%s must see a %s vacancy: %v", v.name, status, err)
				continue
			}
			if !visible {
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("%s must not see a %s vacancy: %v", v.name, status, err)
				}
				continue
			}
			if got.Viewer.CanManage != v.mgr {
				t.Errorf("%s on %s: can_manage = %v", v.name, status, got.Viewer.CanManage)
			}
			if !v.mgr && len(got.Viewer.Transitions) != 0 {
				t.Errorf("%s on %s may not change statuses, got %v", v.name, status, got.Viewer.Transitions)
			}
			if v.mgr && len(got.Viewer.Transitions) != len(NextStatuses(status)) {
				t.Errorf("%s on %s: transitions %v", v.name, status, got.Viewer.Transitions)
			}
		}
	}
	if _, err := w.svc.Get(bg, uuid.New(), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestDeleteOnlyDrafts(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	d := w.create(tm, tm.owner, goodInput())
	if err := w.svc.Delete(bg, tm.owner.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Get(bg, d.ID, &tm.owner.User); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted draft: %v", err)
	}
	if err := w.svc.Delete(bg, tm.owner.User, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	pub := w.published(tm, goodInput())
	if err := w.svc.Delete(bg, tm.owner.User, pub.ID); !errors.Is(err, ErrNotDraft) {
		t.Errorf("published: %v", err)
	}
	if _, err := w.svc.Get(bg, pub.ID, nil); err != nil {
		t.Errorf("a refused delete removed the vacancy: %v", err)
	}
}

func TestPublicListShowsOnlyPublished(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	other := w.team()
	inA := goodInput()
	inA.UnitID = &tm.unitA.ID
	a := w.published(tm, inA)
	b := w.published(tm, goodInput())
	w.create(tm, tm.owner, goodInput()) // черновик
	closed := w.published(tm, goodInput())
	if _, err := w.svc.SetStatus(bg, tm.owner.User, closed.ID, StatusClosed); err != nil {
		t.Fatal(err)
	}
	w.published(other, goodInput())

	res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug})
	if err != nil || res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("org list: %+v %v", res, err)
	}
	for _, c := range res.Items {
		if c.Status != StatusPublished || c.Organization.Slug != tm.slug || len(c.Specialties) != 1 {
			t.Errorf("card: %+v", c)
		}
	}
	res, _ = w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, UnitID: &tm.unitA.ID})
	if res.Total != 1 || res.Items[0].ID != a.ID || res.Items[0].Unit == nil {
		t.Errorf("unit list: %+v", res)
	}
	res, _ = w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Limit: 1, Offset: 1})
	if res.Total != 2 || len(res.Items) != 1 {
		t.Errorf("paging: %+v", res)
	}
	// Без организации: все опубликованные (минимум наши три), новые сверху.
	all, _ := w.svc.Search(bg, SearchParams{Limit: MaxLimit * 3})
	if all.Total < 3 || len(all.Items) > MaxLimit {
		t.Errorf("all: total %d, items %d", all.Total, len(all.Items))
	}
	if _, err := w.svc.Search(bg, SearchParams{OrgSlug: "no-such"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown organization: %v", err)
	}
	if _, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, UnitID: &other.unitA.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a unit from another organization: %v", err)
	}
	_ = b
}

func TestMyVacanciesFollowRights(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	inA, inB := goodInput(), goodInput()
	inA.UnitID, inB.UnitID = &tm.unitA.ID, &tm.unitB.ID
	w.create(tm, tm.owner, inA)
	w.published(tm, inA)
	w.create(tm, tm.owner, inB)
	w.create(tm, tm.owner, goodInput())
	cases := []struct {
		who         person
		draft, pub  int
		total       int
		statusTotal map[string]int
	}{
		{tm.owner, 3, 1, 4, nil},
		{tm.hr, 3, 1, 4, nil},
		{tm.headA, 1, 1, 2, nil},
		{tm.headB, 1, 0, 1, nil},
		{tm.out, 0, 0, 0, nil},
	}
	for _, c := range cases {
		res, err := w.svc.ListMine(bg, c.who.User, "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != c.total || len(res.Items) != c.total || res.Counts[StatusDraft] != c.draft || res.Counts[StatusPublished] != c.pub {
			t.Errorf("%s: %+v", c.who.Name, res)
		}
		for _, st := range Statuses {
			if _, ok := res.Counts[st]; !ok {
				t.Errorf("counts lack %s", st)
			}
		}
		drafts, _ := w.svc.ListMine(bg, c.who.User, StatusDraft, 0, 0)
		if drafts.Total != c.draft || len(drafts.Items) != c.draft || drafts.Counts[StatusPublished] != c.pub {
			t.Errorf("%s drafts: %+v", c.who.Name, drafts)
		}
	}
	if _, err := w.svc.ListMine(bg, tm.owner.User, "weird", 0, 0); !isValidation(err, "status") {
		t.Errorf("unknown status filter: %v", err)
	}
	page, _ := w.svc.ListMine(bg, tm.owner.User, "", 2, 3)
	if page.Total != 4 || len(page.Items) != 1 {
		t.Errorf("paging: total %d items %d", page.Total, len(page.Items))
	}
}

// Руководитель подразделения, которого сняли, и бывший сотрудник не видят вакансий.
func TestMyVacanciesAfterLosingRole(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.UnitID = &tm.unitA.ID
	w.create(tm, tm.owner, in)
	if res, _ := w.svc.ListMine(bg, tm.headA.User, "", 0, 0); res.Total != 1 {
		t.Fatalf("before: %+v", res)
	}
	if err := w.orgs.RemoveMember(bg, tm.owner.User, tm.slug, tm.headA.ID); err != nil {
		t.Fatal(err)
	}
	if res, _ := w.svc.ListMine(bg, tm.headA.User, "", 0, 0); res.Total != 0 {
		t.Errorf("after: %+v", res)
	}
}

func TestTargets(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	get := func(p person) []Target {
		t.Helper()
		res, err := w.svc.Targets(bg, p.User)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if got := get(tm.owner); len(got) != 1 || !got[0].WholeOrg || len(got[0].Units) != 2 || got[0].Organization.Slug != tm.slug {
		t.Errorf("owner: %+v", got)
	}
	if got := get(tm.hr); len(got) != 1 || !got[0].WholeOrg || len(got[0].Units) != 2 {
		t.Errorf("hr: %+v", got)
	}
	if got := get(tm.headA); len(got) != 1 || got[0].WholeOrg || len(got[0].Units) != 1 || got[0].Units[0].ID != tm.unitA.ID {
		t.Errorf("head: %+v", got)
	}
	if got := get(tm.out); got == nil || len(got) != 0 {
		t.Errorf("stranger: %#v", got)
	}
	// Руководитель без подразделения вакансий не ведёт.
	if _, err := w.orgs.SetUnitHead(bg, tm.owner.User, tm.slug, tm.unitA.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := get(tm.headA); len(got) != 0 {
		t.Errorf("head without unit: %+v", got)
	}
}

func TestCreateIsRateLimited(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Create = Limit{Max: 2, Window: time.Hour}
	tm := w.team()
	w.create(tm, tm.owner, goodInput())
	w.create(tm, tm.owner, goodInput())
	_, err := w.svc.Create(bg, tm.owner.User, tm.slug, goodInput())
	var rerr *auth.RateLimitedError
	if !errors.As(err, &rerr) || rerr.RetryAfter <= 0 {
		t.Fatalf("third creation: %v", err)
	}
	// Ошибка в форме лимит не тратит; чужой счёт отдельный.
	if _, err := w.svc.Create(bg, tm.hr.User, tm.slug, Input{}); !isValidation(err, "title") {
		t.Errorf("invalid input: %v", err)
	}
	w.create(tm, tm.hr, goodInput())
	w.clock.Advance(time.Hour + time.Second)
	w.create(tm, tm.owner, goodInput())
}

// Подразделение с вакансиями удалить нельзя; без них можно (D-051).
func TestUnitWithVacanciesCannotBeDeleted(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.UnitID = &tm.unitA.ID
	d := w.create(tm, tm.owner, in)
	if err := w.orgs.DeleteUnit(bg, tm.owner.User, tm.slug, tm.unitA.ID); !errors.Is(err, orgs.ErrUnitHasVacancies) {
		t.Fatalf("delete with a vacancy: %v", err)
	}
	if _, err := w.svc.Get(bg, d.ID, &tm.owner.User); err != nil {
		t.Errorf("the vacancy must survive: %v", err)
	}
	in.UnitID = &tm.unitB.ID
	if _, err := w.svc.Update(bg, tm.owner.User, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := w.orgs.DeleteUnit(bg, tm.owner.User, tm.slug, tm.unitA.ID); err != nil {
		t.Errorf("after moving the vacancy away: %v", err)
	}
}

// Два одновременных перевода одной вакансии не проходят оба.
func TestConcurrentStatusChange(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	d := w.published(tm, goodInput())
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusClosed)
			errs <- err
		}()
	}
	var ok, bad int
	for i := 0; i < 2; i++ {
		switch err := <-errs; {
		case err == nil:
			ok++
		case errors.Is(err, ErrBadTransition):
			bad++
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 || bad != 1 {
		t.Errorf("ok %d, refused %d", ok, bad)
	}
}

func TestSalaryAndOptionalFieldsRoundTrip(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.SalaryFrom, in.SalaryTo = ptr(80000), ptr(120000)
	in.Housing, in.FundingSource, in.FundingNote = HousingService, FundingGrant, "РНФ 24-71-00012"
	in.Degree, in.Requirements = DegreeCandidate, "Опыт работы с синхротронным излучением"
	in.IsCompetition = true
	got := w.create(tm, tm.owner, in)
	if *got.SalaryFrom != 80000 || *got.SalaryTo != 120000 || got.Housing != HousingService || got.FundingSource != FundingGrant ||
		got.FundingNote != "РНФ 24-71-00012" || got.Degree != DegreeCandidate || !got.IsCompetition || got.Requirements == "" ||
		*got.ContractMonths != 36 || *got.RatePercent != 100 || *got.CareerLevel != 3 || got.WorkFormat != FormatOnsite {
		t.Errorf("%+v", got)
	}
	// Без зарплаты и необязательных полей (D-014).
	plain := w.create(tm, tm.owner, goodInput())
	if plain.SalaryFrom != nil || plain.SalaryTo != nil || plain.FundingSource != "" || plain.Housing != HousingNone {
		t.Errorf("%+v", plain)
	}
}

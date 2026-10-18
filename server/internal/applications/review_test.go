package applications

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/dbgen"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// ---- помощники ----

func (w *world) statusOf(id uuid.UUID) string {
	w.T.Helper()
	var s string
	if err := testkit.Pool.QueryRow(bg, `SELECT status FROM applications WHERE id = $1`, id).Scan(&s); err != nil {
		w.T.Fatal(err)
	}
	return s
}

func (w *world) noticeBody(who testkit.Person, kind string) string {
	w.T.Helper()
	var body string
	if err := testkit.Pool.QueryRow(bg, `SELECT body FROM notifications WHERE user_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1`, who.ID, kind).Scan(&body); err != nil {
		w.T.Fatalf("no %q notification for %s: %v", kind, who.Email, err)
	}
	return body
}

func (w *world) mailTo(who testkit.Person, subject string) int {
	w.T.Helper()
	return testkit.Count(w.T, `SELECT count(*) FROM outbox WHERE to_email = $1 AND subject = $2`, who.Email, subject)
}

// staffOfUnitA — те, кто вправе разбирать отклики на вакансию подразделения A.
func (w *world) staffOfUnitA() []testkit.Person {
	return []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA}
}

// viewedApp — отклик, который организация уже открывала.
func (w *world) viewedApp(me testkit.Person) Detail {
	w.T.Helper()
	d := w.mustApply(me, nil)
	if _, err := w.svc.Get(bg, w.tm.Owner.User, d.ID); err != nil {
		w.T.Fatal(err)
	}
	return d
}

func (w *world) makeStaff(p testkit.Person, role string) {
	w.T.Helper()
	if err := dbgen.New(testkit.Pool).AddMember(bg, dbgen.AddMemberParams{OrgID: w.tm.OrgID, UserID: p.ID, Role: role, JoinedAt: time.Now().UTC()}); err != nil {
		w.T.Fatal(err)
	}
}

// ---- просмотр ----

func TestOpeningTheCardMarksTheApplicationViewedOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)

	// Сам соискатель, руководитель чужого подразделения и посторонний статус не двигают.
	if got, err := w.svc.Get(bg, me.User, d.ID); err != nil || got.Status != StatusSent {
		t.Fatalf("applicant opens: %v %v", err, got.Status)
	}
	for _, p := range []testkit.Person{w.tm.HeadB, w.tm.Out} {
		if _, err := w.svc.Get(bg, p.User, d.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s opens: %v", p.Name, err)
		}
	}
	if w.statusOf(d.ID) != StatusSent || notificationsOf(t, me, "application_viewed") != 0 {
		t.Fatal("the status moved without an entitled reader")
	}

	// Руководитель своего подразделения: статус «просмотрен» и уже в ответе.
	got, err := w.svc.Get(bg, w.tm.HeadA.User, d.ID)
	if err != nil || got.Status != StatusViewed || got.Viewer.Role != RoleStaff {
		t.Fatalf("staff opens: %v %v %v", err, got.Status, got.Viewer)
	}
	if w.statusOf(d.ID) != StatusViewed || !got.StatusChangedAt.Equal(w.clock.Now()) {
		t.Errorf("status in the database: %s, changed at %v", w.statusOf(d.ID), got.StatusChangedAt)
	}
	// Остальные открывают сколько угодно: соискатель получает одно уведомление и одно письмо.
	for _, p := range []testkit.Person{w.tm.HeadA, w.tm.Owner, w.tm.HR} {
		if _, err := w.svc.Get(bg, p.User, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if n := notificationsOf(t, me, "application_viewed"); n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
	if n := w.mailTo(me, "Ваш отклик просмотрен"); n != 1 {
		t.Errorf("mails = %d, want 1", n)
	}
	if body := w.noticeBody(me, "application_viewed"); !strings.Contains(body, "«"+w.vacancy.Title+"»") {
		t.Errorf("body = %q", body)
	}
}

func TestOpeningOwnApplicationAsStaffDoesNotMarkItViewed(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	w.makeStaff(me, "hr")
	if _, err := w.svc.Get(bg, me.User, d.ID); err != nil {
		t.Fatal(err)
	}
	if w.statusOf(d.ID) != StatusSent {
		t.Error("the applicant, who is now staff, marked their own application as viewed")
	}
}

func TestSimultaneousOpeningsNotifyOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	var wg sync.WaitGroup
	for _, p := range w.staffOfUnitA() {
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := w.svc.Get(bg, p.User, d.ID); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
	if n := notificationsOf(t, me, "application_viewed"); n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
}

// ---- кто что может ----

// Операции организации: владелец, кадровик и руководитель своего подразделения могут; руководитель чужого подразделения,
// посторонний и сам автор отклика (в том числе ставший сотрудником) получают «нет такого», и ничего не меняется.
func TestWhoCanDoWhat(t *testing.T) {
	type op struct {
		name string
		// prep готовит отклик и возвращает действие.
		prep func(w *world, me testkit.Person) (d Detail, do func(who testkit.Person) error)
	}
	ops := []op{
		{"reject", func(w *world, me testkit.Person) (Detail, func(testkit.Person) error) {
			d := w.viewedApp(me)
			return d, func(who testkit.Person) error { return w.svc.Decide(bg, who.User, d.ID, StatusRejected, "") }
		}},
		{"accept", func(w *world, me testkit.Person) (Detail, func(testkit.Person) error) {
			d := w.viewedApp(me)
			return d, func(who testkit.Person) error { return w.svc.Decide(bg, who.User, d.ID, StatusAccepted, "") }
		}},
	}
	for _, o := range ops {
		for _, who := range []string{"owner", "hr", "head of unit A", "head of unit B", "outsider", "the applicant", "the applicant turned hr"} {
			t.Run(o.name+"/"+who, func(t *testing.T) {
				w := newWorld(t)
				me := w.Applicant("Мария")
				d, do := o.prep(w, me)
				person := map[string]testkit.Person{
					"owner": w.tm.Owner, "hr": w.tm.HR, "head of unit A": w.tm.HeadA, "head of unit B": w.tm.HeadB,
					"outsider": w.tm.Out, "the applicant": me, "the applicant turned hr": me,
				}[who]
				if who == "the applicant turned hr" {
					w.makeStaff(me, "hr")
				}
				allowed := who == "owner" || who == "hr" || who == "head of unit A"

				before := w.snapshot(d.ID)
				err := do(person)
				if allowed {
					if err != nil {
						t.Fatalf("must be allowed: %v", err)
					}
					if w.snapshot(d.ID) == before {
						t.Error("an allowed action changed nothing")
					}
					return
				}
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				if w.snapshot(d.ID) != before {
					t.Error("a refused action left a trace")
				}
			})
		}
	}
}

// snapshot — всё, что может измениться от действий организации: статус, записка, число уведомлений и писем.
func (w *world) snapshot(appID uuid.UUID) string {
	w.T.Helper()
	var s string
	err := testkit.Pool.QueryRow(bg, `
SELECT a.status || '|' || a.decision_note || '|' ||
  (SELECT count(*) FROM notifications) || '|' || (SELECT count(*) FROM outbox)
FROM applications a WHERE a.id = $1`, appID).Scan(&s)
	if err != nil {
		w.T.Fatal(err)
	}
	return s
}

// ---- решение ----

func TestDecide(t *testing.T) {
	cases := []struct {
		from, to string
		ok       bool
	}{
		{StatusSent, StatusRejected, true}, {StatusSent, StatusAccepted, false},
		{StatusViewed, StatusRejected, true}, {StatusViewed, StatusAccepted, true},
		{StatusRejected, StatusRejected, false}, {StatusRejected, StatusAccepted, false},
		{StatusAccepted, StatusRejected, false}, {StatusAccepted, StatusAccepted, false},
		{StatusWithdrawn, StatusRejected, false}, {StatusWithdrawn, StatusAccepted, false},
	}
	for _, c := range cases {
		t.Run(c.from+"→"+c.to, func(t *testing.T) {
			w := newWorld(t)
			me := w.Applicant("Мария")
			d := w.mustApply(me, nil)
			w.setStatus(d.ID, c.from)
			err := w.svc.Decide(bg, w.tm.HR.User, d.ID, c.to, "  Спасибо за отклик  ")
			if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrBadStatus)) {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
			if !c.ok {
				if w.statusOf(d.ID) != c.from || notificationsOf(t, me, "application_"+c.to) != 0 {
					t.Error("a refused decision left a trace")
				}
				return
			}
			got, _ := w.svc.Get(bg, me.User, d.ID)
			if got.Status != c.to || got.DecisionNote != "Спасибо за отклик" || got.Viewer.CanWithdraw || len(got.Viewer.Decisions) != 0 {
				t.Errorf("applicant sees %+v", got)
			}
			staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID)
			if len(staff.Viewer.Decisions) != 0 {
				t.Errorf("staff can still act: %+v", staff.Viewer)
			}
			var by uuid.UUID
			if err := testkit.Pool.QueryRow(bg, `SELECT decided_by FROM applications WHERE id = $1`, d.ID).Scan(&by); err != nil || by != w.tm.HR.ID {
				t.Errorf("decided_by = %v, %v", by, err)
			}
			if n := notificationsOf(t, me, "application_"+c.to); n != 1 {
				t.Errorf("notifications = %d", n)
			}
			if body := w.noticeBody(me, "application_"+c.to); !strings.Contains(body, "Сообщение организации: Спасибо за отклик") {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestDecideValidation(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	for _, to := range []string{"invited", StatusViewed, StatusSent, StatusWithdrawn, "", "unknown"} {
		var verr *auth.ValidationError
		if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, to, ""); !errors.As(err, &verr) || verr.Fields["status"] == "" {
			t.Errorf("decide %q: %v", to, err)
		}
	}
	var verr *auth.ValidationError
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, strings.Repeat("я", 1001)); !errors.As(err, &verr) || verr.Fields["note"] == "" {
		t.Errorf("long note: %v", err)
	}
	if err := w.svc.Decide(bg, w.tm.Owner.User, uuid.New(), StatusRejected, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown application: %v", err)
	}
	if w.statusOf(d.ID) != StatusViewed {
		t.Error("invalid decisions changed the status")
	}
	// Записка ровно в предел.
	if err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, StatusRejected, strings.Repeat("я", 1000)); err != nil {
		t.Errorf("note at the limit: %v", err)
	}
}

func TestSimultaneousDecisionsPassOnce(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.viewedApp(me)
	var ok, bad atomic.Int32
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			to := []string{StatusRejected, StatusAccepted}[i%2]
			switch err := w.svc.Decide(bg, w.tm.Owner.User, d.ID, to, ""); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrBadStatus):
				bad.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || bad.Load() != 5 {
		t.Errorf("passed %d, refused %d", ok.Load(), bad.Load())
	}
	if n := notificationsOf(t, me, "application_rejected") + notificationsOf(t, me, "application_accepted"); n != 1 {
		t.Errorf("notifications = %d", n)
	}
}

// ---- что видит каждая сторона ----

func TestViewerHints(t *testing.T) {
	w := newWorld(t)
	me := w.Applicant("Мария")
	d := w.mustApply(me, nil)
	// Соискатель: кнопок организации нет.
	got, _ := w.svc.Get(bg, me.User, d.ID)
	if len(got.Viewer.Decisions) != 0 {
		t.Errorf("applicant: %+v", got.Viewer)
	}
	// Организация: из «просмотрен» можно принять и отказать. Приглашений внутри отклика нет (D-256).
	staff, _ := w.svc.Get(bg, w.tm.Owner.User, d.ID)
	if len(staff.Viewer.Decisions) != 2 || staff.Viewer.Decisions[0] != StatusAccepted || staff.Viewer.CanWithdraw {
		t.Errorf("staff: %+v", staff.Viewer)
	}
}

// ---- список откликов организации ----

type listWorld struct {
	*world
	other   *testkit.Team // чужая организация
	vacA    vacancies.Detail
	vacB    vacancies.Detail
	vacOth  vacancies.Detail
	appA    []Detail
	appB    Detail
	appOth  Detail
	applics []testkit.Person
}

func newListWorld(t *testing.T) *listWorld {
	w := newWorld(t)
	l := &listWorld{world: w}
	l.vacA = w.vacancy
	l.vacB = w.tm.Published(&w.tm.UnitB.ID)
	l.other = w.Team()
	l.vacOth = l.other.Published(&l.other.UnitA.ID)
	for _, name := range []string{"Анна", "Борис", "Вера"} {
		p := w.Applicant(name)
		l.applics = append(l.applics, p)
		l.appA = append(l.appA, w.mustApply(p, nil))
		w.clock.Advance(time.Minute)
	}
	l.appB = w.mustApply(l.applics[0], func(i *Input) { i.VacancyID = l.vacB.ID })
	l.appOth = w.mustApply(l.applics[1], func(i *Input) { i.VacancyID = l.vacOth.ID })
	return l
}

func ids(l CandidateList) []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, c := range l.Items {
		out[i] = c.ID
	}
	return out
}

func TestCandidatesScopeByRole(t *testing.T) {
	l := newListWorld(t)
	has := func(list CandidateList, id uuid.UUID) bool {
		for _, got := range ids(list) {
			if got == id {
				return true
			}
		}
		return false
	}
	type row struct {
		who  testkit.Person
		name string
		a, b bool // видит отклики на вакансии подразделений A и B
	}
	for _, r := range []row{
		{l.tm.Owner, "owner", true, true}, {l.tm.HR, "hr", true, true},
		{l.tm.HeadA, "head A", true, false}, {l.tm.HeadB, "head B", false, true},
		{l.tm.Out, "outsider", false, false}, {l.applics[0], "applicant", false, false},
	} {
		list, err := l.svc.Candidates(bg, r.who.User, CandidateFilter{})
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		for _, a := range l.appA {
			if has(list, a.ID) != r.a {
				t.Errorf("%s: sees unit A application = %v, want %v", r.name, !r.a, r.a)
			}
		}
		if has(list, l.appB.ID) != r.b {
			t.Errorf("%s: unit B application visibility", r.name)
		}
		if has(list, l.appOth.ID) {
			t.Errorf("%s sees an application of another organization", r.name)
		}
		want := 0
		if r.a {
			want += 3
		}
		if r.b {
			want++
		}
		if list.Total != want || len(list.Items) != want {
			t.Errorf("%s: total %d items %d, want %d", r.name, list.Total, len(list.Items), want)
		}
		vs, err := l.svc.CandidateVacancies(bg, r.who.User)
		if err != nil || len(vs) != btoi(r.a)+btoi(r.b) {
			t.Errorf("%s: vacancies %v %v", r.name, vs, err)
		}
	}
	// Чужой организации тоже ничего не видно с этой стороны; фильтр по чужой вакансии даёт пустоту, а не ошибку.
	list, err := l.svc.Candidates(bg, l.tm.Owner.User, CandidateFilter{VacancyID: l.vacOth.ID})
	if err != nil || list.Total != 0 || len(list.Items) != 0 {
		t.Errorf("foreign vacancy filter: %+v %v", list, err)
	}
	if list, _ := l.svc.Candidates(bg, l.other.Owner.User, CandidateFilter{}); list.Total != 1 || list.Items[0].ID != l.appOth.ID {
		t.Errorf("the other organization: %+v", list)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestCandidatesFiltersCountsAndPages(t *testing.T) {
	l := newListWorld(t)
	owner := l.tm.Owner.User
	// Порядок: новые сверху.
	all, _ := l.svc.Candidates(bg, owner, CandidateFilter{})
	if all.Total != 4 || all.Items[0].ID != l.appB.ID || all.Items[3].ID != l.appA[0].ID {
		t.Errorf("order: %v", ids(all))
	}
	if all.Counts[StatusSent] != 4 || all.Counts[StatusRejected] != 0 || len(all.Counts) != len(Statuses) {
		t.Errorf("counts = %v", all.Counts)
	}
	first := all.Items[3]
	if first.ApplicantName != "Анна" || first.Headline == "" || first.Vacancy.Title != l.vacA.Title || first.UnitName == "" || first.Vacancy.OrgName == "" {
		t.Errorf("row = %+v", first)
	}
	// Вакансия.
	byVac, _ := l.svc.Candidates(bg, owner, CandidateFilter{VacancyID: l.vacA.ID})
	if byVac.Total != 3 || byVac.Counts[StatusSent] != 3 {
		t.Errorf("by vacancy: %+v", byVac)
	}
	// Статус: счётчики остаются полными, список короче.
	l.setStatus(l.appA[0].ID, StatusAccepted)
	l.setStatus(l.appA[1].ID, StatusWithdrawn)
	byStatus, _ := l.svc.Candidates(bg, owner, CandidateFilter{Status: StatusAccepted})
	if byStatus.Total != 1 || byStatus.Items[0].ID != l.appA[0].ID || byStatus.Counts[StatusSent] != 2 || byStatus.Counts[StatusWithdrawn] != 1 {
		t.Errorf("by status: %+v", byStatus)
	}
	both, _ := l.svc.Candidates(bg, owner, CandidateFilter{Status: StatusSent, VacancyID: l.vacA.ID})
	if both.Total != 1 || both.Items[0].ID != l.appA[2].ID {
		t.Errorf("status and vacancy: %+v", both)
	}
	// Страницы: по умолчанию 20, предел 50, отрицательное смещение — с начала.
	page, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 3, Offset: 3})
	if page.Total != 4 || len(page.Items) != 1 {
		t.Errorf("page: %+v", page)
	}
	if beyond, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 3, Offset: 30}); len(beyond.Items) != 0 || beyond.Total != 4 {
		t.Errorf("beyond: %+v", beyond)
	}
	if odd, _ := l.svc.Candidates(bg, owner, CandidateFilter{Limit: 1000, Offset: -5}); len(odd.Items) != 4 {
		t.Errorf("odd paging: %+v", odd)
	}
	// Неизвестный статус.
	var verr *auth.ValidationError
	if _, err := l.svc.Candidates(bg, owner, CandidateFilter{Status: "done"}); !errors.As(err, &verr) || verr.Fields["status"] == "" {
		t.Errorf("unknown status: %v", err)
	}
}

func TestCandidateVacancies(t *testing.T) {
	l := newListWorld(t)
	vs, err := l.svc.CandidateVacancies(bg, l.tm.Owner.User)
	if err != nil || len(vs) != 2 {
		t.Fatalf("%v %v", vs, err)
	}
	byID := map[uuid.UUID]VacancyCount{}
	for _, v := range vs {
		byID[v.ID] = v
	}
	if a := byID[l.vacA.ID]; a.Total != 3 || a.New != 3 || a.Title != l.vacA.Title || a.OrgName == "" || a.OrgSlug != l.tm.Slug || a.Status != "published" {
		t.Errorf("A = %+v", a)
	}
	l.setStatus(l.appA[0].ID, StatusViewed)
	l.setStatus(l.appA[1].ID, StatusWithdrawn)
	vs, _ = l.svc.CandidateVacancies(bg, l.tm.Owner.User)
	for _, v := range vs {
		if v.ID == l.vacA.ID && (v.Total != 2 || v.New != 1) {
			t.Errorf("A after changes = %+v", v)
		}
	}
	// Вакансия без откликов в список не попадает; руководитель видит только свою.
	l.vacancy = l.tm.Published(&l.tm.UnitA.ID)
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.Owner.User); len(vs) != 2 {
		t.Errorf("a vacancy without applications is listed: %d", len(vs))
	}
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.HeadB.User); len(vs) != 1 || vs[0].ID != l.vacB.ID {
		t.Errorf("head B: %v", vs)
	}
	if vs, _ := l.svc.CandidateVacancies(bg, l.tm.Out.User); len(vs) != 0 {
		t.Errorf("outsider: %v", vs)
	}
}

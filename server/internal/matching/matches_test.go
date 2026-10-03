package matching

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/profiles"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

func matchIDs(l MatchList) []uuid.UUID {
	out := make([]uuid.UUID, len(l.Items))
	for i, it := range l.Items {
		out[i] = it.Vacancy.ID
	}
	return out
}

func TestMatchesNeedAProfileWithFields(t *testing.T) {
	w := newWorld(t)
	w.publish(vacancyOpts{specialties: []string{"1.4.4"}})

	nobody := w.User("Без профиля") // профиля ещё нет вообще
	l, err := w.svc.Matches(bg, nobody.User, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if l.Ready || len(l.Items) != 0 || l.Items == nil || l.Basis.Specialties != 0 || l.Basis.Level != 1 {
		t.Errorf("человек без профиля: %+v", l)
	}

	// Профиль создан, областей науки нет: подбирать не по чему.
	empty := w.seeker("Без областей", profiles.CoreInput{RegionCode: "54"})
	l, _ = w.svc.Matches(bg, empty.User, 20, 0)
	if l.Ready || len(l.Items) != 0 || !l.Basis.HasRegion {
		t.Errorf("профиль без областей: %+v", l)
	}
}

func TestMatchesRulesThroughTheDatabase(t *testing.T) {
	w := newWorld(t)
	p := w.seeker("Анна", profiles.CoreInput{
		Specialties: []string{"1.4.4"}, Degree: "candidate", DegreeSpecialty: "1.4.4", RegionCode: "54", City: "Новосибирск",
	})

	exact := w.publish(vacancyOpts{title: "Точное совпадение", specialties: []string{"1.4.4"}, level: intp(2), degree: "candidate", region: "54"})
	group := w.publish(vacancyOpts{title: "Та же группа", specialties: []string{"1.4.2"}, level: intp(2), region: "54"})
	section := w.publish(vacancyOpts{title: "Тот же раздел", specialties: []string{"1.1.1"}, level: intp(3), region: "77"}) // другая группа: не подходит
	remote := w.publish(vacancyOpts{title: "Удалённо", specialties: []string{"1.4.4"}, level: intp(2), format: vacancies.FormatRemote})
	other := w.publish(vacancyOpts{title: "Другая наука", specialties: []string{"3.1.1"}})
	needDoctor := w.publish(vacancyOpts{title: "Нужна докторская", specialties: []string{"1.4.4"}, degree: "doctor"})
	draft := w.create(vacancyOpts{title: "Черновик", specialties: []string{"1.4.4"}})
	closed := w.publish(vacancyOpts{title: "Закрыта", specialties: []string{"1.4.4"}})
	w.setStatus(closed.ID, vacancies.StatusClosed)
	past := w.publish(vacancyOpts{title: "Срок прошёл", specialties: []string{"1.4.4"}})
	y := moscowToday(w.clock.Now()).AddDate(0, 0, -1)
	w.setDeadline(past.ID, &y)
	applied := w.publish(vacancyOpts{title: "Уже откликалась", specialties: []string{"1.4.4"}})
	w.apply(p, applied.ID, "sent")
	withdrawn := w.publish(vacancyOpts{title: "Отозванный отклик", specialties: []string{"1.4.4"}, level: intp(2), region: "54"})
	w.apply(p, withdrawn.ID, "withdrawn")
	_ = draft

	l, err := w.svc.Matches(bg, p.User, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Ready || l.Basis.Specialties != 1 || l.Basis.Level != 2 || !l.Basis.HasRegion || !l.Basis.HasDegree {
		t.Errorf("основа: %+v", l.Basis)
	}
	// 40+25+15+20=100 | 40+25+20 (withdrawn: степень none) = 85 | та же группа 28+25+20=73 | удалённая 40+25+12=77
	got := map[uuid.UUID]MatchItem{}
	for _, it := range l.Items {
		got[it.Vacancy.ID] = it
	}
	for name, id := range map[string]uuid.UUID{
		"другая наука": other.ID, "другая группа того же раздела": section.ID, "нужна докторская": needDoctor.ID, "черновик": draft.ID, "закрыта": closed.ID,
		"срок прошёл": past.ID, "откликалась": applied.ID,
	} {
		if _, bad := got[id]; bad {
			t.Errorf("%s не должна быть в подборке", name)
		}
	}
	want := []uuid.UUID{exact.ID, withdrawn.ID, remote.ID, group.ID}
	sameIDs(t, "порядок по баллам", matchIDs(l), want...)
	scores := map[uuid.UUID]int{exact.ID: 100, withdrawn.ID: 85, remote.ID: 77, group.ID: 73}
	for id, s := range scores {
		if got[id].Score != s {
			t.Errorf("баллы %s: %d, ожидали %d", id, got[id].Score, s)
		}
	}
	if r := got[exact.ID].Reasons; !reflect.DeepEqual(r, []string{ReasonSpecialty, ReasonLevel, ReasonDegree, ReasonRegion}) {
		t.Errorf("причины точной вакансии: %v", r)
	}
	if r := got[remote.ID].Reasons; !reflect.DeepEqual(r, []string{ReasonSpecialty, ReasonLevel, ReasonRemote}) {
		t.Errorf("причины удалённой: %v", r)
	}
	if got[exact.ID].Vacancy.Title != "Точное совпадение" || got[exact.ID].Vacancy.Organization.Name == "" {
		t.Errorf("карточка: %+v", got[exact.ID].Vacancy)
	}

	// Отклик убирает вакансию из подборки.
	w.apply(p, group.ID, "sent")
	l, _ = w.svc.Matches(bg, p.User, 20, 0)
	sameIDs(t, "после отклика", matchIDs(l), exact.ID, withdrawn.ID, remote.ID)
}

func TestMatchesDegreeAndTitleGates(t *testing.T) {
	w := newWorld(t)
	doctor := w.seeker("Доктор", profiles.CoreInput{Specialties: []string{"1.4.4"}, Degree: "doctor", DegreeSpecialty: "1.4.4"})
	none := w.seeker("Аспирант", profiles.CoreInput{Specialties: []string{"1.4.4"}})
	cand := w.publish(vacancyOpts{title: "Нужна кандидатская", specialties: []string{"1.4.4"}, degree: "candidate"})
	doc := w.publish(vacancyOpts{title: "Нужна докторская", specialties: []string{"1.4.4"}, degree: "doctor"})
	free := w.publish(vacancyOpts{title: "Без требований", specialties: []string{"1.4.4"}})
	prof := w.publish(vacancyOpts{title: "Нужен профессор", specialties: []string{"1.4.4"}})
	// Звание в вакансии сервис разрешает только ППС; здесь проверяем само правило, поэтому ставим его напрямую.
	if _, err := testkit.Pool.Exec(bg, `UPDATE vacancies SET title_required = 'professor' WHERE id = $1`, prof.ID); err != nil {
		t.Fatal(err)
	}

	l, _ := w.svc.Matches(bg, doctor.User, 20, 0)
	got := map[uuid.UUID]bool{}
	for _, id := range matchIDs(l) {
		got[id] = true
	}
	if !got[cand.ID] || !got[doc.ID] || !got[free.ID] || got[prof.ID] {
		t.Errorf("доктор без звания: %v (профессорская не нужна, остальные нужны)", got)
	}
	l, _ = w.svc.Matches(bg, none.User, 20, 0)
	sameIDs(t, "без степени только без требований", matchIDs(l), free.ID)
	if l.Basis.HasDegree {
		t.Error("у аспиранта нет степени")
	}
}

// Вакансии своей организации в подборке не нужны: сотрудник сам разбирает на них отклики.
func TestMatchesSkipVacanciesOfMyOwnOrganization(t *testing.T) {
	w := newWorld(t)
	for _, who := range []testkit.Person{w.tm.Owner, w.tm.HR, w.tm.HeadA, w.tm.HeadB, w.tm.Out} {
		if _, err := w.Prof.SaveCore(bg, who.User, profiles.CoreInput{Headline: "Учёный", Specialties: []string{"1.4.4"}}); err != nil {
			t.Fatal(err)
		}
	}
	va := w.publish(vacancyOpts{title: "Вакансия подразделения А", specialties: []string{"1.4.4"}, unit: &w.tm.UnitA.ID})
	vb := w.publish(vacancyOpts{title: "Вакансия подразделения Б", specialties: []string{"1.4.4"}, unit: &w.tm.UnitB.ID})
	vo := w.publish(vacancyOpts{title: "Вся организация", specialties: []string{"1.4.4"}})
	for _, c := range []struct {
		name string
		who  testkit.Person
		sees map[uuid.UUID]bool
	}{
		{"владелец", w.tm.Owner, map[uuid.UUID]bool{va.ID: false, vb.ID: false, vo.ID: false}},
		{"кадровик", w.tm.HR, map[uuid.UUID]bool{va.ID: false, vb.ID: false, vo.ID: false}},
		{"руководитель А", w.tm.HeadA, map[uuid.UUID]bool{va.ID: false, vb.ID: true, vo.ID: true}},
		{"руководитель Б", w.tm.HeadB, map[uuid.UUID]bool{va.ID: true, vb.ID: false, vo.ID: true}},
		{"посторонний", w.tm.Out, map[uuid.UUID]bool{va.ID: true, vb.ID: true, vo.ID: true}},
	} {
		l, err := w.svc.Matches(bg, c.who.User, 20, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := map[uuid.UUID]bool{}
		for _, id := range matchIDs(l) {
			got[id] = true
		}
		for id, want := range c.sees {
			if got[id] != want {
				t.Errorf("%s: вакансия %s в подборке = %v, ожидали %v", c.name, id, got[id], want)
			}
		}
	}
}

func TestMatchesTieBreakAndPages(t *testing.T) {
	w := newWorld(t)
	p := w.seeker("Анна", profiles.CoreInput{Specialties: []string{"1.4.4"}})
	today := moscowToday(w.clock.Now())
	at := func(d int) *time.Time { t := today.AddDate(0, 0, d); return &t }
	// Все четыре дают одинаковые баллы (только специальность): ближайший срок первым, без срока в конце.
	a := w.publish(vacancyOpts{title: "Срок 20", specialties: []string{"1.4.4"}})
	w.setDeadline(a.ID, at(20))
	b := w.publish(vacancyOpts{title: "Срок 5", specialties: []string{"1.4.4"}})
	w.setDeadline(b.ID, at(5))
	c := w.publish(vacancyOpts{title: "Без срока", specialties: []string{"1.4.4"}, noDeadline: true})
	d := w.publish(vacancyOpts{title: "Срок 12", specialties: []string{"1.4.4"}})
	w.setDeadline(d.ID, at(12))

	l, _ := w.svc.Matches(bg, p.User, 20, 0)
	sameIDs(t, "ничья", matchIDs(l), b.ID, d.ID, a.ID, c.ID)
	if l.Total != 4 {
		t.Errorf("всего %d", l.Total)
	}
	l, _ = w.svc.Matches(bg, p.User, 2, 1)
	sameIDs(t, "вторая страница со смещением", matchIDs(l), d.ID, a.ID)
	if l.Total != 4 {
		t.Errorf("всего %d", l.Total)
	}
	l, _ = w.svc.Matches(bg, p.User, 2, 100)
	if len(l.Items) != 0 || l.Total != 4 {
		t.Errorf("за последней страницей: %+v", l)
	}
}

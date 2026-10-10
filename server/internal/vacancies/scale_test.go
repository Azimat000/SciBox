package vacancies

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Тексты для поиска хранят отметку «опубликована» и дату публикации (срез 24): поиск по словам берёт из индекса только
// открытые вакансии и самые свежие совпадения. Отметка ведётся триггером базы, а не кодом сервиса.
func TestSearchRowFollowsTheStatus(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	d := w.create(tm, tm.owner, goodInput())
	state := func() (open bool, published *time.Time) {
		t.Helper()
		if err := sharedPool.QueryRow(bg, `SELECT open, published_at FROM vacancy_search WHERE vacancy_id = $1`, d.ID).Scan(&open, &published); err != nil {
			t.Fatal(err)
		}
		return open, published
	}
	if open, published := state(); open || published != nil {
		t.Fatalf("a draft is searchable: open=%v published=%v", open, published)
	}
	steps := []struct {
		to   string
		open bool
	}{{StatusPublished, true}, {StatusClosed, false}, {StatusPublished, true}, {StatusClosed, false}, {StatusArchived, false}}
	for _, st := range steps {
		w.clock.Advance(time.Hour)
		if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, st.to); err != nil {
			t.Fatalf("%s: %v", st.to, err)
		}
		open, published := state()
		if open != st.open {
			t.Fatalf("after %s: open = %v, want %v", st.to, open, st.open)
		}
		var want *time.Time
		if err := sharedPool.QueryRow(bg, `SELECT published_at FROM vacancies WHERE id = $1`, d.ID).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if (published == nil) != (want == nil) || (want != nil && !published.Equal(*want)) {
			t.Fatalf("after %s: published_at in the search row is %v, in the vacancy %v", st.to, published, want)
		}
	}
}

// Коды специальностей вместе с кодами групп и областей: фильтр «область науки» идёт по ним. Меняются вместе со специальностями.
func TestSearchCodesFollowTheSpecialties(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.Specialties = []string{"1.1.1", "1.5.7"}
	d := w.published(tm, in)
	codes := func() []string {
		t.Helper()
		var c []string
		if err := sharedPool.QueryRow(bg, `SELECT codes FROM vacancy_search WHERE vacancy_id = $1`, d.ID).Scan(&c); err != nil {
			t.Fatal(err)
		}
		slices.Sort(c)
		return c
	}
	if got, want := codes(), []string{"1", "1.1", "1.1.1", "1.5", "1.5.7"}; !slices.Equal(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
	found := func(field string) bool {
		t.Helper()
		res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Fields: []string{field}})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range res.Items {
			if c.ID == d.ID {
				return true
			}
		}
		return false
	}
	for field, want := range map[string]bool{"1": true, "1.1": true, "1.1.1": true, "1.5": true, "1.5.7": true, "1.2": false, "1.1.2": false, "2": false, "1.5.70": false} {
		if got := found(field); got != want {
			t.Errorf("field %q: found = %v, want %v", field, got, want)
		}
	}
	in.Specialties = []string{"1.5.7"}
	if _, err := w.svc.Update(bg, tm.owner.User, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if got, want := codes(), []string{"1", "1.5", "1.5.7"}; !slices.Equal(got, want) {
		t.Fatalf("after the change codes = %v, want %v", got, want)
	}
	if found("1.1") || !found("1.5") {
		t.Fatal("the filter does not follow the changed specialties")
	}
	// Закрытая вакансия по области не находится, даже если коды остались.
	if _, err := w.svc.SetStatus(bg, tm.owner.User, d.ID, StatusClosed); err != nil {
		t.Fatal(err)
	}
	if found("1.5") {
		t.Fatal("a closed vacancy is found by field")
	}
}

// Предел выдачи: сервис отдаёт не больше MaxResults первых результатов, говорит «больше предела» и не листает дальше.
func TestSearchIsCapped(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	word := fmt.Sprintf("предельнослов%d", nameSeq.Add(1))
	var titles []string // от самой свежей к самой старой
	for i := range 5 {
		in := goodInput()
		in.Title = fmt.Sprintf("%s вакансия %d", word, i)
		w.clock.Advance(time.Hour)
		w.published(tm, in)
		titles = slices.Insert(titles, 0, in.Title)
	}
	titleOf := func(r SearchResult) []string {
		var out []string
		for _, c := range r.Items {
			out = append(out, c.Title)
		}
		return out
	}
	search := func(max, limit, offset int, sort string) SearchResult {
		t.Helper()
		w.svc.cfg.MaxResults = max
		res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Query: word, Limit: limit, Offset: offset, Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	t.Run("больше предела", func(t *testing.T) {
		res := search(3, 2, 0, "")
		if res.Total != 3 || !res.Capped || !slices.Equal(titleOf(res), titles[:2]) {
			t.Fatalf("total=%d capped=%v titles=%v", res.Total, res.Capped, titleOf(res))
		}
		res = search(3, 2, 2, "")
		if res.Total != 3 || !res.Capped || !slices.Equal(titleOf(res), titles[2:3]) {
			t.Fatalf("last page: total=%d capped=%v titles=%v", res.Total, res.Capped, titleOf(res))
		}
	})
	t.Run("за пределом пусто, но число и пометка остаются", func(t *testing.T) {
		res := search(3, 2, 4, "")
		if len(res.Items) != 0 || res.Total != 3 || !res.Capped {
			t.Fatalf("items=%d total=%d capped=%v", len(res.Items), res.Total, res.Capped)
		}
	})
	t.Run("ровно предел — не «больше»", func(t *testing.T) {
		res := search(5, 20, 0, "")
		if res.Total != 5 || res.Capped || len(res.Items) != 5 {
			t.Fatalf("total=%d capped=%v items=%d", res.Total, res.Capped, len(res.Items))
		}
	})
	t.Run("по совпадению берутся самые свежие", func(t *testing.T) {
		// Совпадающих пять, предел три: оценка считается среди трёх самых свежих.
		res := search(3, 20, 0, SortRelevance)
		got := titleOf(res)
		slices.Sort(got)
		want := slices.Clone(titles[:3])
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("titles = %v, want the three newest %v", got, want)
		}
	})
	t.Run("предел по умолчанию", func(t *testing.T) {
		w.svc.cfg.MaxResults = 0
		if got := w.svc.cfg.maxResults(); got != MaxResults {
			t.Fatalf("default cap = %d", got)
		}
	})
}

// Поиск с опечатками идёт по триграммам названия, должности и организации и находит слово с одной лишней или чужой буквой.
func TestFuzzySearchUsesTrigrams(t *testing.T) {
	w := newWorld(t)
	tm := w.team()
	in := goodInput()
	in.Title = "Лаборатория хроматографии высокого разрешения"
	d := w.published(tm, in)
	for _, q := range []string{"хроматографии", "хроматографи", "хроматографиии", "хромотографии"} {
		res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Query: q})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(res.Items, func(c Card) bool { return c.ID == d.ID }) {
			t.Errorf("query %q does not find the vacancy (fuzzy=%v, items=%d)", q, res.Fuzzy, len(res.Items))
		}
	}
	res, err := w.svc.Search(bg, SearchParams{OrgSlug: tm.slug, Query: "космическаятелепортация"})
	if err != nil || len(res.Items) != 0 || res.Fuzzy {
		t.Fatalf("an unrelated word found something: %v fuzzy=%v err=%v", len(res.Items), res.Fuzzy, err)
	}
}

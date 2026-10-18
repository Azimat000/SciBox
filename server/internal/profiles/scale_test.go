package profiles

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// Текст профиля для поиска (profile_search) пересобирается триггерами базы, а не кодом сервиса: что бы ни менялось — поля профиля,
// специальности, имя в аккаунте — каталог видит свежее (срез 24).
func TestProfileSearchFollowsTheProfile(t *testing.T) {
	w := newWorld(t)
	oldMark, newMark, nameMark := marker(), marker(), marker()
	p := w.listed("Поисков Иван", oldMark, "public", func(in *CoreInput) { in.Specialties = []string{"1.4.4"} })
	ids := func(q string, fields ...string) []string {
		t.Helper()
		return catalogNames(w.catalog(nil, CatalogParams{Query: q, Fields: fields}))
	}
	if got := ids(oldMark); len(got) != 1 {
		t.Fatalf("a new profile is not found by its headline: %v", got)
	}

	t.Run("должность", func(t *testing.T) {
		if _, err := w.svc.SaveCore(bg, p.User, CoreInput{Headline: "Исследователь " + newMark, City: "Москва", RegionCode: "77", Specialties: []string{"1.4.4"}}); err != nil {
			t.Fatal(err)
		}
		if len(ids(oldMark)) != 0 || len(ids(newMark)) != 1 {
			t.Fatalf("old=%v new=%v", ids(oldMark), ids(newMark))
		}
	})
	t.Run("имя в аккаунте", func(t *testing.T) {
		if _, err := sharedPool.Exec(bg, `UPDATE users SET display_name = $1 WHERE id = $2`, "Переименов "+nameMark, p.ID); err != nil {
			t.Fatal(err)
		}
		if got := ids(nameMark); len(got) != 1 || got[0] != "Переименов "+nameMark {
			t.Fatalf("a renamed account is not found by the new name: %v", got)
		}
	})
	t.Run("специальности и их коды", func(t *testing.T) {
		var codes []string
		if err := sharedPool.QueryRow(bg, `SELECT codes FROM profile_search WHERE profile_id = (SELECT id FROM profiles WHERE user_id = $1)`, p.ID).Scan(&codes); err != nil {
			t.Fatal(err)
		}
		slices.Sort(codes)
		if want := []string{"1", "1.4", "1.4.4"}; !slices.Equal(codes, want) {
			t.Fatalf("codes = %v, want %v", codes, want)
		}
		for field, want := range map[string]bool{"1": true, "1.4": true, "1.4.4": true, "1.5": false, "2": false, "1.4.40": false} {
			if got := len(ids(newMark, field)) == 1; got != want {
				t.Errorf("field %q: found = %v, want %v", field, got, want)
			}
		}
		if _, err := w.svc.SaveCore(bg, p.User, CoreInput{Headline: "Исследователь " + newMark, City: "Москва", RegionCode: "77", Specialties: []string{"1.5.7"}}); err != nil {
			t.Fatal(err)
		}
		if len(ids(newMark, "1.4")) != 0 || len(ids(newMark, "1.5")) != 1 {
			t.Fatalf("the filter does not follow the changed specialties: 1.4=%v 1.5=%v", ids(newMark, "1.4"), ids(newMark, "1.5"))
		}
	})
	t.Run("удаление профиля убирает и текст", func(t *testing.T) {
		if _, err := sharedPool.Exec(bg, `DELETE FROM profiles WHERE user_id = $1`, p.ID); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, `SELECT count(*) FROM profile_search WHERE fuzzy_text LIKE $1`, "%"+newMark+"%"); n != 0 {
			t.Fatalf("%d search rows are left after the profile is gone", n)
		}
	})
}

// Лучшее из четырёх h-index лежит в столбце: каталог сортирует по индексу, а не считает на лету.
func TestHMaxColumn(t *testing.T) {
	w := newWorld(t)
	p := w.listed("Хиндексов", marker(), "public", func(in *CoreInput) {
		in.HRsci, in.HScopus, in.HWos, in.HScholar = ptr(3), ptr(11), nil, ptr(7)
	})
	var h int
	if err := sharedPool.QueryRow(bg, `SELECT h_max FROM profiles WHERE user_id = $1`, p.ID).Scan(&h); err != nil || h != 11 {
		t.Fatalf("h_max = %d, err %v", h, err)
	}
	if _, err := w.svc.SaveCore(bg, p.User, CoreInput{Headline: "Исследователь", City: "Москва", RegionCode: "77"}); err != nil {
		t.Fatal(err)
	}
	if err := sharedPool.QueryRow(bg, `SELECT h_max FROM profiles WHERE user_id = $1`, p.ID).Scan(&h); err != nil || h != 0 {
		t.Fatalf("h_max without indexes = %d, err %v", h, err)
	}
}

// Счётчик статей в Q1–Q2 (profiles.q12_total) ведёт триггер при любой правке публикаций.
func TestQ12CounterFollowsThePublications(t *testing.T) {
	ensureJournals(t)
	w := newWorld(t)
	p := w.user("Счётчикова")
	counter := func() int {
		t.Helper()
		var n int
		if err := sharedPool.QueryRow(bg, `SELECT q12_total FROM profiles WHERE user_id = $1`, p.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	w.setVisibility(p, "hidden") // создаёт профиль
	if counter() != 0 {
		t.Fatalf("a new profile has %d", counter())
	}
	a := w.addItem(p, pubIn(issnQ1, 2025, "10.5555/q24-a"))
	b := w.addItem(p, pubIn(issnQ2, 2024, "10.5555/q24-b"))
	w.addItem(p, pubIn(issnQ3, 2024, "10.5555/q24-c"))
	w.addItem(p, pubIn(issnNoQ, 2024, "10.5555/q24-d"))
	if counter() != 2 {
		t.Fatalf("after four publications: %d, want 2", counter())
	}
	// Правка журнала у публикации.
	edit := pubIn(issnQ3, 2025, "10.5555/q24-a")
	if _, err := w.svc.UpdateItem(bg, p.User, a.ID, edit); err != nil {
		t.Fatal(err)
	}
	if counter() != 1 {
		t.Fatalf("after moving a publication to Q3: %d, want 1", counter())
	}
	// Удаление.
	if err := w.svc.DeleteItem(bg, p.User, b.ID); err != nil {
		t.Fatal(err)
	}
	if counter() != 0 {
		t.Fatalf("after deleting: %d, want 0", counter())
	}
	// Другие виды записей счётчик не трогают.
	w.addItem(p, ItemInput{Kind: KindEducation, ItemFields: ItemFields{Institution: "Университет", Program: "Аспирантура", YearFrom: ptr(2010), YearTo: ptr(2014)}})
	if counter() != 0 {
		t.Fatalf("education changed the counter: %d", counter())
	}
}

// Предел выдачи каталога: сервис отдаёт не больше MaxResults первых профилей и говорит «больше предела».
func TestCatalogIsCapped(t *testing.T) {
	w := newWorld(t)
	m := marker()
	var names []string // от самого свежего к самому старому
	for i := range 5 {
		name := fmt.Sprintf("Предельный %c", rune('А'+i))
		w.listed(name, m, "public", nil)
		names = slices.Insert(names, 0, name)
		w.clock.Advance(time.Minute) // updated_at у соседних профилей различается
	}
	catalog := func(max, limit, offset int, sort string) CatalogResult {
		t.Helper()
		w.svc.cfg.MaxResults = max
		res, err := w.svc.Catalog(bg, CatalogParams{Query: m, Limit: limit, Offset: offset, Sort: sort}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := catalog(3, 2, 0, SortUpdated); res.Total != 3 || !res.Capped || !slices.Equal(catalogNames(res), names[:2]) {
		t.Fatalf("first page: total=%d capped=%v %v", res.Total, res.Capped, catalogNames(res))
	}
	if res := catalog(3, 2, 2, SortUpdated); res.Total != 3 || !res.Capped || !slices.Equal(catalogNames(res), names[2:3]) {
		t.Fatalf("last page: total=%d capped=%v %v", res.Total, res.Capped, catalogNames(res))
	}
	if res := catalog(3, 2, 4, SortUpdated); len(res.Items) != 0 || res.Total != 3 || !res.Capped {
		t.Fatalf("past the cap: items=%d total=%d capped=%v", len(res.Items), res.Total, res.Capped)
	}
	if res := catalog(5, 20, 0, SortUpdated); res.Total != 5 || res.Capped || len(res.Items) != 5 {
		t.Fatalf("exactly the cap: total=%d capped=%v items=%d", res.Total, res.Capped, len(res.Items))
	}
	if res := catalog(3, 20, 0, SortName); res.Total != 3 || !res.Capped || len(res.Items) != 3 {
		t.Fatalf("by name: total=%d capped=%v items=%d", res.Total, res.Capped, len(res.Items))
	}
	w.svc.cfg.MaxResults = 0
	if got := w.svc.cfg.maxResults(); got != MaxCatalogResults {
		t.Fatalf("default cap = %d", got)
	}
}

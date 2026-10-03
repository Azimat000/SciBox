package matching

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/testkit"
)

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var verr *auth.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("ожидали ошибку полей, получили %v", err)
	}
	return verr.Fields
}

func TestCreateSearchStoresCanonicalQuery(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	for _, c := range []struct {
		name, in, want string
	}{
		{"слова", "q=химия", "q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F"},
		{"со знаком вопроса и пробелами вокруг", "  ?field=1.4&region=54  ", "field=1.4&region=54"},
		{"порядок параметров по алфавиту", "region=54&field=1.4&q=физика", "field=1.4&q=%D1%84%D0%B8%D0%B7%D0%B8%D0%BA%D0%B0&region=54"},
		{"несколько значений сохраняют порядок", "field=1.4&field=2.3&level=2&level=3", "field=1.4&field=2.3&level=2&level=3"},
		{"страница, организация, подразделение и размеры страниц отбрасываются", "q=ab&page=3&org=x&unit=" + uuid.NewString() + "&limit=5&offset=10", "q=ab"},
		{"лишние пробелы в словах", "q=%20a%20%20b%20", "q=a+b"},
		{"порядок остаётся", "field=1.4&sort=deadline", "field=1.4&sort=deadline"},
		{"галочки", "housing=true&competition=1&salary_min=80000", "competition=1&housing=1&salary_min=80000"},
		{"пустые значения не пишутся", "q=&region=&field=1.4", "field=1.4"},
		{"все виды фильтров", "format=remote&type=research&degree=candidate&org_kind=institute&funding=grant&rate=100&term=short&deadline=week",
			"deadline=week&degree=candidate&format=remote&funding=grant&org_kind=institute&rate=100&term=short&type=research"},
		{"срок подачи «без срока»", "deadline=none", "deadline=none"},
	} {
		s, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Мой поиск", Query: c.in})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if s.Query != c.want {
			t.Errorf("%s: %q, ожидали %q", c.name, s.Query, c.want)
		}
		if s.Frequency != FreqDaily || s.Name != "Мой поиск" || s.ID == uuid.Nil || s.LastSentAt != nil {
			t.Errorf("%s: %+v", c.name, s)
		}
		// Сохранённое читается обратно без потерь.
		again, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Ещё", Query: s.Query})
		if err != nil || again.Query != s.Query {
			t.Errorf("%s: повторная запись канонического вида: %q %v", c.name, again.Query, err)
		}
		// Лимит 20 на человека: убираем лишнее.
		for _, id := range []uuid.UUID{s.ID, again.ID} {
			if err := w.svc.DeleteSearch(bg, p.User, id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCreateSearchValidation(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	long := strings.Repeat("я", 121)
	manyFields := strings.Repeat("field=1.4&", 31)
	for _, c := range []struct {
		name  string
		in    SearchInput
		field string
		has   string
	}{
		{"нет названия", SearchInput{Name: "  ", Query: "q=a"}, "name", "Назовите"},
		{"слишком длинное название", SearchInput{Name: long, Query: "q=a"}, "name", "слишком длинное"},
		{"управляющие знаки в названии", SearchInput{Name: "а\x00б", Query: "q=a"}, "name", "недопустимые"},
		{"неизвестная частота", SearchInput{Name: "х", Query: "q=a", Frequency: "hourly"}, "frequency", "Выберите"},
		{"пустые условия", SearchInput{Name: "х", Query: ""}, "query", "Введите слова"},
		{"только порядок и страница", SearchInput{Name: "х", Query: "sort=new&page=2"}, "query", "Введите слова"},
		{"только организация", SearchInput{Name: "х", Query: "org=baikal"}, "query", "Введите слова"},
		{"неизвестный формат", SearchInput{Name: "х", Query: "format=teleport"}, "query", "Неизвестное значение"},
		{"неверный регион", SearchInput{Name: "х", Query: "region=abc"}, "query", "регион"},
		{"неверная область науки", SearchInput{Name: "х", Query: "field=abc"}, "query", "область"},
		{"не число в уровне", SearchInput{Name: "х", Query: "level=x"}, "query", "число"},
		{"уровень вне 1–4", SearchInput{Name: "х", Query: "level=9"}, "query", "Уровень"},
		{"сломанная запись адреса", SearchInput{Name: "х", Query: "q=%zz"}, "query", "неверно"},
		{"неверный номер подразделения", SearchInput{Name: "х", Query: "q=a&unit=zzz"}, "query", "неверно"},
		{"слишком длинные слова", SearchInput{Name: "х", Query: "q=" + strings.Repeat("я", 201)}, "query", "длинный"},
		{"слишком длинный адрес", SearchInput{Name: "х", Query: "q=a&" + strings.Repeat("x", 2001)}, "query", "слишком длинные"},
		{"слишком много значений", SearchInput{Name: "х", Query: manyFields}, "query", "много"},
		{"в каноническом виде длиннее предела", SearchInput{Name: "х", Query: encodedTooLong()}, "query", "слишком длинные"},
	} {
		_, err := w.svc.CreateSearch(bg, p.User, c.in)
		fields := fieldsOf(t, err)
		if msg, ok := fields[c.field]; !ok || !strings.Contains(msg, c.has) {
			t.Errorf("%s: поля %v, ожидали в %q «%s»", c.name, fields, c.field, c.has)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM saved_searches WHERE user_id = $1`, p.ID); n != 0 {
		t.Errorf("ошибки проверки оставили %d поисков", n)
	}
	// Несколько ошибок сразу показываются все.
	_, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "", Query: "", Frequency: "x"})
	if f := fieldsOf(t, err); len(f) != 3 {
		t.Errorf("ожидали ошибки трёх полей: %v", f)
	}
}

func TestCreateSearchSchedulesTheFirstRun(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	now := msk(2026, 10, 7, 15, 0)
	w.clock.Set(now)
	for freq, want := range map[string]time.Time{
		"": msk(2026, 10, 8, 9, 0), FreqDaily: msk(2026, 10, 8, 9, 0), FreqWeekly: msk(2026, 10, 12, 9, 0), FreqInstant: now,
	} {
		s, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=a", Frequency: freq})
		if err != nil {
			t.Fatal(err)
		}
		var next, checked time.Time
		if err := testkit.Pool.QueryRow(bg, `SELECT next_run_at, checked_at FROM saved_searches WHERE id = $1`, s.ID).Scan(&next, &checked); err != nil {
			t.Fatal(err)
		}
		if !next.Equal(want) || !checked.Equal(now) {
			t.Errorf("%q: следующий проход %s, граница %s; ожидали %s и %s", freq, next.In(moscow), checked.In(moscow), want.In(moscow), now.In(moscow))
		}
	}
}

func TestSearchesLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.MaxSearches = 3
	p := w.User("Анна")
	var first uuid.UUID
	for i := range 3 {
		s, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Поиск", Query: "q=a"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = s.ID
		}
	}
	if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Лишний", Query: "q=a"}); !errors.Is(err, ErrTooManySearches) {
		t.Fatalf("четвёртый: %v", err)
	}
	// Ошибка проверки не прячется за пределом: сначала поля, потом счёт.
	if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "", Query: "q=a"}); fieldsOf(t, err)["name"] == "" {
		t.Errorf("ожидали ошибку названия")
	}
	if err := w.svc.DeleteSearch(bg, p.User, first); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Снова", Query: "q=a"}); err != nil {
		t.Fatalf("после удаления: %v", err)
	}
	// У другого человека свой счёт.
	other := w.User("Борис")
	if _, err := w.svc.CreateSearch(bg, other.User, SearchInput{Name: "Мой", Query: "q=a"}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchesLimitHoldsUnderRace(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.MaxSearches = 2
	p := w.User("Анна")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Поиск", Query: "q=a"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTooManySearches):
				full++
			default:
				t.Errorf("ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 2 || full != 4 {
		t.Errorf("создано %d, отказано %d", ok, full)
	}
}

func TestSearchesListAndGet(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	if got, err := w.svc.Searches(bg, p.User); err != nil || got == nil || len(got) != 0 {
		t.Errorf("пустой список: %v %v", got, err)
	}
	a, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Первый", Query: "q=a"})
	w.clock.Advance(time.Minute)
	b, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Второй", Query: "q=b", Frequency: FreqOff})
	got, _ := w.svc.Searches(bg, p.User)
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("новые сверху: %+v", got)
	}
	one, err := w.svc.Search(bg, p.User, a.ID)
	if err != nil || one.Name != "Первый" || one.Query != "q=a" {
		t.Errorf("%+v %v", one, err)
	}
	if _, err := w.svc.Search(bg, p.User, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("несуществующий: %v", err)
	}
}

func TestSearchesAreOwn(t *testing.T) {
	w := newWorld(t)
	anna, boris := w.User("Анна"), w.User("Борис")
	s, err := w.svc.CreateSearch(bg, anna.User, SearchInput{Name: "Анин", Query: "q=a"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := w.svc.Searches(bg, boris.User); len(got) != 0 {
		t.Errorf("чужие поиски в списке: %+v", got)
	}
	if _, err := w.svc.Search(bg, boris.User, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чтение чужого: %v", err)
	}
	if _, err := w.svc.UpdateSearch(bg, boris.User, s.ID, SearchInput{Name: "Захвачен", Frequency: FreqOff}); !errors.Is(err, ErrNotFound) {
		t.Errorf("правка чужого: %v", err)
	}
	if err := w.svc.DeleteSearch(bg, boris.User, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("удаление чужого: %v", err)
	}
	now, err := w.svc.Search(bg, anna.User, s.ID)
	if err != nil || now.Name != "Анин" || now.Frequency != FreqDaily {
		t.Errorf("чужие действия ничего не изменили: %+v %v", now, err)
	}
}

func TestUpdateSearch(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	t0 := msk(2026, 10, 7, 15, 0) // среда
	w.clock.Set(t0)
	s, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Старое имя", Query: "q=a", Frequency: FreqDaily})
	state := func() (checked, next time.Time) {
		if err := testkit.Pool.QueryRow(bg, `SELECT checked_at, next_run_at FROM saved_searches WHERE id = $1`, s.ID).Scan(&checked, &next); err != nil {
			t.Fatal(err)
		}
		return
	}

	// Меняется только имя: расписание и граница те же. Условия поиска не меняются, даже если прислать другие.
	w.clock.Advance(time.Hour)
	got, err := w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "  Новое   имя ", Query: "q=другое", Frequency: FreqDaily})
	if err != nil || got.Name != "Новое имя" || got.Query != "q=a" || got.Frequency != FreqDaily {
		t.Fatalf("%+v %v", got, err)
	}
	if checked, next := state(); !checked.Equal(t0) || !next.Equal(msk(2026, 10, 8, 9, 0)) {
		t.Errorf("расписание изменилось: %s %s", checked.In(moscow), next.In(moscow))
	}

	// Другая частота пересчитывает расписание, но границу не трогает.
	got, err = w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "Новое имя", Frequency: FreqWeekly})
	if err != nil || got.Frequency != FreqWeekly {
		t.Fatalf("%+v %v", got, err)
	}
	if checked, next := state(); !checked.Equal(t0) || !next.Equal(msk(2026, 10, 12, 9, 0)) {
		t.Errorf("после смены частоты: %s %s", checked.In(moscow), next.In(moscow))
	}

	// «Не сообщать»: граница стоит. Включение обратно начинает счёт заново, чтобы письмо не засыпало накопленным.
	if _, err := w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "Новое имя", Frequency: FreqOff}); err != nil {
		t.Fatal(err)
	}
	if checked, _ := state(); !checked.Equal(t0) {
		t.Errorf("пауза двигает границу: %s", checked)
	}
	w.clock.Advance(30 * 24 * time.Hour)
	back := w.clock.Now()
	if _, err := w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "Новое имя", Frequency: FreqInstant}); err != nil {
		t.Fatal(err)
	}
	if checked, next := state(); !checked.Equal(back) || !next.Equal(back) {
		t.Errorf("возобновление: граница %s, следующий проход %s, ожидали %s", checked.In(moscow), next.In(moscow), back.In(moscow))
	}
}

func TestUpdateSearchValidation(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	s, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Имя", Query: "q=a"})
	_, err := w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "", Frequency: "никогда"})
	f := fieldsOf(t, err)
	if f["name"] == "" || f["frequency"] == "" {
		t.Errorf("поля: %v", f)
	}
	// Без частоты править нельзя: она обязательна при правке.
	_, err = w.svc.UpdateSearch(bg, p.User, s.ID, SearchInput{Name: "Имя"})
	if fieldsOf(t, err)["frequency"] == "" {
		t.Errorf("частота обязательна")
	}
	now, _ := w.svc.Search(bg, p.User, s.ID)
	if now.Name != "Имя" || now.Frequency != FreqDaily {
		t.Errorf("ошибки проверки ничего не меняют: %+v", now)
	}
}

func TestDeleteSearch(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	s, _ := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "Имя", Query: "q=a"})
	if err := w.svc.DeleteSearch(bg, p.User, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.DeleteSearch(bg, p.User, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	if _, err := w.svc.Search(bg, p.User, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("после удаления: %v", err)
	}
}

// Запись с кириллицей без процентов короче предела, а в каноническом виде (с процентами) уже длиннее.
func encodedTooLong() string {
	rep := func(s string) string { return strings.Repeat(s+"&", 30) }
	return "q=" + strings.Repeat("я", 200) + "&" + rep("org_kind=institute") + rep("funding=grant") + rep("format=onsite")
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.MaxFavorites != 200 || c.MaxSearches != 20 || c.Settle != time.Minute || c.MaxListed != 10 || c.Batch != 100 {
		t.Errorf("%+v", c)
	}
	s := NewService(testkit.Pool, nil, nil, c, silent())
	if d := time.Since(s.now()); d < 0 || d > time.Minute {
		t.Errorf("часы сервиса идут не по настоящему времени: %s", d)
	}
}

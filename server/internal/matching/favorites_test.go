package matching

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

func TestFavoritesLifecycle(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	a, b := w.publish(vacancyOpts{}), w.publish(vacancyOpts{})

	w.addFavorite(p, a.ID)
	w.addFavorite(p, a.ID) // повторное добавление ничего не меняет
	w.addFavorite(p, b.ID)
	got, err := w.svc.FavoriteIDs(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "номера", got, b.ID, a.ID) // новые добавления сверху
	if n := testkit.Count(t, `SELECT count(*) FROM favorites WHERE user_id = $1`, p.ID); n != 2 {
		t.Errorf("в таблице %d записей, ожидали 2", n)
	}

	list, err := w.svc.Favorites(bg, p.User, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 {
		t.Errorf("всего %d", list.Total)
	}
	sameIDs(t, "список", ids(list.Items), b.ID, a.ID)
	if it := list.Items[0]; it.State != StateOpen || it.ApplicationID != nil || it.AddedAt.IsZero() {
		t.Errorf("запись: %+v", it)
	}

	if err := w.svc.RemoveFavorite(bg, p.User, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.RemoveFavorite(bg, p.User, a.ID); err != nil { // убрать то, чего нет, можно
		t.Fatal(err)
	}
	if err := w.svc.RemoveFavorite(bg, p.User, uuid.New()); err != nil {
		t.Fatal(err)
	}
	got, _ = w.svc.FavoriteIDs(bg, p.User)
	sameIDs(t, "после удаления", got, b.ID)
}

func TestFavoritesEmptyListsAreNotNull(t *testing.T) {
	w := newWorld(t)
	p := w.User("Пустой")
	got, err := w.svc.FavoriteIDs(bg, p.User)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("номера: %v %v", got, err)
	}
	list, err := w.svc.Favorites(bg, p.User, 20, 0)
	if err != nil || list.Items == nil || list.Total != 0 {
		t.Errorf("список: %+v %v", list, err)
	}
	cal, err := w.svc.Deadlines(bg, p.User)
	if err != nil || cal.Items == nil || cal.WithoutDeadline != 0 {
		t.Errorf("календарь: %+v %v", cal, err)
	}
}

// Избранным можно сделать только то, что видят все: опубликованную и закрытую вакансию.
func TestFavoritesOnlyPublicVacancies(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	draft := w.create(vacancyOpts{})
	archived := w.publish(vacancyOpts{})
	w.setStatus(archived.ID, vacancies.StatusClosed)
	w.setStatus(archived.ID, vacancies.StatusArchived)
	closed := w.publish(vacancyOpts{})
	w.setStatus(closed.ID, vacancies.StatusClosed)
	open := w.publish(vacancyOpts{})

	for name, id := range map[string]uuid.UUID{"черновик": draft.ID, "архив": archived.ID, "несуществующая": uuid.New()} {
		if err := w.svc.AddFavorite(bg, p.User, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: ожидали ErrNotFound, получили %v", name, err)
		}
	}
	if n := testkit.Count(t, `SELECT count(*) FROM favorites WHERE user_id = $1`, p.ID); n != 0 {
		t.Errorf("отказ оставил %d записей", n)
	}
	w.addFavorite(p, closed.ID, open.ID)
	list, _ := w.svc.Favorites(bg, p.User, 20, 0)
	sameIDs(t, "открытая первой, закрытая после", ids(list.Items), open.ID, closed.ID)
	if list.Items[0].State != StateOpen || list.Items[1].State != StateClosed {
		t.Errorf("состояния: %s, %s", list.Items[0].State, list.Items[1].State)
	}
}

// Вакансия, ставшая архивной, не показывается в списке, но остаётся избранной и возвращается, когда её открывают снова.
func TestArchivedFavoriteIsHiddenAndComesBack(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	v := w.publish(vacancyOpts{})
	w.addFavorite(p, v.ID)

	w.setStatus(v.ID, vacancies.StatusClosed)
	w.setStatus(v.ID, vacancies.StatusArchived)
	list, _ := w.svc.Favorites(bg, p.User, 20, 0)
	if len(list.Items) != 0 || list.Total != 0 {
		t.Errorf("архивная вакансия в списке: %+v", list)
	}
	if got, _ := w.svc.FavoriteIDs(bg, p.User); len(got) != 1 {
		t.Errorf("номер должен остаться: %v", got)
	}

	w.setStatus(v.ID, vacancies.StatusClosed)
	w.setStatus(v.ID, vacancies.StatusPublished)
	list, _ = w.svc.Favorites(bg, p.User, 20, 0)
	sameIDs(t, "после возвращения", ids(list.Items), v.ID)
}

func TestFavoritesAreOwn(t *testing.T) {
	w := newWorld(t)
	anna, boris := w.User("Анна"), w.User("Борис")
	v := w.publish(vacancyOpts{})
	w.addFavorite(anna, v.ID)

	if got, _ := w.svc.FavoriteIDs(bg, boris.User); len(got) != 0 {
		t.Errorf("чужое избранное: %v", got)
	}
	if list, _ := w.svc.Favorites(bg, boris.User, 20, 0); len(list.Items) != 0 {
		t.Errorf("чужое избранное в списке: %+v", list)
	}
	if err := w.svc.RemoveFavorite(bg, boris.User, v.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.svc.FavoriteIDs(bg, anna.User); len(got) != 1 {
		t.Errorf("Борис убрал чужую закладку: %v", got)
	}
	if cal, _ := w.svc.Deadlines(bg, boris.User); len(cal.Items) != 0 {
		t.Errorf("чужой календарь: %+v", cal)
	}
}

func TestFavoritesLimit(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.MaxFavorites = 3
	p := w.User("Анна")
	var vs []vacancies.Detail
	for range 5 {
		vs = append(vs, w.publish(vacancyOpts{}))
	}
	w.addFavorite(p, vs[0].ID, vs[1].ID, vs[2].ID)
	if err := w.svc.AddFavorite(bg, p.User, vs[3].ID); !errors.Is(err, ErrTooManyFavorites) {
		t.Fatalf("четвёртая: %v", err)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM favorites WHERE user_id = $1`, p.ID); n != 3 {
		t.Errorf("после отказа %d записей", n)
	}
	// Уже избранную можно «добавить» и на пределе, а после удаления место освобождается.
	w.addFavorite(p, vs[0].ID)
	if err := w.svc.RemoveFavorite(bg, p.User, vs[1].ID); err != nil {
		t.Fatal(err)
	}
	w.addFavorite(p, vs[3].ID)
}

// Два запроса одновременно не пройдут мимо предела.
func TestFavoritesLimitHoldsUnderRace(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.MaxFavorites = 2
	p := w.User("Анна")
	var vs []vacancies.Detail
	for range 6 {
		vs = append(vs, w.publish(vacancyOpts{}))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	for _, v := range vs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := w.svc.AddFavorite(bg, p.User, v.ID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTooManyFavorites):
				full++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 2 || full != 4 {
		t.Errorf("принято %d, отказано %d", ok, full)
	}
	if n := testkit.Count(t, `SELECT count(*) FROM favorites WHERE user_id = $1`, p.ID); n != 2 {
		t.Errorf("в таблице %d записей", n)
	}
}

func TestFavoritesListOrderStatesAndPages(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	today := moscowToday(w.clock.Now())

	expired := w.publish(vacancyOpts{})
	y := today.AddDate(0, 0, -1)
	w.setDeadline(expired.ID, &y)
	closed := w.publish(vacancyOpts{})
	w.setStatus(closed.ID, vacancies.StatusClosed)
	lastDay := w.publish(vacancyOpts{})
	w.setDeadline(lastDay.ID, &today) // последний день ещё считается открытым
	noDeadline := w.publish(vacancyOpts{noDeadline: true})
	applied := w.publish(vacancyOpts{})
	w.apply(p, applied.ID, "sent")
	withdrawn := w.publish(vacancyOpts{})
	w.apply(p, withdrawn.ID, "withdrawn")

	// Добавляем в таком порядке, чтобы «открытые сверху» нельзя было получить случайно.
	w.addFavorite(p, expired.ID, closed.ID, lastDay.ID, noDeadline.ID, applied.ID, withdrawn.ID)
	list, err := w.svc.Favorites(bg, p.User, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Открытые (новые добавления выше), затем остальные тоже новые выше.
	sameIDs(t, "порядок", ids(list.Items), withdrawn.ID, applied.ID, noDeadline.ID, lastDay.ID, closed.ID, expired.ID)
	states := map[uuid.UUID]string{}
	apps := map[uuid.UUID]bool{}
	for _, it := range list.Items {
		states[it.Vacancy.ID] = it.State
		apps[it.Vacancy.ID] = it.ApplicationID != nil
	}
	for id, want := range map[uuid.UUID]string{
		expired.ID: StateExpired, closed.ID: StateClosed, lastDay.ID: StateOpen, noDeadline.ID: StateOpen, applied.ID: StateOpen,
	} {
		if states[id] != want {
			t.Errorf("состояние %s: %s, ожидали %s", id, states[id], want)
		}
	}
	if !apps[applied.ID] || apps[withdrawn.ID] || apps[lastDay.ID] {
		t.Errorf("отметка об отклике: %v (отозванный отклик не считается)", apps)
	}

	// Страницы: вторая страница и страница дальше последней знают общее число.
	page2, _ := w.svc.Favorites(bg, p.User, 4, 4)
	if len(page2.Items) != 2 || page2.Total != 6 {
		t.Errorf("вторая страница: %d записей из %d", len(page2.Items), page2.Total)
	}
	beyond, _ := w.svc.Favorites(bg, p.User, 4, 40)
	if len(beyond.Items) != 0 || beyond.Total != 6 {
		t.Errorf("за последней страницей: %d записей из %d", len(beyond.Items), beyond.Total)
	}
	// Размер страницы: по умолчанию 20, не больше 50, смещение не бывает отрицательным.
	if got, _ := w.svc.Favorites(bg, p.User, 0, -5); len(got.Items) != 6 {
		t.Errorf("умолчания: %d", len(got.Items))
	}
	if got, _ := w.svc.Favorites(bg, p.User, 1000, 0); len(got.Items) != 6 {
		t.Errorf("предел: %d", len(got.Items))
	}
}

func TestDeadlineCalendar(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	today := moscowToday(w.clock.Now())
	at := func(d int) *time.Time { t := today.AddDate(0, 0, d); return &t }

	far := w.publish(vacancyOpts{})
	w.setDeadline(far.ID, at(40))
	soon := w.publish(vacancyOpts{})
	w.setDeadline(soon.ID, at(3))
	todayV := w.publish(vacancyOpts{})
	w.setDeadline(todayV.ID, at(0))
	gone := w.publish(vacancyOpts{})
	w.setDeadline(gone.ID, at(-1))
	closed := w.publish(vacancyOpts{})
	w.setDeadline(closed.ID, at(5))
	w.setStatus(closed.ID, vacancies.StatusClosed)
	none1, none2 := w.publish(vacancyOpts{noDeadline: true}), w.publish(vacancyOpts{noDeadline: true})
	applied := w.publish(vacancyOpts{})
	w.setDeadline(applied.ID, at(10))
	w.apply(p, applied.ID, "sent")
	w.addFavorite(p, far.ID, soon.ID, todayV.ID, gone.ID, closed.ID, none1.ID, none2.ID, applied.ID)

	cal, err := w.svc.Deadlines(bg, p.User)
	if err != nil {
		t.Fatal(err)
	}
	var gotIDs []uuid.UUID
	days := map[uuid.UUID]int{}
	for _, it := range cal.Items {
		gotIDs = append(gotIDs, it.Vacancy.ID)
		days[it.Vacancy.ID] = it.DaysLeft
	}
	sameIDs(t, "по близости срока", gotIDs, todayV.ID, soon.ID, applied.ID, far.ID)
	if days[todayV.ID] != 0 || days[soon.ID] != 3 || days[far.ID] != 40 {
		t.Errorf("дни: %v", days)
	}
	if cal.WithoutDeadline != 2 {
		t.Errorf("без срока %d, ожидали 2", cal.WithoutDeadline)
	}
	for _, it := range cal.Items {
		if (it.Vacancy.ID == applied.ID) != (it.ApplicationID != nil) {
			t.Errorf("отметка об отклике у %s: %v", it.Vacancy.ID, it.ApplicationID)
		}
	}
}

func TestDaysLeftWithoutDeadline(t *testing.T) {
	if got := daysLeft(*day(2026, 10, 7), nil); got != 0 {
		t.Errorf("%d", got)
	}
	if got := daysLeft(*day(2026, 10, 7), day(2026, 10, 10)); got != 3 {
		t.Errorf("%d", got)
	}
	if stateOf("published", nil, *day(2026, 10, 7)) != StateOpen {
		t.Error("без срока открыта")
	}
}

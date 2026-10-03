package matching

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// Среда, 7 октября 2026 года, 10:00 по Москве: «сегодня» для напоминаний.
var remindNow = msk(2026, 10, 7, 10, 0)

func oct(d int) *time.Time { return day(2026, time.October, d) }

type remindWorld struct {
	*world
	who testkit.Person
}

func newRemindWorld(t *testing.T) *remindWorld {
	t.Helper()
	w := newWorld(t)
	w.clock.Set(remindNow)
	return &remindWorld{world: w, who: w.User("Анна")}
}

// favorite публикует вакансию со сроком deadline, кладёт в избранное и ставит время добавления (по умолчанию давно).
func (r *remindWorld) favorite(who testkit.Person, deadline *time.Time, added time.Time) vacancies.Detail {
	r.T.Helper()
	v := r.publish(vacancyOpts{title: "Вакансия со сроком " + marker()})
	r.setDeadline(v.ID, deadline)
	r.addFavorite(who, v.ID)
	r.backdateFavorite(who, v.ID, added)
	return v
}

func (r *remindWorld) run() int {
	r.T.Helper()
	n, err := r.svc.SendReminders(bg)
	if err != nil {
		r.T.Fatalf("SendReminders: %v", err)
	}
	return n
}

func (r *remindWorld) notices(who testkit.Person) int {
	return notificationsOf(r.T, who, notifications.KindDeadlineReminder)
}

func (r *remindWorld) titles(who testkit.Person) []string {
	r.T.Helper()
	rows, err := testkit.Pool.Query(bg, `SELECT title FROM notifications WHERE user_id = $1 AND kind = 'deadline_reminder' ORDER BY created_at, id`, who.ID)
	if err != nil {
		r.T.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			r.T.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestRemindersTwoStagesOncePerDeadline(t *testing.T) {
	r := newRemindWorld(t)
	v := r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0)) // срок через 5 дней

	if n := r.run(); n != 1 {
		t.Fatalf("за пять дней отправлено %d", n)
	}
	if got := r.titles(r.who); len(got) != 1 || !strings.HasPrefix(got[0], "Срок подачи через 5 дней: «") {
		t.Fatalf("заголовки: %v", got)
	}
	var body, link string
	if err := testkit.Pool.QueryRow(bg, `SELECT body, link FROM notifications WHERE user_id = $1`, r.who.ID).Scan(&body, &link); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "12 октября 2026 г.") || !strings.Contains(body, "Институт набора") || link != "/vacancies/"+v.ID.String() {
		t.Errorf("текст и ссылка: %q %q", body, link)
	}
	if mailCount(t, r.who.Email) != 1 {
		t.Errorf("писем %d", mailCount(t, r.who.Email))
	}

	// Тот же день и следующие дни той же ступени: тишина.
	for _, h := range []time.Duration{0, 5 * time.Hour, 24 * time.Hour, 48 * time.Hour} {
		r.clock.Set(remindNow.Add(h))
		if n := r.run(); n != 0 {
			t.Errorf("через %s отправлено %d", h, n)
		}
	}

	// За сутки: второе напоминание. Потом в день срока ничего нового.
	r.clock.Set(msk(2026, 10, 11, 10, 0))
	if n := r.run(); n != 1 {
		t.Fatalf("за сутки отправлено %d", n)
	}
	if got := r.titles(r.who); len(got) != 2 || !strings.HasPrefix(got[1], "Срок подачи завтра: «") {
		t.Errorf("заголовки: %v", got)
	}
	for _, h := range []time.Duration{time.Hour, 24 * time.Hour} { // 11:00 в тот же день и 12 октября, последний день
		r.clock.Set(msk(2026, 10, 11, 10, 0).Add(h))
		if n := r.run(); n != 0 {
			t.Errorf("повтор: %d", n)
		}
	}
	if r.notices(r.who) != 2 || mailCount(t, r.who.Email) != 2 {
		t.Errorf("уведомлений %d, писем %d", r.notices(r.who), mailCount(t, r.who.Email))
	}
	if n := testkit.Count(t, `SELECT count(*) FROM deadline_reminders WHERE user_id = $1`, r.who.ID); n != 2 {
		t.Errorf("записей о напоминаниях %d", n)
	}
}

func TestRemindersOnTheLastDay(t *testing.T) {
	r := newRemindWorld(t)
	r.favorite(r.who, oct(7), msk(2026, 10, 1, 12, 0)) // срок сегодня
	if n := r.run(); n != 1 {
		t.Fatal(n)
	}
	if got := r.titles(r.who); len(got) != 1 || !strings.HasPrefix(got[0], "Сегодня последний день подачи: «") {
		t.Errorf("%v", got)
	}
}

// Какой «день» выбрать для ступени 7: ровно семь дней, и не восемь.
func TestRemindersWindowEdges(t *testing.T) {
	r := newRemindWorld(t)
	seven := r.favorite(r.who, oct(14), msk(2026, 9, 1, 12, 0))
	eight := r.favorite(r.who, oct(15), msk(2026, 9, 1, 12, 0))
	if n := r.run(); n != 1 {
		t.Fatalf("отправлено %d", n)
	}
	var vid uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT vacancy_id FROM deadline_reminders WHERE user_id = $1`, r.who.ID).Scan(&vid); err != nil {
		t.Fatal(err)
	}
	if vid != seven.ID || vid == eight.ID {
		t.Errorf("напоминание о вакансии %s, ожидали за семь дней %s", vid, seven.ID)
	}
	if got := r.titles(r.who); !strings.HasPrefix(got[0], "Срок подачи через 7 дней: «") {
		t.Errorf("%v", got)
	}
	// На следующий день восемь дней превращаются в семь.
	r.clock.Set(remindNow.Add(24 * time.Hour))
	if n := r.run(); n != 1 {
		t.Errorf("следующий день: %d", n)
	}
}

func TestRemindersNotBeforeNineInTheMorning(t *testing.T) {
	r := newRemindWorld(t)
	r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0))
	for _, at := range []time.Time{msk(2026, 10, 7, 0, 0), msk(2026, 10, 7, 8, 59), time.Date(2026, 10, 6, 21, 30, 0, 0, time.UTC)} {
		r.clock.Set(at)
		if n := r.run(); n != 0 {
			t.Errorf("в %s отправлено %d", at.In(moscow), n)
		}
	}
	r.clock.Set(msk(2026, 10, 7, 9, 0))
	if n := r.run(); n != 1 {
		t.Errorf("в девять отправлено %d", n)
	}
}

func TestRemindersSkipWhoShouldNotGetThem(t *testing.T) {
	r := newRemindWorld(t)
	long := msk(2026, 10, 1, 12, 0)

	applied := r.favorite(r.who, oct(12), long)
	r.apply(r.who, applied.ID, "sent")
	invited := r.favorite(r.who, oct(12), long)
	r.apply(r.who, invited.ID, "invited")
	withdrawn := r.favorite(r.who, oct(12), long) // отозванный отклик не мешает: вакансия снова открыта для человека
	r.apply(r.who, withdrawn.ID, "withdrawn")
	closed := r.favorite(r.who, oct(12), long)
	r.setStatus(closed.ID, vacancies.StatusClosed)
	archived := r.favorite(r.who, oct(12), long)
	r.setStatus(archived.ID, vacancies.StatusClosed)
	r.setStatus(archived.ID, vacancies.StatusArchived)
	gone := r.favorite(r.who, oct(6), long)
	_ = gone
	noDeadline := r.favorite(r.who, nil, long)
	_ = noDeadline
	// В избранном у другого человека: не нашему.
	other := r.User("Борис")
	notFav := r.publish(vacancyOpts{})
	r.setDeadline(notFav.ID, oct(12))
	r.addFavorite(other, notFav.ID)
	r.backdateFavorite(other, notFav.ID, long)
	removed := r.favorite(r.who, oct(12), long)
	if err := r.svc.RemoveFavorite(bg, r.who.User, removed.ID); err != nil {
		t.Fatal(err)
	}
	// Закладки нет совсем.
	r.publish(vacancyOpts{}) // срок через 90 дней, ничьё избранное

	if n := r.run(); n != 2 {
		t.Fatalf("отправлено %d, ожидали 2 (отозванный отклик и чужая закладка)", n)
	}
	if r.notices(r.who) != 1 || r.notices(other) != 1 {
		t.Errorf("у Анны %d, у Бориса %d", r.notices(r.who), r.notices(other))
	}
	var vid uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT vacancy_id FROM deadline_reminders WHERE user_id = $1`, r.who.ID).Scan(&vid); err != nil || vid != withdrawn.ID {
		t.Errorf("Анне напомнили о %s (%v), ожидали об отозванном отклике %s", vid, err, withdrawn.ID)
	}
}

// Кто только что добавил вакансию в избранное, напоминание этой ступени не получает: он сам только что на неё смотрел.
func TestRemindersSkipFreshFavorites(t *testing.T) {
	r := newRemindWorld(t)
	// Срок 12 октября: окно ступени «неделя» началось 5 октября в 00:00 по Москве, ступени «сутки» начнётся 11 октября.
	startWeek := msk(2026, 10, 5, 0, 0)
	late := r.favorite(r.who, oct(12), msk(2026, 10, 6, 12, 0)) // добавлено внутри окна недели
	exact := r.favorite(r.who, oct(12), startWeek)              // ровно в начале окна: уже «только что»
	justBefore := r.favorite(r.who, oct(12), startWeek.Add(-time.Second))
	_ = late
	_ = exact

	if n := r.run(); n != 1 {
		t.Fatalf("отправлено %d, ожидали 1", n)
	}
	var vid uuid.UUID
	if err := testkit.Pool.QueryRow(bg, `SELECT vacancy_id FROM deadline_reminders WHERE user_id = $1`, r.who.ID).Scan(&vid); err != nil || vid != justBefore.ID {
		t.Fatalf("напомнили о %s (%v), ожидали %s", vid, err, justBefore.ID)
	}

	// За сутки напомнят всем, кто добавил до 11 октября 00:00; тем, кто добавил позже, нет.
	r.clock.Set(msk(2026, 10, 11, 10, 0))
	lateDay := r.favorite(r.who, oct(12), msk(2026, 10, 11, 8, 0))
	_ = lateDay
	if n := r.run(); n != 3 {
		t.Fatalf("за сутки отправлено %d, ожидали 3 (все, кроме добавленной утром того дня)", n)
	}
}

func TestRemindersRemindAgainWhenTheDeadlineMoves(t *testing.T) {
	r := newRemindWorld(t)
	v := r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0))
	r.run()
	if r.notices(r.who) != 1 {
		t.Fatal("первое напоминание")
	}
	// Организация перенесла срок: напоминание о новом сроке уходит заново.
	r.setDeadline(v.ID, oct(14))
	if n := r.run(); n != 1 {
		t.Errorf("новый срок: %d", n)
	}
	if got := r.titles(r.who); len(got) != 2 || !strings.HasPrefix(got[1], "Срок подачи через 7 дней") {
		t.Errorf("%v", got)
	}
	if n := r.run(); n != 0 {
		t.Errorf("повтор: %d", n)
	}
}

func TestRemindersRespectEmailSettings(t *testing.T) {
	r := newRemindWorld(t)
	r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0))
	// Выключены письма о новых вакансиях: на напоминания это не влияет.
	if _, err := r.notes.SaveSettings(bg, r.who.User, notifications.Settings{EmailNewVacancies: false, EmailDeadlines: true}); err != nil {
		t.Fatal(err)
	}
	r.run()
	if r.notices(r.who) != 1 || mailCount(t, r.who.Email) != 1 {
		t.Errorf("уведомлений %d, писем %d", r.notices(r.who), mailCount(t, r.who.Email))
	}

	// Выключены письма о сроках: остаётся только уведомление на сайте.
	quiet := r.User("Тихий")
	if _, err := r.notes.SaveSettings(bg, quiet.User, notifications.Settings{EmailNewVacancies: true, EmailDeadlines: false}); err != nil {
		t.Fatal(err)
	}
	v := r.publish(vacancyOpts{})
	r.setDeadline(v.ID, oct(12))
	r.addFavorite(quiet, v.ID)
	r.backdateFavorite(quiet, v.ID, msk(2026, 10, 1, 12, 0))
	r.run()
	if r.notices(quiet) != 1 || mailCount(t, quiet.Email) != 0 {
		t.Errorf("у тихого: уведомлений %d, писем %d", r.notices(quiet), mailCount(t, quiet.Email))
	}
}

func TestRemindersNeverSendTwiceUnderRace(t *testing.T) {
	r := newRemindWorld(t)
	r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0))
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := r.svc.SendReminders(bg)
			if err != nil {
				t.Errorf("%v", err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 || r.notices(r.who) != 1 || mailCount(t, r.who.Email) != 1 {
		t.Errorf("отправлено %d, уведомлений %d, писем %d", total, r.notices(r.who), mailCount(t, r.who.Email))
	}
}

func TestRemindersHandleMoreThanOneBatch(t *testing.T) {
	r := newRemindWorld(t)
	r.svc.cfg.Batch = 2
	var people []testkit.Person
	v := r.publish(vacancyOpts{})
	r.setDeadline(v.ID, oct(12))
	for i := range 5 {
		p := r.User(fmt.Sprintf("Человек %d", i))
		people = append(people, p)
		r.addFavorite(p, v.ID)
		r.backdateFavorite(p, v.ID, msk(2026, 10, 1, 12, 0))
	}
	if n := r.run(); n != 5 {
		t.Fatalf("отправлено %d", n)
	}
	for _, p := range people {
		if r.notices(p) != 1 {
			t.Errorf("у %s нет напоминания", p.Email)
		}
	}
}

func TestPassDoesBothJobsAndSurvivesErrors(t *testing.T) {
	r := newRemindWorld(t)
	r.favorite(r.who, oct(12), msk(2026, 10, 1, 12, 0))
	r.svc.pass(bg)
	if r.notices(r.who) != 1 {
		t.Errorf("напоминание не ушло")
	}
	// Отменённый контекст: оба дела падают, проход всё равно возвращается.
	ctx, cancel := contextCancelled()
	cancel()
	r.svc.pass(ctx)
}

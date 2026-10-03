package matching

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"scibox/server/internal/auth"
	"scibox/server/internal/notifications"
	"scibox/server/internal/testkit"
	"scibox/server/internal/vacancies"
)

// digestWorld — человек с сохранённым поиском по уникальной метке и часы на момент создания поиска.
type digestWorld struct {
	*world
	who    testkit.Person
	mark   string
	search SavedSearch
	t0     time.Time
}

func newDigestWorld(t *testing.T, freq string) *digestWorld {
	t.Helper()
	w := newWorld(t)
	d := &digestWorld{world: w, who: w.User("Анна"), mark: marker()}
	d.t0 = time.Now().UTC()
	w.clock.Set(d.t0)
	s, err := w.svc.CreateSearch(bg, d.who.User, SearchInput{Name: "Поиск " + d.mark, Query: "q=" + d.mark, Frequency: freq})
	if err != nil {
		t.Fatal(err)
	}
	d.search = s
	return d
}

// matching публикует вакансию, которую поиск находит.
func (d *digestWorld) matching(title string) vacancies.Detail {
	d.T.Helper()
	return d.publish(vacancyOpts{title: title + " " + d.mark})
}

// morning ставит часы на пять минут после ближайшего «утра» для поиска и возвращает этот момент.
func (d *digestWorld) morning() time.Time {
	at := NextRun(FreqDaily, d.t0).Add(5 * time.Minute)
	d.clock.Set(at)
	return at
}

func (d *digestWorld) run() int {
	d.T.Helper()
	n, err := d.svc.SendDigests(bg)
	if err != nil {
		d.T.Fatalf("SendDigests: %v", err)
	}
	return n
}

func (d *digestWorld) notices() int {
	return notificationsOf(d.T, d.who, notifications.KindSavedSearch)
}
func (d *digestWorld) mails() int { return mailCount(d.T, d.who.Email) }

func (d *digestWorld) row() (checked, next time.Time, sent *time.Time) {
	d.T.Helper()
	if err := testkit.Pool.QueryRow(bg, `SELECT checked_at, next_run_at, last_sent_at FROM saved_searches WHERE id = $1`, d.search.ID).Scan(&checked, &next, &sent); err != nil {
		d.T.Fatal(err)
	}
	return
}

func TestDigestSendsNewVacanciesOnceAndOnlyNew(t *testing.T) {
	d := newDigestWorld(t, FreqDaily)
	// Эта вакансия вышла до сохранения поиска? Нет, до него вышли только те, что опубликованы ДО CreateSearch:
	// чтобы проверить это, создаём второй поиск после неё.
	d.matching("Первая")
	d.matching("Вторая")
	d.publish(vacancyOpts{title: "Совсем другая вакансия"}) // не подходит под поиск
	draft := d.create(vacancyOpts{title: "Черновик " + d.mark})
	closed := d.matching("Закрытая")
	d.setStatus(closed.ID, vacancies.StatusClosed)
	past := d.matching("Просроченная")
	y := moscowToday(d.clock.Now()).AddDate(0, 0, -1)
	d.setDeadline(past.ID, &y)
	_ = draft

	if n := d.run(); n != 0 {
		t.Fatalf("до утра ничего не отправляется: %d", n)
	}
	at := d.morning()
	if n := d.run(); n != 1 {
		t.Fatalf("отправлено %d, ожидали 1", n)
	}
	if d.notices() != 1 || d.mails() != 1 {
		t.Fatalf("уведомлений %d, писем %d", d.notices(), d.mails())
	}
	var title, body, link string
	if err := testkit.Pool.QueryRow(bg, `SELECT title, body, link FROM notifications WHERE user_id = $1 AND kind = 'saved_search'`, d.who.ID).Scan(&title, &body, &link); err != nil {
		t.Fatal(err)
	}
	if title != "По поиску «Поиск "+d.mark+"»: 2 новые вакансии" {
		t.Errorf("заголовок: %q", title)
	}
	for _, want := range []string{"Первая " + d.mark, "Вторая " + d.mark, "Институт набора", "Срок подачи: "} {
		if !strings.Contains(body, want) {
			t.Errorf("в тексте нет %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"Совсем другая", "Черновик", "Закрытая", "Просроченная"} {
		if strings.Contains(body, bad) {
			t.Errorf("в тексте лишнее %q:\n%s", bad, body)
		}
	}
	if link != "/saved-searches/"+d.search.ID.String() {
		t.Errorf("ссылка: %q", link)
	}
	var mailBody string
	if err := testkit.Pool.QueryRow(bg, `SELECT body FROM outbox WHERE to_email = $1`, d.who.Email).Scan(&mailBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mailBody, "http://localhost:5173/saved-searches/"+d.search.ID.String()) {
		t.Errorf("в письме нет ссылки на поиск:\n%s", mailBody)
	}

	checked, next, sent := d.row()
	if !checked.Equal(at.Add(-time.Minute)) || !next.Equal(NextRun(FreqDaily, at)) || sent == nil || !sent.Equal(at) {
		t.Errorf("состояние: граница %s, следующий %s, отправлено %v", checked, next, sent)
	}

	// Повторный проход ничего не шлёт: поиск ждёт следующего утра, а увиденное второй раз не присылается.
	if n := d.run(); n != 0 {
		t.Errorf("повторный проход: %d", n)
	}
	// Следующее утро без новых вакансий: тишина, но расписание движется.
	d.clock.Advance(24 * time.Hour)
	if n := d.run(); n != 0 {
		t.Errorf("без новых вакансий отправлено %d", n)
	}
	if _, next2, _ := d.row(); !next2.After(next) {
		t.Errorf("расписание не сдвинулось: %s", next2)
	}
	// Новая вакансия попадает в сводку следующего утра, и только она.
	third := d.matching("Третья")
	d.publishedAt(third.ID, d.clock.Now()) // часы теста ушли вперёд, настоящее время нет
	d.clock.Advance(24 * time.Hour)
	if n := d.run(); n != 1 {
		t.Fatalf("новая вакансия: %d", n)
	}
	if d.notices() != 2 {
		t.Errorf("уведомлений %d", d.notices())
	}
	if err := testkit.Pool.QueryRow(bg, `SELECT title FROM notifications WHERE user_id = $1 AND kind = 'saved_search' ORDER BY created_at DESC LIMIT 1`, d.who.ID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Новая вакансия по поиску «Поиск "+d.mark+"»" {
		t.Errorf("заголовок второй сводки: %q", title)
	}
}

func TestDigestIgnoresVacanciesPublishedBeforeTheSearch(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	mark := marker()
	w.publish(vacancyOpts{title: "Старая " + mark})
	w.clock.Set(time.Now().UTC())
	if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=" + mark, Frequency: FreqInstant}); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(10 * time.Minute)
	if n, err := w.svc.SendDigests(bg); err != nil || n != 0 {
		t.Errorf("%d %v: о том, что уже было, не сообщаем", n, err)
	}
}

func TestDigestFrequencies(t *testing.T) {
	t.Run("сразу: на ближайшем проходе, но не раньше запаса", func(t *testing.T) {
		d := newDigestWorld(t, FreqInstant)
		d.matching("Новая")
		// Вакансия опубликована только что: запас в минуту не даёт ей потеряться между проходами.
		d.clock.Advance(10 * time.Second)
		if n := d.run(); n != 0 {
			t.Fatalf("в пределах запаса отправлено %d", n)
		}
		if _, next, _ := d.row(); next.After(d.clock.Now()) {
			t.Errorf("«сразу» должно остаться в очереди на каждый проход: %s", next)
		}
		d.clock.Advance(2 * time.Minute)
		if n := d.run(); n != 1 {
			t.Fatalf("после запаса отправлено %d", n)
		}
		if n := d.run(); n != 0 {
			t.Errorf("повторно: %d", n)
		}
		if d.notices() != 1 {
			t.Errorf("уведомлений %d", d.notices())
		}
	})
	t.Run("раз в неделю: только в понедельник утром", func(t *testing.T) {
		w := newWorld(t)
		p := w.User("Анна")
		mark := marker()
		w.clock.Set(time.Now().UTC())
		start := w.clock.Now()
		if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=" + mark, Frequency: FreqWeekly}); err != nil {
			t.Fatal(err)
		}
		w.publish(vacancyOpts{title: "Новая " + mark})
		first := NextRun(FreqWeekly, start)
		if first.In(moscow).Weekday() != time.Monday {
			t.Fatalf("не понедельник: %s", first)
		}
		w.clock.Set(first.Add(-time.Minute))
		if n, _ := w.svc.SendDigests(bg); n != 0 {
			t.Errorf("до понедельника отправлено %d", n)
		}
		w.clock.Set(first.Add(time.Minute))
		if n, _ := w.svc.SendDigests(bg); n != 1 {
			t.Errorf("в понедельник отправлено %d", n)
		}
	})
	t.Run("не сообщать: никогда", func(t *testing.T) {
		d := newDigestWorld(t, FreqOff)
		d.matching("Новая")
		d.clock.Advance(400 * 24 * time.Hour)
		if n := d.run(); n != 0 {
			t.Errorf("отправлено %d", n)
		}
		if d.notices() != 0 || d.mails() != 0 {
			t.Errorf("уведомлений %d, писем %d", d.notices(), d.mails())
		}
	})
	t.Run("включили после паузы: накопленное не присылается", func(t *testing.T) {
		d := newDigestWorld(t, FreqOff)
		d.matching("Пока была пауза")
		d.clock.Advance(30 * 24 * time.Hour)
		if _, err := d.svc.UpdateSearch(bg, d.who.User, d.search.ID, SearchInput{Name: "х", Frequency: FreqInstant}); err != nil {
			t.Fatal(err)
		}
		d.clock.Advance(2 * time.Minute)
		if n := d.run(); n != 0 {
			t.Errorf("отправлено %d", n)
		}
	})
}

func TestDigestListsAtMostMaxListed(t *testing.T) {
	d := newDigestWorld(t, FreqDaily)
	d.svc.cfg.MaxListed = 2
	for i := range 5 {
		d.matching(fmt.Sprintf("Вакансия %d", i+1))
	}
	d.morning()
	if n := d.run(); n != 1 {
		t.Fatal(n)
	}
	var title, body string
	if err := testkit.Pool.QueryRow(bg, `SELECT title, body FROM notifications WHERE user_id = $1`, d.who.ID).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(title, "5 новых вакансий") || !strings.Contains(body, "И ещё 3 вакансии.") {
		t.Errorf("%q\n%s", title, body)
	}
	if got := strings.Count(body, "\n- ") + strings.Count(body[:2], "- "); got != 2 {
		t.Errorf("перечислено %d вакансий:\n%s", got, body)
	}
	// Новыми сверху: последние две.
	if !strings.Contains(body, "Вакансия 5") || !strings.Contains(body, "Вакансия 4") || strings.Contains(body, "Вакансия 1 ") {
		t.Errorf("не те вакансии:\n%s", body)
	}
}

func TestDigestRespectsEmailSettings(t *testing.T) {
	d := newDigestWorld(t, FreqDaily)
	if _, err := d.notes.SaveSettings(bg, d.who.User, notifications.Settings{EmailNewVacancies: false, EmailDeadlines: true}); err != nil {
		t.Fatal(err)
	}
	d.matching("Новая")
	d.morning()
	if n := d.run(); n != 1 {
		t.Fatal(n)
	}
	if d.notices() != 1 {
		t.Errorf("уведомление на сайте остаётся: %d", d.notices())
	}
	if d.mails() != 0 {
		t.Errorf("письмо отключено, а в очереди %d", d.mails())
	}
}

func TestDigestOnlyGoesToTheOwner(t *testing.T) {
	d := newDigestWorld(t, FreqDaily)
	other := d.User("Борис")
	if _, err := d.svc.CreateSearch(bg, other.User, SearchInput{Name: "Борис ищет другое", Query: "q=" + marker(), Frequency: FreqDaily}); err != nil {
		t.Fatal(err)
	}
	d.matching("Новая")
	d.morning()
	d.run()
	if d.notices() != 1 || notificationsOf(t, other, notifications.KindSavedSearch) != 0 || mailCount(t, other.Email) != 0 {
		t.Errorf("владелец %d, посторонний %d", d.notices(), notificationsOf(t, other, notifications.KindSavedSearch))
	}
}

// Запасной поиск «по похожим словам» в рассылке не нужен: письмо должно говорить только о том, что искали.
func TestDigestDoesNotUseFuzzySearch(t *testing.T) {
	w := newWorld(t)
	p := w.User("Анна")
	w.clock.Set(time.Now().UTC())
	if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=спектрскопия", Frequency: FreqInstant}); err != nil {
		t.Fatal(err)
	}
	w.publish(vacancyOpts{title: "Спектроскопия твёрдого тела"})
	// Обычный поиск с опечаткой находит вакансию по похожим словам…
	res, err := w.Vac.Search(bg, vacancies.SearchParams{Query: "спектрскопия"})
	if err != nil || !res.Fuzzy || res.Total == 0 {
		t.Fatalf("обычный поиск: %+v %v", res, err)
	}
	// …а рассылка нет.
	w.clock.Advance(5 * time.Minute)
	if n, err := w.svc.SendDigests(bg); err != nil || n != 0 {
		t.Errorf("рассылка по похожим словам: %d %v", n, err)
	}
}

func TestDigestSurvivesUnusableSavedQueries(t *testing.T) {
	d := newDigestWorld(t, FreqInstant)
	d.matching("Новая")
	d.clock.Advance(5 * time.Minute)
	for _, q := range []string{"q=%zz", "level=x", "unit=zzz", "field=abc", "format=teleport"} {
		if _, err := testkit.Pool.Exec(bg, `UPDATE saved_searches SET query = $2, checked_at = $3, next_run_at = $3 WHERE id = $1`, d.search.ID, q, d.t0); err != nil {
			t.Fatal(err)
		}
		n, err := d.svc.SendDigests(bg)
		if err != nil || n != 0 {
			t.Errorf("%q: %d %v", q, n, err)
		}
		// Поиск не застрял: граница ушла вперёд.
		if checked, _, _ := d.row(); !checked.After(d.t0) {
			t.Errorf("%q: граница не сдвинулась", q)
		}
	}
	if d.notices() != 0 {
		t.Errorf("уведомлений %d", d.notices())
	}
}

type stubSearcher struct {
	res vacancies.SearchResult
	err error
}

func (s stubSearcher) Search(context.Context, vacancies.SearchParams) (vacancies.SearchResult, error) {
	return s.res, s.err
}

// Сбой самого поиска не должен «съедать» вакансии: граница стоит на месте, следующий проход всё доставит.
func TestDigestRetriesAfterASearchFailure(t *testing.T) {
	d := newDigestWorld(t, FreqInstant)
	d.matching("Новая")
	d.clock.Advance(5 * time.Minute)
	good := d.svc.vac

	d.svc.vac = stubSearcher{err: errors.New("база недоступна")}
	n, err := d.svc.SendDigests(bg)
	if err == nil || n != 0 || !strings.Contains(err.Error(), "база недоступна") {
		t.Fatalf("ожидали ошибку: %d %v", n, err)
	}
	if checked, _, _ := d.row(); !checked.Equal(d.t0) || d.notices() != 0 {
		t.Errorf("после сбоя граница %s, уведомлений %d", checked, d.notices())
	}

	d.svc.vac = good
	if n := d.run(); n != 1 {
		t.Errorf("после восстановления: %d", n)
	}
}

// Условия, которые поиск отверг как недопустимые, не считаются сбоем: иначе поиск застрянет навсегда.
func TestDigestTreatsRejectedConditionsAsNothingNew(t *testing.T) {
	for name, err := range map[string]error{
		"поля": &auth.ValidationError{Fields: map[string]string{"format": "нет такого"}}, "нет организации": vacancies.ErrNotFound,
	} {
		d := newDigestWorld(t, FreqInstant)
		d.svc.vac = stubSearcher{err: err}
		d.clock.Advance(5 * time.Minute)
		if n, e := d.svc.SendDigests(bg); e != nil || n != 0 {
			t.Errorf("%s: %d %v", name, n, e)
		}
		if checked, _, _ := d.row(); !checked.After(d.t0) {
			t.Errorf("%s: граница не сдвинулась", name)
		}
	}
}

// Два одновременных прохода (два сервера или два запуска) не пришлют письмо дважды.
func TestDigestNeverSendsTwiceUnderRace(t *testing.T) {
	d := newDigestWorld(t, FreqInstant)
	d.matching("Новая")
	d.clock.Advance(5 * time.Minute)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := d.svc.SendDigests(bg)
			if err != nil {
				t.Errorf("%v", err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 || d.notices() != 1 || d.mails() != 1 {
		t.Errorf("отправлено %d, уведомлений %d, писем %d", total, d.notices(), d.mails())
	}
}

func TestDigestHandlesMoreSearchesThanOneBatch(t *testing.T) {
	w := newWorld(t)
	w.svc.cfg.Batch = 2
	mark := marker()
	w.clock.Set(time.Now().UTC())
	var people []testkit.Person
	for i := range 5 {
		p := w.User(fmt.Sprintf("Человек %d", i))
		people = append(people, p)
		if _, err := w.svc.CreateSearch(bg, p.User, SearchInput{Name: "х", Query: "q=" + mark, Frequency: FreqInstant}); err != nil {
			t.Fatal(err)
		}
	}
	w.publish(vacancyOpts{title: "Новая " + mark})
	w.clock.Advance(5 * time.Minute)
	n, err := w.svc.SendDigests(bg)
	if err != nil || n != 5 {
		t.Fatalf("отправлено %d (%v), ожидали 5", n, err)
	}
	for _, p := range people {
		if notificationsOf(t, p, notifications.KindSavedSearch) != 1 {
			t.Errorf("у %s нет уведомления", p.Email)
		}
	}
}

func TestDigestIsSkippedWhenTheSearchDisappears(t *testing.T) {
	d := newDigestWorld(t, FreqInstant)
	d.matching("Новая")
	d.clock.Advance(5 * time.Minute)
	if err := d.svc.DeleteSearch(bg, d.who.User, d.search.ID); err != nil {
		t.Fatal(err)
	}
	if n := d.run(); n != 0 {
		t.Errorf("отправлено %d по удалённому поиску", n)
	}
	// Прямой вызов по номеру, который уже не подходит: тоже тишина.
	if ok, err := d.svc.digest(bg, d.search.ID); ok || err != nil {
		t.Errorf("%v %v", ok, err)
	}
	if ok, err := d.svc.digest(bg, uuid.New()); ok || err != nil {
		t.Errorf("%v %v", ok, err)
	}
}

// Если часы ушли назад, граница не возвращается назад: иначе одни и те же вакансии пришли бы дважды.
func TestDigestNeverMovesTheBoundaryBack(t *testing.T) {
	d := newDigestWorld(t, FreqInstant)
	d.matching("Новая")
	d.clock.Advance(5 * time.Minute)
	d.run()
	checked, _, _ := d.row()
	d.clock.Set(d.t0.Add(-time.Hour))
	if _, err := testkit.Pool.Exec(bg, `UPDATE saved_searches SET next_run_at = $2 WHERE id = $1`, d.search.ID, d.t0.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := d.run(); n != 0 {
		t.Errorf("отправлено %d", n)
	}
	if after, _, _ := d.row(); after.Before(checked) {
		t.Errorf("граница вернулась назад: %s < %s", after, checked)
	}
	if d.notices() != 1 {
		t.Errorf("уведомлений %d", d.notices())
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { w.svc.Run(ctx, 10*time.Millisecond); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("цикл не остановился")
	}
}

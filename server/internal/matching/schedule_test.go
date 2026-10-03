package matching

import (
	"testing"
	"time"
)

func msk(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, moscow)
}

func TestNextRun(t *testing.T) {
	// 7 октября 2026 года — среда.
	for _, c := range []struct {
		name string
		freq string
		now  time.Time
		want time.Time
	}{
		{"сразу: сейчас", FreqInstant, msk(2026, 10, 7, 14, 30), msk(2026, 10, 7, 14, 30)},
		{"раз в день до девяти: сегодня в девять", FreqDaily, msk(2026, 10, 7, 8, 59), msk(2026, 10, 7, 9, 0)},
		{"раз в день ровно в девять: уже завтра", FreqDaily, msk(2026, 10, 7, 9, 0), msk(2026, 10, 8, 9, 0)},
		{"раз в день после девяти: завтра", FreqDaily, msk(2026, 10, 7, 15, 0), msk(2026, 10, 8, 9, 0)},
		{"раз в день вечером в конце месяца", FreqDaily, msk(2026, 10, 31, 23, 59), msk(2026, 11, 1, 9, 0)},
		{"раз в день: полночь по Москве", FreqDaily, msk(2026, 10, 7, 0, 0), msk(2026, 10, 7, 9, 0)},
		{"раз в неделю в среду: следующий понедельник", FreqWeekly, msk(2026, 10, 7, 12, 0), msk(2026, 10, 12, 9, 0)},
		{"раз в неделю в понедельник до девяти: сегодня", FreqWeekly, msk(2026, 10, 12, 8, 0), msk(2026, 10, 12, 9, 0)},
		{"раз в неделю в понедельник после девяти: через неделю", FreqWeekly, msk(2026, 10, 12, 9, 0), msk(2026, 10, 19, 9, 0)},
		{"раз в неделю в воскресенье: завтра", FreqWeekly, msk(2026, 10, 11, 23, 0), msk(2026, 10, 12, 9, 0)},
		{"время в UTC пересчитывается на Москву", FreqDaily, time.Date(2026, 10, 7, 21, 30, 0, 0, time.UTC), msk(2026, 10, 8, 9, 0)},
	} {
		got := NextRun(c.freq, c.now)
		if !got.Equal(c.want) {
			t.Errorf("%s: %s, ожидали %s", c.name, got.In(moscow), c.want.In(moscow))
		}
		if got.Location() != time.UTC && c.freq != FreqInstant {
			t.Errorf("%s: время должно быть в UTC", c.name)
		}
	}
}

func TestNextRunOffIsFarAway(t *testing.T) {
	now := msk(2026, 10, 7, 12, 0)
	if got := NextRun(FreqOff, now); !got.After(now.AddDate(50, 0, 0)) {
		t.Errorf("«не сообщать» должно уходить далеко вперёд, а вышло %s", got)
	}
	if got := NextRun("что-то", now); !got.After(now.AddDate(50, 0, 0)) {
		t.Errorf("неизвестная частота как «не сообщать»: %s", got)
	}
}

func TestValidFrequency(t *testing.T) {
	for _, f := range Frequencies {
		if !validFrequency(f) {
			t.Errorf("%q должно быть допустимо", f)
		}
	}
	for _, f := range []string{"", "hourly", "Daily", "DAILY"} {
		if validFrequency(f) {
			t.Errorf("%q недопустимо", f)
		}
	}
	if !validFrequency(DefaultFrequency) || DefaultFrequency != FreqDaily {
		t.Errorf("по умолчанию раз в день (D-104)")
	}
}

func TestMoscowToday(t *testing.T) {
	// 21:00 UTC уже следующий день по Москве.
	got := moscowToday(time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("%s, ожидали %s", got, want)
	}
	got = moscowToday(time.Date(2026, 10, 7, 20, 59, 59, 0, time.UTC))
	if want := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("%s, ожидали %s", got, want)
	}
}

func TestReminderHour(t *testing.T) {
	for _, c := range []struct {
		at time.Time
		ok bool
	}{
		{msk(2026, 10, 7, 0, 0), false}, {msk(2026, 10, 7, 8, 59), false}, {msk(2026, 10, 7, 9, 0), true}, {msk(2026, 10, 7, 23, 59), true},
		{time.Date(2026, 10, 7, 5, 59, 0, 0, time.UTC), false}, {time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC), true},
	} {
		if got := reminderHourOK(c.at); got != c.ok {
			t.Errorf("%s: %v, ожидали %v", c.at, got, c.ok)
		}
	}
}

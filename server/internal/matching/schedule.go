package matching

import "time"

// Время везде московское (D-057, D-089): в России нет перехода на летнее время.
var moscow = time.FixedZone("MSK", 3*60*60)

// Письма не уходят раньше этого часа по Москве: утренняя сводка, а не ночной звонок.
const sendHour = 9

// Частота рассылки по сохранённому поиску (D-104).
const (
	FreqInstant = "instant" // как только появится
	FreqDaily   = "daily"   // раз в день, утром
	FreqWeekly  = "weekly"  // раз в неделю, по понедельникам утром
	FreqOff     = "off"     // не сообщать
)

// Frequencies — все допустимые частоты.
var Frequencies = []string{FreqInstant, FreqDaily, FreqWeekly, FreqOff}

// DefaultFrequency — частота нового поиска.
const DefaultFrequency = FreqDaily

func validFrequency(f string) bool {
	for _, x := range Frequencies {
		if x == f {
			return true
		}
	}
	return false
}

// NextRun — когда фоновый цикл должен посмотреть в поиск в следующий раз. «Сразу» — на ближайшем проходе цикла, «раз в
// день» — следующее утро (9:00 МСК) строго после now, «раз в неделю» — ближайший понедельник, 9:00 МСК, строго после now.
func NextRun(freq string, now time.Time) time.Time {
	switch freq {
	case FreqInstant:
		return now
	case FreqDaily, FreqWeekly:
		n := now.In(moscow)
		at := time.Date(n.Year(), n.Month(), n.Day(), sendHour, 0, 0, 0, moscow)
		for !at.After(n) || (freq == FreqWeekly && at.Weekday() != time.Monday) {
			at = at.AddDate(0, 0, 1)
		}
		return at.UTC()
	}
	// «Не сообщать»: время не важно, такой поиск цикл не берёт.
	return now.AddDate(100, 0, 0)
}

// Напоминания о сроке (D-105) уходят за неделю и за сутки; какая ступень нужна сегодня, решает запрос
// ListReminderCandidates (там же правило «не напоминать тем, кто добавил вакансию только что»).
const reminderHorizonDays = 7

// moscowToday — сегодняшняя дата по Москве как полночь в UTC (так сравниваются сроки подачи).
func moscowToday(now time.Time) time.Time {
	n := now.In(moscow)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// reminderHourOK — не раньше девяти утра по Москве.
func reminderHourOK(now time.Time) bool { return now.In(moscow).Hour() >= sendHour }

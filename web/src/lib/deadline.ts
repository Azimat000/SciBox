import { t } from '../i18n'
import { plural } from './plural'

export type DeadlineView = {
  /** Дата словами: «9 октября». */
  date: string
  /** Сколько осталось: «осталось 7 дней», «сегодня последний день», «срок прошёл 3 дня назад». */
  relative: string
  /** Срок близко (до двух недель включительно) и ещё не прошёл: такие сроки отмечаем маркером. */
  urgent: boolean
  expired: boolean
}

export const URGENT_DAYS = 14

const MS_PER_DAY = 86_400_000

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' })
const dateYearFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })

function startOfDayUtc(d: Date): number {
  return Date.UTC(d.getFullYear(), d.getMonth(), d.getDate())
}

/** Целых календарных дней от `now` до срока (в местном времени). Отрицательное: срок прошёл. */
export function daysUntil(deadline: Date, now: Date): number {
  return Math.round((startOfDayUtc(deadline) - startOfDayUtc(now)) / MS_PER_DAY)
}

/** Срок подачи для интерфейса. `deadline` — дата вида `2026-11-14`. */
export function describeDeadline(deadline: string, now: Date): DeadlineView | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(deadline)
  if (!match) return null
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  if (Number.isNaN(date.getTime()) || date.getMonth() !== Number(match[2]) - 1) return null

  const d = t.deadline
  const days = daysUntil(date, now)
  const sameYear = date.getFullYear() === now.getFullYear()
  const dateText = (sameYear ? dateFormat : dateYearFormat).format(date)

  if (days < 0) {
    return { date: dateText, relative: d.passed(-days, plural(-days, d.units)), urgent: false, expired: true }
  }
  if (days === 0) return { date: dateText, relative: d.lastDay, urgent: true, expired: false }
  if (days === 1) return { date: dateText, relative: d.oneDay, urgent: true, expired: false }
  return {
    date: dateText,
    relative: d.daysLeft(days, plural(days, d.words), plural(days, d.units)),
    urgent: days <= URGENT_DAYS,
    expired: false,
  }
}

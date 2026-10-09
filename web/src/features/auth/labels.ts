import { t } from '../../i18n'
import type { SessionInfo } from './api'

// Время на сайте московское (D-057, D-089): срок удаления и время входа совпадают с письмами.
const dayFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'Europe/Moscow' })
const momentFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' })

// Число и месяц не разрываются переносом строки: «22 / октября» на узком экране читается плохо.
const keepDayWithMonth = (s: string) => s.replace(/^(\d+) /, '$1\u00a0')

/** «22 октября 2026» — без «г.», как в письмах. */
export function formatDay(iso: string): string {
  return keepDayWithMonth(dayFormat.format(new Date(iso)).replace(/\s?г\.$/, ''))
}

/** «2 октября, 14:05» по Москве. */
export function formatMoment(iso: string): string {
  return keepDayWithMonth(momentFormat.format(new Date(iso)).replace(' в ', ', '))
}

/** «Chrome, Windows», «Safari, iOS · телефон», «Неизвестное устройство». */
export function deviceLabel(s: Pick<SessionInfo, 'browser' | 'os' | 'mobile'>): string {
  const browser = t.auth.sessions.browsers[s.browser] ?? ''
  const os = t.auth.sessions.systems[s.os] ?? ''
  let label = browser && os ? t.auth.sessions.on(browser, os) : browser || os || t.auth.sessions.unknownDevice
  if (s.mobile) label += ` · ${t.auth.sessions.phone}`
  return label
}

/** Сколько ждать до следующей попытки: «15 мин», «3 ч». Округляется вверх, чтобы не обещать раньше срока. */
export function waitLabel(seconds: number): string {
  const minutes = Math.max(1, Math.ceil(seconds / 60))
  return minutes < 60 ? t.auth.dataExport.waitMinutes(minutes) : t.auth.dataExport.waitHours(Math.ceil(minutes / 60))
}

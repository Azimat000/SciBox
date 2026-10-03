import { t } from '../../i18n'
import type { AppStatus, RefStatus } from './api'

export const statusLabel = (s: string) => (t.applications.statuses as Record<string, string>)[s] ?? s

/** Положительные статусы отмечаются акцентом, остальные нейтральны. */
export const statusTone = (s: AppStatus): 'neutral' | 'accent' => (s === 'invited' || s === 'accepted' ? 'accent' : 'neutral')

export const refStatusLabel = (s: RefStatus) => t.applications.detail.refStatus[s]

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
const dateTimeFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })

/** «3 октября 2026» из момента времени. */
export const dateText = (iso: string) => dateFormat.format(new Date(iso))

/** «3 октября, 14:20». */
export const dateTimeText = (iso: string) => dateTimeFormat.format(new Date(iso))

const momentFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' })

/** «15 октября 2026 г. в 14:00 (МСК)»: время приглашений везде московское (D-089). */
export const momentText = (iso: string) => `${momentFormat.format(new Date(iso))} (${t.applications.invitation.msk})`

/** Из значений полей даты и времени («2026-10-15», «14:00») получается момент по Москве для сервера. */
export const mskMoment = (date: string, time: string) => `${date}T${time}:00+03:00`

export const kindLabel = (k: string) => (t.applications.invitation.kinds as Record<string, string>)[k] ?? k
export const invStatusLabel = (s: string) => (t.applications.invitation.statuses as Record<string, string>)[s] ?? s

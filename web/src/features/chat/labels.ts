import { t } from '../../i18n'
import type { Author, ChatStatus, Counterpart, LastMessage } from './api'

/** Имя собеседника: у анонимного соискателя подпись вместо имени (D-199), у пустого имени организации — тоже подпись. */
export function counterpartName(c: Counterpart): string {
  if (c.kind === 'scientist' && (c.anonymous || !c.name)) return t.chat.anonymous
  return c.name
}

/** Подпись автора сообщения: ушедший сотрудник и анонимный соискатель без имени. */
export function authorName(a: Author): string {
  if (a.former) return t.chat.formerStaff
  if (a.anonymous || !a.name) return a.side === 'scientist' ? t.chat.anonymous : t.chat.formerStaff
  return a.name
}

/** Состояние отклика или приглашения на вакансию словами чата. */
export function statusText(s: ChatStatus): string {
  const labels: Record<string, string> = s.source === 'offer' ? t.chat.statuses.offer : t.chat.statuses.application
  return labels[s.value] ?? s.value
}

/**
 * Строка о последнем сообщении в списке: «Вы: …», у коллеги по своей стороне «Имя: …», у собеседника просто текст,
 * у системной строки название события, у удалённого «Сообщение удалено».
 */
export function previewText(m: LastMessage | null, mySide: string): string {
  if (!m) return t.chat.previewEmpty
  if (m.kind === 'system') {
    // Блокировку глазами того, кто смотрит: соискателю «Вы заблокировали…», организации «Соискатель ограничил…»
    const own = mySide === 'scientist' ? 'scientist' : 'org'
    if (m.event_type === 'chat.blocked') return t.chat.line.blocked[own]
    if (m.event_type === 'chat.unblocked') return t.chat.line.unblocked[own]
    return (t.chat.events as Record<string, string>)[m.event_type ?? ''] ?? t.chat.eventFallback
  }
  if (m.deleted) return t.chat.previewDeleted
  const text = m.text.replace(/\s+/g, ' ').trim()
  if (m.author?.is_me) return t.chat.previewMine(text)
  if (m.author && m.author.side === mySide) return t.chat.previewAuthor(authorName(m.author), text)
  return text
}

/** Число непрочитанных на значке: больше 99 не помещается. */
export const badgeCount = (n: number) => (n > 99 ? '99+' : String(n))

const timeFormat = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' })
const dayFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short' })
const fullFormat = new Intl.DateTimeFormat('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' })

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`

/** Время в списке по часам человека: сегодня «14:20», вчера «вчера», в этом году «12 окт.», раньше «12.10.2025». */
export function chatTime(iso: string, now = new Date()): string {
  const d = new Date(iso)
  if (dayKey(d) === dayKey(now)) return timeFormat.format(d)
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (dayKey(d) === dayKey(yesterday)) return t.chat.yesterday
  if (d.getFullYear() === now.getFullYear()) return dayFormat.format(d)
  return fullFormat.format(d)
}

import { t } from '../../i18n'
import type { Offer, OfferStatus } from './api'

export const statusLabel = (s: string) => (t.offers.statuses as Record<string, string>)[s] ?? s

/** Ждущее ответа и положительное отмечаются акцентом, остальное нейтрально. */
export const statusTone = (s: OfferStatus): 'neutral' | 'accent' => (s === 'pending' || s === 'interested' ? 'accent' : 'neutral')

/** Принимает ли вакансия отклики: опубликована и срок не вышел (по сроку из приглашения, по дням). */
export function vacancyOpen(o: Offer, now = new Date()): 'open' | 'closed' | 'deadline' {
  if (o.vacancy.status !== 'published') return 'closed'
  const deadline = o.vacancy.deadline
  if (deadline) {
    const today = now.toLocaleDateString('sv-SE', { timeZone: 'Europe/Moscow' })
    if (deadline < today) return 'deadline'
  }
  return 'open'
}

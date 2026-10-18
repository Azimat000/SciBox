import { oldInvitationKinds, type OldInvitation, type OldInvitationKind, type OldInvitationStatus } from './oldInvitation'
import type { Message, Side } from './api'

// Системные строки переписки (CHAT.md «Системные строки», contracts/chat/messages.json): что в них лежит и в каком
// состоянии приглашение. Снимок старого приглашения внутри отклика (до D-256) в строке «создано» не меняется, а ответ,
// отмена и принятое время лежат следующими строками: итоговое состояние считается по ним.

type Data = Record<string, unknown>

const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const strOrNull = (v: unknown): string | null => (typeof v === 'string' && v !== '' ? v : null)

const isKind = (v: unknown): v is OldInvitationKind => (oldInvitationKinds as readonly unknown[]).includes(v)

/** Приглашение из данных строки; нет нужных полей — null (строка покажется без карточки). */
export function invitationOf(data: Data): OldInvitation | null {
  const raw = data.invitation
  if (typeof raw !== 'object' || raw === null) return null
  const v = raw as Record<string, unknown>
  if (typeof v.id !== 'string' || !isKind(v.kind)) return null
  const answer = typeof v.answer === 'object' && v.answer !== null ? (v.answer as Record<string, unknown>) : null
  return {
    id: v.id,
    kind: v.kind,
    status: (str(v.status) || 'pending') as OldInvitationStatus,
    message: str(v.message),
    starts_at: strOrNull(v.starts_at),
    place_kind: v.place_kind === 'online' || v.place_kind === 'onsite' ? v.place_kind : '',
    place: str(v.place),
    contact_name: str(v.contact_name),
    contact_email: str(v.contact_email),
    contact_phone: str(v.contact_phone),
    answer: answer ? { proposed_at: strOrNull(answer.proposed_at), note: str(answer.note), contact: str(answer.contact), time: str(answer.time) } : null,
  }
}

/** Старое приглашение таким, каким оно стало к концу ленты: ответ, принятое время и отмена лежат следующими строками. */
export function effectiveInvitation(items: readonly Message[], created: Message): OldInvitation | null {
  const base = created.event ? invitationOf(created.event.data) : null
  if (!base) return null
  const inv: OldInvitation = { ...base }
  for (const m of items) {
    if (m.seq <= created.seq || !m.event) continue
    const d = m.event.data
    if (str(d.invitation_id) !== inv.id) continue
    if (m.event.type === 'invitation.answered') {
      const answer = str(d.answer)
      inv.status = answer === 'proposed' ? 'proposed' : answer === 'answered' ? 'answered' : 'confirmed'
      inv.answer = { proposed_at: strOrNull(d.proposed_at), note: str(d.note), contact: str(d.contact), time: str(d.time) }
    } else if (m.event.type === 'invitation.proposal_accepted') {
      inv.status = 'confirmed'
      inv.starts_at = strOrNull(d.starts_at) ?? inv.starts_at
    } else if (m.event.type === 'invitation.cancelled') {
      inv.status = 'cancelled'
    }
  }
  return inv
}

export type OfferStatus = 'pending' | 'interested' | 'declined' | 'cancelled'

export type OfferView = { id: string; message: string; status: OfferStatus; canAnswer: boolean; note: string }

const OFFER_STATUSES: readonly string[] = ['pending', 'interested', 'declined', 'cancelled']

/** Приглашение на вакансию к концу ленты: ответить «Интересно» или «Не сейчас» можно один раз, пока оно ждёт. */
export function effectiveOffer(items: readonly Message[], created: Message, side: Side, canWrite: boolean): OfferView | null {
  if (!created.event) return null
  const d = created.event.data
  const id = str(d.offer_id)
  if (!id) return null
  const start = str(d.status)
  const view: OfferView = { id, message: str(d.message), status: (OFFER_STATUSES.includes(start) ? start : 'pending') as OfferStatus, canAnswer: false, note: '' }
  for (const m of items) {
    if (m.seq <= created.seq || !m.event || str(m.event.data.offer_id) !== id) continue
    if (m.event.type === 'offer.answered') {
      const a = str(m.event.data.answer)
      view.status = a === 'declined' ? 'declined' : 'interested'
      view.note = str(m.event.data.note)
    } else if (m.event.type === 'offer.cancelled') {
      view.status = 'cancelled'
    }
  }
  view.canAnswer = canWrite && side === 'scientist' && view.status === 'pending'
  return view
}

/** Текст, который длиннее этого, в ленте сворачивается (сопроводительное письмо, записка). */
export const COLLAPSE_AT = 280

/** Данные строки как есть, для показа текстовых полей. */
export const field = (m: Message, name: string): string => (m.event ? str(m.event.data[name]) : '')
export const momentField = (m: Message, name: string): string | null => (m.event ? strOrNull(m.event.data[name]) : null)

import { invitationKinds, type Invitation, type InvitationKind, type InvitationStatus } from '../applications/api'
import type { Message, Side } from './api'

// Системные строки переписки (CHAT.md «Системные строки», contracts/chat/messages.json): что в них лежит и в каком
// состоянии приглашение сейчас. Снимок приглашения в строке «создано» не меняется, а ответ, отмена и принятое
// время приходят следующими строками: состояние считается по ним, чтобы кнопки пропадали сразу после ответа.

type Data = Record<string, unknown>

const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const strOrNull = (v: unknown): string | null => (typeof v === 'string' && v !== '' ? v : null)

const isKind = (v: unknown): v is InvitationKind => (invitationKinds as readonly unknown[]).includes(v)

/** Приглашение из данных строки; нет нужных полей — null (строка покажется без карточки). */
export function invitationOf(data: Data): Invitation | null {
  const raw = data.invitation
  if (typeof raw !== 'object' || raw === null) return null
  const v = raw as Record<string, unknown>
  if (typeof v.id !== 'string' || !isKind(v.kind)) return null
  const answer = typeof v.answer === 'object' && v.answer !== null ? (v.answer as Record<string, unknown>) : null
  return {
    id: v.id,
    kind: v.kind,
    status: (str(v.status) || 'pending') as InvitationStatus,
    message: str(v.message),
    starts_at: strOrNull(v.starts_at),
    place_kind: v.place_kind === 'online' || v.place_kind === 'onsite' ? v.place_kind : '',
    place: str(v.place),
    contact_name: str(v.contact_name),
    contact_email: str(v.contact_email),
    contact_phone: str(v.contact_phone),
    answer: answer
      ? { proposed_at: strOrNull(answer.proposed_at), note: str(answer.note), contact: str(answer.contact), time: str(answer.time), answered_at: str(answer.answered_at) }
      : null,
    created_at: str(v.created_at),
    can_answer: v.can_answer === true,
    can_cancel: v.can_cancel === true,
    can_accept_proposal: v.can_accept_proposal === true,
  }
}

const OPEN_FOR_CANCEL: readonly InvitationStatus[] = ['pending', 'proposed', 'confirmed']

/**
 * Приглашение таким, каким оно стало к концу ленты, с правами смотрящего: ответить может только соискатель и только
 * на ждущее ответа; принять время и отменить может только организация; в закрытой переписке кнопок нет.
 */
export function effectiveInvitation(items: readonly Message[], created: Message, side: Side, canWrite: boolean): Invitation | null {
  const base = created.event ? invitationOf(created.event.data) : null
  if (!base) return null
  const inv: Invitation = { ...base }
  for (const m of items) {
    if (m.seq <= created.seq || !m.event) continue
    const d = m.event.data
    if (str(d.invitation_id) !== inv.id) continue
    if (m.event.type === 'invitation.answered') {
      const answer = str(d.answer)
      inv.status = answer === 'proposed' ? 'proposed' : answer === 'answered' ? 'answered' : 'confirmed'
      inv.answer = { proposed_at: strOrNull(d.proposed_at), note: str(d.note), contact: str(d.contact), time: str(d.time), answered_at: m.created_at }
    } else if (m.event.type === 'invitation.proposal_accepted') {
      inv.status = 'confirmed'
      inv.starts_at = strOrNull(d.starts_at) ?? inv.starts_at
    } else if (m.event.type === 'invitation.cancelled') {
      inv.status = 'cancelled'
    }
  }
  inv.can_answer = canWrite && side === 'scientist' && inv.status === 'pending' && (inv.kind === 'interview' || inv.kind === 'request_contacts')
  inv.can_accept_proposal = canWrite && side === 'org' && inv.kind === 'interview' && inv.status === 'proposed'
  inv.can_cancel = canWrite && side === 'org' && OPEN_FOR_CANCEL.includes(inv.status)
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

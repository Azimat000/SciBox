import { effectiveInvitation, invitationOf } from '../events'
import type { ChatServer } from './server'
import type { MockChat, MockReply } from './types'

// Действия над откликами и приглашениями, которые из окна переписки и со страниц откликов должны оставлять след в
// переписке (срез 35, CHAT.md «Системные строки»). Настоящий сервер чата (срез 38) делает это сам; пока подставной сервер:
//  - для демо-записей (их нет на настоящем сервере) отвечает сам и пишет строки;
//  - для настоящих записей даёт сделать запрос настоящему серверу и после успеха дописывает строки (mirror).

type Json = Record<string, unknown>

export type Action =
  | { kind: 'invite'; appId: string }
  | { kind: 'answer'; appId: string; invId: string }
  | { kind: 'accept'; appId: string; invId: string }
  | { kind: 'cancel'; appId: string; invId: string }
  | { kind: 'decide'; appId: string }
  | { kind: 'withdraw'; appId: string }
  | { kind: 'offerAnswer'; offerId: string }
  | { kind: 'offerCancel'; offerId: string }

const seg = '([^/]+)'

export function parseAction(method: string, path: string): Action | null {
  let m: RegExpExecArray | null
  if (method === 'POST') {
    if ((m = new RegExp(`^/api/applications/${seg}/invitations$`).exec(path))) return { kind: 'invite', appId: m[1] }
    if ((m = new RegExp(`^/api/applications/${seg}/invitations/${seg}/answer$`).exec(path))) return { kind: 'answer', appId: m[1], invId: m[2] }
    if ((m = new RegExp(`^/api/applications/${seg}/invitations/${seg}/accept-proposal$`).exec(path))) return { kind: 'accept', appId: m[1], invId: m[2] }
    if ((m = new RegExp(`^/api/applications/${seg}/status$`).exec(path))) return { kind: 'decide', appId: m[1] }
    if ((m = new RegExp(`^/api/applications/${seg}/withdraw$`).exec(path))) return { kind: 'withdraw', appId: m[1] }
    if ((m = new RegExp(`^/api/offers/${seg}/answer$`).exec(path))) return { kind: 'offerAnswer', offerId: m[1] }
    if ((m = new RegExp(`^/api/offers/${seg}/cancel$`).exec(path))) return { kind: 'offerCancel', offerId: m[1] }
  }
  if (method === 'DELETE' && (m = new RegExp(`^/api/applications/${seg}/invitations/${seg}$`).exec(path))) return { kind: 'cancel', appId: m[1], invId: m[2] }
  return null
}

const idOf = (v: string) => decodeURIComponent(v)

function targets(server: ChatServer, a: Action): MockChat[] {
  if ('offerId' in a) {
    const id = idOf(a.offerId)
    return server.state.chats.filter((c) => c.offer_id === id)
  }
  const id = idOf(a.appId)
  return server.state.chats.filter((c) => c.application_id === id)
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')

const createdLine = (c: MockChat, invId: string) =>
  c.messages.find((m) => m.event?.type === 'invitation.created' && m.event.data.invitation && invitationOf(m.event.data)?.id === invId)

function conflict(): MockReply {
  return { status: 409, body: { error: { code: 'invalid_state', message: 'Приглашение уже изменилось, обновите страницу' } } }
}

/**
 * Демо-запись: подставной сервер проверяет, что действие возможно, и записывает его. null — записи с таким номером в
 * подставном сервере нет или она настоящая (ответит настоящий сервер, потом mirror допишет строки).
 */
export function tryDemo(server: ChatServer, a: Action, body: unknown): MockReply | null {
  const demo = targets(server, a).filter((c) => c.link === undefined)
  if (demo.length === 0) return null
  if (a.kind === 'answer' || a.kind === 'accept' || a.kind === 'cancel') {
    const invId = idOf(a.invId)
    for (const c of demo) {
      const created = createdLine(c, invId)
      if (!created) continue
      const inv = effectiveInvitation(c.messages, created, c.side === 'org' ? 'org' : 'scientist', true)
      if (!inv) return conflict()
      const act = str((body as Json | null)?.action)
      const ok =
        a.kind === 'accept'
          ? inv.status === 'proposed'
          : a.kind === 'cancel'
            ? inv.status === 'pending' || inv.status === 'proposed' || inv.status === 'confirmed'
            : inv.status === 'pending' && ((inv.kind === 'interview' && (act === 'confirm' || act === 'propose')) || (inv.kind === 'request_contacts' && act === 'reply'))
      if (!ok) return conflict()
      break
    }
  }
  if (a.kind === 'offerAnswer') {
    const act = str((body as Json | null)?.action)
    if (act !== 'interested' && act !== 'declined') return { status: 422, body: { error: { code: 'validation_failed', message: 'Проверьте поля', fields: { action: 'Выберите ответ' } } } }
    if (demo.every((c) => c.status.source !== 'offer' || c.status.value !== 'pending')) return conflict()
  }
  record(server, a, body, undefined)
  return { status: 204 }
}

/** После успешного ответа настоящего сервера: строки в переписки, которые подставной сервер ведёт по этим записям. */
export function mirror(server: ChatServer, method: string, path: string, requestBody: unknown, responseBody: unknown) {
  const a = parseAction(method, path)
  if (a) record(server, a, requestBody, responseBody)
}

function record(server: ChatServer, a: Action, body: unknown, response: unknown) {
  const b = (body ?? {}) as Json
  for (const c of targets(server, a)) {
    switch (a.kind) {
      case 'invite': {
        const raw = (response as { invitation?: Json } | undefined)?.invitation
        if (!raw) break
        const kind = str(raw.kind)
        server.line(c, 'invitation.created', { application_id: c.application_id, invitation: { ...raw, can_answer: c.side === 'scientist' && kind !== 'contacts', can_cancel: c.side === 'org', can_accept_proposal: false } }, 'org')
        server.patchState(c, { status: { source: 'application', value: 'invited' }, needs_answer: c.side === 'scientist' && kind !== 'contacts' })
        break
      }
      case 'answer': {
        const invId = idOf(a.invId)
        const created = createdLine(c, invId)
        const kind = created ? (invitationOf(created.event?.data ?? {})?.kind ?? '') : str(b.action) === 'reply' ? 'request_contacts' : 'interview'
        const action = str(b.action)
        const answer = action === 'propose' ? 'proposed' : action === 'reply' ? 'answered' : 'confirmed'
        server.line(
          c,
          'invitation.answered',
          { application_id: c.application_id, invitation_id: invId, kind, answer, proposed_at: answer === 'proposed' ? str(b.proposed_at) || null : null, note: str(b.note), contact: str(b.contact), time: str(b.time) },
          'scientist',
        )
        server.patchState(c, { needs_answer: c.side === 'org' && answer === 'proposed' })
        break
      }
      case 'accept': {
        const invId = idOf(a.invId)
        const answered = [...c.messages].reverse().find((m) => m.event?.type === 'invitation.answered' && m.event.data.invitation_id === invId)
        const created = createdLine(c, invId)
        const startsAt = str(answered?.event?.data.proposed_at) || (created ? (invitationOf(created.event?.data ?? {})?.starts_at ?? '') : '')
        server.line(c, 'invitation.proposal_accepted', { application_id: c.application_id, invitation_id: invId, starts_at: startsAt || null }, 'org')
        server.patchState(c, { needs_answer: false })
        break
      }
      case 'cancel': {
        const invId = idOf(a.invId)
        const created = createdLine(c, invId)
        const kind = created ? (invitationOf(created.event?.data ?? {})?.kind ?? '') : ''
        server.line(c, 'invitation.cancelled', { application_id: c.application_id, invitation_id: invId, kind }, 'org')
        server.patchState(c, { needs_answer: false })
        break
      }
      case 'decide': {
        const decision = str(b.status) === 'accepted' ? 'accepted' : 'rejected'
        server.line(c, 'application.decided', { application_id: c.application_id, decision, note: str(b.note) }, 'org')
        server.patchState(c, decision === 'accepted' ? { status: { source: 'application', value: 'accepted' }, needs_answer: false } : { status: { source: 'application', value: 'rejected' }, needs_answer: false, can_write: false, closed_reason: 'rejected' })
        break
      }
      case 'withdraw':
        server.line(c, 'application.withdrawn', { application_id: c.application_id }, 'scientist')
        server.patchState(c, { status: { source: 'application', value: 'withdrawn' }, needs_answer: false, can_write: false, closed_reason: 'withdrawn' })
        break
      case 'offerAnswer': {
        const answer = str(b.action) === 'declined' ? 'declined' : 'interested'
        server.line(c, 'offer.answered', { offer_id: c.offer_id, answer, note: str(b.note) }, 'scientist')
        server.patchState(c, answer === 'declined' ? { status: { source: 'offer', value: 'declined' }, needs_answer: false, can_write: false, closed_reason: 'offer_declined' } : { status: { source: 'offer', value: 'interested' }, needs_answer: false })
        break
      }
      case 'offerCancel':
        server.line(c, 'offer.cancelled', { offer_id: c.offer_id }, 'org')
        server.patchState(c, { status: { source: 'offer', value: 'cancelled' }, needs_answer: false, can_write: false, closed_reason: 'offer_cancelled' })
        break
    }
  }
}

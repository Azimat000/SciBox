import { describe, expect, it, vi } from 'vitest'
import { demoState } from './data'
import { mirror, parseAction, tryDemo } from './actions'
import { createChatServer } from './server'
import type { MockChat } from './types'

// Ответы на приглашения и решения по откликам оставляют строки в переписках подставного сервера (срез 35).

const NOW = new Date('2026-10-14T12:00:00Z')
const server = () => createChatServer({ state: demoState(NOW), now: () => NOW, later: () => {} })
const chat = (s: ReturnType<typeof server>, n: number) => s.state.chats.find((c) => c.id === `c0000000-0000-4000-8000-${String(n).padStart(12, '0')}`) as MockChat
const last = (c: MockChat) => c.messages[c.messages.length - 1]

const APP1 = 'b0000000-0000-4000-8000-000000000001'
const INV1 = 'f0000000-0000-4000-8000-000000000001'
const APP8 = 'b0000000-0000-4000-8000-000000000008'
const INV8 = 'f0000000-0000-4000-8000-000000000008'

describe('parseAction', () => {
  it('knows the addresses that change an application or an invitation', () => {
    expect(parseAction('POST', '/api/applications/a1/invitations')).toEqual({ kind: 'invite', appId: 'a1' })
    expect(parseAction('POST', '/api/applications/a1/invitations/i1/answer')).toEqual({ kind: 'answer', appId: 'a1', invId: 'i1' })
    expect(parseAction('POST', '/api/applications/a1/invitations/i1/accept-proposal')).toEqual({ kind: 'accept', appId: 'a1', invId: 'i1' })
    expect(parseAction('DELETE', '/api/applications/a1/invitations/i1')).toEqual({ kind: 'cancel', appId: 'a1', invId: 'i1' })
    expect(parseAction('POST', '/api/applications/a1/status')).toEqual({ kind: 'decide', appId: 'a1' })
    expect(parseAction('POST', '/api/applications/a1/withdraw')).toEqual({ kind: 'withdraw', appId: 'a1' })
    expect(parseAction('POST', '/api/offers/o1/answer')).toEqual({ kind: 'offerAnswer', offerId: 'o1' })
    expect(parseAction('POST', '/api/offers/o1/cancel')).toEqual({ kind: 'offerCancel', offerId: 'o1' })
  })
  it('ignores everything else', () => {
    expect(parseAction('GET', '/api/applications/a1/invitations')).toBeNull()
    expect(parseAction('POST', '/api/applications/a1')).toBeNull()
    expect(parseAction('PATCH', '/api/offers/o1/answer')).toBeNull()
  })
})

describe('tryDemo', () => {
  it('records the answer in the scientist conversation and closes the question', () => {
    const s = server()
    const c = chat(s, 1)
    const reply = tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'confirm' })
    expect(reply).toEqual({ status: 204 })
    expect(last(c).event).toMatchObject({ type: 'invitation.answered', data: { invitation_id: INV1, kind: 'interview', answer: 'confirmed' } })
    expect(c.needs_answer).toBe(false)
    expect(last(c).from).toBe('scientist')
    expect(c.read_mine).toBe(last(c).seq)
  })

  it('records a proposed time with the note', () => {
    const s = server()
    expect(tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'propose', proposed_at: '2026-10-23T15:30:00+03:00', note: 'Позже' })).toEqual({ status: 204 })
    expect(last(chat(s, 1)).event?.data).toMatchObject({ answer: 'proposed', proposed_at: '2026-10-23T15:30:00+03:00', note: 'Позже' })
  })

  it('refuses a second answer and answers of a wrong kind', () => {
    const s = server()
    expect(tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'reply' })?.status).toBe(409)
    expect(tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'confirm' })).toEqual({ status: 204 })
    expect(tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'confirm' })?.status).toBe(409)
  })

  it('lets the organisation accept a proposed time and records the start', () => {
    const s = server()
    const c = chat(s, 8)
    const proposed = c.messages.find((m) => m.event?.type === 'invitation.answered')!.event!.data.proposed_at
    expect(tryDemo(s, { kind: 'accept', appId: APP8, invId: INV8 }, {})).toEqual({ status: 204 })
    expect(last(c).event).toMatchObject({ type: 'invitation.proposal_accepted', data: { invitation_id: INV8, starts_at: proposed } })
    expect(c.needs_answer).toBe(false)
    // повторно принять нельзя
    expect(tryDemo(s, { kind: 'accept', appId: APP8, invId: INV8 }, {})?.status).toBe(409)
  })

  it('lets the organisation cancel an invitation once', () => {
    const s = server()
    const c = chat(s, 8)
    expect(tryDemo(s, { kind: 'cancel', appId: APP8, invId: INV8 }, {})).toEqual({ status: 204 })
    expect(last(c).event).toMatchObject({ type: 'invitation.cancelled', data: { invitation_id: INV8, kind: 'interview' } })
    expect(tryDemo(s, { kind: 'cancel', appId: APP8, invId: INV8 }, {})?.status).toBe(409)
  })

  it('answers an offer: interested keeps the conversation open, declined closes it', () => {
    const s = server()
    const c = chat(s, 3)
    expect(tryDemo(s, { kind: 'offerAnswer', offerId: 'd0000000-0000-4000-8000-000000000003' }, { action: 'interested', note: 'Да' })).toEqual({ status: 204 })
    expect(last(c).event).toMatchObject({ type: 'offer.answered', data: { answer: 'interested', note: 'Да' } })
    expect(c).toMatchObject({ status: { source: 'offer', value: 'interested' }, can_write: true, needs_answer: false })
    expect(tryDemo(s, { kind: 'offerAnswer', offerId: 'd0000000-0000-4000-8000-000000000003' }, { action: 'declined' })?.status).toBe(409)

    const s2 = server()
    expect(tryDemo(s2, { kind: 'offerAnswer', offerId: 'd0000000-0000-4000-8000-000000000003' }, { action: 'declined' })).toEqual({ status: 204 })
    expect(chat(s2, 3)).toMatchObject({ status: { value: 'declined' }, can_write: false, closed_reason: 'offer_declined' })
  })

  it('rejects an offer answer with a wrong action', () => {
    const reply = tryDemo(server(), { kind: 'offerAnswer', offerId: 'd0000000-0000-4000-8000-000000000003' }, { action: 'maybe' })
    expect(reply?.status).toBe(422)
  })

  it('records a decision and a withdrawal and closes the conversation when it ends', () => {
    const s = server()
    expect(tryDemo(s, { kind: 'decide', appId: APP8 }, { status: 'rejected', note: 'Не подошло' })).toEqual({ status: 204 })
    expect(last(chat(s, 8)).event).toMatchObject({ type: 'application.decided', data: { decision: 'rejected', note: 'Не подошло' } })
    expect(chat(s, 8)).toMatchObject({ can_write: false, closed_reason: 'rejected', status: { value: 'rejected' } })
    const s2 = server()
    tryDemo(s2, { kind: 'decide', appId: APP8 }, { status: 'accepted', note: '' })
    expect(chat(s2, 8)).toMatchObject({ can_write: true, status: { value: 'accepted' } })
    const s3 = server()
    tryDemo(s3, { kind: 'withdraw', appId: APP1 }, {})
    expect(chat(s3, 1)).toMatchObject({ can_write: false, closed_reason: 'withdrawn' })
    expect(last(chat(s3, 1)).event?.type).toBe('application.withdrawn')
  })

  it('records an offer cancel', () => {
    const s = server()
    expect(tryDemo(s, { kind: 'offerCancel', offerId: 'd0000000-0000-4000-8000-000000000007' }, {})).toEqual({ status: 204 })
    expect(chat(s, 7)).toMatchObject({ can_write: false, closed_reason: 'offer_cancelled' })
  })

  it('records a new invitation as a line with the buttons for the scientist', () => {
    const s = server()
    const c = chat(s, 2)
    c.application_id = 'b0000000-0000-4000-8000-000000000002'
    const inv = { id: 'f-new', kind: 'request_contacts', status: 'pending', message: 'Оставьте контакты', created_at: NOW.toISOString() }
    mirror(s, 'POST', '/api/applications/b0000000-0000-4000-8000-000000000002/invitations', {}, { invitation: inv })
    expect(last(c).event).toMatchObject({ type: 'invitation.created', data: { invitation: { id: 'f-new', can_answer: true, can_cancel: false } } })
    expect(c.needs_answer).toBe(true)
    // ответ без приглашения в ответе сервера ничего не пишет
    const before = c.messages.length
    mirror(s, 'POST', '/api/applications/b0000000-0000-4000-8000-000000000002/invitations', {}, {})
    expect(c.messages.length).toBe(before)
  })

  it('returns null when no demo conversation has this record, so the real server answers', () => {
    expect(tryDemo(server(), { kind: 'answer', appId: 'нет-такого', invId: 'x' }, { action: 'confirm' })).toBeNull()
    expect(tryDemo(server(), { kind: 'offerAnswer', offerId: 'нет-такого' }, { action: 'interested' })).toBeNull()
  })

  it('does not take over a record bound to a real application', () => {
    const s = server()
    const c = chat(s, 1)
    c.link = 'sci-vac:real'
    expect(tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'confirm' })).toBeNull()
    // после настоящего ответа строки дописываются
    mirror(s, 'POST', `/api/applications/${APP1}/invitations/${INV1}/answer`, { action: 'propose', proposed_at: '2026-10-23T15:30:00+03:00' }, undefined)
    expect(last(c).event).toMatchObject({ type: 'invitation.answered', data: { answer: 'proposed' } })
    mirror(s, 'GET', `/api/applications/${APP1}`, undefined, {})
  })

  it('writes the line for an invitation the demo conversation does not hold (real data)', () => {
    const s = server()
    const c = chat(s, 2)
    c.application_id = 'real-app'
    c.link = 'sci-vac:x'
    mirror(s, 'POST', '/api/applications/real-app/invitations/real-inv/answer', { action: 'reply', contact: '+7 921', time: 'вечером' }, undefined)
    expect(last(c).event).toMatchObject({ type: 'invitation.answered', data: { invitation_id: 'real-inv', kind: 'request_contacts', answer: 'answered', contact: '+7 921', time: 'вечером' } })
    mirror(s, 'DELETE', '/api/applications/real-app/invitations/real-inv', undefined, undefined)
    expect(last(c).event?.type).toBe('invitation.cancelled')
    mirror(s, 'POST', '/api/applications/real-app/invitations/real-inv/accept-proposal', undefined, undefined)
    expect(last(c).event?.type).toBe('invitation.proposal_accepted')
  })

  it('emits the events of the stream for every line', () => {
    const s = server()
    const spy = vi.spyOn(s, 'line')
    tryDemo(s, { kind: 'answer', appId: APP1, invId: INV1 }, { action: 'confirm' })
    expect(spy).toHaveBeenCalledTimes(1)
  })
})

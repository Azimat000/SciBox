import { describe, expect, it } from 'vitest'
import type { Message } from './api'
import { effectiveInvitation, effectiveOffer, field, invitationOf, momentField } from './events'

const line = (seq: number, type: string, data: Record<string, unknown>): Message => ({
  id: `l${seq}`,
  seq,
  kind: 'system',
  author: null,
  text: '',
  event: { type, data },
  created_at: '2026-10-14T10:00:00Z',
  edited_at: null,
  deleted: false,
  editable_until: null,
  client_id: null,
})

const invitation = (over: Record<string, unknown> = {}) => ({
  id: 'inv1',
  kind: 'interview',
  status: 'pending',
  message: 'Приглашаем',
  starts_at: '2026-10-22T08:00:00Z',
  place_kind: 'online',
  place: 'https://telemost.yandex.ru/j/1',
  contact_name: '',
  contact_email: '',
  contact_phone: '',
  answer: null,
  created_at: '2026-10-14T09:00:00Z',
  can_answer: true,
  can_cancel: false,
  can_accept_proposal: false,
  ...over,
})

const created = (over: Record<string, unknown> = {}) => line(1, 'invitation.created', { application_id: 'a1', invitation: invitation(over) })

// Приглашения внутри отклика были до D-256: старые строки читаются как история, без кнопок.
describe('invitationOf', () => {
  it('reads a full invitation', () => {
    const inv = invitationOf(created().event!.data)!
    expect(inv).toMatchObject({ id: 'inv1', kind: 'interview', status: 'pending', place_kind: 'online' })
    expect(inv.answer).toBeNull()
  })

  it('reads the answer and fills defaults for missing fields', () => {
    const inv = invitationOf({ invitation: { id: 'x', kind: 'request_contacts', answer: { contact: '+7 921', note: 'n' } } })!
    expect(inv.status).toBe('pending')
    expect(inv.place_kind).toBe('')
    expect(inv.starts_at).toBeNull()
    expect(inv.answer).toEqual({ proposed_at: null, note: 'n', contact: '+7 921', time: '' })
  })

  it('returns null without an invitation or with an unknown kind', () => {
    expect(invitationOf({})).toBeNull()
    expect(invitationOf({ invitation: 'x' })).toBeNull()
    expect(invitationOf({ invitation: { id: 1, kind: 'interview' } })).toBeNull()
    expect(invitationOf({ invitation: { id: 'x', kind: 'party' } })).toBeNull()
  })
})

describe('effectiveInvitation', () => {
  const answered = (answer: string, extra: Record<string, unknown> = {}) => line(2, 'invitation.answered', { invitation_id: 'inv1', kind: 'interview', answer, proposed_at: null, note: '', contact: '', time: '', ...extra })

  it('moves to confirmed after the scientist confirmed and takes the answer', () => {
    const c = created()
    expect(effectiveInvitation([c], c)!.status).toBe('pending')
    const inv = effectiveInvitation([c, answered('confirmed', { note: 'ок' })], c)!
    expect(inv.status).toBe('confirmed')
    expect(inv.answer?.note).toBe('ок')
  })

  it('takes a proposed time, then the accepted one becomes the start', () => {
    const c = created()
    const proposed = answered('proposed', { proposed_at: '2026-10-23T09:00:00Z' })
    const asked = effectiveInvitation([c, proposed], c)!
    expect(asked.status).toBe('proposed')
    expect(asked.answer?.proposed_at).toBe('2026-10-23T09:00:00Z')
    const accepted = line(3, 'invitation.proposal_accepted', { invitation_id: 'inv1', starts_at: '2026-10-23T09:00:00Z' })
    expect(effectiveInvitation([c, proposed, accepted], c)).toMatchObject({ status: 'confirmed', starts_at: '2026-10-23T09:00:00Z' })
    // без новой даты остаётся прежняя
    const bare = line(3, 'invitation.proposal_accepted', { invitation_id: 'inv1' })
    expect(effectiveInvitation([c, bare], c)!.starts_at).toBe('2026-10-22T08:00:00Z')
  })

  it('maps the contacts answer and cancel', () => {
    const c = created({ kind: 'request_contacts' })
    expect(effectiveInvitation([c, answered('answered')], c)!.status).toBe('answered')
    const cancelled = line(3, 'invitation.cancelled', { invitation_id: 'inv1', kind: 'request_contacts' })
    expect(effectiveInvitation([c, cancelled], c)!.status).toBe('cancelled')
  })

  it('ignores lines about another invitation, earlier lines and lines without an event', () => {
    const c = created()
    const other = line(2, 'invitation.cancelled', { invitation_id: 'other' })
    const earlier = line(0, 'invitation.cancelled', { invitation_id: 'inv1' })
    const plain: Message = { ...line(3, 'x', {}), event: null }
    expect(effectiveInvitation([earlier, c, other, plain], c)!.status).toBe('pending')
  })

  it('returns null when the line carries no invitation', () => {
    const bare = line(1, 'invitation.created', {})
    expect(effectiveInvitation([bare], bare)).toBeNull()
    const noEvent: Message = { ...bare, event: null }
    expect(effectiveInvitation([noEvent], noEvent)).toBeNull()
  })
})

describe('effectiveOffer', () => {
  const offer = line(1, 'offer.created', { offer_id: 'o1', message: 'Приглашаем', status: 'pending', can_answer: true })

  it('lets the scientist answer a waiting offer', () => {
    expect(effectiveOffer([offer], offer, 'scientist', true)).toMatchObject({ id: 'o1', message: 'Приглашаем', status: 'pending', canAnswer: true })
    expect(effectiveOffer([offer], offer, 'org', true)!.canAnswer).toBe(false)
    expect(effectiveOffer([offer], offer, 'scientist', false)!.canAnswer).toBe(false)
  })

  it('follows the answer and the cancel', () => {
    const interested = line(2, 'offer.answered', { offer_id: 'o1', answer: 'interested', note: 'Хорошо' })
    expect(effectiveOffer([offer, interested], offer, 'scientist', true)).toMatchObject({ status: 'interested', canAnswer: false, note: 'Хорошо' })
    const declined = line(2, 'offer.answered', { offer_id: 'o1', answer: 'declined', note: '' })
    expect(effectiveOffer([offer, declined], offer, 'scientist', true)!.status).toBe('declined')
    const cancelled = line(2, 'offer.cancelled', { offer_id: 'o1' })
    expect(effectiveOffer([offer, cancelled], offer, 'scientist', true)).toMatchObject({ status: 'cancelled', canAnswer: false })
  })

  it('takes an unknown start status as waiting and skips other offers', () => {
    const odd = line(1, 'offer.created', { offer_id: 'o1', message: '', status: 'weird' })
    expect(effectiveOffer([odd], odd, 'scientist', true)!.status).toBe('pending')
    const other = line(2, 'offer.cancelled', { offer_id: 'o2' })
    expect(effectiveOffer([offer, other], offer, 'scientist', true)!.status).toBe('pending')
  })

  it('returns null without an offer id or an event', () => {
    const bare = line(1, 'offer.created', {})
    expect(effectiveOffer([bare], bare, 'scientist', true)).toBeNull()
    expect(effectiveOffer([{ ...bare, event: null }], { ...bare, event: null }, 'scientist', true)).toBeNull()
  })
})

describe('field helpers', () => {
  it('reads strings and moments, empty when missing', () => {
    const m = line(1, 'x', { note: 'a', n: 5, at: '2026-10-14T10:00:00Z', empty: '' })
    expect(field(m, 'note')).toBe('a')
    expect(field(m, 'n')).toBe('')
    expect(field(m, 'nope')).toBe('')
    expect(momentField(m, 'at')).toBe('2026-10-14T10:00:00Z')
    expect(momentField(m, 'empty')).toBeNull()
    expect(field({ ...m, event: null }, 'note')).toBe('')
    expect(momentField({ ...m, event: null }, 'at')).toBeNull()
  })
})

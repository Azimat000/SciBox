import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { OldInvitationCard } from './InvitationHistory'
import type { OldInvitation } from './oldInvitation'

const interview = (place: string, over: Partial<OldInvitation> = {}): OldInvitation => ({
  id: 'i1', kind: 'interview', status: 'pending', message: '', starts_at: '2026-10-15T14:00:00+03:00', place_kind: 'online', place,
  contact_name: '', contact_email: '', contact_phone: '', answer: null, ...over,
})

// Приглашения внутри отклика были до D-256: в переписке они остались историей, только для чтения.
describe('an old invitation in a chat', () => {
  it('links an honest address of the interview and has no buttons', () => {
    render(<OldInvitationCard inv={interview('https://meet.example.org/room-1')} side="scientist" />)
    expect(screen.getByRole('link', { name: 'https://meet.example.org/room-1' })).toHaveAttribute('href', 'https://meet.example.org/room-1')
    expect(screen.getByText(/15 октября 2026 г\. в 14:00 \(МСК\)/)).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it.each([
    'https://ya.ru@evil.example/login',
    'https://аpple.com/login',
    'http://127.0.0.1:8080/x',
    'javascript:alert(1)',
    'https://meet.example.org/room 1 пароль 5',
  ])('shows %s as plain text (T-117)', (place) => {
    render(<OldInvitationCard inv={interview(place)} side="org" />)
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getByText(place, { exact: false })).toBeInTheDocument()
  })

  it('shows an onsite address, contacts and every kind of answer', () => {
    const { unmount } = render(<OldInvitationCard inv={interview('Новосибирск, пр. Лаврентьева, 5', { place_kind: 'onsite', status: 'confirmed', answer: { proposed_at: null, note: 'Буду', contact: '', time: '' } })} side="org" />)
    expect(screen.getByText(/Очно: Новосибирск/)).toBeInTheDocument()
    expect(screen.getByText('Ответ кандидата')).toBeInTheDocument()
    expect(screen.getByText('Время подходит.')).toBeInTheDocument()
    expect(screen.getByText('Комментарий: Буду')).toBeInTheDocument()
    unmount()
    const proposed = { proposed_at: '2026-10-17T12:00:00Z', note: '', contact: '', time: '' }
    const second = render(<OldInvitationCard inv={interview('x', { status: 'proposed', answer: proposed })} side="scientist" />)
    expect(screen.getByText('Ваш ответ')).toBeInTheDocument()
    expect(screen.getByText(/Предложено другое время: 17 октября 2026/)).toBeInTheDocument()
    second.unmount()
    const third = render(<OldInvitationCard inv={interview('x', { status: 'confirmed', answer: proposed })} side="org" />)
    expect(screen.getByText('Вы приняли время, которое предложил кандидат.')).toBeInTheDocument()
    third.unmount()
    const fourth = render(<OldInvitationCard inv={interview('x', { status: 'confirmed', answer: proposed })} side="scientist" />)
    expect(screen.getByText('Организация приняла предложенное вами время.')).toBeInTheDocument()
    fourth.unmount()
    const contacts: OldInvitation = { ...interview(''), kind: 'contacts', status: 'shared', starts_at: null, place_kind: '', contact_name: 'Ольга', contact_email: 'hr@example.ru', contact_phone: '+7 913 000-00-00', message: 'Пишите' }
    const fifth = render(<OldInvitationCard inv={contacts} side="scientist" />)
    expect(screen.getByRole('link', { name: 'hr@example.ru' })).toHaveAttribute('href', 'mailto:hr@example.ru')
    expect(screen.getByText('+7 913 000-00-00')).toBeInTheDocument()
    expect(screen.getByText('Ольга')).toBeInTheDocument()
    expect(screen.getByText('Пишите')).toBeInTheDocument()
    fifth.unmount()
    const request: OldInvitation = { ...interview(''), kind: 'request_contacts', status: 'answered', starts_at: null, place_kind: '', answer: { proposed_at: null, note: '', contact: '+7 900', time: 'после 15:00' } }
    render(<OldInvitationCard inv={request} side="org" />)
    expect(screen.getByText('Как связаться: +7 900')).toBeInTheDocument()
    expect(screen.getByText('Когда удобно: после 15:00')).toBeInTheDocument()
  })

  it('shows no answer block when the answer is empty', () => {
    render(<OldInvitationCard inv={{ ...interview(''), kind: 'request_contacts', status: 'answered', starts_at: null, place_kind: '', answer: { proposed_at: null, note: '', contact: '', time: '' } }} side="org" />)
    expect(screen.queryByText('Ответ кандидата')).toBeNull()
  })
})

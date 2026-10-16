import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { Invitation } from './api'
import { Details } from './Invitations'

const interview = (place: string): Invitation => ({
  id: 'i1', kind: 'interview', status: 'pending', message: '', starts_at: '2026-10-15T14:00:00+03:00', place_kind: 'online', place,
  contact_name: '', contact_email: '', contact_phone: '', answer: null, created_at: '2026-10-10T10:00:00Z', can_answer: false, can_cancel: false,
  can_accept_proposal: false,
})

describe('the place of an interview (T-117)', () => {
  it('links an honest address', () => {
    render(<Details inv={interview('https://meet.example.org/room-1')} />)
    expect(screen.getByRole('link', { name: 'https://meet.example.org/room-1' })).toHaveAttribute('href', 'https://meet.example.org/room-1')
  })

  it.each([
    'https://ya.ru@evil.example/login',
    'https://аpple.com/login',
    'http://127.0.0.1:8080/x',
    'javascript:alert(1)',
    'https://meet.example.org/room 1 пароль 5',
  ])('shows %s as plain text', (place) => {
    render(<Details inv={interview(place)} />)
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getByText(place, { exact: false })).toBeInTheDocument()
  })
})

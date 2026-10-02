import { describe, expect, it } from 'vitest'
import { daysUntil, describeDeadline } from './deadline'

const now = new Date(2026, 9, 2, 15, 30) // 2 октября 2026, после обеда

describe('daysUntil', () => {
  it('counts calendar days, not 24-hour spans', () => {
    expect(daysUntil(new Date(2026, 9, 3), new Date(2026, 9, 2, 23, 59))).toBe(1)
    expect(daysUntil(new Date(2026, 9, 2), new Date(2026, 9, 2, 0, 1))).toBe(0)
    expect(daysUntil(new Date(2026, 9, 1), now)).toBe(-1)
  })
})

describe('describeDeadline', () => {
  it('marks a deadline within two weeks as urgent', () => {
    expect(describeDeadline('2026-10-09', now)).toEqual({
      date: '9 октября',
      relative: 'осталось 7 дней',
      urgent: true,
      expired: false,
    })
  })

  it('treats exactly 14 days as urgent and 15 as not', () => {
    expect(describeDeadline('2026-10-16', now)?.urgent).toBe(true)
    expect(describeDeadline('2026-10-17', now)?.urgent).toBe(false)
  })

  it('handles a far deadline with correct plural', () => {
    expect(describeDeadline('2026-11-14', now)?.relative).toBe('осталось 43 дня')
    expect(describeDeadline('2026-10-23', now)?.relative).toBe('остался 21 день')
  })

  it('handles the last day and tomorrow', () => {
    expect(describeDeadline('2026-10-02', now)).toMatchObject({ relative: 'сегодня последний день', urgent: true })
    expect(describeDeadline('2026-10-03', now)).toMatchObject({ relative: 'остался 1 день', urgent: true })
  })

  it('reports an expired deadline', () => {
    expect(describeDeadline('2026-09-29', now)).toEqual({
      date: '29 сентября',
      relative: 'срок прошёл 3 дня назад',
      urgent: false,
      expired: true,
    })
  })

  it('adds the year when it differs from the current one', () => {
    expect(describeDeadline('2027-01-15', now)?.date).toBe('15 января 2027 г.')
  })

  it('returns null for malformed or impossible dates', () => {
    expect(describeDeadline('', now)).toBeNull()
    expect(describeDeadline('14.11.2026', now)).toBeNull()
    expect(describeDeadline('2026-02-30', now)).toBeNull()
  })
})

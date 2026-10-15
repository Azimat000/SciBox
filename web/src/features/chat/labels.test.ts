import { describe, expect, it } from 'vitest'
import { authorName, badgeCount, chatTime, counterpartName, previewText, statusText } from './labels'

describe('chat labels', () => {
  it('names the counterpart, hiding an anonymous scientist', () => {
    expect(counterpartName({ kind: 'org', name: 'Нейрофотоника', anonymous: false, profile_id: null })).toBe('Нейрофотоника')
    expect(counterpartName({ kind: 'scientist', name: 'Алина Гусева', anonymous: false, profile_id: 'p' })).toBe('Алина Гусева')
    expect(counterpartName({ kind: 'scientist', name: 'Имя не должно утечь', anonymous: true, profile_id: null })).toBe('Анонимный соискатель')
    expect(counterpartName({ kind: 'scientist', name: '', anonymous: false, profile_id: null })).toBe('Анонимный соискатель')
  })

  it('signs authors, former staff and hidden names', () => {
    const a = { side: 'org' as const, name: 'Иван Петров', is_me: false, anonymous: false, former: false }
    expect(authorName(a)).toBe('Иван Петров')
    expect(authorName({ ...a, former: true })).toBe('Сотрудник организации')
    expect(authorName({ ...a, name: '' })).toBe('Сотрудник организации')
    expect(authorName({ ...a, side: 'scientist', anonymous: true })).toBe('Анонимный соискатель')
  })

  it('says the state of an application or an offer', () => {
    expect(statusText({ source: 'application', value: 'sent' })).toBe('Отклик отправлен')
    expect(statusText({ source: 'offer', value: 'declined' })).toBe('Не сейчас')
    expect(statusText({ source: 'offer', value: 'new_state' })).toBe('new_state')
  })

  it('caps the badge', () => {
    expect(badgeCount(7)).toBe('7')
    expect(badgeCount(99)).toBe('99')
    expect(badgeCount(100)).toBe('99+')
  })

  it('builds the preview line', () => {
    const base = { seq: 1, kind: 'text' as const, author: { side: 'scientist' as const, name: 'Анна', is_me: false, anonymous: false, former: false }, text: ' Привет,\n  как дела ', event_type: null, deleted: false, created_at: '2026-10-14T09:00:00Z' }
    expect(previewText(null, 'org')).toBe('Сообщений пока нет')
    expect(previewText(base, 'org')).toBe('Привет, как дела')
    expect(previewText({ ...base, author: { ...base.author, is_me: true } }, 'scientist')).toBe('Вы: Привет, как дела')
    expect(previewText(base, 'scientist')).toBe('Анна: Привет, как дела')
    expect(previewText({ ...base, author: null }, 'org')).toBe('Привет, как дела')
    expect(previewText({ ...base, deleted: true, text: '' }, 'org')).toBe('Сообщение удалено')
    expect(previewText({ ...base, kind: 'system', author: null, event_type: 'application.decided' }, 'org')).toBe('Решение по отклику')
    expect(previewText({ ...base, kind: 'system', author: null, event_type: null }, 'org')).toBe('Новое событие по отклику')
  })

  it('tells a block in the words of the one who looks', () => {
    const line = (event_type: string) => ({ seq: 1, kind: 'system' as const, author: null, text: '', event_type, deleted: false, created_at: '2026-10-14T09:00:00Z' })
    expect(previewText(line('chat.blocked'), 'scientist')).toBe('Вы заблокировали организацию')
    expect(previewText(line('chat.blocked'), 'org')).toBe('Соискатель ограничил переписку с организацией')
    expect(previewText(line('chat.unblocked'), 'scientist')).toBe('Вы сняли блокировку с организации')
    expect(previewText(line('chat.unblocked'), 'org')).toBe('Переписка снова открыта')
  })

  it('writes time like a mail list: today, yesterday, this year, earlier', () => {
    const now = new Date(2026, 9, 14, 15, 0)
    expect(chatTime(new Date(2026, 9, 14, 9, 5).toISOString(), now)).toBe('09:05')
    expect(chatTime(new Date(2026, 9, 13, 23, 59).toISOString(), now)).toBe('вчера')
    expect(chatTime(new Date(2026, 0, 3, 12, 0).toISOString(), now)).toBe('3 янв.')
    expect(chatTime(new Date(2025, 9, 14, 12, 0).toISOString(), now)).toBe('14.10.2025')
    // Первое число месяца: «вчера» — последний день прошлого месяца
    expect(chatTime(new Date(2026, 8, 30, 20, 0).toISOString(), new Date(2026, 9, 1, 8, 0))).toBe('вчера')
    expect(chatTime(new Date().toISOString())).toMatch(/^\d\d:\d\d$/)
  })
})

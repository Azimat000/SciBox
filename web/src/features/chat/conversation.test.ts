import { describe, expect, it, vi } from 'vitest'
import type { Message } from './api'
import { buildRows, canModify, convReducer, dayLabel, delivery, firstUnreadSeq, initialConv, mergeMessages, messageTime, newClientId, type PendingMessage } from './conversation'

const author = (over: Partial<NonNullable<Message['author']>> = {}) => ({ side: 'org' as const, name: 'Елена Орлова', is_me: false, anonymous: false, former: false, ...over })

const msg = (seq: number, over: Partial<Message> = {}): Message => ({
  id: `id-${seq}`,
  seq,
  kind: 'text',
  author: author(),
  text: `Сообщение ${seq}`,
  event: null,
  created_at: '2026-10-14T10:00:00Z',
  edited_at: null,
  deleted: false,
  editable_until: null,
  client_id: null,
  ...over,
})

const system = (seq: number, type = 'application.viewed'): Message => msg(seq, { kind: 'system', author: null, text: '', event: { type, data: {} } })

const labels = { today: 'Сегодня', yesterday: 'Вчера' }

describe('mergeMessages', () => {
  it('orders by seq and drops duplicates', () => {
    expect(mergeMessages([msg(3), msg(1)], [msg(2), msg(3)]).map((m) => m.seq)).toEqual([1, 2, 3])
  })

  it('does not let an old version overwrite a newer one', () => {
    const edited = msg(1, { text: 'новое', edited_at: '2026-10-14T10:05:00Z' })
    expect(mergeMessages([edited], [msg(1, { text: 'старое' })])[0].text).toBe('новое')
    expect(mergeMessages([msg(1, { text: 'старое' })], [edited])[0].text).toBe('новое')
  })

  it('lets a deleted message win in either order', () => {
    const gone = msg(1, { deleted: true, text: '' })
    expect(mergeMessages([gone], [msg(1)])[0].deleted).toBe(true)
    expect(mergeMessages([msg(1)], [gone])[0].deleted).toBe(true)
  })
})

describe('convReducer', () => {
  const start = initialConv(0)

  it('loads, then merges later events', () => {
    let s = convReducer(start, { type: 'loaded', items: [msg(1), msg(2)], hasMore: true })
    expect(s.status).toBe('ready')
    expect(s.hasMore).toBe(true)
    s = convReducer(s, { type: 'merge', items: [msg(3)] })
    expect(s.items.map((m) => m.seq)).toEqual([1, 2, 3])
  })

  it('keeps what arrived during loading', () => {
    let s = convReducer(start, { type: 'merge', items: [msg(5)] })
    s = convReducer(s, { type: 'loaded', items: [msg(4)], hasMore: false })
    expect(s.items.map((m) => m.seq)).toEqual([4, 5])
  })

  it('goes through error and reload', () => {
    let s = convReducer(start, { type: 'failed', error: new Error('x') })
    expect(s.status).toBe('error')
    s = convReducer(s, { type: 'reload' })
    expect(s.status).toBe('loading')
    expect(s.error).toBeNull()
  })

  it('tracks older pages: start, success, failure', () => {
    let s = convReducer(start, { type: 'loaded', items: [msg(10)], hasMore: true })
    s = convReducer(s, { type: 'olderStart' })
    expect(s.olderLoading).toBe(true)
    s = convReducer(s, { type: 'olderFailed' })
    expect(s.olderLoading).toBe(false)
    expect(s.olderFailed).toBe(true)
    s = convReducer(s, { type: 'olderStart' })
    expect(s.olderFailed).toBe(false)
    s = convReducer(s, { type: 'older', items: [msg(8), msg(9)], hasMore: false })
    expect(s.items.map((m) => m.seq)).toEqual([8, 9, 10])
    expect(s.hasMore).toBe(false)
    expect(s.olderLoading).toBe(false)
  })

  it('drops a pending message when the server version arrives by client_id', () => {
    const pending: PendingMessage = { client_id: 'c1', text: 'Привет', created_at: '2026-10-14T10:00:00Z', status: 'sending' }
    let s = convReducer(convReducer(start, { type: 'loaded', items: [], hasMore: false }), { type: 'pendingAdd', pending })
    expect(s.pending).toHaveLength(1)
    s = convReducer(s, { type: 'pendingState', clientId: 'c1', status: 'failed', error: 'нет сети' })
    expect(s.pending[0]).toMatchObject({ status: 'failed', error: 'нет сети' })
    // тот же client_id ставится заново без дубля
    s = convReducer(s, { type: 'pendingAdd', pending })
    expect(s.pending).toHaveLength(1)
    s = convReducer(s, { type: 'merge', items: [msg(1, { client_id: 'c1', author: author({ is_me: true }) })] })
    expect(s.pending).toHaveLength(0)
    expect(s.items).toHaveLength(1)
  })

  it('keeps pending when an unrelated message arrives and drops it explicitly', () => {
    const pending: PendingMessage = { client_id: 'c2', text: 'x', created_at: '2026-10-14T10:00:00Z', status: 'sending' }
    let s = convReducer(start, { type: 'pendingAdd', pending })
    const before = s.pending
    s = convReducer(s, { type: 'merge', items: [msg(1)] })
    expect(s.pending).toBe(before)
    s = convReducer(s, { type: 'pendingDrop', clientId: 'c2' })
    expect(s.pending).toEqual([])
  })
})

describe('firstUnreadSeq', () => {
  it('points at the first message after the read mark that is not mine', () => {
    const items = [msg(1), msg(2, { author: author({ is_me: true }) }), msg(3), msg(4)]
    expect(firstUnreadSeq(items, 1)).toBe(3)
    expect(firstUnreadSeq(items, 0)).toBe(1)
    expect(firstUnreadSeq(items, 4)).toBeNull()
  })

  it('counts system lines, which have no author', () => {
    expect(firstUnreadSeq([msg(1), system(2)], 1)).toBe(2)
  })
})

describe('canModify', () => {
  const now = Date.parse('2026-10-14T12:00:00Z')
  const mine = (over: Partial<Message> = {}) => msg(1, { author: author({ is_me: true }), editable_until: '2026-10-14T13:00:00Z', ...over })

  it('allows my own text inside the window', () => {
    expect(canModify(mine(), now)).toBe(true)
  })
  it('refuses after the window, for deleted, others, system and without a deadline', () => {
    expect(canModify(mine({ editable_until: '2026-10-14T11:59:59Z' }), now)).toBe(false)
    expect(canModify(mine({ deleted: true }), now)).toBe(false)
    expect(canModify(mine({ author: author() }), now)).toBe(false)
    expect(canModify(mine({ author: null }), now)).toBe(false)
    expect(canModify(mine({ kind: 'system' }), now)).toBe(false)
    expect(canModify(mine({ editable_until: null }), now)).toBe(false)
  })
})

describe('delivery', () => {
  it('is delivered until the other side reads up to it', () => {
    expect(delivery(msg(5), 4)).toBe('delivered')
    expect(delivery(msg(5), 5)).toBe('read')
    expect(delivery(msg(5), 9)).toBe('read')
  })
})

describe('dayLabel', () => {
  const now = new Date(2026, 9, 14, 15, 0)
  it('names today and yesterday and writes older dates in words', () => {
    expect(dayLabel(new Date(2026, 9, 14, 0, 1).toISOString(), now, labels)).toBe('Сегодня')
    expect(dayLabel(new Date(2026, 9, 13, 23, 59).toISOString(), now, labels)).toBe('Вчера')
    expect(dayLabel(new Date(2026, 9, 2, 12, 0).toISOString(), now, labels)).toBe('2 октября')
    expect(dayLabel(new Date(2025, 11, 30, 12, 0).toISOString(), now, labels)).toBe('30 декабря 2025 г.')
  })
})

describe('messageTime and newClientId', () => {
  it('formats the time in 24 hours', () => {
    expect(messageTime(new Date(2026, 9, 14, 9, 5).toISOString())).toBe('09:05')
  })
  it('makes a UUID with and without crypto.randomUUID', () => {
    const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-8[0-9a-f]{3}-[0-9a-f]{12}$/
    expect(newClientId()).toMatch(/^[0-9a-f-]{36}$/)
    const original = crypto.randomUUID
    vi.stubGlobal('crypto', {})
    expect(newClientId()).toMatch(uuid)
    vi.unstubAllGlobals()
    expect(crypto.randomUUID).toBe(original)
  })
})

describe('buildRows', () => {
  const now = new Date(2026, 9, 14, 15, 0)
  const at = (d: number, h: number, m = 0) => new Date(2026, 9, d, h, m).toISOString()

  it('puts a day separator, the unread line, authors per run and pending last', () => {
    const items = [
      msg(1, { created_at: at(13, 10) }),
      msg(2, { created_at: at(13, 10, 2) }),
      msg(3, { created_at: at(13, 10, 30) }),
      system(4),
      msg(5, { created_at: at(14, 9), author: author({ is_me: true }) }),
      msg(6, { created_at: at(14, 9, 1), author: author({ side: 'org', name: 'Другой' }) }),
    ].map((m) => (m.kind === 'system' ? { ...m, created_at: at(13, 11) } : m))
    const pending: PendingMessage = { client_id: 'c', text: 'x', created_at: at(14, 9, 5), status: 'sending' }
    const rows = buildRows({ items, pending: [pending], readAtOpen: 4 }, now, labels)
    expect(rows.map((r) => r.kind)).toEqual(['day', 'message', 'message', 'message', 'message', 'day', 'message', 'unread', 'message', 'pending'])
    const messages = rows.filter((r) => r.kind === 'message')
    // 1 начало серии; 2 та же серия; 3 через 28 минут: новая серия; 4 система; 5 моё; 6 другой автор
    expect(messages.map((r) => (r.kind === 'message' ? r.showAuthor : null))).toEqual([true, false, true, true, true, true])
    expect(rows[0]).toMatchObject({ kind: 'day', label: 'Вчера' })
    expect(rows[5]).toMatchObject({ kind: 'day', label: 'Сегодня' })
  })

  it('starts a run again after a system line or a deleted gap and on another day', () => {
    const items = [msg(1, { created_at: at(14, 9) }), msg(2, { created_at: at(14, 9, 1) }), system(3), msg(4, { created_at: at(14, 9, 2) })].map((m) => (m.kind === 'system' ? { ...m, created_at: at(14, 9, 1) } : m))
    const rows = buildRows({ items, pending: [], readAtOpen: 99 }, now, labels)
    const flags = rows.filter((r) => r.kind === 'message').map((r) => (r.kind === 'message' ? r.showAuthor : null))
    expect(flags).toEqual([true, false, true, true])
  })

  it('is empty for an empty conversation', () => {
    expect(buildRows({ items: [], pending: [], readAtOpen: 0 }, now, labels)).toEqual([])
  })
})

describe('convReducer deleted', () => {
  it('marks only that message as deleted and clears its text and edit window', () => {
    const s = convReducer(initialConv(0), { type: 'loaded', items: [msg(1), msg(2, { editable_until: '2100-01-01T00:00:00Z' })], hasMore: false })
    const next = convReducer(s, { type: 'deleted', seq: 2 })
    expect(next.items[1]).toMatchObject({ deleted: true, text: '', editable_until: null })
    expect(next.items[0].deleted).toBe(false)
    // номера, которого нет, ничего не меняют
    expect(convReducer(s, { type: 'deleted', seq: 9 }).items).toEqual(s.items)
  })
})

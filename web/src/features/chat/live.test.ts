import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChatStreamEvent } from './api'
import { backoff, BACKOFF_MAX_MS, parseSse, readStream, runStream, toStreamEvent } from './live'

const enc = new TextEncoder()

function streamOf(chunks: string[], fail = false): Response {
  let i = 0
  const body = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) controller.enqueue(enc.encode(chunks[i++]))
      else if (fail) controller.error(new Error('обрыв'))
      else controller.close()
    },
  })
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('parseSse', () => {
  it('splits events, joins data lines and skips comments', () => {
    const raw = ': connected\n\nid: 5\nevent: chat.read\ndata: {"a":1}\n\n: ping\n\nid: 6\nevent: unread.count\ndata: {"total":\ndata: 2}\n\n'
    const { events, rest } = parseSse(raw)
    expect(events).toEqual([
      { id: '5', event: 'chat.read', data: '{"a":1}' },
      { id: '6', event: 'unread.count', data: '{"total":\n2}' },
    ])
    expect(rest).toBe('')
  })

  it('keeps an unfinished event for the next chunk and understands CRLF', () => {
    const first = parseSse('id: 1\r\nevent: chat.read\r\ndata: {}\r\n\r\nid: 2\nevent: ch')
    expect(first.events).toHaveLength(1)
    expect(first.rest).toBe('id: 2\nevent: ch')
    expect(parseSse(`${first.rest}at.read\ndata: {}\n\n`).events[0]).toEqual({ id: '2', event: 'chat.read', data: '{}' })
  })

  it('uses the default name and tolerates a field without value', () => {
    expect(parseSse('data: x\n\n').events[0]).toEqual({ id: null, event: 'message', data: 'x' })
    expect(parseSse('data\nid\n\n').events).toEqual([{ id: '', event: 'message', data: '' }])
  })
})

describe('toStreamEvent', () => {
  it('turns a known event into a site event', () => {
    expect(toStreamEvent({ id: '1', event: 'unread.count', data: '{"total":3,"scientist":2,"org":1}' })).toEqual({ type: 'unread.count', total: 3, scientist: 2, org: 1 })
  })
  it('skips unknown kinds, broken JSON and non-objects', () => {
    expect(toStreamEvent({ id: null, event: 'chat.unknown', data: '{}' })).toBeNull()
    expect(toStreamEvent({ id: null, event: 'chat.read', data: '{oops' })).toBeNull()
    expect(toStreamEvent({ id: null, event: 'chat.read', data: '[1]' })).toBeNull()
    expect(toStreamEvent({ id: null, event: 'chat.read', data: '5' })).toBeNull()
  })
})

describe('backoff', () => {
  it('doubles up to the cap with a spread', () => {
    expect(backoff(0, () => 0.5)).toBe(1000)
    expect(backoff(2, () => 0.5)).toBe(4000)
    expect(backoff(10, () => 0.5)).toBe(BACKOFF_MAX_MS)
    expect(backoff(0, () => 0)).toBe(750)
    expect(backoff(0, () => 1)).toBe(1250)
  })
})

describe('readStream', () => {
  const base = { signal: new AbortController().signal, lastEventId: null as string | null }

  it('reads events across chunks and reports the end', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => streamOf([': hi\n\nid: 1\nevent: chat.read\nda', 'ta: {"chat_id":"c"}\n\nid: 2\nevent: unread.count\ndata: {"total":1,"scientist":1,"org":0}\n\n'])))
    const got: [string, string | null][] = []
    let opened = 0
    const end = await readStream({ ...base, onOpen: () => opened++, onEvent: (e, id) => got.push([e.type, id]) })
    expect(end).toBe('closed')
    expect(opened).toBe(1)
    expect(got).toEqual([['chat.read', '1'], ['unread.count', '2']])
  })

  it('sends Last-Event-ID on reconnect', async () => {
    const fetchMock = vi.fn(async () => streamOf([]))
    vi.stubGlobal('fetch', fetchMock)
    await readStream({ ...base, lastEventId: '41', onOpen: () => {}, onEvent: () => {} })
    const init = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect((init[1].headers as Record<string, string>)['Last-Event-ID']).toBe('41')
    expect((init[1].headers as Record<string, string>).Accept).toBe('text/event-stream')
  })

  it('tells a missing address, no access, a failure and a broken connection apart', async () => {
    const run = () => readStream({ ...base, onOpen: () => {}, onEvent: () => {} })
    vi.stubGlobal('fetch', async () => new Response(null, { status: 404 }))
    expect(await run()).toBe('gone')
    vi.stubGlobal('fetch', async () => new Response(null, { status: 401 }))
    expect(await run()).toBe('denied')
    vi.stubGlobal('fetch', async () => new Response(null, { status: 403 }))
    expect(await run()).toBe('denied')
    vi.stubGlobal('fetch', async () => new Response(null, { status: 500 }))
    expect(await run()).toBe('error')
    vi.stubGlobal('fetch', async () => {
      throw new TypeError('network')
    })
    expect(await run()).toBe('error')
    vi.stubGlobal('fetch', async () => streamOf(['id: 1\n'], true))
    expect(await run()).toBe('error')
  })
})

describe('runStream', () => {
  it('reconnects with the last id, announces every connection and stops on abort', async () => {
    vi.useFakeTimers()
    const calls: (string | undefined)[] = []
    let n = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init: RequestInit) => {
        calls.push((init.headers as Record<string, string>)['Last-Event-ID'])
        n += 1
        if (n === 1) return streamOf(['id: 7\nevent: chat.read\ndata: {"chat_id":"c"}\n\n'])
        if (n === 2) return new Response(null, { status: 500 })
        return streamOf([])
      }),
    )
    const ctl = new AbortController()
    const events: ChatStreamEvent[] = []
    let connected = 0
    const done = runStream({ signal: ctl.signal, onEvent: (e) => events.push(e), onConnected: () => connected++, random: () => 0.5 })
    await vi.advanceTimersByTimeAsync(100)
    expect(events.map((e) => e.type)).toEqual(['chat.read'])
    await vi.advanceTimersByTimeAsync(1100) // пауза после закрытого потока, второй запрос падает
    await vi.advanceTimersByTimeAsync(2100) // повтор через 2 секунды: третье подключение
    expect(connected).toBeGreaterThanOrEqual(2)
    expect(calls[0]).toBeUndefined()
    expect(calls[1]).toBe('7')
    ctl.abort()
    await vi.advanceTimersByTimeAsync(40_000)
    await done
  })

  it('gives up quietly when the address does not exist or is denied', async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 404 }))
    vi.stubGlobal('fetch', fetchMock)
    await runStream({ signal: new AbortController().signal, onEvent: () => {}, onConnected: () => {} })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    fetchMock.mockClear()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 401 })))
    await runStream({ signal: new AbortController().signal, onEvent: () => {}, onConnected: () => {} })
  })

  it('does not even start when already aborted', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const ctl = new AbortController()
    ctl.abort()
    await runStream({ signal: ctl.signal, onEvent: () => {}, onConnected: () => {} })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

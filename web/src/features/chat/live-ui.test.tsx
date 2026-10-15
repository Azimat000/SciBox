import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ann } from '../../test/api'
import { chatHeaders } from '../../test/chat'
import { keys, type ChatHeader, type ChatStreamEvent, type UnreadCount } from './api'
import { applyEvent } from './applyEvent'
import { ChatLive } from './ChatLive'
import { chatHub } from './hub'
import { useChatEvents } from './hub'

// Живой поток для сайта (срез 35): правка кэша по событиям и один поток на вошедшего человека.

const enc = new TextEncoder()
const ID = chatHeaders.scientistInvited().id

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('applyEvent', () => {
  const setup = () => {
    const client = new QueryClient()
    const schedule = vi.fn()
    client.setQueryData<ChatHeader>(keys.one(ann.id, ID), chatHeaders.scientistInvited())
    return { client, schedule }
  }
  const header = (client: QueryClient) => client.getQueryData<ChatHeader>(keys.one(ann.id, ID))!

  it('puts the unread counts into the badge cache and refreshes the lists', () => {
    const { client, schedule } = setup()
    applyEvent(client, ann.id, { type: 'unread.count', total: 4, scientist: 3, org: 1 }, schedule)
    expect(client.getQueryData<UnreadCount>(keys.unread(ann.id))).toEqual({ total: 4, scientist: 3, org: 1 })
    expect(schedule).toHaveBeenCalledTimes(1)
  })

  it('moves the other side read mark forward and never back', () => {
    const { client, schedule } = setup()
    applyEvent(client, ann.id, { type: 'chat.read', chat_id: ID, side: 'scientist', reader_side: 'org', up_to: 9 }, schedule)
    expect(header(client).read).toEqual({ mine: 5, theirs: 9 })
    applyEvent(client, ann.id, { type: 'chat.read', chat_id: ID, side: 'scientist', reader_side: 'org', up_to: 6 }, schedule)
    expect(header(client).read.theirs).toBe(9)
    expect(schedule).not.toHaveBeenCalled()
  })

  it('takes my own read mark from another tab', () => {
    const { client, schedule } = setup()
    applyEvent(client, ann.id, { type: 'chat.read', chat_id: ID, side: 'scientist', reader_side: 'scientist', up_to: 8 }, schedule)
    expect(header(client).read).toEqual({ mine: 8, theirs: 4 })
  })

  it('ignores a read mark for a conversation that is not open', () => {
    const { client, schedule } = setup()
    applyEvent(client, ann.id, { type: 'chat.read', chat_id: 'другая', side: 'scientist', reader_side: 'org', up_to: 9 }, schedule)
    expect(client.getQueryData(keys.one(ann.id, 'другая'))).toBeUndefined()
  })

  it('applies a state change to the header and refreshes the lists', () => {
    const { client, schedule } = setup()
    applyEvent(client, ann.id, { type: 'chat.state', chat_id: ID, side: 'scientist', status: { source: 'application', value: 'rejected' }, needs_answer: false, can_write: false, closed_reason: 'rejected' }, schedule)
    expect(header(client)).toMatchObject({ can_write: false, closed_reason: 'rejected', needs_answer: false, status: { value: 'rejected' } })
    expect(schedule).toHaveBeenCalledTimes(1)
    applyEvent(client, ann.id, { type: 'chat.state', chat_id: 'другая', side: 'scientist', status: { source: 'offer', value: 'pending' }, needs_answer: false, can_write: true, closed_reason: null }, schedule)
    expect(client.getQueryData(keys.one(ann.id, 'другая'))).toBeUndefined()
  })

  it('refreshes the lists for message events and leaves typing and the rest alone', () => {
    const { client, schedule } = setup()
    const message = { id: 'm', seq: 1, kind: 'text', author: null, text: '', event: null, created_at: '', edited_at: null, deleted: false, editable_until: null, client_id: null } as const
    for (const type of ['message.created', 'message.edited', 'message.deleted'] as const) applyEvent(client, ann.id, { type, chat_id: ID, side: 'scientist', message }, schedule)
    expect(schedule).toHaveBeenCalledTimes(3)
    applyEvent(client, ann.id, { type: 'chat.typing', chat_id: ID, side: 'scientist', author: { side: 'org', name: 'Е', anonymous: false } }, schedule)
    applyEvent(client, ann.id, { type: 'reconnected' }, schedule)
    expect(schedule).toHaveBeenCalledTimes(3)
  })
})

describe('ChatLive', () => {
  function mount(me: unknown, stream: () => Response) {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url === '/api/auth/me') return new Response(JSON.stringify({ user: me }), { status: 200, headers: { 'Content-Type': 'application/json' } })
        if (url === '/api/chats/events') return stream()
        return new Response(null, { status: 404 })
      }),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const seen: ChatStreamEvent[] = []
    function Probe() {
      useChatEvents((e) => seen.push(e))
      return null
    }
    const view = render(
      <QueryClientProvider client={client}>
        <ChatLive />
        <Probe />
      </QueryClientProvider>,
    )
    return { client, seen, ...view }
  }

  it('does nothing for a visitor', async () => {
    const fetchSpy = vi.fn()
    const { seen } = mount(null, () => {
      fetchSpy()
      return new Response(null, { status: 404 })
    })
    await new Promise((r) => setTimeout(r, 50))
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(seen).toEqual([])
  })

  it('opens the stream for a signed-in person, announces the connection and updates the badge cache', async () => {
    const { client, seen, unmount } = mount(ann, () => {
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(enc.encode(': connected\n\nid: 1\nevent: unread.count\ndata: {"total":2,"scientist":2,"org":0}\n\n'))
        },
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    })
    await waitFor(() => expect(client.getQueryData(keys.unread(ann.id))).toEqual({ total: 2, scientist: 2, org: 0 }))
    expect(seen.map((e) => e.type)).toEqual(['reconnected', 'unread.count'])
    unmount()
  })

  it('refreshes the lists once for a burst of message events', async () => {
    const { client, unmount } = mount(ann, () => {
      const message = JSON.stringify({ chat_id: ID, side: 'scientist', message: { id: 'm', seq: 1, kind: 'text', author: null, text: 'x', event: null, created_at: '', edited_at: null, deleted: false, editable_until: null, client_id: null } })
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          for (let i = 1; i <= 3; i += 1) controller.enqueue(enc.encode(`id: ${i}\nevent: message.created\ndata: ${message}\n\n`))
        },
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    })
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    await waitFor(() => expect(invalidate).toHaveBeenCalled(), { timeout: 2000 })
    await new Promise((r) => setTimeout(r, 100))
    expect(invalidate.mock.calls.filter((c) => JSON.stringify(c[0]).includes('list'))).toHaveLength(1)
    unmount()
  })

  it('aborts the stream when the page is left', async () => {
    const signals: AbortSignal[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/auth/me') return new Response(JSON.stringify({ user: ann }), { status: 200, headers: { 'Content-Type': 'application/json' } })
        if (init?.signal) signals.push(init.signal)
        return new Response(new ReadableStream<Uint8Array>({ start: (c) => c.enqueue(enc.encode(': hi\n\n')) }), { status: 200 })
      }),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = render(
      <QueryClientProvider client={client}>
        <ChatLive />
      </QueryClientProvider>,
    )
    await waitFor(() => expect(signals.length).toBeGreaterThan(0))
    const eventsSignal = signals[signals.length - 1]
    expect(eventsSignal.aborted).toBe(false)
    view.unmount()
    expect(eventsSignal.aborted).toBe(true)
  })
})

describe('chatHub', () => {
  it('delivers to subscribers and forgets them after unsubscribe', () => {
    const got: string[] = []
    const off = chatHub.subscribe((e) => got.push(e.type))
    chatHub.emit({ type: 'reconnected' })
    off()
    chatHub.emit({ type: 'reconnected' })
    expect(got).toEqual(['reconnected'])
  })
})

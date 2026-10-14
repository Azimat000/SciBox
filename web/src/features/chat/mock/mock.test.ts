import { afterEach, describe, expect, it, vi } from 'vitest'
import { application, candidate, staffApplication, summary } from '../../../test/applications'
import { offer, sentOffer } from '../../../test/offers'
import { demoState } from './data'
import { decorate } from './decorate'
import { CHAT_MOCK_MARKER, exposeForConsole, installChatMock, toResponse } from './index'
import { createChatServer } from './server'
import { datasetFromUrl, mountDataToggle } from './toggle'
import { dataset } from './worst'

// Подставной сервер чата в разработке (срез 34): подмена fetch, поле chat у откликов и приглашений, хранение в браузере.

const NOW = new Date('2026-10-14T12:00:00Z')
const server = () => createChatServer({ state: demoState(NOW), now: () => NOW, later: () => {} })

function memoryStorage(broken = false): Storage {
  const data = new Map<string, string>()
  return {
    get length() {
      return data.size
    },
    clear: () => data.clear(),
    key: (i) => [...data.keys()][i] ?? null,
    getItem: (k) => (broken ? (() => { throw new Error('нет доступа') })() : (data.get(k) ?? null)),
    setItem: (k, v) => {
      if (broken) throw new Error('нет доступа')
      data.set(k, v)
    },
    removeItem: (k) => void data.delete(k),
  }
}

const json = (status: number, body: unknown, type = 'application/json') => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': type, 'Content-Length': '999' } })

function setup(real: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>, storage = memoryStorage()) {
  const target = { fetch: vi.fn(real) as unknown as typeof fetch, location: { origin: 'http://localhost:5173' } }
  const realFetch = target.fetch
  const installed = installChatMock({ target, storage, delay: () => 0, incomingEveryMs: () => 0 })
  return { target, realFetch, storage, ...installed }
}

afterEach(() => vi.useRealTimers())

describe('mock install', () => {
  it('answers chat addresses itself and passes the rest to the real server', async () => {
    const { target, realFetch, uninstall } = setup(async () => json(200, { ok: true }))
    const list = await target.fetch('/api/chats?side=scientist')
    expect(list.headers.get('Content-Type')).toBe('application/json')
    expect(((await list.json()) as { items: unknown[] }).items.length).toBeGreaterThan(0)
    expect((await target.fetch('/api/chats/c0000000-0000-4000-8000-000000000001/archive', { method: 'POST' })).status).toBe(204)
    const send = await target.fetch(new Request('http://localhost:5173/api/chats/c0000000-0000-4000-8000-000000000001/messages'), {
      method: 'POST',
      body: JSON.stringify({ text: 'Привет', client_id: '1f7c3d2e-8a41-4b0e-9c55-0d3e6f2a7b10' }),
    })
    expect(send.status).toBe(201)
    expect((await target.fetch('/api/chats/x/messages', { method: 'POST', body: 'не JSON' })).status).toBe(404)
    const events = await target.fetch('/api/chats/events')
    expect(events.headers.get('Content-Type')).toBe('text/event-stream')
    await events.body?.cancel()
    expect(realFetch).not.toHaveBeenCalled()
    expect(await (await target.fetch('/api/vacancies')).json()).toEqual({ ok: true })
    expect(realFetch).toHaveBeenCalledTimes(1)
    uninstall()
    expect(target.fetch).toBe(realFetch)
  })

  it('adds the chat field to real answers and leaves the rest as is', async () => {
    const { target } = setup(async (input, init) => {
      const path = String(input)
      if (init?.method === 'POST') return json(200, { items: [summary] })
      if (path.startsWith('/api/applications?')) return json(200, { items: [summary], total: 1 })
      if (path === '/api/text') return json(200, { a: 1 }, 'text/plain')
      if (path === '/api/fail') return json(500, { error: { code: 'internal', message: 'x' } })
      return json(200, { items: [] })
    })
    const decorated = (await (await target.fetch('/api/applications?limit=20')).json()) as { items: { chat: { id: string } }[] }
    expect(decorated.items[0].chat.id).toBeTruthy()
    expect(await (await target.fetch('/api/text')).json()).toEqual({ a: 1 })
    expect((await target.fetch('/api/fail')).status).toBe(500)
    expect(await (await target.fetch('/api/applications?x', { method: 'POST' })).json()).toEqual({ items: [summary] })
  })

  it('keeps the message mail setting itself', async () => {
    const seen: unknown[] = []
    let fail = false
    const { target } = setup(async (_input, init) => {
      seen.push(init?.body === undefined ? undefined : JSON.parse(String(init.body)))
      return fail ? json(500, { error: { code: 'internal', message: 'x' } }) : json(200, { email_new_vacancies: true, email_deadlines: true })
    })
    expect(await (await target.fetch('/api/notification-settings')).json()).toEqual({ email_new_vacancies: true, email_deadlines: true, email_messages: true })
    const put = await target.fetch('/api/notification-settings', { method: 'PUT', body: JSON.stringify({ email_new_vacancies: true, email_deadlines: true, email_messages: false }) })
    expect(await put.json()).toEqual({ email_new_vacancies: true, email_deadlines: true, email_messages: false })
    expect(seen[1]).toEqual({ email_new_vacancies: true, email_deadlines: true })
    fail = true
    expect((await target.fetch('/api/notification-settings', { method: 'PUT', body: '{}' })).status).toBe(500)
  })

  it('remembers its state between reloads and starts over from broken data', async () => {
    const storage = memoryStorage()
    const first = setup(async () => json(200, {}), storage)
    await first.target.fetch('/api/chats/c0000000-0000-4000-8000-000000000001/archive', { method: 'POST' })
    first.uninstall()
    const again = setup(async () => json(200, {}), storage)
    expect(again.server.state.chats.find((c) => c.id === 'c0000000-0000-4000-8000-000000000001')?.archived).toBe(true)
    again.uninstall()
    storage.setItem('scibox.chatMock.v1', '{"version":2}')
    expect(setup(async () => json(200, {}), storage).server.state.chats.find((c) => c.id === 'c0000000-0000-4000-8000-000000000001')?.archived).toBe(false)
    storage.setItem('scibox.chatMock.v1', 'не JSON')
    expect(setup(async () => json(200, {}), storage).server.state.version).toBe(1)
  })

  it('works without browser storage', async () => {
    const { target } = setup(async () => json(200, {}), memoryStorage(true))
    expect((await target.fetch('/api/chats/c0000000-0000-4000-8000-000000000001/archive', { method: 'POST' })).status).toBe(204)
  })

  it('lets the counterpart write by a timer', async () => {
    vi.useFakeTimers()
    const target = { fetch: vi.fn() as unknown as typeof fetch, location: { origin: 'http://localhost' } }
    const times = [1000, 0]
    const { server: s, uninstall } = installChatMock({ target, storage: memoryStorage(), delay: () => 0, incomingEveryMs: () => times.shift() ?? 0 })
    const before = s.state.chats.reduce((n, c) => n + c.messages.length, 0)
    await vi.advanceTimersByTimeAsync(1000 + 2500)
    expect(s.state.chats.reduce((n, c) => n + c.messages.length, 0)).toBe(before + 1)
    uninstall()
  })

  it('uses window and its storage by default and shows itself in the console', async () => {
    const real = window.fetch
    const { server: s, uninstall } = installChatMock({ delay: () => 0, incomingEveryMs: () => 0 })
    expect(window.fetch).not.toBe(real)
    expect(window.localStorage.getItem('scibox.chatMock.v1')).toContain('"version":1')
    exposeForConsole(s)
    const handle = (window as unknown as { __chatMock: { marker: string; incoming: () => void; server: unknown } }).__chatMock
    expect(handle.marker).toBe(CHAT_MOCK_MARKER)
    expect(handle.server).toBe(s)
    handle.incoming()
    uninstall()
    expect(window.fetch).toBe(real)
    window.localStorage.clear()
  })

  it('turns replies into responses', async () => {
    expect(toResponse({ status: 204 }).status).toBe(204)
    expect(await toResponse({ status: 200, body: { a: 1 } }).json()).toEqual({ a: 1 })
  })
})

describe('chat field on real answers', () => {
  it('links scientist applications and offers of one vacancy to one chat', () => {
    const s = server()
    const app = decorate(s, 'GET', '/api/applications', { items: [summary], total: 1 }) as { items: { chat: { id: string; started: boolean } }[] }
    const sameVacancyOffer = { ...offer, vacancy: { ...offer.vacancy, id: summary.vacancy.id } }
    const off = decorate(s, 'GET', '/api/offers', { items: [sameVacancyOffer] }) as { items: { chat: { id: string } }[] }
    expect(off.items[0].chat.id).toBe(app.items[0].chat.id)
    expect(app.items[0].chat.started).toBe(false)
    const one = decorate(s, 'GET', `/api/offers/${offer.id}`, { offer: sameVacancyOffer }) as { offer: { chat: { id: string } } }
    expect(one.offer.chat.id).toBe(app.items[0].chat.id)
    // Статус отклика главнее приглашения той же пары
    expect(s.state.chats.find((c) => c.id === app.items[0].chat.id)?.status.source).toBe('application')
  })

  it('follows the record status: a refusal closes the chat, a block stays', () => {
    const s = server()
    const decorated = decorate(s, 'GET', `/api/applications/${application.id}`, { application }) as { application: { chat: { id: string; can_write: boolean } } }
    expect(decorated.application.chat.can_write).toBe(true)
    const rejected = decorate(s, 'GET', `/api/applications/${application.id}`, { application: { ...application, status: 'rejected' } }) as typeof decorated
    expect(rejected.application.chat.can_write).toBe(false)
    const c = s.state.chats.find((x) => x.id === decorated.application.chat.id)!
    expect(c.closed_reason).toBe('rejected')
    c.closed_reason = 'blocked'
    decorate(s, 'GET', `/api/applications/${application.id}`, { application })
    expect(c.closed_reason).toBe('blocked')
    expect(c.messages[0].event?.type).toBe('application.sent')
    expect(c.messages[0].event?.data.cover_letter).toBe(application.cover_letter)
  })

  it('gives the organization its own chats: candidates, card, sent offers', () => {
    const s = server()
    const cands = decorate(s, 'GET', '/api/my/candidates', { items: [candidate, { ...candidate, id: 'gone', anonymized: true }], total: 2 }) as { items: { chat: { id: string } | null }[] }
    expect(cands.items[1].chat).toBeNull()
    const card = decorate(s, 'GET', `/api/applications/${staffApplication.id}`, { application: { ...staffApplication, id: candidate.id } }) as { application: { chat: { id: string } } }
    expect(card.application.chat.id).toBe(cands.items[0].chat!.id)
    const chat = s.state.chats.find((c) => c.id === card.application.chat.id)!
    expect(chat.side).toBe('org')
    expect(chat.counterpart.name).toBe(candidate.applicant_name)
    const anonymous = { ...sentOffer, id: 'anon-offer', scientist: { profile_id: 'p', name: 'Скрыто', anonymous: true } }
    const sent = decorate(s, 'GET', '/api/my/sent-offers', { items: [sentOffer, anonymous] }) as { items: { chat: { id: string } }[] }
    const anon = s.state.chats.find((c) => c.id === sent.items[1].chat.id)!
    expect(anon.counterpart).toEqual({ kind: 'scientist', name: '', anonymous: true, profile_id: null })
    expect(anon.messages[0].event?.type).toBe('offer.created')
  })

  it('leaves other answers alone', () => {
    const s = server()
    const body = { items: [summary] }
    expect(decorate(s, 'POST', '/api/applications', body)).toBe(body)
    expect(decorate(s, 'GET', '/api/vacancies', body)).toBe(body)
    expect(decorate(s, 'GET', '/api/applications', null)).toBeNull()
    expect(decorate(s, 'GET', '/api/applications/x', { other: 1 })).toEqual({ other: 1 })
    expect(decorate(s, 'GET', '/api/offers/x', { other: 1 })).toEqual({ other: 1 })
  })
})

describe('datasets for break-ui', () => {
  it('builds demo, worst, empty and a thousand rows', () => {
    const now = new Date('2026-10-14T12:00:00Z')
    expect(dataset('demo', now).chats).toHaveLength(10)
    expect(dataset('empty', now).chats).toHaveLength(0)
    const many = dataset('many', now)
    expect(many.chats.filter((c) => c.side === 'scientist')).toHaveLength(1000)
    expect(many.chats.filter((c) => c.side === 'org')).toHaveLength(1000)
    const s = createChatServer({ state: dataset('worst', now), now: () => now, later: () => {} })
    const sci = s.handle({ method: 'GET', url: new URL('http://x/api/chats?side=scientist'), body: undefined })!.body as { items: { unread: number; closed_reason: string | null; counterpart: { name: string } }[] }
    expect(sci.items[0].unread).toBe(1500)
    expect(sci.items.some((c) => c.closed_reason === 'withdrawn')).toBe(true)
    const org = s.handle({ method: 'GET', url: new URL('http://x/api/chats?side=org'), body: undefined })!.body as { items: { unread: number; counterpart: { anonymous: boolean; name: string } }[]; next_cursor: string | null }
    expect(org.items[0].unread).toBe(12345)
    expect(org.items.find((c) => c.counterpart.anonymous)?.counterpart.name).toBe('')
    expect(s.state.chats.some((c) => c.closed_reason === 'offer_declined')).toBe(true)
  })

  it('does not keep a non-demo dataset in the browser', () => {
    const storage = memoryStorage()
    const target = { fetch: vi.fn() as unknown as typeof fetch, location: { origin: 'http://localhost' } }
    const { server: s, uninstall } = installChatMock({ target, storage, data: 'worst', delay: () => 0, incomingEveryMs: () => 0 })
    s.state.chats[0].archived = true
    s.handle({ method: 'POST', url: new URL(`http://x/api/chats/${s.state.chats[1].id}/archive`), body: undefined })
    expect(storage.getItem('scibox.chatMock.v1')).toBeNull()
    uninstall()
  })

  it('reads the dataset from the address and switches it with a toggle', () => {
    expect(datasetFromUrl('?data=worst')).toBe('worst')
    expect(datasetFromUrl('?data=empty')).toBe('empty')
    expect(datasetFromUrl('?data=many')).toBe('many')
    expect(datasetFromUrl('?data=other')).toBe('demo')
    expect(datasetFromUrl('')).toBe('demo')
    const go = vi.fn()
    const bar = mountDataToggle('worst', document, go)
    const buttons = [...bar.querySelectorAll('button')]
    expect(buttons.map((b) => b.textContent)).toEqual(['Демо', 'Худший случай', 'Пусто', '1000 строк'])
    expect(buttons[1]).toHaveAttribute('aria-pressed', 'true')
    buttons[3].click()
    expect(new URL(go.mock.calls[0][0] as string).searchParams.get('data')).toBe('many')
    buttons[0].click()
    expect(new URL(go.mock.calls[1][0] as string).searchParams.has('data')).toBe(false)
    bar.remove()
  })
})

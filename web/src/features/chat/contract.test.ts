import { describe, expect, it } from 'vitest'
import { contractEnums, requestErrors, responseErrors, type ContractFile } from '../../test/contract'
import { demoState } from './mock/data'
import { createChatServer } from './mock/server'
import type { MockReply } from './mock/types'

// Контракт API чата (contracts/chat, срез 34): примеры согласованы сами с собой, покрывают каждый адрес и каждую
// системную строку, а подставной сервер отвечает строго по ним. Сервер (срез 37) сверяется с теми же файлами.

const files = import.meta.glob<ContractFile>('../../../../contracts/chat/*.json', { eager: true, import: 'default' })
const byName = Object.fromEntries(Object.entries(files).map(([path, file]) => [path.split('/').pop()!.replace('.json', ''), file]))
const file = (name: string) => {
  const f = byName[name]
  if (!f) throw new Error(`нет файла контракта ${name}.json`)
  return f
}

const endpointFiles: Record<string, string> = {
  'GET /api/chats': 'list',
  'GET /api/chats/unread-count': 'unread-count',
  'GET /api/chats/vacancies': 'vacancies',
  'GET /api/chats/{id}': 'chat',
  'GET /api/chats/{id}/messages': 'messages',
  'POST /api/chats/{id}/messages': 'send',
  'PATCH /api/chats/{id}/messages/{seq}': 'edit',
  'DELETE /api/chats/{id}/messages/{seq}': 'delete',
  'POST /api/chats/{id}/read': 'read',
  'POST /api/chats/{id}/archive': 'archive',
  'POST /api/chats/{id}/unarchive': 'unarchive',
  'POST /api/chats/{id}/typing': 'typing',
  'GET /api/chats/events': 'events',
  'GET /api/orgs/{orgId}/chat-templates': 'templates-list',
  'POST /api/orgs/{orgId}/chat-templates': 'templates-create',
  'PATCH /api/orgs/{orgId}/chat-templates/{id}': 'templates-update',
  'DELETE /api/orgs/{orgId}/chat-templates/{id}': 'templates-delete',
  'POST /api/chats/{id}/block': 'block',
  'GET /api/chat-blocks': 'blocks-list',
  'DELETE /api/chat-blocks/{orgId}': 'unblock',
  'POST /api/chats/{id}/complaints': 'complaint',
  'GET /api/notification-settings, PUT /api/notification-settings': 'notification-settings',
}

describe('chat contract files', () => {
  it('has a file for every endpoint of docs/CHAT.md and nothing else', () => {
    for (const [endpoint, name] of Object.entries(endpointFiles)) expect(file(name).endpoint).toBe(endpoint)
    const known = new Set([...Object.values(endpointFiles), 'enums', 'errors', 'links'])
    expect(Object.keys(byName).filter((n) => !known.has(n))).toEqual([])
  })

  it('every example agrees with the shape of its file, its enums and variants', () => {
    for (const [name, f] of Object.entries(byName)) {
      if (name === 'enums') continue
      for (const [example, e] of Object.entries(f.examples)) {
        expect(responseErrors(f, e.response), `${name}.${example}`).toEqual([])
        if (e.request?.body !== undefined) expect(requestErrors(f, e.request.body), `${name}.${example} request`).toEqual([])
      }
    }
  })

  it('every enum named by a file exists', () => {
    for (const [name, f] of Object.entries(byName)) {
      for (const enumName of Object.values({ ...f.enums, ...f.request_enums })) {
        for (const part of enumName.split('|')) expect(contractEnums[part], `${name}: ${part}`).toBeDefined()
      }
    }
  })

  it('shows every kind of system line and every stream event', () => {
    const types = new Set(
      Object.values(file('messages').examples).flatMap((e) =>
        (e.response as { items: { event: { type: string } | null }[] }).items.flatMap((m) => (m.event ? [m.event.type] : [])),
      ),
    )
    expect([...types].sort()).toEqual([...contractEnums.event_type].sort())
    const events = new Set(Object.values(file('events').examples).map((e) => (e.response as { event: string }).event))
    expect([...events].sort()).toEqual([...contractEnums.stream_event].sort())
  })

  it('every error named by an endpoint is described in errors.json', () => {
    const codes = new Set(Object.values(file('errors').examples).map((e) => (e.response as { error: { code: string } }).error.code))
    expect([...codes].sort()).toEqual([...contractEnums.error_code].sort())
    for (const [name, f] of Object.entries(byName)) {
      for (const code of f.errors ?? []) expect(codes.has(code), `${name}: ${code}`).toBe(true)
    }
  })
})

describe('contract checker', () => {
  const f: ContractFile = {
    endpoint: 'GET /x',
    enums: { 'items[].kind': 'message_kind' },
    variants: { 'items[].event': 'type' },
    examples: {
      a: { response: { items: [{ kind: 'text', n: 1, opt: 'x', event: null }], cursor: null } },
      b: { response: { items: [{ kind: 'system', n: 2, event: { type: 'p', data: { a: 1 } } }], cursor: 'c' } },
      c: { response: { items: [{ kind: 'system', n: 3, event: { type: 'q', data: { b: true } } }], cursor: null } },
    },
  }

  it('accepts a matching value and optional fields', () => {
    expect(responseErrors(f, { items: [], cursor: 'x' })).toEqual([])
    expect(responseErrors(f, { items: [{ kind: 'text', n: 5, event: null }], cursor: null })).toEqual([])
  })

  it('finds wrong types, extra and missing fields, values outside enums and unknown variants', () => {
    expect(responseErrors(f, { items: [{ kind: 'text', n: '5', event: null }], cursor: null })).toEqual(['items[].n: тип string, ожидался number'])
    expect(responseErrors(f, { items: [], cursor: null, extra: 1 })).toEqual(['(корень): лишнее поле extra'])
    expect(responseErrors(f, { cursor: null })).toEqual(['(корень): нет поля items'])
    expect(responseErrors(f, { items: [{ kind: 'video', n: 1, event: null }], cursor: null })).toEqual(['items[].kind: значение «video» не из перечня message_kind'])
    expect(responseErrors(f, { items: [{ kind: 'system', n: 1, event: { type: 'z', data: {} } }], cursor: null })).toEqual(['items[].event: неизвестный вид type=«z»'])
    expect(responseErrors(f, { items: [{ kind: 'system', n: 1, event: { type: 'p', data: { b: true } } }], cursor: null })).toEqual([
      'items[].event.data: нет поля a',
      'items[].event.data: лишнее поле b',
    ])
  })

  it('knows endpoints without a body', () => {
    const empty = file('archive')
    expect(responseErrors(empty, undefined)).toEqual([])
    expect(responseErrors(empty, { ok: true })).toEqual(['у адреса ответ без тела'])
    expect(requestErrors(empty, undefined)).toEqual([])
    expect(requestErrors(empty, {})).toEqual([])
    expect(requestErrors(empty, { a: 1 })).toEqual(['у адреса запрос без тела'])
    expect(requestErrors(file('send'), { text: 'Привет', client_id: 'x' })).toEqual([])
    expect(requestErrors(file('send'), { text: 1 })).toEqual(['(корень): нет поля client_id', 'text: тип number, ожидался string'])
  })
})

describe('mock server follows the contract', () => {
  const NOW = new Date('2026-10-14T12:00:00Z')
  const setup = () => {
    const delayed: (() => void)[] = []
    const server = createChatServer({ state: demoState(NOW), now: () => NOW, random: () => 0, later: (fn) => void delayed.push(fn) })
    const call = (method: string, path: string, body?: unknown, headers?: Headers): MockReply => {
      const reply = server.handle({ method, url: new URL(path, 'http://localhost'), body, headers })
      if (!reply) throw new Error(`не адрес чата: ${path}`)
      return reply
    }
    const flush = () => {
      while (delayed.length > 0) delayed.shift()!()
    }
    return { server, call, flush }
  }
  const ok = (name: string, reply: MockReply, status: number) => {
    expect(reply.status).toBe(status)
    expect(responseErrors(file(name), reply.body)).toEqual([])
  }
  const err = (reply: MockReply, status: number, code: string) => {
    expect(reply.status).toBe(status)
    expect(responseErrors(file('errors'), reply.body)).toEqual([])
    expect((reply.body as { error: { code: string } }).error.code).toBe(code)
  }
  const sciChat = 'c0000000-0000-4000-8000-000000000001'
  const closedChat = 'c0000000-0000-4000-8000-000000000004'
  const orgChat = 'c0000000-0000-4000-8000-000000000006'

  it('lists, counts and pages by key', () => {
    const { call } = setup()
    for (const side of ['scientist', 'org']) {
      for (const box of ['all', 'unread', 'archive']) ok('list', call('GET', `/api/chats?side=${side}&box=${box}`), 200)
    }
    const first = call('GET', '/api/chats?side=scientist&limit=2').body as { items: { id: string }[]; next_cursor: string }
    expect(first.items).toHaveLength(2)
    const second = call('GET', `/api/chats?side=scientist&limit=2&cursor=${first.next_cursor}`).body as { items: { id: string }[]; next_cursor: string | null }
    expect(second.items.map((c) => c.id)).not.toContain(first.items[0].id)
    ok('list', call('GET', '/api/chats?side=org&q=ГУСЕВА'), 200)
    ok('list', call('GET', '/api/chats?side=org&vacancy=a0000000-0000-4000-8000-000000000006'), 200)
    ok('unread-count', call('GET', '/api/chats/unread-count'), 200)
    ok('vacancies', call('GET', '/api/chats/vacancies'), 200)
    for (const q of ['side=x', 'side=org&box=x', 'side=org&limit=0', 'side=org&limit=51', 'side=org&cursor=@@', `side=org&q=${'я'.repeat(101)}`]) {
      err(call('GET', `/api/chats?${q}`), 400, 'bad_request')
    }
  })

  it('opens a chat and its messages in both directions', () => {
    const { call } = setup()
    for (const id of [sciChat, closedChat, orgChat, 'c0000000-0000-4000-8000-000000000007']) {
      ok('chat', call('GET', `/api/chats/${id}`), 200)
      ok('messages', call('GET', `/api/chats/${id}/messages`), 200)
    }
    ok('messages', call('GET', `/api/chats/${sciChat}/messages?before=3&limit=1`), 200)
    ok('messages', call('GET', `/api/chats/${sciChat}/messages?after=2&limit=2`), 200)
    err(call('GET', '/api/chats/nope'), 404, 'not_found')
    err(call('GET', '/api/chats/nope/messages'), 404, 'not_found')
    err(call('GET', `/api/chats/${sciChat}/unknown`), 404, 'not_found')
    for (const q of ['before=-1', 'after=x', 'limit=101']) err(call('GET', `/api/chats/${sciChat}/messages?${q}`), 400, 'bad_request')
  })

  it('sends once per client_id, edits, deletes and marks read', () => {
    const { call, flush } = setup()
    const body = { text: '  Спасибо, буду.  ', client_id: '9a3c6e1f-4b2d-4f8a-b7c1-2e5d8f0a6c93' }
    expect(requestErrors(file('send'), body)).toEqual([])
    const sent = call('POST', `/api/chats/${sciChat}/messages`, body)
    ok('send', sent, 201)
    const message = (sent.body as { message: { seq: number; text: string } }).message
    expect(message.text).toBe('Спасибо, буду.')
    ok('send', call('POST', `/api/chats/${sciChat}/messages`, body), 200)
    flush()
    ok('edit', call('PATCH', `/api/chats/${sciChat}/messages/${message.seq}`, { text: 'Спасибо!' }), 200)
    expect(call('DELETE', `/api/chats/${sciChat}/messages/${message.seq}`).status).toBe(204)
    err(call('PATCH', `/api/chats/${sciChat}/messages/${message.seq}`, { text: 'ещё' }), 409, 'edit_window_closed')
    err(call('PATCH', `/api/chats/${sciChat}/messages/1`, { text: 'x' }), 409, 'system_message')
    err(call('PATCH', `/api/chats/${sciChat}/messages/3`, { text: 'x' }), 403, 'not_author')
    err(call('DELETE', `/api/chats/${sciChat}/messages/999`), 404, 'not_found')
    expect(requestErrors(file('read'), { up_to: 6 })).toEqual([])
    expect(call('POST', `/api/chats/${sciChat}/read`, { up_to: 6 }).status).toBe(204)
    err(call('POST', `/api/chats/${sciChat}/read`, { up_to: 'all' }), 400, 'bad_request')
  })

  it('refuses bad and closed sends', () => {
    const { call } = setup()
    const id = '1f7c3d2e-8a41-4b0e-9c55-0d3e6f2a7b10'
    err(call('POST', `/api/chats/${sciChat}/messages`, { text: 'x' }), 400, 'bad_request')
    err(call('POST', `/api/chats/${sciChat}/messages`, { text: '   ', client_id: id }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${sciChat}/messages`, { text: 'я'.repeat(4001), client_id: id }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${sciChat}/messages`, { text: 'a\u0000b', client_id: id }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${closedChat}/messages`, { text: 'Почему?', client_id: id }), 409, 'chat_closed')
    err(call('POST', `/api/chats/${closedChat}/typing`), 409, 'chat_closed')
    expect(call('POST', `/api/chats/${sciChat}/typing`).status).toBe(204)
  })

  it('archives, blocks, unblocks and takes complaints', () => {
    const { call } = setup()
    expect(call('POST', `/api/chats/${sciChat}/archive`).status).toBe(204)
    expect(call('POST', `/api/chats/${sciChat}/unarchive`).status).toBe(204)
    ok('block', call('POST', `/api/chats/${sciChat}/block`), 201)
    err(call('POST', `/api/chats/${sciChat}/block`), 409, 'already_blocked')
    err(call('POST', `/api/chats/${orgChat}/block`), 403, 'forbidden')
    ok('blocks-list', call('GET', '/api/chat-blocks'), 200)
    ok('chat', call('GET', `/api/chats/${sciChat}`), 200)
    ok('messages', call('GET', `/api/chats/${sciChat}/messages`), 200)
    expect(call('DELETE', '/api/chat-blocks/org-sibirskiy-institut-kvantovykh-materialov').status).toBe(204)
    err(call('DELETE', '/api/chat-blocks/org-sibirskiy-institut-kvantovykh-materialov'), 404, 'not_found')
    const complaint = { reason: 'spam', note: 'Реклама курсов' }
    expect(requestErrors(file('complaint'), complaint)).toEqual([])
    ok('complaint', call('POST', `/api/chats/${sciChat}/complaints`, complaint), 201)
    err(call('POST', `/api/chats/${sciChat}/complaints`, { reason: 'other' }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${sciChat}/complaints`, { reason: 'boring', note: 1 }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${sciChat}/complaints`, { reason: 'spam', note: 'я'.repeat(2001) }), 422, 'validation_failed')
    err(call('POST', `/api/chats/${orgChat}/complaints`, complaint), 403, 'forbidden')
  })

  it('keeps organization templates', () => {
    const { call } = setup()
    const base = '/api/orgs/org-1/chat-templates'
    ok('templates-list', call('GET', base), 200)
    const created = call('POST', base, { title: 'Документы', text: 'Пришлите диплом.' })
    ok('templates-create', created, 201)
    const id = (created.body as { template: { id: string } }).template.id
    ok('templates-update', call('PATCH', `${base}/${id}`, { title: 'Документы', text: 'Пришлите диплом и список публикаций.' }), 200)
    ok('templates-list', call('GET', base), 200)
    err(call('POST', base, { title: '', text: '' }), 422, 'validation_failed')
    err(call('POST', base, { title: 'я'.repeat(121), text: 'x' }), 422, 'validation_failed')
    err(call('PATCH', `${base}/${id}`, { title: 'x' }), 422, 'validation_failed')
    err(call('PUT', `${base}/${id}`, {}), 404, 'not_found')
    err(call('PUT', base, {}), 404, 'not_found')
    expect(call('DELETE', `${base}/${id}`).status).toBe(204)
    err(call('DELETE', `${base}/${id}`), 404, 'not_found')
    for (let i = 0; i < 50; i++) call('POST', base, { title: `Шаблон ${i}`, text: 'x' })
    err(call('POST', base, { title: 'Лишний', text: 'x' }), 409, 'limit_reached')
  })

  it('streams events in the contract shape and replays after a reconnect', async () => {
    const { call, flush, server } = setup()
    const read = async (stream: ReadableStream<Uint8Array>, done: (text: string) => boolean) => {
      const reader = stream.getReader()
      const dec = new TextDecoder()
      let text = ''
      while (!done(text)) {
        const { value } = await reader.read()
        text += dec.decode(value)
      }
      await reader.cancel()
      return text
        .split('\n\n')
        .filter((block) => block.includes('event: '))
        .map((block) => {
          const line = (p: string) => block.split('\n').find((l) => l.startsWith(p))!.slice(p.length)
          return { id: line('id: '), event: line('event: '), data: JSON.parse(line('data: ')) as unknown }
        })
    }
    const live = call('GET', '/api/chats/events').stream!
    const sent = call('POST', `/api/chats/${sciChat}/messages`, { text: 'Привет', client_id: '1f7c3d2e-8a41-4b0e-9c55-0d3e6f2a7b10' })
    flush()
    server.simulateIncoming()
    flush()
    call('POST', `/api/chats/${sciChat}/read`, { up_to: 99 })
    const seq = (sent.body as { message: { seq: number } }).message.seq
    call('PATCH', `/api/chats/${sciChat}/messages/${seq}`, { text: 'Привет!' })
    call('DELETE', `/api/chats/${sciChat}/messages/${seq}`)
    call('POST', `/api/chats/${sciChat}/block`)
    const events = await read(live, (text) => contractEnums.stream_event.every((kind) => text.includes(`event: ${kind}\n`)))
    const kinds = new Set(events.map((e) => e.event))
    expect(kinds).toEqual(new Set(contractEnums.stream_event))
    for (const e of events) expect(responseErrors(file('events'), e), e.event).toEqual([])
    // После обрыва: события после Last-Event-ID
    const replay = await read(call('GET', '/api/chats/events', undefined, new Headers({ 'Last-Event-ID': events[0].id })).stream!, (text) => text.includes('event: '))
    expect(replay[0].id).toBe(events[1].id)
  })

  it('answers only chat addresses', () => {
    const { server } = setup()
    expect(server.handle({ method: 'GET', url: new URL('http://localhost/api/vacancies'), body: undefined })).toBeNull()
  })
})

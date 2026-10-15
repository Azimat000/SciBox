import { describe, expect, it } from 'vitest'
import { contractEnums, requestErrors, responseErrors, type ContractFile } from '../../test/contract'

// Контракт API чата (contracts/chat, срез 34): примеры согласованы сами с собой, покрывают каждый адрес и каждую
// системную строку. Настоящий сервер (`server/internal/chat`, `contract_test.go`, `live_test.go`) сверяется с теми же файлами.

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

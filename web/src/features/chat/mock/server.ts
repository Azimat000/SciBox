import type { ChatHeader, ChatSummary, LastMessage, Message, Side } from '../api'
import { author, incomingPhrases, MOCK_ORG_ID, orgViewer, scientistViewer } from './data'
import type { MockChat, MockMessage, MockReply, MockRequest, MockState, StreamEvent } from './types'

// Подставной сервер чата в памяти браузера (срез 34, CHAT.md «Подставной сервер»). Отвечает строго по contracts/chat/*.json
// (это проверяет contract.test.ts), сам присылает входящие сообщения, «печатает» и галочки. Удаляется в срезе 39.

const DAY = 24 * 60 * 60 * 1000
const MAX_TEXT = 4000
const MAX_LIMIT = 50
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const HISTORY = 200

export type ServerOptions = {
  state: MockState
  now?: () => Date
  random?: () => number
  /** Отложенное действие («собеседник прочитал», «ответил»); в тестах можно выполнять сразу или не выполнять. */
  later?: (fn: () => void, ms: number) => void
  /** Состояние изменилось: сохранить его. */
  onChange?: (state: MockState) => void
}

const error = (status: number, code: string, message: string, extra: Record<string, unknown> = {}): MockReply => ({
  status,
  body: { error: { code, message, ...extra } },
})
const notFound = () => error(404, 'not_found', 'Такой переписки нет')
const badRequest = (param: string) => error(400, 'bad_request', `Неверный параметр запроса: ${param}`)
const invalid = (fields: Record<string, string>) => error(422, 'validation_failed', 'Проверьте поля', { fields })

/** Ключ листания: время и номер последней строки страницы. */
const encodeCursor = (c: ChatSummary) => btoa(JSON.stringify({ t: c.updated_at, id: c.id })).replace(/=+$/, '')
function decodeCursor(raw: string): { t: string; id: string } | null {
  try {
    const v = JSON.parse(atob(raw)) as unknown
    if (typeof v === 'object' && v !== null && typeof (v as { t?: unknown }).t === 'string' && typeof (v as { id?: unknown }).id === 'string') return v as { t: string; id: string }
  } catch {
    // ниже: неверный ключ
  }
  return null
}

const fold = (s: string) => s.toLocaleLowerCase('ru').replaceAll('ё', 'е')

/** Наружу сообщение уходит без служебного поля from. */
function publicMessage(m: MockMessage): Message {
  const { from: _from, ...rest } = m
  return rest
}

export function createChatServer(opts: ServerOptions) {
  const state = opts.state
  const now = opts.now ?? (() => new Date())
  const random = opts.random ?? Math.random
  const later = opts.later ?? ((fn, ms) => void setTimeout(fn, ms))
  const listeners = new Set<(e: StreamEvent) => void>()
  const history: StreamEvent[] = []
  let eventId = 0

  const changed = () => opts.onChange?.(state)
  const nextId = () => {
    state.next_id += 1
    return `${String(state.next_id).padStart(8, '0')}-0000-4000-8000-000000000000`
  }

  function emit(event: string, data: Record<string, unknown>) {
    eventId += 1
    const e: StreamEvent = { id: eventId, event, data }
    history.push(e)
    if (history.length > HISTORY) history.shift()
    for (const l of listeners) l(e)
  }

  const find = (id: string) => state.chats.find((c) => c.id === id)
  const unreadOf = (c: MockChat) => c.messages.filter((m) => m.from !== c.side && m.seq > c.read_mine && !m.deleted).length

  function unreadCount() {
    const per = (side: Side) => state.chats.filter((c) => c.side === side && !c.archived && unreadOf(c) > 0).length
    const scientist = per('scientist')
    const org = per('org')
    return { total: scientist + org, scientist, org }
  }

  function lastMessage(c: MockChat): LastMessage | null {
    const m = c.messages[c.messages.length - 1]
    if (!m) return null
    return { seq: m.seq, kind: m.kind, author: m.author, text: m.text, event_type: m.event?.type ?? null, deleted: m.deleted, created_at: m.created_at }
  }

  function summary(c: MockChat): ChatSummary {
    return {
      id: c.id,
      side: c.side,
      vacancy: { id: c.vacancy.id, title: c.vacancy.title, org_name: c.vacancy.org_name, org_slug: c.vacancy.org_slug, unit_name: c.vacancy.unit_name },
      counterpart: c.counterpart,
      status: c.status,
      needs_answer: c.needs_answer,
      last_message: lastMessage(c),
      unread: unreadOf(c),
      archived: c.archived,
      can_write: c.can_write,
      closed_reason: c.closed_reason,
      updated_at: c.updated_at,
    }
  }

  function header(c: MockChat): ChatHeader {
    return {
      id: c.id,
      side: c.side,
      vacancy: c.vacancy,
      counterpart: c.counterpart,
      application_id: c.application_id,
      offer_id: c.offer_id,
      status: c.status,
      needs_answer: c.needs_answer,
      can_write: c.can_write,
      closed_reason: c.closed_reason,
      archived: c.archived,
      viewer: c.viewer,
      read: { mine: c.read_mine, theirs: c.read_theirs },
    }
  }

  const byNewest = (a: MockChat, b: MockChat) => (a.updated_at === b.updated_at ? b.id.localeCompare(a.id) : b.updated_at.localeCompare(a.updated_at))

  function list(url: URL): MockReply {
    const q = url.searchParams
    const side = q.get('side')
    if (side !== 'scientist' && side !== 'org') return badRequest('side')
    const box = q.get('box') ?? 'all'
    if (box !== 'all' && box !== 'unread' && box !== 'archive') return badRequest('box')
    const limitRaw = q.get('limit')
    const limit = limitRaw === null ? 30 : Number(limitRaw)
    if (!Number.isInteger(limit) || limit < 1 || limit > MAX_LIMIT) return badRequest('limit')
    const words = fold((q.get('q') ?? '').trim())
    if (words.length > 100) return badRequest('q')
    const vacancy = side === 'org' ? (q.get('vacancy') ?? '') : ''
    const rawCursor = q.get('cursor')
    const cursor = rawCursor === null ? null : decodeCursor(rawCursor)
    if (rawCursor !== null && cursor === null) return badRequest('cursor')

    const mine = state.chats.filter((c) => c.side === side)
    const picked = mine
      .filter((c) => (box === 'archive' ? c.archived : !c.archived && (box === 'all' || unreadOf(c) > 0)))
      .filter((c) => !vacancy || c.vacancy.id === vacancy)
      .filter((c) => !words || fold(c.vacancy.title).includes(words) || (!c.counterpart.anonymous && fold(c.counterpart.name).includes(words)))
      .sort(byNewest)
      .filter((c) => !cursor || c.updated_at < cursor.t || (c.updated_at === cursor.t && c.id < cursor.id))
    const page = picked.slice(0, limit).map(summary)
    const more = picked.length > limit
    return {
      status: 200,
      body: {
        items: page,
        next_cursor: more ? encodeCursor(page[page.length - 1]) : null,
        counts: { unread: mine.filter((c) => !c.archived && unreadOf(c) > 0).length, archive: mine.filter((c) => c.archived).length },
      },
    }
  }

  function vacancies(): MockReply {
    const groups = new Map<string, { id: string; title: string; org_name: string; unit_name: string; chats: number }>()
    for (const c of state.chats.filter((x) => x.side === 'org')) {
      const g = groups.get(c.vacancy.id) ?? { id: c.vacancy.id, title: c.vacancy.title, org_name: c.vacancy.org_name, unit_name: c.vacancy.unit_name, chats: 0 }
      g.chats += 1
      groups.set(c.vacancy.id, g)
    }
    return { status: 200, body: { items: [...groups.values()].sort((a, b) => a.title.localeCompare(b.title, 'ru')) } }
  }

  function messages(c: MockChat, url: URL): MockReply {
    const q = url.searchParams
    const num = (name: string) => {
      const raw = q.get(name)
      if (raw === null) return undefined
      const n = Number(raw)
      return Number.isInteger(n) && n >= 0 ? n : null
    }
    const before = num('before')
    const after = num('after')
    const limit = num('limit') ?? 50
    if (before === null) return badRequest('before')
    if (after === null) return badRequest('after')
    if (limit === null || limit < 1 || limit > 100) return badRequest('limit')
    let items: MockMessage[]
    let more: boolean
    if (after !== undefined) {
      const newer = c.messages.filter((m) => m.seq > after)
      items = newer.slice(0, limit)
      more = newer.length > limit
    } else {
      const older = c.messages.filter((m) => before === undefined || m.seq < before)
      items = older.slice(-limit)
      more = older.length > limit
    }
    return { status: 200, body: { items: items.map(publicMessage), has_more: more } }
  }

  function checkText(raw: unknown): string | Record<string, string> {
    if (typeof raw !== 'string') return { text: 'Напишите сообщение' }
    const text = raw.trim()
    if (!text) return { text: 'Напишите сообщение' }
    if (text.includes('\u0000')) return { text: 'В сообщении недопустимые знаки' }
    if ([...text].length > MAX_TEXT) return { text: `Сообщение не длиннее ${MAX_TEXT} знаков` }
    return text
  }

  const closed = () => error(409, 'chat_closed', 'Переписка закрыта, отправить сообщение нельзя')

  function touch(c: MockChat, at: string) {
    c.updated_at = at
    c.archived = false
  }

  function push(c: MockChat, m: Omit<MockMessage, 'seq' | 'id'>): MockMessage {
    const full: MockMessage = { ...m, id: nextId(), seq: (c.messages[c.messages.length - 1]?.seq ?? 0) + 1 }
    c.messages.push(full)
    touch(c, full.created_at)
    return full
  }

  function send(c: MockChat, body: unknown): MockReply {
    const b = (body ?? {}) as { text?: unknown; client_id?: unknown }
    if (typeof b.client_id !== 'string' || !UUID.test(b.client_id)) return badRequest('client_id')
    const repeat = c.messages.find((m) => m.client_id === b.client_id)
    if (repeat) return { status: 200, body: { message: publicMessage(repeat) } }
    if (!c.can_write) return closed()
    const text = checkText(b.text)
    if (typeof text !== 'string') return invalid(text)
    const at = now()
    const m = push(c, {
      kind: 'text',
      author: c.side === 'org' ? author('org', 'Вы', true) : author('scientist', 'Вы', true),
      text,
      event: null,
      created_at: at.toISOString(),
      edited_at: null,
      deleted: false,
      editable_until: new Date(at.getTime() + DAY).toISOString(),
      client_id: b.client_id,
      from: c.side,
    })
    c.read_mine = m.seq
    changed()
    emit('message.created', { chat_id: c.id, side: c.side, message: publicMessage(m) })
    // Собеседник прочитал через пару секунд и иногда отвечает.
    later(() => {
      c.read_theirs = Math.max(c.read_theirs, m.seq)
      changed()
      emit('chat.read', { chat_id: c.id, side: c.side, reader_side: other(c.side), up_to: m.seq })
    }, 2000)
    if (random() < 0.5) later(() => incoming(c), 6000)
    return { status: 201, body: { message: publicMessage(m) } }
  }

  function own(c: MockChat, seq: number): MockMessage | MockReply {
    const m = c.messages.find((x) => x.seq === seq)
    if (!m) return error(404, 'not_found', 'Такого сообщения нет')
    if (m.kind === 'system') return error(409, 'system_message', 'Это сообщение нельзя изменить')
    if (!m.author?.is_me) return error(403, 'not_author', 'Изменить можно только своё сообщение')
    if (!c.can_write) return closed()
    if (m.deleted || now().getTime() > new Date(m.created_at).getTime() + DAY) {
      return error(409, 'edit_window_closed', 'Сообщение можно исправить или удалить только в течение суток')
    }
    return m
  }

  function edit(c: MockChat, seq: number, body: unknown): MockReply {
    const m = own(c, seq)
    if (!('seq' in m)) return m
    const text = checkText((body as { text?: unknown } | null)?.text)
    if (typeof text !== 'string') return invalid(text)
    m.text = text
    m.edited_at = now().toISOString()
    changed()
    emit('message.edited', { chat_id: c.id, side: c.side, message: publicMessage(m) })
    return { status: 200, body: { message: publicMessage(m) } }
  }

  function remove(c: MockChat, seq: number): MockReply {
    const m = own(c, seq)
    if (!('seq' in m)) return m
    m.deleted = true
    m.text = ''
    m.editable_until = null
    changed()
    emit('message.deleted', { chat_id: c.id, side: c.side, message: publicMessage(m) })
    return { status: 204 }
  }

  function read(c: MockChat, body: unknown): MockReply {
    const upTo = (body as { up_to?: unknown } | null)?.up_to
    if (typeof upTo !== 'number' || !Number.isInteger(upTo) || upTo < 0) return badRequest('up_to')
    if (upTo > c.read_mine) {
      c.read_mine = Math.min(upTo, c.messages[c.messages.length - 1]?.seq ?? 0)
      changed()
      emit('unread.count', unreadCount())
    }
    return { status: 204 }
  }

  const other = (side: Side): Side => (side === 'org' ? 'scientist' : 'org')

  /** Собеседник «печатает» и через пару секунд присылает сообщение. */
  function incoming(c: MockChat) {
    if (!c.can_write) return
    const from = other(c.side)
    const who =
      from === 'org'
        ? author('org', 'Елена Орлова', false)
        : author('scientist', c.counterpart.name, false, { anonymous: c.counterpart.anonymous })
    emit('chat.typing', { chat_id: c.id, side: c.side, author: { side: who.side, name: who.name, anonymous: who.anonymous } })
    later(() => {
      if (!c.can_write) return
      const phrases = incomingPhrases[from]
      const m = push(c, {
        kind: 'text',
        author: who,
        text: phrases[Math.floor(random() * phrases.length) % phrases.length],
        event: null,
        created_at: now().toISOString(),
        edited_at: null,
        deleted: false,
        editable_until: null,
        client_id: null,
        from,
      })
      changed()
      emit('message.created', { chat_id: c.id, side: c.side, message: publicMessage(m) })
      emit('unread.count', unreadCount())
    }, 2500)
  }

  /** Случайная открытая переписка получает входящее сообщение (зовёт index.ts по таймеру). */
  function simulateIncoming() {
    const open = state.chats.filter((c) => c.can_write && !c.archived)
    if (open.length === 0) return
    incoming(open[Math.floor(random() * open.length) % open.length])
  }

  function setArchived(c: MockChat, archived: boolean): MockReply {
    if (c.archived !== archived) {
      c.archived = archived
      changed()
      emit('unread.count', unreadCount())
    }
    return { status: 204 }
  }

  function typing(c: MockChat): MockReply {
    return c.can_write ? { status: 204 } : closed()
  }

  // Шаблоны организации
  type Checked = { ok: true; title: string; text: string } | { ok: false; errors: Record<string, string> }
  function templateFields(b: unknown): Checked {
    const { title, text } = (b ?? {}) as { title?: unknown; text?: unknown }
    const errors: Record<string, string> = {}
    if (typeof title !== 'string' || !title.trim()) errors.title = 'Назовите шаблон'
    else if ([...title.trim()].length > 120) errors.title = 'Название не длиннее 120 знаков'
    const checked = checkText(text)
    if (typeof checked !== 'string') Object.assign(errors, checked)
    if (Object.keys(errors).length > 0) return { ok: false, errors }
    return { ok: true, title: (title as string).trim(), text: checked as string }
  }

  function templates(method: string, _orgId: string, id: string | undefined, body: unknown): MockReply {
    // Одна общая корзина на всех: у демо-организации номер из шапки переписки один, у страницы правки (настоящий номер из
    // демо-данных сайта) другой, а шаблоны должны быть теми же. Настоящий сервер различает организации по номеру.
    const list = (state.templates[MOCK_ORG_ID] ??= [])
    if (id === undefined) {
      if (method === 'GET') return { status: 200, body: { items: [...list].sort((a, b) => a.title.localeCompare(b.title, 'ru')), can_edit: true, limit: 50 } }
      if (method !== 'POST') return notFound()
      if (list.length >= 50) return error(409, 'limit_reached', 'У организации уже 50 шаблонов')
      const f = templateFields(body)
      if (!f.ok) return invalid(f.errors)
      const tpl = { id: nextId(), title: f.title, text: f.text, updated_at: now().toISOString() }
      list.push(tpl)
      changed()
      return { status: 201, body: { template: tpl } }
    }
    const tpl = list.find((x) => x.id === id)
    if (!tpl) return notFound()
    if (method === 'DELETE') {
      list.splice(list.indexOf(tpl), 1)
      changed()
      return { status: 204 }
    }
    if (method !== 'PATCH') return notFound()
    const f = templateFields(body)
    if (!f.ok) return invalid(f.errors)
    Object.assign(tpl, { title: f.title, text: f.text, updated_at: now().toISOString() })
    changed()
    return { status: 200, body: { template: tpl } }
  }

  const orgIdOf = (c: MockChat) => `org-${c.vacancy.org_slug}`

  function block(c: MockChat): MockReply {
    if (c.side !== 'scientist') return error(403, 'forbidden', 'Недостаточно прав')
    const orgId = orgIdOf(c)
    if (state.blocks.some((b) => b.org_id === orgId)) return error(409, 'already_blocked', 'Эта организация уже заблокирована')
    const at = now().toISOString()
    const b = { org_id: orgId, org_name: c.vacancy.org_name, org_slug: c.vacancy.org_slug, created_at: at }
    state.blocks.unshift(b)
    for (const x of state.chats.filter((y) => y.side === 'scientist' && orgIdOf(y) === orgId)) {
      if (x.can_write) {
        x.can_write = false
        x.closed_reason = 'blocked'
      }
      line(x, 'chat.blocked', {}, 'scientist', at)
      patchState(x, {})
    }
    changed()
    return { status: 201, body: { block: b } }
  }

  function unblock(orgId: string): MockReply {
    const i = state.blocks.findIndex((b) => b.org_id === orgId)
    if (i < 0) return notFound()
    state.blocks.splice(i, 1)
    const at = now().toISOString()
    for (const x of state.chats.filter((y) => y.side === 'scientist' && orgIdOf(y) === orgId)) {
      if (x.closed_reason === 'blocked') {
        x.can_write = true
        x.closed_reason = null
      }
      line(x, 'chat.unblocked', {}, 'scientist', at)
      patchState(x, {})
    }
    changed()
    return { status: 204 }
  }

  function complain(c: MockChat, body: unknown): MockReply {
    if (c.side !== 'scientist') return error(403, 'forbidden', 'Недостаточно прав')
    const { reason, note } = (body ?? {}) as { reason?: unknown; note?: unknown }
    const errs: Record<string, string> = {}
    if (reason !== 'spam' && reason !== 'fraud' && reason !== 'abuse' && reason !== 'other') errs.reason = 'Выберите причину'
    const n = typeof note === 'string' ? note.trim() : ''
    if (note !== undefined && typeof note !== 'string') errs.note = 'Пояснение должно быть текстом'
    else if ([...n].length > 2000) errs.note = 'Пояснение не длиннее 2000 знаков'
    else if (reason === 'other' && !n) errs.note = 'Опишите, что случилось'
    if (Object.keys(errs).length > 0) return invalid(errs)
    const complaint = { id: nextId(), chat_id: c.id, reason: reason as string, note: n, created_at: now().toISOString() }
    state.complaints.push(complaint)
    changed()
    return { status: 201, body: { complaint: { id: complaint.id, created_at: complaint.created_at } } }
  }

  /** Системная строка в переписку: у того, чьё это действие, она прочитана, у другой стороны нет. Приходит и событием потока. */
  function line(c: MockChat, type: string, data: Record<string, unknown>, from: Side, at: string = now().toISOString()): MockMessage {
    const m = push(c, systemLine(type, data, from, at))
    if (from === c.side) c.read_mine = m.seq
    changed()
    emit('message.created', { chat_id: c.id, side: c.side, message: publicMessage(m) })
    emit('unread.count', unreadCount())
    return m
  }

  /** Состояние переписки изменилось (статус, нужен ли ответ, можно ли писать): шапка и списки обновятся по событию. */
  function patchState(c: MockChat, patch: Partial<Pick<MockChat, 'status' | 'needs_answer' | 'can_write' | 'closed_reason'>>) {
    Object.assign(c, patch)
    changed()
    emit('chat.state', { chat_id: c.id, side: c.side, status: c.status, needs_answer: c.needs_answer, can_write: c.can_write, closed_reason: c.closed_reason })
  }

  function systemLine(type: string, data: Record<string, unknown>, from: Side, at: string): Omit<MockMessage, 'seq' | 'id'> {
    return { kind: 'system', author: null, text: '', event: { type, data }, created_at: at, edited_at: null, deleted: false, editable_until: null, client_id: null, from }
  }

  /** Живой поток: события после Last-Event-ID, затем новые; каждые 25 секунд строка-комментарий. */
  function stream(lastEventId: string | null): MockReply {
    const enc = new TextEncoder()
    let stop = () => {}
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        const write = (e: StreamEvent) => controller.enqueue(enc.encode(`id: ${e.id}\nevent: ${e.event}\ndata: ${JSON.stringify(e.data)}\n\n`))
        controller.enqueue(enc.encode(': connected\n\n'))
        const last = lastEventId === null ? null : Number(lastEventId)
        if (last !== null && Number.isFinite(last)) {
          const missed = history.filter((e) => e.id > last)
          // Пропущено больше, чем помнит сервер: пусть сайт перечитает списки.
          if (history.length > 0 && history[0].id > last + 1) write({ id: eventId, event: 'unread.count', data: unreadCount() })
          else missed.forEach(write)
        }
        listeners.add(write)
        const ping = setInterval(() => controller.enqueue(enc.encode(': ping\n\n')), 25_000)
        stop = () => {
          listeners.delete(write)
          clearInterval(ping)
        }
      },
      cancel() {
        stop()
      },
    })
    return { status: 200, stream: body }
  }

  /** Ответ на запрос к адресам чата; null — адрес не чата (его обслуживает настоящий сервер). */
  function handle(req: MockRequest): MockReply | null {
    const { method, url, body } = req
    const path = url.pathname
    const tpl = /^\/api\/orgs\/([^/]+)\/chat-templates(?:\/([^/]+))?$/.exec(path)
    if (tpl) return templates(method, decodeURIComponent(tpl[1]), tpl[2] === undefined ? undefined : decodeURIComponent(tpl[2]), body)
    if (path === '/api/chat-blocks' && method === 'GET') return { status: 200, body: { items: state.blocks } }
    const unb = /^\/api\/chat-blocks\/([^/]+)$/.exec(path)
    if (unb && method === 'DELETE') return unblock(decodeURIComponent(unb[1]))
    if (!path.startsWith('/api/chats')) return null
    if (path === '/api/chats' && method === 'GET') return list(url)
    if (path === '/api/chats/unread-count' && method === 'GET') return { status: 200, body: unreadCount() }
    if (path === '/api/chats/vacancies' && method === 'GET') return vacancies()
    if (path === '/api/chats/events' && method === 'GET') return stream(req.headers?.get('Last-Event-ID') ?? null)
    const m = /^\/api\/chats\/([^/]+)(\/.*)?$/.exec(path)
    const c = m ? find(decodeURIComponent(m[1])) : undefined
    if (!m || !c) return notFound()
    const rest = m[2] ?? ''
    const seqMatch = /^\/messages\/(\d+)$/.exec(rest)
    if (rest === '' && method === 'GET') return { status: 200, body: { chat: header(c) } }
    if (rest === '/messages' && method === 'GET') return messages(c, url)
    if (rest === '/messages' && method === 'POST') return send(c, body)
    if (seqMatch && method === 'PATCH') return edit(c, Number(seqMatch[1]), body)
    if (seqMatch && method === 'DELETE') return remove(c, Number(seqMatch[1]))
    if (rest === '/read' && method === 'POST') return read(c, body)
    if (rest === '/archive' && method === 'POST') return setArchived(c, true)
    if (rest === '/unarchive' && method === 'POST') return setArchived(c, false)
    if (rest === '/typing' && method === 'POST') return typing(c)
    if (rest === '/block' && method === 'POST') return block(c)
    if (rest === '/complaints' && method === 'POST') return complain(c, body)
    return notFound()
  }

  /** Переписка для настоящего отклика или приглашения: находит или заводит её (со строкой о начале). */
  function linked(link: string, make: () => Omit<MockChat, 'messages' | 'read_mine' | 'read_theirs' | 'updated_at' | 'link'> & { first: { type: string; data: Record<string, unknown>; from: Side; at: string } }) {
    const existing = state.chats.find((c) => c.link === link)
    if (existing) return existing
    const { first, ...fields } = make()
    const c: MockChat = { ...fields, link, messages: [], read_mine: 0, read_theirs: 0, updated_at: first.at }
    push(c, systemLine(first.type, first.data, first.from, first.at))
    c.read_mine = 1
    c.read_theirs = 1
    c.archived = false
    state.chats.push(c)
    changed()
    return c
  }

  const chatRef = (c: MockChat) => ({
    id: c.id,
    unread: unreadOf(c),
    can_write: c.can_write,
    started: c.messages.some((m) => m.kind === 'text'),
  })

  return { state, handle, simulateIncoming, linked, chatRef, nextId, line, patchState, viewers: { scientist: scientistViewer, org: orgViewer } }
}

export type ChatServer = ReturnType<typeof createChatServer>

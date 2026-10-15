import type { ChatStreamEvent } from './api'

// Клиент живого потока чата (срез 35, contracts/chat/events.json). EventSource подменить нельзя (подставной сервер
// подменяет fetch), поэтому поток читается через fetch и ReadableStream; формат тот же: Server-Sent Events.

export const STREAM_PATH = '/api/chats/events'

const KNOWN = new Set(['message.created', 'message.edited', 'message.deleted', 'chat.read', 'chat.typing', 'chat.state', 'unread.count'])

export type RawEvent = { id: string | null; event: string; data: string }

/** Разбирает накопленный текст потока: целые события и недочитанный хвост. Комментарии («: ping») пропускаются. */
export function parseSse(buffer: string): { events: RawEvent[]; rest: string } {
  const text = buffer.replaceAll('\r\n', '\n')
  const cut = text.lastIndexOf('\n\n')
  if (cut < 0) return { events: [], rest: text }
  const events: RawEvent[] = []
  for (const block of text.slice(0, cut).split('\n\n')) {
    let id: string | null = null
    let event = 'message'
    const data: string[] = []
    for (const line of block.split('\n')) {
      if (line === '' || line.startsWith(':')) continue
      const i = line.indexOf(':')
      const field = i < 0 ? line : line.slice(0, i)
      const value = i < 0 ? '' : line.slice(i + 1).replace(/^ /, '')
      if (field === 'id') id = value
      else if (field === 'event') event = value
      else if (field === 'data') data.push(value)
    }
    if (data.length > 0) events.push({ id, event, data: data.join('\n') })
  }
  return { events, rest: text.slice(cut + 2) }
}

/** Событие потока в событие сайта; неизвестный вид и битый JSON пропускаются (сервер может быть новее сайта). */
export function toStreamEvent(raw: RawEvent): ChatStreamEvent | null {
  if (!KNOWN.has(raw.event)) return null
  let data: unknown
  try {
    data = JSON.parse(raw.data)
  } catch {
    return null
  }
  if (typeof data !== 'object' || data === null || Array.isArray(data)) return null
  return { type: raw.event, ...(data as object) } as ChatStreamEvent
}

export type StreamEnd = 'closed' | 'gone' | 'denied' | 'error'

/** Одно соединение: читает поток до конца. Ответ не 2xx различается по статусу: 404 (нет такого адреса) и 401/403 повтору не подлежат. */
export async function readStream(opts: {
  signal: AbortSignal
  lastEventId: string | null
  onOpen: () => void
  onEvent: (e: ChatStreamEvent, id: string | null) => void
}): Promise<StreamEnd> {
  const headers: Record<string, string> = { Accept: 'text/event-stream' }
  if (opts.lastEventId !== null) headers['Last-Event-ID'] = opts.lastEventId
  let res: Response
  try {
    res = await fetch(STREAM_PATH, { headers, signal: opts.signal, cache: 'no-store' })
  } catch {
    return 'error'
  }
  if (res.status === 404) return 'gone'
  if (res.status === 401 || res.status === 403) return 'denied'
  if (!res.ok || !res.body) return 'error'
  opts.onOpen()
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) return 'closed'
      const parsed = parseSse(buffer + decoder.decode(value, { stream: true }))
      buffer = parsed.rest
      for (const raw of parsed.events) {
        const e = toStreamEvent(raw)
        if (e) opts.onEvent(e, raw.id)
      }
    }
  } catch {
    return 'error'
  }
}

export const BACKOFF_MIN_MS = 1000
export const BACKOFF_MAX_MS = 30_000

/** Пауза перед повтором: 1, 2, 4… секунд до 30, с разбросом, чтобы вкладки не стучались разом. */
export function backoff(attempt: number, random: () => number = Math.random): number {
  const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** attempt)
  return Math.round(base * (0.75 + random() * 0.5))
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) return resolve()
    const timer = setTimeout(done, ms)
    function done() {
      clearTimeout(timer)
      signal.removeEventListener('abort', done)
      resolve()
    }
    signal.addEventListener('abort', done)
  })
}

/**
 * Держит поток открытым, пока не отменят: после обрыва переподключается с Last-Event-ID (пропущенное сервер досылает).
 * Каждое успешное подключение сообщает onConnected: окно дочитывает то, что могло пройти мимо (сервер помнит не всё).
 * Нет такого адреса (404) или нет доступа: повторять нечего, тихо выходим.
 */
export async function runStream(opts: {
  signal: AbortSignal
  onEvent: (e: ChatStreamEvent) => void
  onConnected: () => void
  random?: () => number
}): Promise<void> {
  let lastId: string | null = null
  let attempt = 0
  while (!opts.signal.aborted) {
    let opened = false
    const end = await readStream({
      signal: opts.signal,
      lastEventId: lastId,
      onOpen: () => {
        opened = true
        opts.onConnected()
      },
      onEvent: (e, id) => {
        if (id !== null) lastId = id
        opts.onEvent(e)
      },
    })
    if (opts.signal.aborted || end === 'gone' || end === 'denied') return
    attempt = opened ? 0 : attempt + 1
    await sleep(opened ? BACKOFF_MIN_MS : backoff(attempt, opts.random), opts.signal)
  }
}

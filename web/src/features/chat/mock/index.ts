import { mirror, parseAction, tryDemo } from './actions'
import { demoState } from './data'
import { decorate } from './decorate'
import { createChatServer, type ChatServer } from './server'
import { mountDataToggle } from './toggle'
import type { MockReply, MockState } from './types'
import { dataset, type Dataset } from './worst'

// Подключение подставного сервера чата к сайту в разработке: VITE_CHAT_MOCK=1 make dev (main.tsx грузит этот модуль
// динамическим импортом только при import.meta.env.DEV, поэтому в боевую сборку он не попадает: T-104, тест build.test.ts).
// Адреса чата отвечает подставной сервер, ответы откликов и приглашений получают поле chat, остальное идёт на настоящий сервер.

export { datasetFromUrl } from './toggle'

/** Метка модуля: по ней тест сборки ищет подставной сервер в боевых файлах. */
export const CHAT_MOCK_MARKER = 'scibox-chat-mock-v1'

const STORAGE_KEY = 'scibox.chatMock.v1'

function load(storage: Storage): MockState | null {
  try {
    const raw = storage.getItem(STORAGE_KEY)
    if (!raw) return null
    const state = JSON.parse(raw) as MockState
    return state.version === 1 && Array.isArray(state.chats) ? state : null
  } catch {
    return null
  }
}

function save(storage: Storage, state: MockState) {
  try {
    storage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // Хранилище браузера недоступно: подставной сервер работает до перезагрузки страницы.
  }
}

export function toResponse(reply: MockReply): Response {
  if (reply.stream) return new Response(reply.stream, { status: reply.status, headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' } })
  if (reply.body === undefined) return new Response(null, { status: reply.status })
  return new Response(JSON.stringify(reply.body), { status: reply.status, headers: { 'Content-Type': 'application/json' } })
}

/** Заголовки настоящего ответа без длины и сжатия: тело собрано заново. */
function jsonHeaders(res: Response) {
  const h = new Headers(res.headers)
  h.delete('Content-Length')
  h.delete('Content-Encoding')
  return h
}

function bodyOf(init?: RequestInit): unknown {
  if (typeof init?.body !== 'string') return undefined
  try {
    return JSON.parse(init.body) as unknown
  } catch {
    return undefined
  }
}

type InstallOptions = {
  target?: { fetch: typeof fetch; location: { origin: string } }
  storage?: Storage
  /** Задержка ответа, чтобы были видны состояния загрузки. */
  delay?: () => number
  /** Как часто «собеседник» пишет сам; 0 — не писать. */
  incomingEveryMs?: () => number
  /** Набор данных (break-ui): не «демо» живёт только до перезагрузки и в браузере не сохраняется. */
  data?: Dataset
}

/** Подменяет fetch: адреса чата отвечает подставной сервер. Возвращает сервер и функцию снятия подмены. */
export function installChatMock(opts: InstallOptions = {}): { server: ChatServer; uninstall: () => void } {
  const target = opts.target ?? window
  const storage = opts.storage ?? window.localStorage
  const delay = opts.delay ?? (() => 150 + Math.random() * 250)
  const every = opts.incomingEveryMs ?? (() => 40_000 + Math.random() * 30_000)
  const data = opts.data ?? 'demo'
  const saved = data === 'demo' ? load(storage) : null
  const state = data === 'demo' ? (saved ?? demoState(new Date())) : dataset(data, new Date())
  const server = createChatServer({ state, onChange: (s) => data === 'demo' && save(storage, s) })
  if (data === 'demo' && !saved) save(storage, server.state)

  const original = target.fetch
  const realFetch = original.bind(target)
  const wait = () => new Promise<void>((resolve) => setTimeout(resolve, delay()))

  const mocked: typeof fetch = async (input, init) => {
    const href = input instanceof Request ? input.url : String(input)
    const url = new URL(href, target.location.origin)
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    const reply = server.handle({ method, url, body: bodyOf(init), headers: new Headers(init?.headers) })
    if (reply) {
      await wait()
      return toResponse(reply)
    }
    if (url.pathname === '/api/notification-settings') return settings(method, input, init)
    const action = parseAction(method, url.pathname)
    if (action) {
      // Демо-запись: отвечает подставной сервер; настоящая: настоящий сервер, затем строки в переписку (actions.ts)
      const demo = tryDemo(server, action, bodyOf(init))
      if (demo) {
        await wait()
        return toResponse(demo)
      }
      const real = await realFetch(input, init)
      if (real.ok) {
        const payload = real.headers.get('Content-Type')?.includes('json') ? ((await real.clone().json().catch(() => undefined)) as unknown) : undefined
        mirror(server, method, url.pathname, bodyOf(init), payload)
      }
      return real
    }
    const res = await realFetch(input, init)
    if (!res.ok || method !== 'GET' || !res.headers.get('Content-Type')?.includes('json')) return res
    const json = (await res.json()) as unknown
    return new Response(JSON.stringify(decorate(server, method, url.pathname, json)), { status: res.status, headers: jsonHeaders(res) })
  }

  /** Настройки писем: email_messages хранит подставной сервер, остальное настоящий. */
  async function settings(method: string, input: RequestInfo | URL, init?: RequestInit) {
    let sendInit = init
    if (method === 'PUT') {
      const { email_messages: wanted, ...rest } = (bodyOf(init) ?? {}) as { email_messages?: unknown }
      if (typeof wanted === 'boolean') server.state.email_messages = wanted
      save(storage, server.state)
      sendInit = { ...init, body: JSON.stringify(rest) }
    }
    const res = await realFetch(input, sendInit)
    if (!res.ok) return res
    const json = (await res.json()) as Record<string, unknown>
    return new Response(JSON.stringify({ ...json, email_messages: server.state.email_messages }), { status: res.status, headers: jsonHeaders(res) })
  }

  target.fetch = mocked
  let timer: ReturnType<typeof setTimeout> | undefined
  const loop = () => {
    const ms = every()
    if (ms <= 0) return
    timer = setTimeout(() => {
      server.simulateIncoming()
      loop()
    }, ms)
  }
  loop()

  return {
    server,
    uninstall: () => {
      clearTimeout(timer)
      target.fetch = original
    },
  }
}

/**
 * Для консоли браузера: window.__chatMock.reset() возвращает демо-переписки, .incoming() присылает сообщение сейчас.
 * Внизу экрана переключатель наборов данных: демо, худший случай, пусто, 1000 строк (?data=…).
 */
export function exposeForConsole(server: ChatServer, storage: Storage = window.localStorage, data: Dataset = 'demo') {
  mountDataToggle(data)
  Object.assign(window, {
    __chatMock: {
      marker: CHAT_MOCK_MARKER,
      server,
      incoming: () => server.simulateIncoming(),
      reset: () => {
        storage.removeItem(STORAGE_KEY)
        window.location.reload()
      },
    },
  })
}

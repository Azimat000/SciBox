import { vi } from 'vitest'

/** Что ответит подставной сервер: статус и тело JSON. */
export type Reply = { status: number; body?: unknown }
export type Call = { method: string; path: string; body: unknown }
export type Route = Reply | ((call: Call) => Reply | Promise<Reply>)

export const reply = (status: number, body?: unknown): Reply => ({ status, body })

/** Ответ сервера в формате ошибок API. */
export const apiError = (status: number, code: string, message: string, extra: Record<string, unknown> = {}): Reply =>
  reply(status, { error: { code, message, ...extra } })

/**
 * Подставляет fetch, который отвечает по таблице «МЕТОД /адрес» → ответ.
 * По умолчанию никто не вошёл. Адрес, которого в таблице нет, возвращает 404: тест сразу видит лишний запрос.
 * Возвращает список сделанных запросов.
 */
export function stubApi(routes: Record<string, Route> = {}) {
  const calls: Call[] = []
  const table: Record<string, Route> = { 'GET /api/auth/me': reply(200, { user: null }), ...routes }
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const method = init?.method ?? 'GET'
    const call: Call = { method, path, body: init?.body ? JSON.parse(String(init.body)) : undefined }
    calls.push(call)
    const key = `${method} ${path}`
    // Ключ, оканчивающийся на «*», отвечает на любой адрес с таким началом (поиск с любыми параметрами).
    const route = table[key] ?? Object.entries(table).find(([k]) => k.endsWith('*') && key.startsWith(k.slice(0, -1)))?.[1]
    if (!route) return respond(apiError(404, 'not_found', `в тесте нет ответа для ${method} ${path}`))
    return respond(typeof route === 'function' ? await route(call) : route)
  })
  vi.stubGlobal('fetch', fn)
  return { calls, fetch: fn, called: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path) }
}

function respond(r: Reply): Response {
  if (r.status === 204 || r.body === undefined) return new Response(null, { status: r.status })
  return new Response(JSON.stringify(r.body), { status: r.status, headers: { 'Content-Type': 'application/json' } })
}

export const ann = {
  id: '11111111-1111-1111-1111-111111111111',
  email: 'anna@example.ru',
  name: 'Анна Смирнова',
  email_confirmed: true,
  created_at: '2026-10-02T12:00:00Z',
}

/** Состояние «Анна уже вошла»: подставляется в stubApi. */
export const signedInAs = (user = ann): Record<string, Route> => ({ 'GET /api/auth/me': reply(200, { user }) })

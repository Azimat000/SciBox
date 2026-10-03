import { t } from '../i18n'

// Ответ сервера с ошибкой в едином формате (D-032).
type ErrorBody = {
  error: { code: string; message: string; fields?: Record<string, string>; retry_after?: number }
}

/** Код, когда сервер недоступен или ответил не в формате API. */
export const UNREACHABLE = 'unreachable'
/** Код, когда сервер ответил успешно, но тело не JSON. */
export const BAD_RESPONSE = 'bad_response'

type ApiErrorExtra = { fields?: Record<string, string>; retryAfter?: number }

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  /** Что не так в каждом поле формы (ответ 422): имя поля → текст. */
  readonly fields: Record<string, string>
  /** Через сколько секунд можно повторить (ответ 429), если сервер сказал. */
  readonly retryAfter: number | undefined

  constructor(status: number, code: string, message: string, extra: ApiErrorExtra = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.fields = extra.fields ?? {}
    this.retryAfter = extra.retryAfter
  }
}

function isErrorBody(v: unknown): v is ErrorBody {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false
  const e = (v as { error: unknown }).error
  return typeof e === 'object' && e !== null && typeof (e as { code?: unknown }).code === 'string' && typeof (e as { message?: unknown }).message === 'string'
}

async function readJSON(res: Response): Promise<unknown> {
  try {
    return await res.json()
  } catch {
    return undefined
  }
}

function fieldsOf(e: ErrorBody['error']): Record<string, string> | undefined {
  if (typeof e.fields !== 'object' || e.fields === null) return undefined
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(e.fields)) if (typeof v === 'string') out[k] = v
  return out
}

type Options = { method: string; signal?: AbortSignal; body?: unknown; form?: FormData }

async function request<T>(path: string, { method, signal, body, form }: Options, allowEmpty: boolean): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const init: RequestInit = { headers, signal, ...(method === 'GET' ? {} : { method }) }
  if (form) {
    // Тип с границей частей выставляет сам браузер.
    init.body = form
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError(0, UNREACHABLE, t.api.unreachable)
  }
  if (res.ok && allowEmpty && res.status === 204) return undefined as T
  const data = await readJSON(res)
  if (res.ok) {
    if (data === undefined) throw new ApiError(res.status, BAD_RESPONSE, t.api.unknown)
    return data as T
  }
  if (isErrorBody(data)) {
    throw new ApiError(res.status, data.error.code, data.error.message, {
      fields: fieldsOf(data.error),
      retryAfter: typeof data.error.retry_after === 'number' ? data.error.retry_after : undefined,
    })
  }
  // Не наш формат: так отвечает прокси разработки, когда сервер не запущен.
  throw new ApiError(res.status, UNREACHABLE, t.api.unreachable)
}

/** GET-запрос к API. Любая неудача превращается в ApiError. */
export function apiGet<T>(path: string, init?: { signal?: AbortSignal }): Promise<T> {
  return request<T>(path, { method: 'GET', signal: init?.signal }, false)
}

/** Запрос, меняющий данные (POST, PATCH), с телом JSON. Ответ 204 (без тела) даёт undefined. */
export function apiSend<T = void>(method: 'POST' | 'PATCH' | 'PUT' | 'DELETE', path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method, body: body ?? {} }, true)
}

/** Запрос с файлами (multipart): поля одним JSON в части `data`, файлы частями `file`. Ответ 204 даёт undefined. */
export function apiSendForm<T = void>(method: 'POST' | 'PUT', path: string, data: unknown, files: readonly File[] = []): Promise<T> {
  const form = new FormData()
  form.append('data', JSON.stringify(data))
  for (const f of files) form.append('file', f, f.name)
  return request<T>(path, { method, form }, true)
}

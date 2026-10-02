import { t } from '../i18n'

// Ответ сервера с ошибкой в едином формате (D-032).
type ErrorBody = { error: { code: string; message: string } }

/** Код, когда сервер недоступен или ответил не в формате API. */
export const UNREACHABLE = 'unreachable'
/** Код, когда сервер ответил успешно, но тело не JSON. */
export const BAD_RESPONSE = 'bad_response'

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

function isErrorBody(v: unknown): v is ErrorBody {
  if (typeof v !== 'object' || v === null || !('error' in v)) return false
  const e = (v as { error: unknown }).error
  return (
    typeof e === 'object' &&
    e !== null &&
    typeof (e as { code?: unknown }).code === 'string' &&
    typeof (e as { message?: unknown }).message === 'string'
  )
}

async function readJSON(res: Response): Promise<unknown> {
  try {
    return await res.json()
  } catch {
    return undefined
  }
}

/** GET-запрос к API. Любая неудача превращается в ApiError. */
export async function apiGet<T>(path: string, init?: { signal?: AbortSignal }): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, { headers: { Accept: 'application/json' }, signal: init?.signal })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError(0, UNREACHABLE, t.api.unreachable)
  }
  const body = await readJSON(res)
  if (res.ok) {
    if (body === undefined) throw new ApiError(res.status, BAD_RESPONSE, t.api.unknown)
    return body as T
  }
  if (isErrorBody(body)) throw new ApiError(res.status, body.error.code, body.error.message)
  // Не наш формат: так отвечает прокси разработки, когда сервер не запущен.
  throw new ApiError(res.status, UNREACHABLE, t.api.unreachable)
}

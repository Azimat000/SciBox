import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'

/** Текст для сообщения над формой по ошибке сервера. */
export function describeError(err: unknown): string {
  if (!(err instanceof ApiError)) return t.api.unreachable
  switch (err.code) {
    case 'invalid_credentials':
      return t.auth.login.invalid
    case 'email_not_confirmed':
      return t.auth.login.unconfirmed
    case 'validation_failed':
      return t.auth.errors.fixFields
    case 'rate_limited': {
      if (!err.retryAfter) return t.auth.errors.rateLimitedShort
      const minutes = Math.max(1, Math.ceil(err.retryAfter / 60))
      return t.auth.errors.rateLimited(minutes, plural(minutes, t.auth.errors.minuteForms))
    }
    default:
      return err.message
  }
}

/** Ошибки по полям из ответа сервера (пусто, если ошибка не про поля). */
export function fieldErrorsOf(err: unknown): Record<string, string> {
  return err instanceof ApiError ? err.fields : {}
}

/** Код ошибки сервера, если это она. */
export function errorCode(err: unknown): string | undefined {
  return err instanceof ApiError ? err.code : undefined
}

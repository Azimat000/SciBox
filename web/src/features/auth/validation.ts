import { t } from '../../i18n'

// Быстрая проверка в браузере, чтобы не гонять запрос ради пустого поля.
// Правила строже на сервере: он решает окончательно, здесь только очевидное.

export type FieldErrors = Record<string, string>

const EMAIL_SHAPE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export const MIN_PASSWORD_LENGTH = 10

export function checkName(value: string): string | undefined {
  return value.trim() === '' ? t.auth.errors.nameRequired : undefined
}

export function checkEmail(value: string): string | undefined {
  const v = value.trim()
  if (v === '') return t.auth.errors.emailRequired
  return EMAIL_SHAPE.test(v) ? undefined : t.auth.errors.emailInvalid
}

export function checkNewPassword(value: string): string | undefined {
  if (value === '') return t.auth.errors.passwordRequired
  // Считаем символы, а не байты: кириллица тоже по одному знаку.
  return [...value].length < MIN_PASSWORD_LENGTH ? t.auth.errors.passwordShort : undefined
}

export function checkPasswordPresent(value: string): string | undefined {
  return value === '' ? t.auth.errors.passwordRequired : undefined
}

/** Убирает из набора ошибок пустые значения. */
export function compact(errors: Record<string, string | undefined>): FieldErrors {
  const out: FieldErrors = {}
  for (const [k, v] of Object.entries(errors)) if (v) out[k] = v
  return out
}

/** Безопасный адрес для возврата после входа: только внутри сайта. */
export function safeNext(next: string | null | undefined): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return '/'
  return next
}

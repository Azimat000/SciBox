import { describe, expect, it } from 'vitest'
import { ApiError } from '../../api/client'
import { describeError, errorCode, fieldErrorsOf } from './errors'

describe('describeError', () => {
  it('speaks plainly for the known codes', () => {
    expect(describeError(new ApiError(401, 'invalid_credentials', 'x'))).toMatch(/Почта или пароль указаны неверно/)
    expect(describeError(new ApiError(403, 'email_not_confirmed', 'x'))).toMatch(/ещё не подтверждена/)
    expect(describeError(new ApiError(422, 'validation_failed', 'x'))).toBe('Проверьте отмеченные поля')
  })

  it.each([
    [60, 'Слишком много попыток. Попробуйте снова через 1 минуту'],
    [61, 'Слишком много попыток. Попробуйте снова через 2 минуты'],
    [900, 'Слишком много попыток. Попробуйте снова через 15 минут'],
    [1, 'Слишком много попыток. Попробуйте снова через 1 минуту'],
    [3600, 'Слишком много попыток. Попробуйте снова через 60 минут'],
    [1260, 'Слишком много попыток. Попробуйте снова через 21 минуту'],
  ])('rate limit of %d s reads as minutes', (seconds, text) => {
    expect(describeError(new ApiError(429, 'rate_limited', 'x', { retryAfter: seconds }))).toBe(text)
  })

  it('does not invent a delay the server did not give', () => {
    expect(describeError(new ApiError(429, 'rate_limited', 'x'))).toBe('Слишком много попыток. Подождите немного и попробуйте снова')
  })

  it('passes through the server message for other codes and says "no answer" for non-API errors', () => {
    expect(describeError(new ApiError(500, 'internal', 'Что-то сломалось'))).toBe('Что-то сломалось')
    expect(describeError(new Error('boom'))).toBe('Сервер не отвечает')
    expect(describeError(undefined)).toBe('Сервер не отвечает')
  })
})

describe('error helpers', () => {
  it('reads fields and code only from API errors', () => {
    const err = new ApiError(422, 'validation_failed', 'x', { fields: { email: 'плохо' } })
    expect(fieldErrorsOf(err)).toEqual({ email: 'плохо' })
    expect(errorCode(err)).toBe('validation_failed')
    expect(fieldErrorsOf(new Error('x'))).toEqual({})
    expect(fieldErrorsOf(null)).toEqual({})
    expect(errorCode(new Error('x'))).toBeUndefined()
  })
})

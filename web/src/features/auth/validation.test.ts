import { describe, expect, it } from 'vitest'
import { checkEmail, checkName, checkNewPassword, checkPasswordPresent, compact, safeNext } from './validation'

describe('form checks', () => {
  it('requires a name', () => {
    expect(checkName('')).toBe('Укажите, как вас зовут')
    expect(checkName('   ')).toBe('Укажите, как вас зовут')
    expect(checkName('Анна')).toBeUndefined()
  })

  it.each([
    ['', 'Укажите почту'],
    ['   ', 'Укажите почту'],
    ['anna', 'Похоже, в адресе почты опечатка. Он должен выглядеть так: name@example.ru'],
    ['anna@', 'Похоже, в адресе почты опечатка. Он должен выглядеть так: name@example.ru'],
    ['anna@example', 'Похоже, в адресе почты опечатка. Он должен выглядеть так: name@example.ru'],
    ['an na@example.ru', 'Похоже, в адресе почты опечатка. Он должен выглядеть так: name@example.ru'],
  ])('rejects the address %j', (value, message) => {
    expect(checkEmail(value)).toBe(message)
  })

  it('accepts ordinary and non-latin addresses, ignoring surrounding spaces', () => {
    expect(checkEmail(' anna@example.ru ')).toBeUndefined()
    expect(checkEmail('иван@пример.рф')).toBeUndefined()
  })

  it('counts characters, not bytes, in the new password', () => {
    expect(checkNewPassword('')).toBe('Введите пароль')
    expect(checkNewPassword('коротко12')).toBe('Пароль слишком короткий: нужно не меньше 10 знаков')
    expect(checkNewPassword('девять 9')).toBeDefined()
    expect(checkNewPassword('кофе на рассвете')).toBeUndefined()
    expect(checkNewPassword('0123456789')).toBeUndefined()
  })

  it('only asks that the current password is present', () => {
    expect(checkPasswordPresent('')).toBe('Введите пароль')
    expect(checkPasswordPresent('x')).toBeUndefined()
  })

  it('compact drops empty results', () => {
    expect(compact({ a: 'x', b: undefined, c: '' })).toEqual({ a: 'x' })
  })
})

describe('safeNext', () => {
  it.each([
    ['/account', '/account'],
    ['/vacancies?x=1', '/vacancies?x=1'],
    [null, '/'],
    [undefined, '/'],
    ['', '/'],
    ['account', '/'],
    ['https://evil.example', '/'],
    ['//evil.example', '/'],
    ['/\\evil.example', '/'],
    ['javascript:alert(1)', '/'],
  ])('%j → %j', (input, expected) => {
    expect(safeNext(input)).toBe(expected)
  })
})

import { describe, expect, it } from 'vitest'
import { mailtoHref } from './mailto'

describe('mailtoHref', () => {
  it('keeps an ordinary address readable', () => {
    expect(mailtoHref('ivan.petrov+jobs@example.ru')).toBe('mailto:ivan.petrov%2Bjobs@example.ru')
  })
  it('does not let an address turn into letter parameters', () => {
    expect(mailtoHref('a@b.ru?bcc=x@y.ru&body=hi')).toBe('mailto:a@b.ru%3Fbcc%3Dx@y.ru%26body%3Dhi')
  })
  it('encodes non-latin addresses', () => {
    expect(mailtoHref('иван@пример.рф')).toContain('%D0%B8')
  })
})

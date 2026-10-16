import { describe, expect, it } from 'vitest'
import { isSafeHttp } from './safeLink'

describe('isSafeHttp', () => {
  it('accepts an honest http or https address', () => {
    for (const s of ['https://example.org', 'http://example.ru/a?b=1#c', 'https://мой-сайт.рф/путь']) expect(isSafeHttp(s)).toBe(true)
  })

  it('refuses addresses that look like another site, point inside a network or are not web addresses', () => {
    for (const s of [
      'https://ya.ru@evil.example',
      'https://user:pass@example.org',
      'https://аpple.com',
      'http://127.0.0.1',
      'http://2130706433',
      'http://[::1]/x',
      'http://localhost',
      'ftp://example.org',
      'not a url',
      '',
    ]) {
      expect(isSafeHttp(s)).toBe(false)
    }
  })
})

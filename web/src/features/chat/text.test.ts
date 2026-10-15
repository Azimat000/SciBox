import { describe, expect, it } from 'vitest'
import { linkify, textLength } from './text'

const joined = (s: string) => linkify(s).map((p) => p.text).join('')

describe('linkify', () => {
  it('keeps plain text as one piece', () => {
    expect(linkify('Добрый день!')).toEqual([{ text: 'Добрый день!' }])
    expect(linkify('')).toEqual([])
  })

  it('makes http and https addresses links and leaves the rest as text', () => {
    expect(linkify('Смотрите https://example.org/a?b=1 и http://example.ru.')).toEqual([
      { text: 'Смотрите ' },
      { text: 'https://example.org/a?b=1', href: 'https://example.org/a?b=1' },
      { text: ' и ' },
      { text: 'http://example.ru', href: 'http://example.ru' },
      { text: '.' },
    ])
  })

  it('never links other schemes', () => {
    for (const text of ['javascript:alert(1)', 'mailto:me@example.org', 'data:text/html,<b>x</b>', 'ftp://example.org/file', 'file:///etc/passwd']) {
      expect(linkify(text).every((p) => p.href === undefined)).toBe(true)
    }
  })

  it('skips an address without a real host', () => {
    expect(linkify('https://').every((p) => p.href === undefined)).toBe(true)
    expect(linkify('http://localhost:3000').every((p) => p.href === undefined)).toBe(true)
  })

  it('cuts sentence punctuation and a closing bracket that is not part of the address', () => {
    expect(linkify('(https://example.org/x).')[1]).toEqual({ text: 'https://example.org/x', href: 'https://example.org/x' })
    expect(linkify('«https://example.org/x»,')[1]).toEqual({ text: 'https://example.org/x', href: 'https://example.org/x' })
    expect(linkify('https://ru.wikipedia.org/wiki/A_(B)')[0]).toEqual({ text: 'https://ru.wikipedia.org/wiki/A_(B)', href: 'https://ru.wikipedia.org/wiki/A_(B)' })
    expect(linkify('https://example.org/a?x=1!!!')[0].text).toBe('https://example.org/a?x=1')
  })

  it('does not reshape the text: pieces always join to the original', () => {
    for (const s of ['a https://example.org/x b', '\n\nhttps://example.org/\nhttps://example.net', 'https://x.y/<b>', 'Ж'.repeat(50), `https://example.org/${'a'.repeat(4000)}`]) {
      expect(joined(s)).toBe(s)
    }
  })

  it('stops an address at whitespace and angle brackets', () => {
    expect(linkify('https://example.org/<script>')[0].text).toBe('https://example.org/')
  })
})

describe('textLength', () => {
  it('counts code points the way the server does', () => {
    expect(textLength('abc')).toBe(3)
    expect(textLength('😀😀')).toBe(2)
    expect(textLength('Ёж')).toBe(2)
  })
})

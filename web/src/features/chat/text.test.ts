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

describe('linkify against look-alike addresses (T-98)', () => {
  const noLink = (text: string) => linkify(text).every((p) => p.href === undefined)

  it('does not link an address with a login before the host', () => {
    for (const s of ['https://ya.ru@evil.example/', 'https://ya.ru:pass@evil.example', 'http://user@example.org', 'https://ya.ru%40evil.example@x.org/']) {
      expect(noLink(s), s).toBe(true)
    }
  })

  it('does not link a name that mixes Latin and Cyrillic letters in one part', () => {
    expect(noLink('https://аpple.com/login')).toBe(true) // первая буква кириллическая
    expect(noLink('https://exampIe.оrg')).toBe(true)
    // Обычное имя целиком кириллицей или целиком латиницей остаётся ссылкой.
    expect(linkify('https://научная-лаборатория.рф/')[0].href).toBe('https://научная-лаборатория.рф/')
    expect(linkify('https://sub.example.org/путь')[0].href).toBe('https://sub.example.org/путь')
    // Кириллица в пути не мешает.
    expect(linkify('https://example.org/статья')[0].href).toBe('https://example.org/статья')
  })

  it('does not link numeric hosts, including the short forms that browsers expand', () => {
    for (const s of ['http://127.0.0.1:8080/', 'http://192.168.0.1', 'http://2130706433/', 'http://0x7f.1/', 'http://[::1]/', 'http://[fe80::1]:80/', 'https://1.2.3.4']) {
      expect(noLink(s), s).toBe(true)
    }
  })

  it('does not link a name whose last part is a number', () => {
    expect(noLink('http://example.123/')).toBe(true)
  })

  it('takes the host the way the browser does: a backslash ends it, so the link goes where it reads', () => {
    const [piece] = linkify('https://ya.ru\\@evil.example/')
    expect(new URL(piece.href ?? 'about:blank').hostname).toBe('ya.ru')
  })

  it('keeps text safe on generated combinations: a link, if any, has a plain host and joins back', () => {
    const hosts = ['example.org', 'ya.ru', 'а.рф', 'a.рф', 'exаmple.org', 'ya.ru@evil.org', '127.0.0.1', '[::1]', '2130706433', 'x.1', 'Sub.Example.ORG', 'xn--80aswg.xn--p1ai']
    const paths = ['', '/', '/a?b=1#c', '/<b>', '/ю', '/a)', '.', ')']
    const schemes = ['http://', 'https://', 'HTTPS://', 'ftp://', 'javascript:', 'data:', '//']
    for (const sc of schemes)
      for (const h of hosts)
        for (const pa of paths) {
          const text = `см. ${sc}${h}${pa} дальше`
          const pieces = linkify(text)
          expect(pieces.map((p) => p.text).join(''), text).toBe(text)
          for (const p of pieces) {
            if (!p.href) continue
            const u = new URL(p.href)
            expect(['http:', 'https:'], text).toContain(u.protocol)
            expect(u.username + u.password, text).toBe('')
            expect(/^[\d.]+$/.test(u.hostname) || u.hostname.includes(':'), text).toBe(false)
            expect(p.text, text).toBe(p.href)
          }
        }
  })
})

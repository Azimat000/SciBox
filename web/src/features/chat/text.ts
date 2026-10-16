// Ссылки в тексте сообщения (CHAT.md «Сообщения»): кликабельны только http и https, остальное остаётся текстом.
// Текст выводится как текст: ни HTML, ни разметки, ссылка показывается так, как её написали (адрес виден целиком).

export type Piece = { text: string; href?: undefined } | { text: string; href: string }

const URL_AT = /https?:\/\/[^\s<>"]+/gi
const TAIL = '.,;:!?…»”’\'")]}'
const PAIRS: Record<string, string> = { ')': '(', ']': '[', '}': '{' }
const count = (s: string, ch: string) => s.split(ch).length - 1

/** Знаки в конце адреса, которые относятся к предложению, а не к ссылке; закрывающая скобка остаётся, если открывающая внутри. */
function trimTail(raw: string): string {
  let url = raw
  while (url.length > 0) {
    const ch = url[url.length - 1]
    if (!TAIL.includes(ch)) break
    const open = PAIRS[ch]
    if (open && count(url, ch) <= count(url, open)) break
    url = url.slice(0, -1)
  }
  return url
}

const LATIN = /[a-z]/i
const CYRILLIC = /[\u0400-\u052f]/
const IPV4_LIKE = /^[\d.]+$/

/** Адрес в том виде, как его написали: часть между «//» и первым «/», «?» или «#». */
function writtenHost(s: string): string {
  const rest = s.slice(s.indexOf('//') + 2)
  const end = rest.search(/[/?#\\]/)
  return end === -1 ? rest : rest.slice(0, end)
}

/**
 * Ссылкой становится только адрес, по которому видно, куда он ведёт (T-98): http или https, настоящее имя сайта с точкой.
 * Не ссылка: адрес с «логином» до «@» (`https://ya.ru@чужой.сайт` выглядит как ya.ru), имя, где в одной части смешаны
 * латиница и кириллица (подмена букв), адрес из цифр или в скобках (внутренняя сеть и числовые записи вроде 2130706433).
 */
function isSafeHttp(s: string): boolean {
  let u: URL
  try {
    u = new URL(s)
  } catch {
    return false
  }
  if ((u.protocol !== 'http:' && u.protocol !== 'https:') || !u.hostname.includes('.')) return false
  if (u.username !== '' || u.password !== '' || writtenHost(s).includes('@')) return false
  if (u.hostname.includes(':') || IPV4_LIKE.test(u.hostname) || /^\d+$/.test(u.hostname.split('.').at(-1) ?? '')) return false
  return !writtenHost(s)
    .split('.')
    .some((label) => LATIN.test(label) && CYRILLIC.test(label))
}

/** Разбор текста на куски: обычный текст и ссылки. Склейка кусков всегда даёт исходный текст. */
export function linkify(text: string): Piece[] {
  const pieces: Piece[] = []
  let last = 0
  for (const m of text.matchAll(URL_AT)) {
    const start = m.index
    const url = trimTail(m[0])
    if (!isSafeHttp(url)) continue
    if (start > last) pieces.push({ text: text.slice(last, start) })
    pieces.push({ text: url, href: url })
    last = start + url.length
  }
  if (last < text.length) pieces.push({ text: text.slice(last) })
  return pieces
}

/** Знаки по кодовым точкам: так же считает сервер (эмодзи — один знак). */
export const textLength = (s: string) => [...s].length

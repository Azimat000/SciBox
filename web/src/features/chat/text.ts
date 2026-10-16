// Ссылки в тексте сообщения (CHAT.md «Сообщения»): кликабельны только http и https, остальное остаётся текстом.
// Текст выводится как текст: ни HTML, ни разметки, ссылка показывается так, как её написали (адрес виден целиком).

import { isSafeHttp } from '../../lib/safeLink'

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

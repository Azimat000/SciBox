// Адрес, по которому видно, куда он ведёт (T-98, T-117). Общий для текста сообщений и для места собеседования: везде, где
// чужой текст становится ссылкой, она проверяется одним правилом.

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
export function isSafeHttp(s: string): boolean {
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

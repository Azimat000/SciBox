import { useMemo } from 'react'
import { linkify } from './text'

/** Rel по CHAT.md: ссылка из чужого сообщения не передаёт ни источник перехода, ни вес в поиске. */
export const LINK_REL = 'noopener noreferrer nofollow ugc'

/** Текст сообщения как текст: переносы строк сохраняются стилем, ссылки http и https кликабельны, остальное не разбирается. */
export function MessageText({ text }: { text: string }) {
  const pieces = useMemo(() => linkify(text), [text])
  return (
    <>
      {pieces.map((p, i) =>
        p.href ? (
          <a key={i} href={p.href} target="_blank" rel={LINK_REL}>
            {p.text}
          </a>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  )
}

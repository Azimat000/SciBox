import { Link } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { ChatIcon } from '../../ui/icons'
import type { ChatRef } from './api'
import { badgeCount } from './labels'
import './chat-entry.css'

/**
 * Вход в переписку со страниц откликов и приглашений (CHAT.md «Экраны»). «Написать», пока людей в переписке не было и писать
 * можно; иначе «Перейти к переписке» с числом новых. Нет переписки (chat пуст) — нет и кнопки.
 * compact — ссылка в строке списка; без него кнопка на странице отклика или приглашения.
 */
export function ChatButton({ chat, compact = false }: { chat: ChatRef | null | undefined; compact?: boolean }) {
  if (!chat) return null
  const label = !chat.started && chat.can_write ? t.chat.entry.write : t.chat.entry.open
  const to = `/chats/${encodeURIComponent(chat.id)}`
  const count =
    chat.unread > 0 ? (
      <span className="chat-entry-count num">
        <span aria-hidden="true">{badgeCount(chat.unread)}</span>
        <span className="visually-hidden">, {t.chat.entry.unread(chat.unread)}</span>
      </span>
    ) : null
  if (compact) {
    return (
      <Link className="chat-entry-link" to={to}>
        <ChatIcon size={16} />
        {label}
        {count}
      </Link>
    )
  }
  return (
    <ButtonLink className="chat-entry-button" to={to} variant="secondary">
      <ChatIcon size={18} />
      {label}
      {count}
    </ButtonLink>
  )
}

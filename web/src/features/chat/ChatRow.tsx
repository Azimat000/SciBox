import { Link } from 'react-router'
import { t } from '../../i18n'
import { Tag } from '../../ui/Tag'
import type { ChatSummary } from './api'
import { badgeCount, chatTime, counterpartName, previewText, statusText } from './labels'

/**
 * Строка списка «как в почте» (D-217): собеседник и время; вакансия и состояние; последнее сообщение и число непрочитанных.
 * У учёного собеседник — организация, у организации — соискатель (или «Анонимный соискатель»).
 */
export function ChatRow({ chat, selected, href }: { chat: ChatSummary; selected: boolean; href: string }) {
  const unread = chat.unread > 0
  const when = chat.last_message?.created_at ?? chat.updated_at
  const name = counterpartName(chat.counterpart)
  return (
    <li>
      <Link className="chat-row" to={href} aria-current={selected ? 'page' : undefined} data-unread={unread || undefined}>
        <span className="chat-row-line">
          <span className="chat-row-who" title={name} data-anonymous={chat.counterpart.kind === 'scientist' && chat.counterpart.anonymous ? 'true' : undefined}>
            {name}
          </span>
          <time className="chat-row-time num" dateTime={when}>
            {chatTime(when)}
          </time>
        </span>
        <span className="chat-row-line chat-row-line-wrap">
          <span className="chat-row-vacancy" title={chat.vacancy.title}>
            {chat.vacancy.title}
          </span>
          <span className="chat-row-state">{chat.needs_answer ? <Tag tone="accent">{t.chat.needsAnswer}</Tag> : <Tag>{statusText(chat.status)}</Tag>}</span>
        </span>
        <span className="chat-row-line">
          <span className="chat-row-preview">{previewText(chat.last_message, chat.side)}</span>
          {unread && (
            <span className="chat-row-unread num">
              <span aria-hidden="true">{badgeCount(chat.unread)}</span>
              <span className="visually-hidden">{t.chat.unreadLabel(chat.unread)}</span>
            </span>
          )}
        </span>
      </Link>
    </li>
  )
}

import { Link } from 'react-router'
import { t } from '../../i18n'
import { Tag } from '../../ui/Tag'
import type { ChatSummary } from './api'
import { useArchiveToggle } from './useArchive'
import { badgeCount, chatTime, counterpartName, previewText, statusText } from './labels'

/**
 * Строка списка «как в почте» (D-217): собеседник и время; вакансия и состояние; последнее сообщение и число непрочитанных.
 * У учёного собеседник — организация, у организации — соискатель (или «Анонимный соискатель»).
 */
export function ChatRow({ chat, selected, href }: { chat: ChatSummary; selected: boolean; href: string }) {
  const unread = chat.unread > 0
  const when = chat.last_message?.created_at ?? chat.updated_at
  const name = counterpartName(chat.counterpart)
  const archive = useArchiveToggle()
  const a = t.chat.archive
  return (
    <li className="chat-row-item">
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
      {/* Рядом со строкой, а не внутри ссылки: кнопка внутри ссылки недоступна с клавиатуры и чтению с экрана */}
      <button
        type="button"
        className="chat-row-archive"
        disabled={archive.isPending}
        aria-label={chat.archived ? a.backLabel(name) : a.toLabel(name)}
        title={chat.archived ? a.back : a.to}
        onClick={() => archive.mutate({ id: chat.id, archived: !chat.archived })}
      >
        <svg viewBox="0 0 20 20" width="18" height="18" fill="none" aria-hidden="true" focusable="false">
          {chat.archived ? (
            <path d="M4 8l6-5 6 5M4 8v8h12V8M8 13l2-2 2 2M10 11v4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
          ) : (
            <path d="M3 5h14v3H3zM4.5 8v8h11V8M8 12h4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
          )}
        </svg>
      </button>
    </li>
  )
}

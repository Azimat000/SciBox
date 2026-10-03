import { t } from '../../i18n'
import { dateTimeText } from '../applications/labels'
import type { Notice } from './api'

/** Одно уведомление: заголовок, текст, время. Непрочитанное отмечено точкой и подписью, а не только цветом. */
export function NoticeRow({ item, compact = false, onOpen }: { item: Notice; compact?: boolean; onOpen: () => void }) {
  const content = (
    <>
      <span className="notice-head">
        {!item.read && <span className="notice-dot" aria-hidden="true" />}
        <span className="notice-title">{item.title}</span>
        {!item.read && <span className="visually-hidden"> ({t.notifications.unread})</span>}
      </span>
      {item.body && <span className="notice-body">{item.body}</span>}
      <span className="notice-time num">{dateTimeText(item.created_at)}</span>
    </>
  )
  return (
    <li className="notice-row" data-unread={!item.read || undefined} data-compact={compact || undefined}>
      {/* Уведомление без ссылки открывать нечего, но прочитанным его отметить можно */}
      <button type="button" className="notice-button" onClick={onOpen}>
        {content}
      </button>
    </li>
  )
}

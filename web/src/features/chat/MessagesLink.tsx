import { Link, useLocation } from 'react-router'
import { t } from '../../i18n'
import { ChatIcon } from '../../ui/icons'
import { useUnreadChats } from './api'
import { badgeCount } from './labels'
import '../notifications/notifications.css'

/** «Сообщения» в шапке: значок с числом переписок, где есть непрочитанное (обе стороны вместе). Только для вошедших. */
export function MessagesLink() {
  const { pathname } = useLocation()
  const unread = useUnreadChats()
  const count = unread.data?.total ?? 0
  const current = pathname === '/chats' || pathname.startsWith('/chats/')
  const label = count > 0 ? t.chat.navWithCount(count) : t.chat.nav
  return (
    <Link to="/chats" className="header-icon" aria-label={label} title={t.chat.nav} aria-current={current ? 'page' : undefined}>
      <ChatIcon size={22} />
      {count > 0 && (
        <span className="bell-count num" aria-hidden="true">
          {badgeCount(count)}
        </span>
      )}
    </Link>
  )
}

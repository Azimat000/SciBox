import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { BellIcon } from '../../ui/icons'
import { useMarkRead, useRecent, useUnreadCount, type Notice } from './api'
import { NoticeRow } from './NoticeRow'
import './notifications.css'

/** Колокольчик в шапке: число непрочитанных и окно с последними уведомлениями. Только для вошедших. */
export function NotificationBell() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  // Окно открыто «на этой странице»: перешли на другую, и оно закрылось само
  const [openAt, setOpenAt] = useState<string | null>(null)
  const open = openAt === pathname
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const unread = useUnreadCount()
  const recent = useRecent(open)
  const mark = useMarkRead()
  const n = t.notifications
  const count = unread.data ?? 0

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setOpenAt(null)
      buttonRef.current?.focus()
    }
    const onPointer = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpenAt(null)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onPointer)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onPointer)
    }
  }, [open])

  const openNotice = (item: Notice) => {
    if (!item.read) mark.one.mutate(item.id)
    setOpenAt(null)
    if (item.link) void navigate(item.link)
  }

  return (
    <div className="bell" ref={rootRef}>
      <button
        type="button"
        ref={buttonRef}
        className="bell-button"
        aria-expanded={open}
        aria-controls="bell-panel"
        aria-label={count > 0 ? n.bellWithCount(count) : n.bell}
        onClick={() => setOpenAt(open ? null : pathname)}
      >
        <BellIcon size={22} />
        {count > 0 && (
          <span className="bell-count num" aria-hidden="true">
            {count > 99 ? '99+' : count}
          </span>
        )}
      </button>
      <div id="bell-panel" className="bell-panel" hidden={!open}>
        <div className="bell-panel-head">
          <p className="bell-panel-title">{n.title}</p>
          {count > 0 && (
            <Button variant="quiet" size="sm" loading={mark.all.isPending} onClick={() => mark.all.mutate()}>
              {n.markAll}
            </Button>
          )}
        </div>
        {recent.isPending && open ? (
          <p className="bell-empty" role="status">
            {t.common.loading}
          </p>
        ) : recent.isError ? (
          <p className="bell-empty">{n.loadError}</p>
        ) : recent.data && recent.data.items.length > 0 ? (
          <ul className="notice-list">
            {recent.data.items.map((item) => (
              <NoticeRow key={item.id} item={item} compact onOpen={() => openNotice(item)} />
            ))}
          </ul>
        ) : (
          <p className="bell-empty">{n.panelEmpty}</p>
        )}
        <Link to="/notifications" className="bell-all">
          {n.all}
        </Link>
      </div>
    </div>
  )
}

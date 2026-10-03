import { useNavigate, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { PAGE_SIZE, useMarkRead, useNoticePage, type Notice } from './api'
import { NoticeRow } from './NoticeRow'
import './notifications.css'

/** Все уведомления страницами; нажатие открывает то, о чём уведомление, и отмечает его прочитанным. */
export function NotificationsPage() {
  return <RequireUser>{() => <List />}</RequireUser>
}

function List() {
  const n = t.notifications
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useNoticePage(page)
  const mark = useMarkRead()

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={n.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, total, unread } = query.data
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (p: number) => {
    const next = new URLSearchParams(params)
    if (p > 1) next.set('page', String(p))
    else next.delete('page')
    setParams(next)
  }
  const open = (item: Notice) => {
    if (!item.read) mark.one.mutate(item.id)
    if (item.link) void navigate(item.link)
  }

  return (
    <div className="page org-page">
      <h1>{n.title}</h1>
      <p className="lead">{n.lead}</p>
      {total === 0 ? (
        <EmptyState headingLevel={2} title={n.empty} text={n.emptyText} />
      ) : (
        <>
          {unread > 0 && (
            <div className="notice-toolbar">
              <Button variant="secondary" size="sm" loading={mark.all.isPending} onClick={() => mark.all.mutate()}>
                {n.markAll}
              </Button>
            </div>
          )}
          <ul className="notice-list notice-list-page">
            {items.map((item) => (
              <NoticeRow key={item.id} item={item} onOpen={() => open(item)} />
            ))}
          </ul>
          {pages > 1 && (
            <nav className="pager" aria-label={n.pager}>
              <Button variant="secondary" disabled={page <= 1} onClick={() => go(page - 1)}>
                {n.prev}
              </Button>
              <span className="pager-text num">{n.page(page, pages)}</span>
              <Button variant="secondary" disabled={page >= pages} onClick={() => go(page + 1)}>
                {n.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  )
}

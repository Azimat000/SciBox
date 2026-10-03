import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { RequireUser } from '../orgs/RequireUser'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { OffersTabs } from '../offers/OffersTabs'
import { PAGE_SIZE, useMyApplications, type Summary } from './api'
import { dateText, statusLabel, statusTone } from './labels'
import './applications.css'

/** «Мои отклики»: новые сверху, у каждого статус и сколько рекомендательных писем пришло. */
export function MyApplicationsPage() {
  return <RequireUser>{() => <List />}</RequireUser>
}

function List() {
  const m = t.applications.mine
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useMyApplications(page)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={m.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, total } = query.data
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (n: number) => {
    const next = new URLSearchParams(params)
    if (n > 1) next.set('page', String(n))
    else next.delete('page')
    setParams(next)
  }

  return (
    <div className="page org-page">
      <h1>{m.title}</h1>
      <p className="lead">{m.lead}</p>
      <OffersTabs side="seeker" current="applications" />
      {total === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.empty}
          text={m.emptyText}
          action={
            <ButtonLink to="/vacancies" variant="secondary">
              {m.search}
            </ButtonLink>
          }
        />
      ) : (
        <>
          <p className="app-count num">{m.count(total, plural(total, m.countForms))}</p>
          <ol className="app-list">
            {items.map((a) => (
              <Entry key={a.id} app={a} />
            ))}
          </ol>
          {pages > 1 && (
            <nav className="pager" aria-label={m.pager}>
              <Button variant="secondary" disabled={page <= 1} onClick={() => go(page - 1)}>
                {m.prev}
              </Button>
              <span className="pager-text num">{m.page(page, pages)}</span>
              <Button variant="secondary" disabled={page >= pages} onClick={() => go(page + 1)}>
                {m.next}
              </Button>
            </nav>
          )}
        </>
      )}
    </div>
  )
}

function Entry({ app }: { app: Summary }) {
  const m = t.applications.mine
  const refs = app.references
  return (
    <li className="app-entry">
      <h2 className="app-entry-title" data-long={longName(app.vacancy.title)}>
        <Link to={`/applications/${app.id}`}>{app.vacancy.title}</Link>
      </h2>
      <p className="app-entry-org">
        <Link to={`/organizations/${app.vacancy.org_slug}`}>{app.vacancy.org_name}</Link>
        {app.vacancy.status === 'closed' && <span>{m.vacancyClosed}</span>}
      </p>
      <p className="app-entry-meta">
        <Tag tone={statusTone(app.status)}>{statusLabel(app.status)}</Tag>
        <span>{m.sentAt(dateText(app.created_at))}</span>
        {app.pending_invitations > 0 && <Tag tone="accent">{m.waitsYou}</Tag>}
        {refs.total > 0 && <span>{m.refs(refs.received, refs.total)}</span>}
      </p>
    </li>
  )
}

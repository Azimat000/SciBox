import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { dateText } from '../applications/labels'
import { RequireUser } from '../orgs/RequireUser'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { PAGE_SIZE, useMyOffers, type Offer } from './api'
import { statusLabel } from './labels'
import { OffersTabs } from './OffersTabs'
import '../applications/applications.css'
import './offers.css'

/** «Приглашения» учёного: кто и на какую вакансию его позвал, новые сверху. */
export function MyOffersPage() {
  return <RequireUser>{() => <List />}</RequireUser>
}

function List() {
  const m = t.offers.mine
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useMyOffers(page)

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
      <OffersTabs side="seeker" current="offers" />
      {total === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.emptyTitle}
          text={m.emptyText}
          action={
            <ButtonLink to="/profile" variant="secondary">
              {m.emptyAction}
            </ButtonLink>
          }
        />
      ) : (
        <>
          <p className="app-count num">{m.count(total, plural(total, m.countForms))}</p>
          <ol className="app-list">
            {items.map((o) => (
              <Entry key={o.id} offer={o} />
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

function Entry({ offer }: { offer: Offer }) {
  const m = t.offers.mine
  return (
    <li className="app-entry">
      <h2 className="app-entry-title" data-long={longName(offer.vacancy.title)}>
        <Link to={`/offers/${offer.id}`}>{offer.vacancy.title}</Link>
      </h2>
      <p className="app-entry-org">
        <Link to={`/organizations/${offer.vacancy.org_slug}`}>{offer.vacancy.org_name}</Link>
        {offer.vacancy.status !== 'published' && <span>{m.vacancyClosed}</span>}
      </p>
      <p className="app-entry-meta">
        {offer.status === 'pending' ? <Tag tone="accent">{m.waitsYou}</Tag> : <Tag>{m.youAnswered(statusLabel(offer.status))}</Tag>}
        <span>{m.receivedAt(dateText(offer.created_at))}</span>
        {offer.application_id && <span>{m.applied}</span>}
      </p>
    </li>
  )
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { dateText } from '../applications/labels'
import { describeError } from '../auth/errors'
import { refreshNotifications } from '../notifications/refresh'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { RequireUser } from '../orgs/RequireUser'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { PAGE_SIZE, cancelOffer, refreshOffers, statuses, useSentOffers, type Offer, type OfferStatus } from './api'
import { statusLabel, statusTone } from './labels'
import { OffersTabs } from './OffersTabs'
import '../applications/applications.css'
import '../applications/review.css'
import '../orgs/orgs.css'
import './offers.css'

/** Порядок вкладок: сначала то, что ждёт ответа. */
const tabs: readonly OfferStatus[] = ['pending', 'interested', 'declined', 'cancelled']

/** «Отправленные приглашения» организации: кого позвали, на какую вакансию и что ответили. Вкладки по состояниям. */
export function SentOffersPage() {
  return <RequireUser>{() => <List />}</RequireUser>
}

function List() {
  const s = t.offers.sent
  const [params, setParams] = useSearchParams()
  const rawStatus = params.get('status') ?? ''
  const status = (statuses as readonly string[]).includes(rawStatus) ? rawStatus : ''
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useSentOffers(status, page)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={s.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, total, counts } = query.data
  const everything = statuses.reduce((n, st) => n + counts[st], 0)
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (n: number) => {
    const next = new URLSearchParams(params)
    if (n > 1) next.set('page', String(n))
    else next.delete('page')
    setParams(next)
  }
  const tabHref = (st: string) => (st ? `/sent-offers?status=${st}` : '/sent-offers')

  if (everything === 0 && !status) {
    return (
      <div className="page org-page">
        <h1>{s.title}</h1>
        <OffersTabs side="employer" current="offers" />
        <EmptyState
          headingLevel={2}
          title={s.emptyTitle}
          text={s.emptyText}
          action={
            <ButtonLink to="/scientists" variant="secondary">
              {s.emptyAction}
            </ButtonLink>
          }
        />
      </div>
    )
  }

  return (
    <div className="page org-page">
      <h1>{s.title}</h1>
      <p className="lead">{s.lead}</p>
      <OffersTabs side="employer" current="offers" />

      <nav className="manage-tabs cand-tabs" aria-label={s.tabs}>
        {['', ...tabs].map((st) => {
          const label = st ? s.tabNames[st as OfferStatus] : s.all
          const count = st ? counts[st as OfferStatus] : everything
          return (
            <Link key={st || 'all'} className="manage-tab" to={tabHref(st)} aria-current={st === status ? 'page' : undefined} aria-label={s.tabLabel(label, count)}>
              {label}
              <span className="tab-count num" aria-hidden="true">
                {count}
              </span>
            </Link>
          )
        })}
      </nav>

      <div className="manage-body">
        {items.length === 0 ? (
          <EmptyState headingLevel={2} title={s.emptyTabTitle} text={s.emptyTabText} />
        ) : (
          <>
            <p className="app-count num">{s.count(total, plural(total, s.countForms))}</p>
            <ol className="cand-list" aria-busy={query.isPlaceholderData}>
              {items.map((o) => (
                <Row key={o.id} offer={o} />
              ))}
            </ol>
            {pages > 1 && (
              <nav className="pager" aria-label={s.pager}>
                <Button variant="secondary" disabled={page <= 1} onClick={() => go(page - 1)}>
                  {s.prev}
                </Button>
                <span className="pager-text num">{s.page(page, pages)}</span>
                <Button variant="secondary" disabled={page >= pages} onClick={() => go(page + 1)}>
                  {s.next}
                </Button>
              </nav>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Row({ offer }: { offer: Offer }) {
  const s = t.offers.sent
  const client = useQueryClient()
  const toast = useToast()
  const [confirm, setConfirm] = useState(false)
  const cancel = useMutation({
    mutationFn: () => cancelOffer(offer.id),
    onSuccess: async () => {
      await Promise.all([refreshOffers(client), refreshNotifications(client)])
      setConfirm(false)
      toast.show({ kind: 'success', title: s.cancelled })
    },
    onError: (err) => {
      setConfirm(false)
      toast.show({ kind: 'error', title: err instanceof Error ? describeError(err) : s.cancelled })
    },
  })
  const who = offer.scientist
  return (
    <li className="cand-row">
      <h2 className="cand-name" data-long={who ? longName(who.name) : undefined}>
        {who ? <Link to={`/scientists/${who.profile_id}`}>{who.name}</Link> : null}
      </h2>
      <p className="cand-vacancy">
        <Link to={`/vacancies/${offer.vacancy.id}`}>{offer.vacancy.title}</Link>
        {offer.vacancy.unit_name ? ` · ${offer.vacancy.unit_name}` : ''}
      </p>
      <p className="cand-meta">
        <Tag tone={statusTone(offer.status)}>{statusLabel(offer.status)}</Tag>
        <span>{s.sentAt(dateText(offer.created_at))}</span>
        {offer.answered_at && <span>{s.answeredAt(dateText(offer.answered_at))}</span>}
      </p>
      {offer.message && (
        <p className="review-note">
          <span>{s.yourMessage}: </span>
          {offer.message}
        </p>
      )}
      {offer.answer_note && (
        <p className="review-note">
          <span>{s.theirNote}: </span>
          {offer.answer_note}
        </p>
      )}
      {offer.can_cancel && (
        <div className="offer-actions">
          <Button size="sm" variant="quiet" onClick={() => setConfirm(true)}>
            {s.cancel}
          </Button>
        </div>
      )}
      <ConfirmModal
        open={confirm}
        title={s.cancelTitle}
        text={s.cancelText}
        confirmLabel={s.cancel}
        pending={cancel.isPending}
        onConfirm={() => cancel.mutate()}
        onClose={() => setConfirm(false)}
      />
    </li>
  )
}

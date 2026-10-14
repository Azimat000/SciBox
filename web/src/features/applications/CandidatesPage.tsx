import { useMutation } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { saveFile } from '../../lib/saveFile'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Select } from '../../ui/Select'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { OffersTabs } from '../offers/OffersTabs'
import { RequireUser } from '../orgs/RequireUser'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { EXPORT_LIMIT, PAGE_SIZE, exportCandidates, statuses, useCandidateVacancies, useCandidates, type AppStatus, type Candidate } from './api'
import { dateText, statusLabel, statusTone } from './labels'
import './applications.css'
import './review.css'
import { ChatButton } from '../chat/ChatButton'

/** Порядок вкладок: сначала то, до чего ещё не дошли руки. */
const tabs: readonly AppStatus[] = ['sent', 'viewed', 'invited', 'accepted', 'rejected', 'withdrawn']

/** Список откликов на вакансии, которые человек ведёт: отбор по вакансии и статусу, всё в адресе страницы. */
export function CandidatesPage() {
  return <RequireUser>{() => <List />}</RequireUser>
}

function List() {
  const c = t.applications.candidates
  const [params, setParams] = useSearchParams()
  const rawStatus = params.get('status') ?? ''
  const status = (statuses as readonly string[]).includes(rawStatus) ? rawStatus : ''
  const vacancy = params.get('vacancy') ?? ''
  const page = Math.max(1, Number(params.get('page')) || 1)
  const list = useCandidates({ vacancy, status, page })
  const vacancies = useCandidateVacancies()
  const toast = useToast()
  // Выгружается то, что выбрано сейчас (вакансия и вкладка), целиком, а не страница списка.
  const download = useMutation({
    mutationFn: () => exportCandidates({ vacancy, status }),
    onSuccess: ({ blob, filename }) => {
      saveFile(blob, filename ?? 'otkliki.csv')
      const n = list.data?.total ?? 0
      const capped = n > EXPORT_LIMIT
      toast.show({
        kind: 'success',
        title: c.exported,
        text: capped ? c.exportedCapped(EXPORT_LIMIT) : c.exportedText(n, plural(n, c.countForms)),
      })
    },
    onError: (err) =>
      toast.show({
        kind: 'error',
        title: c.exportFailed,
        text: err instanceof ApiError && err.code === 'rate_limited' ? c.exportLimited : describeError(err),
      }),
  })

  if (list.isPending) return <PageSkeleton />
  if (list.isError) return <LoadFailed title={c.loadError} error={list.error} onRetry={() => void list.refetch()} />

  const { items, total, counts } = list.data
  const everything = statuses.reduce((n, s) => n + counts[s], 0)
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const change = (patch: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries(patch)) {
      if (v) next.set(k, v)
      else next.delete(k)
    }
    next.delete('page')
    setParams(next)
  }
  const go = (n: number) => {
    const next = new URLSearchParams(params)
    if (n > 1) next.set('page', String(n))
    else next.delete('page')
    setParams(next)
  }
  const tabHref = (s: string) => {
    const next = new URLSearchParams(params)
    next.delete('page')
    if (s) next.set('status', s)
    else next.delete('status')
    const qs = next.toString()
    return qs ? `/candidates?${qs}` : '/candidates'
  }

  // Совсем пусто: ни отбора, ни откликов.
  if (everything === 0 && !vacancy && !status) {
    return (
      <div className="page org-page">
        <h1>{c.title}</h1>
        <OffersTabs side="employer" current="applications" />
        <EmptyState
          headingLevel={2}
          title={c.emptyTitle}
          text={c.emptyText}
          action={
            <ButtonLink to="/my-vacancies" variant="secondary">
              {c.emptyAction}
            </ButtonLink>
          }
        />
      </div>
    )
  }

  return (
    <div className="page org-page">
      <h1>{c.title}</h1>
      <p className="lead">{c.lead}</p>
      <OffersTabs side="employer" current="applications" />

      {vacancies.data && vacancies.data.length > 0 && (
        <Select
          className="cand-filter"
          label={c.vacancyFilter}
          name="vacancy"
          value={vacancy}
          placeholder={c.allVacancies}
          onChange={(ev) => change({ vacancy: ev.target.value })}
          options={vacancies.data.map((v) => ({ value: v.id, label: c.vacancyOption(v.title, v.total, v.new) }))}
        />
      )}

      <nav className="manage-tabs cand-tabs" aria-label={c.tabs}>
        {['', ...tabs].map((s) => {
          const label = s ? t.applications.statusTabs[s as AppStatus] : t.applications.statusTabs.all
          const count = s ? counts[s as AppStatus] : everything
          return (
            <Link key={s || 'all'} className="manage-tab" to={tabHref(s)} aria-current={s === status ? 'page' : undefined} aria-label={c.tabLabel(label, count)}>
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
          <EmptyState headingLevel={2} title={c.emptyTabTitle} text={c.emptyTabText} />
        ) : (
          <>
            <div className="cand-toolbar">
              <p className="app-count num">{c.count(total, plural(total, c.countForms))}</p>
              <Button variant="secondary" size="sm" loading={download.isPending} onClick={() => download.mutate()} aria-describedby="cand-export-hint">
                {c.export}
              </Button>
            </div>
            <p id="cand-export-hint" className="cand-export-hint">
              {c.exportHint(EXPORT_LIMIT)}
            </p>
            <ol className="cand-list">
              {items.map((a) => (
                <Row key={a.id} cand={a} showVacancy={!vacancy} />
              ))}
            </ol>
            {pages > 1 && (
              <nav className="pager" aria-label={c.pager}>
                <Button variant="secondary" disabled={page <= 1} onClick={() => go(page - 1)}>
                  {c.prev}
                </Button>
                <span className="pager-text num">{c.page(page, pages)}</span>
                <Button variant="secondary" disabled={page >= pages} onClick={() => go(page + 1)}>
                  {c.next}
                </Button>
              </nav>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Row({ cand, showVacancy }: { cand: Candidate; showVacancy: boolean }) {
  const c = t.applications.candidates
  const refs = cand.references
  return (
    <li className="cand-row">
      <h2 className="cand-name" data-long={longName(cand.applicant_name)}>
        <Link to={`/candidates/${cand.id}`}>{cand.applicant_name}</Link>
      </h2>
      <p className="cand-headline">{cand.anonymized ? c.anonymizedHeadline : cand.headline || c.noHeadline}</p>
      {showVacancy && (
        <p className="cand-vacancy">
          {cand.vacancy.title}
          {' · '}
          {cand.unit_name || c.noUnit}
        </p>
      )}
      <p className="cand-meta">
        <Tag tone={statusTone(cand.status)}>{statusLabel(cand.status)}</Tag>
        {cand.proposed_invitations > 0 && <Tag tone="accent">{c.proposed}</Tag>}
        {cand.pending_invitations > 0 && <span>{c.waitsAnswer}</span>}
        <span>{c.sentAt(dateText(cand.created_at))}</span>
        {refs.total > 0 && <span>{c.refs(refs.received, refs.total)}</span>}
        <ChatButton chat={cand.chat} compact />
      </p>
    </li>
  )
}

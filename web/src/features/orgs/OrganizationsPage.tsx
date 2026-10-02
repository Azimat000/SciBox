import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { SearchBar } from '../../ui/SearchBar'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import '../../ui/ResultsHeader.css'
import { orgKinds, PAGE_SIZE, useCatalog, type OrgSummary } from './api'
import { kindLabel, kindOptions, longName } from './labels'
import { LoadFailed } from './states'
import './orgs.css'

/** Каталог организаций: поиск по названию и городу, тип, страницы. Всё в адресе, поэтому ссылкой можно поделиться. */
export function OrganizationsPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const rawKind = params.get('kind') ?? ''
  const kind = (orgKinds as readonly string[]).includes(rawKind) ? rawKind : ''
  const page = Math.max(1, Number.parseInt(params.get('page') ?? '', 10) || 1)

  // Что напечатано в строке поиска, пока человек не нажал «Найти». Если адрес поменялся сам (сброс), строка следует за ним.
  const [draft, setDraft] = useState(q)
  const [seenQuery, setSeenQuery] = useState(q)
  if (q !== seenQuery) {
    setSeenQuery(q)
    setDraft(q)
  }

  const catalog = useCatalog({ q, kind, page })
  const filtered = q !== '' || kind !== ''

  const apply = (next: { q: string; kind: string; page?: number }) => {
    const out = new URLSearchParams()
    if (next.q.trim()) out.set('q', next.q.trim())
    if (next.kind) out.set('kind', next.kind)
    if (next.page && next.page > 1) out.set('page', String(next.page))
    setParams(out)
  }

  const total = catalog.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <div className="page org-page">
      <h1>{t.orgs.catalog.title}</h1>
      <p className="lead">{t.orgs.catalog.lead}</p>

      <div className="catalog-search">
        <SearchBar
          query={draft}
          onQueryChange={setDraft}
          regions={kindOptions}
          region={kind}
          onRegionChange={(k) => apply({ q: draft, kind: k })}
          onSubmit={() => apply({ q: draft, kind })}
          labels={{
            query: t.orgs.catalog.queryLabel,
            placeholder: t.orgs.catalog.queryPlaceholder,
            select: t.orgs.catalog.kindLabel,
            allOptions: t.orgs.catalog.anyKind,
          }}
        />
      </div>

      <div className="catalog-results">
        {catalog.isPending ? (
          <CatalogSkeleton />
        ) : catalog.isError ? (
          <LoadFailed title={t.orgs.catalog.loadError} error={catalog.error} onRetry={() => void catalog.refetch()} />
        ) : (
          <>
            <p className="results-header" aria-live="polite">
              <span className="num">
                {t.orgs.catalog.found(total, plural(total, t.orgs.catalog.foundForms), plural(total, t.orgs.catalog.orgForms))}
              </span>
            </p>
            {catalog.data.items.length === 0 ? (
              filtered ? (
                <EmptyState
                  title={t.orgs.catalog.emptyFilteredTitle}
                  text={t.orgs.catalog.emptyFilteredText}
                  action={
                    <Button variant="secondary" onClick={() => apply({ q: '', kind: '' })}>
                      {t.orgs.catalog.reset}
                    </Button>
                  }
                />
              ) : (
                <EmptyState
                  title={t.orgs.catalog.emptyTitle}
                  text={t.orgs.catalog.emptyText}
                  action={<ButtonLink to="/organizations/new">{t.orgs.catalog.create}</ButtonLink>}
                />
              )
            ) : (
              <ul className="org-list">
                {catalog.data.items.map((o) => (
                  <OrgEntry key={o.id} org={o} />
                ))}
              </ul>
            )}
            {pages > 1 && (
              <nav className="pager" aria-label={t.orgs.catalog.pager}>
                <Button variant="secondary" disabled={page <= 1} onClick={() => apply({ q, kind, page: page - 1 })}>
                  {t.orgs.catalog.prev}
                </Button>
                <span className="pager-text num">{t.orgs.catalog.page(page, pages)}</span>
                <Button variant="secondary" disabled={page >= pages} onClick={() => apply({ q, kind, page: page + 1 })}>
                  {t.orgs.catalog.next}
                </Button>
              </nav>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function OrgEntry({ org }: { org: OrgSummary }) {
  return (
    <li className="org-entry">
      <h2 className="org-entry-title" data-long={longName(org.name)}>
        <Link to={`/organizations/${org.slug}`}>{org.name}</Link>
      </h2>
      <p className="org-entry-meta">
        {kindLabel(org.kind)} · {org.city}
      </p>
      {org.summary ? <p className="org-entry-summary">{org.summary}</p> : <p className="org-entry-summary" data-empty>{t.orgs.catalog.noSummary}</p>}
      <p className="org-entry-facts num">
        {org.unit_count > 0 ? (
          <Tag>{t.orgs.catalog.units(org.unit_count, plural(org.unit_count, t.orgs.catalog.unitForms))}</Tag>
        ) : (
          <span>{t.orgs.catalog.noUnits}</span>
        )}
      </p>
    </li>
  )
}

function CatalogSkeleton() {
  return (
    <div role="status" aria-busy="true" aria-label={t.common.loading}>
      {[0, 1, 2].map((i) => (
        <div className="catalog-skeleton" key={i} aria-hidden="true">
          <Skeleton width="55%" height="1.4rem" />
          <Skeleton width="30%" height="0.9rem" />
          <Skeleton width="85%" height="0.95rem" />
          <Skeleton width="65%" height="0.95rem" />
        </div>
      ))}
      <span className="visually-hidden">{t.common.loading}</span>
    </div>
  )
}

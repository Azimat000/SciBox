import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { useRole } from '../shell/useRole'
import { plural } from '../../lib/plural'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { ChevronDownIcon, CloseIcon } from '../../ui/icons'
import { SearchBar } from '../../ui/SearchBar'
import { Select } from '../../ui/Select'
import { Skeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'
import { fieldNames } from '../search/labels'
import { useReference } from '../vacancies/api'
import { useCatalog } from './api'
import { CatalogFilters } from './CatalogFilters'
import { filterLabel } from './labels'
import {
  PAGE_SIZE,
  activeFilters,
  change,
  defaultSort,
  effectiveSort,
  emptyCatalog,
  filterCount,
  parseCatalog,
  removeFilter,
  sorts,
  toParams,
  withoutFilters,
  type CatalogSearch,
  type Sort,
} from './params'
import { ScientistEntry } from './ScientistEntry'
// Каталог устроен как поиск вакансий: колонка фильтров, чипы, порядок, страницы. Общий вид лежит в одной таблице стилей.
import '../search/search.css'
import '../../ui/ResultsHeader.css'
import './catalog.css'

/** Каталог учёных. Всё выбранное лежит в адресе: ссылкой можно поделиться, «Назад» возвращает прежнюю выдачу. */
export function CatalogPage() {
  const c = t.catalog
  const [params, setParams] = useSearchParams()
  const search = parseCatalog(params)
  const reference = useReference()
  const result = useCatalog(search)

  const go = (next: CatalogSearch) => setParams(toParams(next))

  // Слова в строке поиска меняются без запроса, пока человек не нажал «Найти»; если адрес поменялся сам, строка следует за ним.
  const [draft, setDraft] = useState(search.q)
  const [seenQuery, setSeenQuery] = useState(search.q)
  if (search.q !== seenQuery) {
    setSeenQuery(search.q)
    setDraft(search.q)
  }

  const [filtersOpen, setFiltersOpen] = useState(false)
  const names = fieldNames(reference.data)
  const regionName = (code: string) => reference.data?.regions.find((r) => r.code === code)?.name ?? code
  const regions = (reference.data?.regions ?? []).map((r) => ({ value: r.code, label: r.name }))
  const chips = activeFilters(search)
  const nFilters = filterCount(search)

  // После смены страницы взгляд возвращается к началу выдачи.
  const top = useRef<HTMLDivElement>(null)
  const previousPage = useRef(search.page)
  useEffect(() => {
    if (previousPage.current !== search.page) top.current?.scrollIntoView?.({ block: 'start' })
    previousPage.current = search.page
  }, [search.page])

  const total = result.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const sortNow = effectiveSort(search)
  const sortOptions = sorts.filter((s) => s !== 'relevance' || search.q).map((s) => ({ value: s, label: c.sorts[s] }))
  const word = plural(total, c.scientistForms)
  const { role } = useRole()

  return (
    <div className="page search-page">
      <h1>{c.title}</h1>
      <p className="lead">{role === 'employer' ? c.lead : c.leadSeeker}</p>

      <div className="search-bar">
        <SearchBar
          query={draft}
          onQueryChange={setDraft}
          regions={regions}
          region={search.region}
          onRegionChange={(region) => go(change(search, { region }))}
          onSubmit={() => go(change(search, { q: draft.trim() }))}
          labels={{ query: c.search.query, placeholder: c.search.placeholder, select: c.search.region, allOptions: c.search.allRegions }}
        />
      </div>

      <div className="search-layout">
        <aside className="search-filters" aria-label={t.find.filtersTitle}>
          <button
            type="button"
            className="filters-toggle"
            aria-expanded={filtersOpen}
            aria-controls="catalog-filter-panel"
            onClick={() => setFiltersOpen((v) => !v)}
          >
            <span>{nFilters > 0 ? t.find.filtersCount(nFilters) : t.find.filters}</span>
            <ChevronDownIcon size={18} />
          </button>
          <div id="catalog-filter-panel" className="filters-body" data-open={filtersOpen}>
            <CatalogFilters search={search} onChange={go} reference={reference.data} />
            <Button
              className="filters-done"
              onClick={() => {
                setFiltersOpen(false)
                top.current?.scrollIntoView?.({ block: 'start' })
              }}
            >
              {result.data ? t.find.showResults(total, word) : t.find.hideFilters}
            </Button>
          </div>
        </aside>

        <section className="search-results" aria-label={c.results} ref={top}>
          {chips.length > 0 && (
            <div className="active-filters">
              <ul aria-label={t.find.activeFilters}>
                {chips.map((ref) => {
                  const label = filterLabel(ref, search, names, regionName)
                  return (
                    <li key={`${ref.kind}:${'value' in ref ? ref.value : ''}`}>
                      <button type="button" className="filter-tag" aria-label={t.find.removeFilter(label)} onClick={() => go(removeFilter(search, ref))}>
                        <span>{label}</span>
                        <CloseIcon size={14} />
                      </button>
                    </li>
                  )
                })}
              </ul>
              <Button variant="quiet" size="sm" onClick={() => go(withoutFilters(search))}>
                {t.find.resetAll}
              </Button>
            </div>
          )}

          {result.isPending ? (
            <ResultsSkeleton />
          ) : result.isError ? (
            <EmptyState
              headingLevel={2}
              tone="error"
              title={c.loadError}
              text={describeError(result.error)}
              action={<Button onClick={() => void result.refetch()}>{t.common.retry}</Button>}
            />
          ) : (
            <>
              <div className="results-bar">
                <p className="results-header" aria-live="polite">
                  <span className="num">{result.data.fuzzy ? c.similarFound(total, word) : c.found(total, plural(total, c.foundForms), word)}</span>
                </p>
                {total > 1 && (
                  <Select
                    className="results-sort"
                    label={t.find.sortLabel}
                    options={sortOptions}
                    value={sortNow}
                    onChange={(e) => {
                      const sort = e.target.value as Sort
                      go(change(search, { sort: sort === defaultSort(search) ? '' : sort }))
                    }}
                  />
                )}
              </div>

              {result.data.fuzzy && (
                <Alert kind="info" title={c.fuzzyTitle}>
                  {c.fuzzyText(search.q)}
                </Alert>
              )}

              {result.data.items.length > 0 ? (
                <ul className="sci-list" aria-busy={result.isPlaceholderData}>
                  {result.data.items.map((card) => (
                    <li key={card.id}>
                      <ScientistEntry card={card} headingLevel={2} />
                    </li>
                  ))}
                </ul>
              ) : search.page > 1 && total > 0 ? (
                <EmptyState
                  headingLevel={2}
                  title={t.find.pageGoneTitle(search.page)}
                  text={t.find.pageGoneText}
                  action={<Button onClick={() => go(change(search, { page: 1 }))}>{t.find.toFirstPage}</Button>}
                />
              ) : (
                <EmptyState
                  headingLevel={2}
                  title={c.emptyTitle}
                  text={nFilters > 0 ? c.emptyText : search.q ? c.emptyQueryText : c.emptyNoFilters}
                  action={
                    nFilters > 0 || search.q ? (
                      <Button variant="secondary" onClick={() => go(emptyCatalog())}>
                        {t.find.resetAll}
                      </Button>
                    ) : undefined
                  }
                />
              )}

              {pages > 1 && result.data.items.length > 0 && (
                <nav className="pager" aria-label={t.find.pager}>
                  <Button variant="secondary" disabled={search.page <= 1} onClick={() => go({ ...search, page: search.page - 1 })}>
                    {t.find.prev}
                  </Button>
                  <span className="pager-text num">{t.find.page(search.page, pages)}</span>
                  <Button variant="secondary" disabled={search.page >= pages} onClick={() => go({ ...search, page: search.page + 1 })}>
                    {t.find.next}
                  </Button>
                </nav>
              )}
            </>
          )}
          <p className="catalog-note">{c.note}</p>
        </section>
      </div>
    </div>
  )
}

function ResultsSkeleton() {
  return (
    <div role="status" aria-busy="true" aria-label={t.common.loading}>
      {[0, 1, 2, 3].map((i) => (
        <div className="search-skeleton" key={i} aria-hidden="true">
          <Skeleton width="45%" height="1.4rem" />
          <Skeleton width="70%" height="1rem" />
          <Skeleton width="55%" height="0.9rem" />
        </div>
      ))}
      <span className="visually-hidden">{t.common.loading}</span>
    </div>
  )
}

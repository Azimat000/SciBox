import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { ChevronDownIcon, CloseIcon } from '../../ui/icons'
import { SearchBar } from '../../ui/SearchBar'
import { Select } from '../../ui/Select'
import { Skeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'
import { useReference } from '../vacancies/api'
import { VacancyCard } from '../vacancies/VacancyList'
import { useSearch } from './api'
import { FilterPanel } from './FilterPanel'
import { fieldNames, filterLabel } from './labels'
import {
  PAGE_SIZE,
  activeFilters,
  change,
  defaultSort,
  emptySearch,
  effectiveSort,
  filterCount,
  parseSearch,
  removeFilter,
  sorts,
  toParams,
  withoutFilters,
  type Search,
  type Sort,
} from './params'
import '../../ui/ResultsHeader.css'
import './search.css'

/** Поиск вакансий. Всё выбранное лежит в адресе: ссылкой можно поделиться, «Назад» возвращает прежнюю выдачу. */
export function SearchPage() {
  const [params, setParams] = useSearchParams()
  const search = parseSearch(params)
  const reference = useReference()
  const result = useSearch(search)

  const go = (next: Search) => setParams(toParams(next))

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
  const sortOptions = sorts.filter((s) => s !== 'relevance' || search.q).map((s) => ({ value: s, label: t.find.sorts[s] }))

  return (
    <div className="page search-page">
      <h1>{t.find.title}</h1>
      <p className="lead">{t.find.lead}</p>

      <div className="search-bar">
        <SearchBar
          query={draft}
          onQueryChange={setDraft}
          regions={regions}
          region={search.region}
          onRegionChange={(region) => go(change(search, { region }))}
          onSubmit={() => go(change(search, { q: draft.trim() }))}
        />
      </div>

      <div className="search-layout">
        <aside className="search-filters" aria-label={t.find.filtersTitle}>
          <button
            type="button"
            className="filters-toggle"
            aria-expanded={filtersOpen}
            aria-controls="search-filter-panel"
            onClick={() => setFiltersOpen((v) => !v)}
          >
            <span>{nFilters > 0 ? t.find.filtersCount(nFilters) : t.find.filters}</span>
            <ChevronDownIcon size={18} />
          </button>
          <div id="search-filter-panel" className="filters-body" data-open={filtersOpen}>
            <FilterPanel search={search} onChange={go} reference={reference.data} />
            <Button
              className="filters-done"
              onClick={() => {
                setFiltersOpen(false)
                top.current?.scrollIntoView?.({ block: 'start' })
              }}
            >
              {result.data ? t.find.showResults(total, plural(total, t.search.vacancyForms)) : t.find.hideFilters}
            </Button>
          </div>
        </aside>

        <section className="search-results" aria-label={t.find.results} ref={top}>
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
              title={t.find.loadError}
              text={describeError(result.error)}
              action={<Button onClick={() => void result.refetch()}>{t.common.retry}</Button>}
            />
          ) : (
            <>
              <div className="results-bar">
                <p className="results-header" aria-live="polite">
                  <span className="num">
                    {result.data.fuzzy
                      ? t.find.similarFound(total, plural(total, t.search.vacancyForms))
                      : t.search.found(total, plural(total, t.search.foundForms), plural(total, t.search.vacancyForms))}
                  </span>
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

              {result.data.fuzzy && <Alert kind="info" title={t.find.fuzzyTitle}>{t.find.fuzzyText(search.q)}</Alert>}

              {result.data.items.length > 0 ? (
                <ul className="vacancy-list" aria-busy={result.isPlaceholderData}>
                  {result.data.items.map((c) => (
                    <li key={c.id}>
                      <VacancyCard card={c} headingLevel={2} />
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
                  title={t.find.emptyTitle}
                  text={nFilters > 0 ? t.find.emptyText : search.q ? t.find.emptyQueryText : t.find.emptyNoFilters}
                  action={
                    nFilters > 0 || search.q ? (
                      <Button variant="secondary" onClick={() => go(emptySearch())}>
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
          <Skeleton width="60%" height="1.4rem" />
          <Skeleton width="35%" height="0.9rem" />
          <Skeleton width="92%" height="1rem" />
          <Skeleton width="70%" height="1rem" />
        </div>
      ))}
      <span className="visually-hidden">{t.common.loading}</span>
    </div>
  )
}

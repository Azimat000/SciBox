import { useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { VacancyCard } from '../vacancies/VacancyList'
import { PAGE_SIZE, useMatches, type MatchBasis, type MatchItem } from './api'
import { reasonLabel } from './labels'
import { Pager } from './Pager'
import { ShelfTabs } from './ShelfTabs'
import '../../ui/ResultsHeader.css'
import './matching.css'

/** «Подходящие вам»: вакансии, отобранные по профилю простыми правилами. У каждой написано, почему она в списке. */
export function MatchesPage() {
  return <RequireUser>{() => <Matches />}</RequireUser>
}

function Matches() {
  const m = t.matching.matches
  const [params, setParams] = useSearchParams()
  const page = Math.max(1, Number(params.get('page')) || 1)
  const query = useMatches(page)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={m.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, total, ready, basis } = query.data
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (n: number) => {
    const next = new URLSearchParams(params)
    if (n > 1) next.set('page', String(n))
    else next.delete('page')
    setParams(next)
  }

  return (
    <div className="page shelf-page">
      <h1>{m.title}</h1>
      <p className="lead">{m.lead}</p>
      <ShelfTabs current="matches" />
      {!ready ? (
        <EmptyState
          headingLevel={2}
          title={m.notReadyTitle}
          text={m.notReadyText}
          action={<ButtonLink to="/profile/edit">{m.notReadyAction}</ButtonLink>}
        />
      ) : (
        <>
          <Basis basis={basis} />
          {total === 0 ? (
            <EmptyState
              headingLevel={2}
              title={m.emptyTitle}
              text={m.emptyText}
              action={<ButtonLink to="/vacancies">{m.emptyAction}</ButtonLink>}
            />
          ) : items.length === 0 ? (
            <EmptyState
              headingLevel={2}
              title={t.matching.favorites.pageGoneTitle(page)}
              text={t.matching.favorites.pageGoneText}
              action={<Button onClick={() => go(1)}>{t.matching.favorites.toFirstPage}</Button>}
            />
          ) : (
            <>
              <p className="results-header num">{m.count(total, plural(total, m.countForms))}</p>
              <ul className="vacancy-list" aria-busy={query.isPlaceholderData}>
                {items.map((item) => (
                  <li key={item.vacancy.id}>
                    <VacancyCard card={item.vacancy} headingLevel={2} footer={<Reasons item={item} />} />
                  </li>
                ))}
              </ul>
              <Pager page={page} pages={pages} onPage={go} />
            </>
          )}
        </>
      )}
    </div>
  )
}

/** На что опирается подбор: человек видит, что учтено, и может это поправить в профиле. */
function Basis({ basis }: { basis: MatchBasis }) {
  const m = t.matching.matches
  const parts = [
    m.basisFields(basis.specialties, plural(basis.specialties, m.basisFieldForms)),
    m.basisLevel(basis.level),
    basis.has_degree ? m.basisDegree : null,
    basis.has_region ? m.basisRegion : null,
  ].filter((part): part is string => part !== null)
  return (
    <section className="match-basis" aria-labelledby="match-basis-title">
      <h2 id="match-basis-title">{m.basisTitle}</h2>
      <p>
        {parts.join(' · ')}. {m.basisNote}
      </p>
      <ButtonLink to="/profile/edit" variant="secondary" size="sm">
        {m.toProfile}
      </ButtonLink>
    </section>
  )
}

function Reasons({ item }: { item: MatchItem }) {
  return (
    <div className="match-why">
      <span className="match-why-label">{t.matching.matches.reasonsLabel}:</span>
      <ul>
        {item.reasons.map((code) => (
          <li key={code}>{reasonLabel(code)}</li>
        ))}
      </ul>
    </div>
  )
}

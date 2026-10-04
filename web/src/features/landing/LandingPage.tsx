import { useState, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { typo } from '../../lib/typo'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { SearchBar } from '../../ui/SearchBar'
import { Skeleton, VacancyListSkeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'
import { useMe } from '../auth/api'
import { change, emptySearch, toParams, toggleMulti, type MultiKey, type Search } from '../search/params'
import { fieldNames } from '../search/labels'
import { useReference } from '../vacancies/api'
import { positionTypeLabel } from '../vacancies/labels'
import { VacancyCard } from '../vacancies/VacancyList'
import { FRESH_COUNT, useFreshVacancies, useLandingStats, type LandingStats } from './api'
import './landing.css'

/** Главная страница: что это за сайт, поиск, свежие вакансии и входы для учёных и организаций (D-016). */
export function LandingPage() {
  const stats = useLandingStats()
  return (
    <div className="page landing">
      <Hero stats={stats.data} />
      <Fresh total={stats.data?.open_vacancies} />
      <Browse stats={stats.data} pending={stats.isPending} />
      <Tracks />
      <Science />
    </div>
  )
}

/** Адрес поиска с условиями: без условий это просто /vacancies. */
function searchPath(search: Search): string {
  const qs = toParams(search).toString()
  return qs ? `/vacancies?${qs}` : '/vacancies'
}

/** Ссылка в поиск с одним условием: область науки или вид позиции. */
const searchLink = (key: Extract<MultiKey, 'field' | 'type'>, value: string) => searchPath(toggleMulti(emptySearch(), key, value))

function Hero({ stats }: { stats: LandingStats | undefined }) {
  const navigate = useNavigate()
  const reference = useReference()
  const [q, setQ] = useState('')
  const [region, setRegion] = useState('')
  const regions = (reference.data?.regions ?? []).map((r) => ({ value: r.code, label: r.name }))

  // Поиск живёт на /vacancies: главная только собирает условия и передаёт их туда.
  const go = () => navigate(searchPath(change(emptySearch(), { q: q.trim(), region })))

  return (
    <section className="hero" aria-labelledby="landing-title">
      <div className="hero-main">
        <h1 id="landing-title" className="hero-title">
          <span>{typo(t.landing.titleFirst)}</span> <span className="hero-title-second">{typo(t.landing.titleSecond)}</span>
        </h1>
        <p className="hero-lead">{typo(t.landing.lead)}</p>
        <div className="hero-search">
          <SearchBar query={q} onQueryChange={setQ} regions={regions} region={region} onRegionChange={setRegion} onSubmit={go} />
        </div>
        <div className="hero-facts-slot">{stats && <Facts stats={stats} />}</div>
      </div>
    </section>
  )
}

/** Три числа одной строкой, каждое ведёт туда, где его можно проверить. Нулевые не показываем. */
function Facts({ stats }: { stats: LandingStats }) {
  const items = [
    { n: stats.open_vacancies, to: '/vacancies', forms: t.landing.vacancyForms },
    { n: stats.hiring_organizations, to: '/organizations', forms: t.landing.organizationForms },
    { n: stats.scientists, to: '/scientists', forms: t.landing.scientistForms },
  ].filter((i) => i.n > 0)
  if (items.length === 0) return null
  return (
    <ul className="hero-facts" aria-label={t.landing.factsLabel}>
      {items.map((i) => (
        <li key={i.to}>
          <Link to={i.to}>
            <strong className="num">{i.n}</strong> {plural(i.n, i.forms)}
          </Link>
        </li>
      ))}
    </ul>
  )
}

/**
 * Оглавление: открытые вакансии по областям науки и по видам позиций. На компьютере стоит справа от заголовка, на телефоне
 * уходит под свежие вакансии (в разметке оно после них).
 */
function Browse({ stats, pending }: { stats: LandingStats | undefined; pending: boolean }) {
  const reference = useReference()
  if (pending) return <BrowseSkeleton />
  if (!stats || (stats.fields.length === 0 && stats.types.length === 0)) return null
  const names = fieldNames(reference.data)
  const row = (key: string, to: string, name: string, n: number) => (
    <li key={key}>
      <Link to={to} aria-label={t.landing.browseCount(name, n, plural(n, t.search.vacancyForms))}>
        <span className="browse-name">{name}</span>
        <span className="browse-count num" aria-hidden="true">
          {n}
        </span>
      </Link>
    </li>
  )
  return (
    <nav className="hero-browse" aria-label={t.landing.browseLabel}>
      {stats.fields.length > 0 && (
        <section>
          <h2 className="browse-title">{t.landing.byField}</h2>
          <ul className="browse-list">
            {stats.fields.map((f) => row(`f${f.code}`, searchLink('field', f.code), names.get(f.code) ?? t.landing.fieldUnknown(f.code), f.vacancies))}
          </ul>
        </section>
      )}
      {stats.types.length > 0 && (
        <section>
          <h2 className="browse-title">{t.landing.byType}</h2>
          <ul className="browse-list">
            {stats.types.map((ty) => row(`t${ty.type}`, searchLink('type', ty.type), positionTypeLabel(ty.type), ty.vacancies))}
          </ul>
        </section>
      )}
    </nav>
  )
}

/** Пока числа не пришли, место оглавления занято, чтобы страница не прыгала. */
function BrowseSkeleton() {
  return (
    <div className="hero-browse" role="status" aria-busy="true" aria-label={t.common.loading}>
      <section>
        <div className="browse-skeleton" aria-hidden="true">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} width={`${70 - i * 6}%`} height="1rem" />
          ))}
        </div>
      </section>
    </div>
  )
}

function Fresh({ total }: { total: number | undefined }) {
  const fresh = useFreshVacancies()
  const { user } = useMe()
  return (
    <section className="landing-section" aria-labelledby="fresh-title">
      <div className="section-head">
        <h2 id="fresh-title">{t.landing.fresh.title}</h2>
        {fresh.data && fresh.data.items.length > 0 && (
          <Link to="/vacancies" className="section-more">
            {total && total > fresh.data.items.length ? t.landing.fresh.allCount(total) : t.landing.fresh.all}
          </Link>
        )}
      </div>
      {fresh.isPending ? (
        <VacancyListSkeleton count={3} />
      ) : fresh.isError ? (
        <EmptyState
          headingLevel={3}
          tone="error"
          title={t.landing.fresh.loadError}
          text={describeError(fresh.error)}
          action={<Button onClick={() => void fresh.refetch()}>{t.common.retry}</Button>}
        />
      ) : fresh.data.items.length === 0 ? (
        <EmptyState
          headingLevel={3}
          title={t.landing.fresh.emptyTitle}
          text={t.landing.fresh.emptyText}
          action={<ButtonLink to={user ? '/my-vacancies/new' : '/login?next=%2Fmy-vacancies%2Fnew'}>{t.landing.fresh.emptyAction}</ButtonLink>}
        />
      ) : (
        <ul className="vacancy-list">
          {fresh.data.items.slice(0, FRESH_COUNT).map((c) => (
            <li key={c.id}>
              <VacancyCard card={c} headingLevel={3} />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** Две дорожки рядом: человек читает свою и видит, что у другой стороны то же самое. */
function Tracks() {
  const { user, isLoading } = useMe()
  return (
    <section className="tracks" aria-label={t.landing.tracksLabel}>
      <Track copy={t.landing.seeker}>
        {!isLoading && (
          <>
            <ButtonLink to={user ? '/profile' : '/register'}>{user ? t.landing.seeker.mine : t.landing.seeker.create}</ButtonLink>
            <ButtonLink to="/organizations" variant="secondary">
              {t.landing.seeker.organizations}
            </ButtonLink>
          </>
        )}
      </Track>
      <Track copy={t.landing.employer}>
        {!isLoading && (
          <>
            <ButtonLink to={user ? '/my-organization' : '/organizations/new'}>{user ? t.landing.employer.mine : t.landing.employer.create}</ButtonLink>
            <ButtonLink to="/scientists" variant="secondary">
              {t.landing.employer.scientists}
            </ButtonLink>
          </>
        )}
      </Track>
    </section>
  )
}

function Track({ copy, children }: { copy: { title: string; text: string; steps: readonly string[] }; children: ReactNode }) {
  return (
    <div className="track">
      <h2>{copy.title}</h2>
      <p className="track-text">{copy.text}</p>
      <ol className="track-steps">
        {copy.steps.map((s) => (
          <li key={s}>{s}</li>
        ))}
      </ol>
      <div className="track-actions">{children}</div>
    </div>
  )
}

function Science() {
  return (
    <section className="landing-section science" aria-labelledby="science-title">
      <h2 id="science-title">{t.landing.science.title}</h2>
      <dl className="science-list">
        {t.landing.science.items.map((item) => (
          <div key={item.term} className="science-item">
            <dt>{item.term}</dt>
            <dd>{item.text}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

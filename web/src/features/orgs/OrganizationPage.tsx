import { Link, useParams } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { Tag } from '../../ui/Tag'
import { VacancyList } from '../vacancies/VacancyList'
import { NotFoundPage } from '../status/NotFoundPage'
import { useOrganization, type Unit, isNotFound } from './api'
import { kindLabel, longName, siteLabel, unitKindShort } from './labels'
import { LoadFailed, PageSkeleton } from './states'
import './orgs.css'

/** Публичная страница организации: описание, подразделения, вакансии. */
export function OrganizationPage() {
  const { slug = '' } = useParams()
  const query = useOrganization(slug)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <NotFoundPage />
    ) : (
      <LoadFailed title={t.orgs.page.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  const { organization: org, units, viewer } = query.data
  const isMember = viewer !== null && viewer.role !== ''

  return (
    <div className="page org-page">
      <header className="org-head">
        <h1 data-long={longName(org.name)}>{org.name}</h1>
        <p className="org-meta">
          <Tag tone="accent">{kindLabel(org.kind)}</Tag>
          <span>{org.city}</span>
          {org.website && (
            <a href={org.website} target="_blank" rel="noopener noreferrer">
              {siteLabel(org.website)}
            </a>
          )}
        </p>
        {isMember && (
          <div className="org-actions">
            <ButtonLink to={`/my-organization/${org.slug}`} variant="secondary">
              {t.orgs.page.manage}
            </ButtonLink>
          </div>
        )}
      </header>

      {org.description && <p className="org-abstract">{org.description}</p>}

      <section className="org-section" aria-labelledby="org-units">
        <h2 id="org-units">{t.orgs.page.units}</h2>
        {units.length === 0 ? (
          <p className="org-section-note">{t.orgs.page.unitsEmpty}</p>
        ) : (
          <ul className="org-list">
            {units.map((u) => (
              <UnitEntry key={u.id} slug={org.slug} unit={u} />
            ))}
          </ul>
        )}
      </section>

      <section className="org-section" aria-labelledby="org-vacancies">
        <h2 id="org-vacancies">{t.orgs.page.vacancies}</h2>
        <VacancyList slug={org.slug} />
      </section>
    </div>
  )
}

function UnitEntry({ slug, unit }: { slug: string; unit: Unit }) {
  return (
    <li className="org-entry">
      <h3 className="org-entry-title" data-long={longName(unit.name)}>
        <Link to={`/organizations/${slug}/units/${unit.id}`}>{unit.name}</Link>
      </h3>
      <p className="org-entry-meta">
        {unitKindShort(unit.kind)}
        {unit.head_name ? ` · ${t.orgs.page.head}: ${unit.head_name}` : ''}
      </p>
      {unit.description && <p className="org-entry-summary">{unit.description}</p>}
      {unit.topics.length > 0 && (
        <ul className="org-topics" aria-label={t.orgs.page.topicsList}>
          {unit.topics.map((topic) => (
            <li key={topic}>
              <Tag>{topic}</Tag>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

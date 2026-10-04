import { usePageTitle } from '../../app/pageTitle'
import { Link, useParams } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { Tag } from '../../ui/Tag'
import { VacancyList } from '../vacancies/VacancyList'
import { NotFoundPage } from '../status/NotFoundPage'
import { useUnit, isNotFound } from './api'
import { kindLabel, longName, unitKindLabel } from './labels'
import { LoadFailed, PageSkeleton } from './states'
import './orgs.css'

/** Публичная страница подразделения: описание, темы, руководитель, вакансии. */
export function UnitPage() {
  const { slug = '', unitId = '' } = useParams()
  const query = useUnit(slug, unitId)
  usePageTitle(query.data?.unit.name)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <NotFoundPage />
    ) : (
      <LoadFailed title={t.orgs.page.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  const { organization: org, unit, viewer } = query.data
  const canEdit = viewer !== null && (viewer.can_manage_units || viewer.editable_units.includes(unit.id))

  return (
    <div className="page org-page">
      <header className="org-head">
        <h1 data-long={longName(unit.name)}>{unit.name}</h1>
        <p className="org-meta">
          <Tag tone="accent">{unitKindLabel(unit.kind)}</Tag>
          <span>
            <Link to={`/organizations/${org.slug}`}>{org.name}</Link>, {org.city}
          </span>
          <span>{kindLabel(org.kind)}</span>
        </p>
        <p className="org-meta org-head-person">
          {unit.head_name ? (
            <span>
              {t.orgs.page.head}: <strong>{unit.head_name}</strong>
            </span>
          ) : (
            <span>{t.orgs.page.noHead}</span>
          )}
        </p>
        {canEdit && (
          <div className="org-actions">
            <ButtonLink to={`/my-organization/${org.slug}/units/${unit.id}`} variant="secondary">
              {t.orgs.page.edit}
            </ButtonLink>
          </div>
        )}
      </header>

      <p className="org-abstract">{unit.description || t.orgs.page.noDescription}</p>

      {unit.topics.length > 0 && (
        <section className="org-section" aria-labelledby="unit-topics">
          <h2 id="unit-topics">{t.orgs.page.topics}</h2>
          <ul className="org-topics">
            {unit.topics.map((topic) => (
              <li key={topic}>
                <Tag>{topic}</Tag>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="org-section" aria-labelledby="unit-vacancies">
        <h2 id="unit-vacancies">{t.orgs.page.vacancies}</h2>
        <VacancyList slug={org.slug} unitId={unit.id} />
      </section>
    </div>
  )
}

import type { ReactNode } from 'react'
import { t } from '../../i18n'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { VacancyEntry } from '../../ui/VacancyEntry'
import { describeError } from '../auth/errors'
import { FavoriteButton } from '../matching/FavoriteButton'
import { useOrgVacancies, type Card } from './api'
import { entryFacts, entryTopics, levelParts, placeText } from './labels'
import './vacancies.css'

/**
 * Строка вакансии в списке: город берём у самой вакансии, если его нет (удалённая работа), то у организации.
 * Справа под сроком стоит закладка «В избранное» (action заменяет её, null убирает); footer — дополнение под строкой
 * фактов (например, причины подбора); срок можно не показывать (у закрытых вакансий он уже ни о чём не говорит).
 */
export function VacancyCard({
  card,
  headingLevel = 3,
  footer,
  action,
  showDeadline = true,
}: {
  card: Card
  headingLevel?: 2 | 3 | 4
  footer?: ReactNode
  action?: ReactNode
  showDeadline?: boolean
}) {
  return (
    <VacancyEntry
      to={`/vacancies/${card.id}`}
      title={card.title}
      organization={card.organization.name}
      city={placeText(card) || card.organization.city}
      abstract={card.summary || undefined}
      level={levelParts(card.career_level) ?? undefined}
      facts={entryFacts(card)}
      topics={entryTopics(card) || undefined}
      competition={card.is_competition}
      deadline={showDeadline ? card.deadline || null : undefined}
      headingLevel={headingLevel}
      footer={footer}
      action={action === undefined ? <FavoriteButton vacancyId={card.id} title={card.title} /> : action}
    />
  )
}

/** Опубликованные вакансии организации или одного подразделения: на публичных страницах. */
export function VacancyList({ slug, unitId = null }: { slug: string; unitId?: string | null }) {
  const query = useOrgVacancies(slug, unitId)

  if (query.isPending) {
    return (
      <div role="status" aria-busy="true" aria-label={t.common.loading} className="vacancy-skeleton">
        <Skeleton width="60%" height="1.4rem" />
        <Skeleton width="35%" height="0.9rem" />
        <Skeleton width="90%" height="1rem" />
      </div>
    )
  }
  if (query.isError) {
    return (
      <EmptyState headingLevel={3} tone="error" title={t.vacancies.list.loadError} text={describeError(query.error)} />
    )
  }
  const { items, total } = query.data
  if (items.length === 0) {
    return unitId ? (
      <EmptyState headingLevel={3} title={t.vacancies.list.emptyUnitTitle} text={t.vacancies.list.emptyUnitText} />
    ) : (
      <EmptyState headingLevel={3} title={t.vacancies.list.emptyTitle} text={t.vacancies.list.emptyText} />
    )
  }
  return (
    <>
      <ul className="vacancy-list">
        {items.map((c) => (
          <li key={c.id}>
            <VacancyCard card={c} />
          </li>
        ))}
      </ul>
      {total > items.length && <p className="org-section-note">{t.vacancies.list.more(items.length, total)}</p>}
    </>
  )
}

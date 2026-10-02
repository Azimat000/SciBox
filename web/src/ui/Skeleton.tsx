import { t } from '../i18n'
import './Skeleton.css'

type SkeletonProps = { width?: string; height?: string; className?: string }

/** Серая заготовка на месте текста, пока он грузится. Для чтения с экрана невидима. */
export function Skeleton({ width = '100%', height = '1em', className }: SkeletonProps) {
  return <span className={['skeleton', className ?? ''].filter(Boolean).join(' ')} style={{ width, height }} aria-hidden="true" />
}

/** Заготовка одной вакансии в списке: повторяет форму VacancyEntry, чтобы страница не прыгала. */
export function VacancyEntrySkeleton() {
  return (
    <div className="entry-skeleton" aria-hidden="true">
      <div className="entry-skeleton-main">
        <Skeleton width="62%" height="1.4rem" />
        <Skeleton width="34%" height="0.9rem" />
        <Skeleton width="86%" height="0.95rem" />
        <Skeleton width="70%" height="0.95rem" />
        <Skeleton width="48%" height="0.85rem" />
      </div>
      <div className="entry-skeleton-side">
        <Skeleton width="8rem" height="1rem" />
        <Skeleton width="5rem" height="0.85rem" />
      </div>
    </div>
  )
}

/** Список заготовок с одной подписью для программ чтения с экрана. */
export function VacancyListSkeleton({ count = 3 }: { count?: number }) {
  return (
    <div role="status" aria-busy="true" aria-label={t.common.loading}>
      {Array.from({ length: count }, (_, i) => (
        <VacancyEntrySkeleton key={i} />
      ))}
      <span className="visually-hidden">{t.common.loading}</span>
    </div>
  )
}

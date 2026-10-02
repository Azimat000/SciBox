import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'

/** Серые заготовки, пока страница грузится: заголовок и несколько строк. */
export function PageSkeleton() {
  return (
    <div className="page org-page" role="status" aria-busy="true" aria-label={t.common.loading}>
      <Skeleton width="55%" height="2.25rem" />
      <div className="org-skeleton">
        <Skeleton width="30%" height="1rem" />
        <Skeleton width="90%" height="1.1rem" />
        <Skeleton width="75%" height="1.1rem" />
        <Skeleton width="60%" height="1.1rem" />
      </div>
    </div>
  )
}

/** Не загрузилось: говорим, что именно, и даём повторить. */
export function LoadFailed({ title, error, onRetry }: { title: string; error: unknown; onRetry: () => void }) {
  return (
    <div className="page">
      <EmptyState
        headingLevel={2}
        tone="error"
        title={title}
        text={describeError(error)}
        action={<Button onClick={onRetry}>{t.common.retry}</Button>}
      />
    </div>
  )
}

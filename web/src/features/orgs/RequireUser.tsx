import { useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { useMe, type User } from '../auth/api'
import { PageSkeleton } from './states'

/**
 * Страница только для вошедших. Без входа ведёт на форму входа и после неё возвращает сюда.
 * Если человек был здесь вошедшим, а потом вышел сам, ведём на главную: просьба «войдите, чтобы открыть эту страницу»
 * после намеренного выхода только сбивает с толку.
 */
export function RequireUser({ children }: { children: (user: User) => ReactNode }) {
  const { user, isLoading, isError, refetch } = useMe()
  const location = useLocation()
  const [wasSignedIn, setWasSignedIn] = useState(false)
  if (user && !wasSignedIn) setWasSignedIn(true)

  if (isLoading) return <PageSkeleton />
  if (isError) {
    return (
      <div className="page">
        <EmptyState
          headingLevel={2}
          tone="error"
          title={t.auth.account.loadError}
          text={t.api.unreachable}
          action={<Button onClick={() => void refetch()}>{t.common.retry}</Button>}
        />
      </div>
    )
  }
  if (!user) {
    if (wasSignedIn) return <Navigate to="/" replace />
    const next = encodeURIComponent(location.pathname + location.search)
    return <Navigate to={`/login?next=${next}`} state={{ notice: 'need-login' }} replace />
  }
  return <>{children(user)}</>
}

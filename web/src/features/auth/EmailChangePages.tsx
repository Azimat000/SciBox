import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { confirmEmailChange, meQueryKey, revertEmailChange, setMe, type User } from './api'
import { AuthPage } from './AuthPage'
import { describeError, errorCode } from './errors'

/** Ссылка из письма на новый адрес: почта меняется сразу при открытии (как подтверждение регистрации). */
export function ConfirmEmailChangePage() {
  const token = useSearchParams()[0].get('token') ?? ''
  const client = useQueryClient()
  // Общий ключ: два запуска (режим разработки) шлют один запрос, иначе второй сжёг бы ссылку.
  const confirm = useQuery({
    queryKey: ['confirm-email-change', token],
    enabled: token !== '',
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
    queryFn: async () => {
      const user = await confirmEmailChange(token)
      // Если в этом браузере вошёл тот же человек, шапка и настройки сразу покажут новую почту.
      const me = client.getQueryData<User | null>(meQueryKey)
      if (me && me.id === user.id) await setMe(client, { ...me, email: user.email })
      return user
    },
  })
  const signedIn = Boolean(client.getQueryData<User | null>(meQueryKey))

  if (!token) return <LinkProblem title={t.auth.confirmEmailChange.invalidTitle} text={t.auth.confirmEmailChange.invalidText} />
  if (confirm.isPending) {
    return (
      <AuthPage title={t.auth.confirmEmailChange.checking}>
        <div role="status" aria-busy="true" aria-label={t.auth.confirmEmailChange.checking}>
          <Skeleton width="60%" height="1.25rem" />
        </div>
      </AuthPage>
    )
  }
  if (confirm.isError) {
    const code = errorCode(confirm.error)
    if (code === 'invalid_token') return <LinkProblem title={t.auth.confirmEmailChange.invalidTitle} text={t.auth.confirmEmailChange.invalidText} />
    if (code === 'email_taken') return <LinkProblem title={t.auth.confirmEmailChange.takenTitle} text={t.auth.confirmEmailChange.takenText} />
    return (
      <div className="page page-narrow">
        <EmptyState
          headingLevel={2}
          tone="error"
          title={t.auth.confirmEmailChange.failedTitle}
          text={describeError(confirm.error) === t.api.unreachable ? t.auth.confirmEmailChange.failedText : describeError(confirm.error)}
          action={<Button onClick={() => void confirm.refetch()}>{t.common.retry}</Button>}
        />
      </div>
    )
  }
  return (
    <AuthPage title={t.auth.confirmEmailChange.doneTitle} lead={t.auth.confirmEmailChange.doneText(confirm.data.email)}>
      <div className="auth-status">
        {signedIn ? (
          <ButtonLink to="/account/security">{t.auth.confirmEmailChange.toAccount}</ButtonLink>
        ) : (
          <ButtonLink to="/login">{t.auth.confirmEmailChange.toLogin}</ButtonLink>
        )}
      </div>
    </AuthPage>
  )
}

function LinkProblem({ title, text }: { title: string; text: string }) {
  return (
    <AuthPage title={title} lead={text}>
      <div className="auth-status">
        <ButtonLink to="/account/security" variant="secondary">
          {t.auth.confirmEmailChange.toAccount}
        </ButtonLink>
      </div>
    </AuthPage>
  )
}

/**
 * Ссылка «это не я» из письма на прежний адрес. Срабатывает только по кнопке: она завершает все сеансы,
 * и почтовые программы, которые сами открывают ссылки для проверки, не должны делать этого за человека.
 */
export function RevertEmailChangePage() {
  const token = useSearchParams()[0].get('token') ?? ''
  const client = useQueryClient()
  const revert = useMutation({
    mutationFn: () => revertEmailChange(token),
    onSuccess: async () => {
      client.removeQueries({ predicate: (query) => query.queryKey[0] !== meQueryKey[0] })
      await setMe(client, null)
    },
  })
  const r = t.auth.revertEmailChange

  if (revert.isSuccess) {
    return (
      <AuthPage title={r.doneTitle} lead={r.doneText}>
        <div className="auth-status">
          <ButtonLink to="/login">{t.auth.confirmEmailChange.toLogin}</ButtonLink>
        </div>
      </AuthPage>
    )
  }
  const code = errorCode(revert.error)
  if (!token || code === 'invalid_token' || code === 'email_taken') {
    const taken = code === 'email_taken'
    return (
      <AuthPage title={taken ? r.takenTitle : r.invalidTitle} lead={taken ? r.takenText : r.invalidText}>
        <div className="auth-status">
          <ButtonLink to="/forgot-password" variant="secondary">
            {r.toForgot}
          </ButtonLink>
        </div>
      </AuthPage>
    )
  }
  return (
    <AuthPage title={r.title} lead={r.lead}>
      <div className="auth-status">
        {revert.error && <Alert kind="error">{describeError(revert.error)}</Alert>}
        <Button loading={revert.isPending} onClick={() => revert.mutate()}>
          {r.submit}
        </Button>
      </div>
    </AuthPage>
  )
}

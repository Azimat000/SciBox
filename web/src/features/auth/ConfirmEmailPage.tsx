import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { TextField } from '../../ui/TextField'
import { confirmEmail, resendConfirmation, setMe } from './api'
import { AuthPage } from './AuthPage'
import { describeError } from './errors'
import { useForm } from './useForm'
import { checkEmail, compact } from './validation'

export function ConfirmEmailPage() {
  const token = useSearchParams()[0].get('token') ?? ''
  const client = useQueryClient()

  // Запрос с общим ключом: два одинаковых запуска (как в режиме разработки) шлют на сервер один запрос,
  // иначе вторая отправка сожгла бы ссылку, и человек увидел бы «ссылка не работает».
  const confirm = useQuery({
    queryKey: ['confirm-email', token],
    enabled: token !== '',
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
    queryFn: async () => {
      const user = await confirmEmail(token)
      await setMe(client, user)
      return user
    },
  })

  if (!token) return <InvalidLink />
  if (confirm.isPending) {
    return (
      <AuthPage title={t.auth.confirm.checking}>
        <div role="status" aria-busy="true" aria-label={t.auth.confirm.checking}>
          <Skeleton width="60%" height="1.25rem" />
        </div>
      </AuthPage>
    )
  }
  if (confirm.isError) {
    const err = confirm.error
    if (err instanceof ApiError && err.code === 'invalid_token') return <InvalidLink />
    return (
      <div className="page page-narrow">
        <EmptyState
          headingLevel={2}
          tone="error"
          title={t.auth.confirm.failedTitle}
          text={describeError(err) === t.api.unreachable ? t.auth.confirm.failedText : describeError(err)}
          action={<Button onClick={() => void confirm.refetch()}>{t.common.retry}</Button>}
        />
      </div>
    )
  }
  return (
    <AuthPage title={t.auth.confirm.doneTitle} lead={t.auth.confirm.doneText}>
      <div className="auth-status">
        <ButtonLink to="/">{t.common.toHome}</ButtonLink>
      </div>
    </AuthPage>
  )
}

/** Ссылка из письма не сработала: даём заказать новую, не заставляя начинать с регистрации. */
function InvalidLink() {
  const formRef = useRef<HTMLFormElement>(null)
  const [sent, setSent] = useState(false)
  const resend = useMutation({
    mutationFn: (email: string) => resendConfirmation(email),
    onSuccess: () => setSent(true),
  })
  const { values, set, errors, validate } = useForm({ email: '' }, resend.error, formRef)

  if (sent) {
    return (
      <AuthPage title={t.auth.confirm.resentTitle} lead={t.auth.confirm.resentText}>
        <div className="auth-status">
          <ButtonLink to="/login" variant="secondary">
            {t.auth.confirm.toLogin}
          </ButtonLink>
        </div>
      </AuthPage>
    )
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ email: checkEmail(values.email) }))) resend.mutate(values.email.trim())
  }

  return (
    <AuthPage title={t.auth.confirm.invalidTitle} lead={t.auth.confirm.invalidText}>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {resend.error && <Alert kind="error">{describeError(resend.error)}</Alert>}
        <TextField
          label={t.auth.fields.email}
          type="email"
          name="email"
          autoComplete="email"
          inputMode="email"
          autoCapitalize="none"
          spellCheck={false}
          value={values.email}
          onChange={(e) => set('email', e.target.value)}
          error={errors.email}
          required
        />
        <Button type="submit" loading={resend.isPending}>
          {t.auth.confirm.resend}
        </Button>
      </form>
    </AuthPage>
  )
}

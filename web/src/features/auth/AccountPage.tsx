import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Navigate } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { PasswordField } from '../../ui/PasswordField'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { changePassword, revokeOtherSessions, setMe, updateName, useMe, type User } from './api'
import { AuthPage } from './AuthPage'
import { describeError, fieldErrorsOf } from './errors'
import { SocialSoon } from './SocialSoon'
import { useForm } from './useForm'
import { useSignOut } from './useSignOut'
import { checkName, checkNewPassword, checkPasswordPresent, compact } from './validation'

export function AccountPage() {
  const { user, isLoading, isError, refetch } = useMe()
  // Был ли здесь вошедший человек. Если был, а теперь его нет (вышел сам или сессия кончилась), ведём на главную:
  // просьба «войдите, чтобы открыть эту страницу» после намеренного выхода только сбивает с толку.
  const [wasSignedIn, setWasSignedIn] = useState(false)
  if (user && !wasSignedIn) setWasSignedIn(true)

  if (isLoading) {
    return (
      <AuthPage title={t.auth.account.title} wide>
        <div role="status" aria-busy="true" aria-label={t.common.loading}>
          <Skeleton width="70%" height="1.25rem" />
        </div>
      </AuthPage>
    )
  }
  if (isError) {
    return (
      <div className="page page-narrow">
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
    return wasSignedIn ? <Navigate to="/" replace /> : <Navigate to="/login?next=/account" state={{ notice: 'need-login' }} replace />
  }

  return <Account user={user} />
}

function Account({ user }: { user: User }) {
  const signOut = useSignOut()
  return (
    <AuthPage title={t.auth.account.title} lead={t.auth.account.lead} wide>
      <ProfileSection user={user} />
      <PasswordSection />
      <DevicesSection />
      <section className="account-section">
        <SocialSoon title={t.auth.account.linked} note={t.auth.account.linkedText} />
      </section>
      <section className="account-section">
        <Button variant="secondary" loading={signOut.isPending} onClick={() => signOut.mutate()}>
          {t.auth.account.signOut}
        </Button>
      </section>
    </AuthPage>
  )
}

function ProfileSection({ user }: { user: User }) {
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const save = useMutation({
    mutationFn: (name: string) => updateName(name.trim()),
    onSuccess: async (u) => {
      await setMe(client, u)
      toast.show({ kind: 'success', title: t.auth.account.nameSaved })
    },
  })
  const { values, set, errors, validate } = useForm({ name: user.name }, save.error, formRef)

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ name: checkName(values.name) }))) save.mutate(values.name)
  }
  const formError = save.error && Object.keys(fieldErrorsOf(save.error)).length === 0 ? describeError(save.error) : undefined

  return (
    <section className="account-section" aria-labelledby="account-profile">
      <h2 id="account-profile" className="auth-section-title">
        {t.auth.account.profile}
      </h2>
      <div className="account-email">
        <span className="account-email-label">{t.auth.account.emailLabel}</span>
        <span>{user.email}</span>
        {user.email_confirmed && <Tag tone="accent">{t.auth.account.emailConfirmed}</Tag>}
        <p className="auth-note">{t.auth.account.emailNote}</p>
      </div>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextField
          label={t.auth.fields.name}
          name="name"
          autoComplete="name"
          value={values.name}
          onChange={(e) => set('name', e.target.value)}
          error={errors.name}
          required
        />
        <Button type="submit" loading={save.isPending}>
          {t.auth.account.saveName}
        </Button>
      </form>
    </section>
  )
}

function PasswordSection() {
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const change = useMutation({
    mutationFn: (v: { current: string; next: string }) => changePassword(v.current, v.next),
    onSuccess: () => {
      reset()
      toast.show({ kind: 'success', title: t.auth.account.passwordChanged })
    },
  })
  const { values, set, errors, validate } = useForm({ current: '', next: '' }, change.error, formRef)

  // Сервер называет поля current_password и new_password; в форме они current и next.
  const fieldErrors = { current: errors.current ?? errors.current_password, next: errors.next ?? errors.new_password }
  const reset = () => {
    set('current', '')
    set('next', '')
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({ current: checkPasswordPresent(values.current), next: checkNewPassword(values.next) })
    if (validate(found)) change.mutate({ current: values.current, next: values.next })
  }
  const formError = change.error && Object.keys(fieldErrorsOf(change.error)).length === 0 ? describeError(change.error) : undefined

  return (
    <section className="account-section" aria-labelledby="account-password">
      <h2 id="account-password" className="auth-section-title">
        {t.auth.account.password}
      </h2>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <PasswordField
          label={t.auth.fields.currentPassword}
          name="current-password"
          autoComplete="current-password"
          value={values.current}
          onChange={(e) => set('current', e.target.value)}
          error={fieldErrors.current}
          required
        />
        <PasswordField
          label={t.auth.fields.newPassword}
          hint={t.auth.passwordHint}
          name="new-password"
          autoComplete="new-password"
          value={values.next}
          onChange={(e) => set('next', e.target.value)}
          error={fieldErrors.next}
          required
        />
        <p className="auth-note">{t.auth.account.passwordNote}</p>
        <Button type="submit" loading={change.isPending}>
          {t.auth.account.changePassword}
        </Button>
      </form>
    </section>
  )
}

function DevicesSection() {
  const toast = useToast()
  const revoke = useMutation({
    mutationFn: revokeOtherSessions,
    onSuccess: () => toast.show({ kind: 'success', title: t.auth.account.revoked }),
    onError: (err) => toast.show({ kind: 'error', title: t.auth.account.revokeFailed, text: describeError(err) }),
  })
  return (
    <section className="account-section" aria-labelledby="account-devices">
      <h2 id="account-devices" className="auth-section-title">
        {t.auth.account.devices}
      </h2>
      <p className="auth-note">{t.auth.account.devicesText}</p>
      <Button variant="secondary" loading={revoke.isPending} onClick={() => revoke.mutate()}>
        {t.auth.account.revoke}
      </Button>
    </section>
  )
}

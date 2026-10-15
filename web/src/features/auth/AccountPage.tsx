import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link, Navigate } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { BlockedOrganizations } from '../chat/BlockedOrganizations'
import { MailSettings } from '../matching/MailSettings'
import { setMe, updateName, useMe, type User } from './api'
import { AuthPage } from './AuthPage'
import { describeError, fieldErrorsOf } from './errors'
import { SocialSoon } from './SocialSoon'
import { useForm } from './useForm'
import { useSignOut } from './useSignOut'
import { checkName, compact } from './validation'

export type AccountTab = 'account' | 'security'

const tabPaths: Record<AccountTab, string> = { account: '/account', security: '/account/security' }

/** Две вкладки настроек: «Аккаунт» (имя, письма) и «Безопасность» (пароль, почта, устройства, удаление). */
export function AccountTabs({ current }: { current: AccountTab }) {
  return (
    <nav className="manage-tabs account-tabs" aria-label={t.auth.account.tabs.label}>
      {(Object.keys(tabPaths) as AccountTab[]).map((tab) => (
        <Link key={tab} className="manage-tab" to={tabPaths[tab]} aria-current={current === tab ? 'page' : undefined}>
          {t.auth.account.tabs[tab]}
        </Link>
      ))}
    </nav>
  )
}

type FrameProps = { tab: AccountTab; lead: string; children: (user: User) => ReactNode }

/**
 * Рамка обеих вкладок: ждёт, кто вошёл, ведёт гостя на вход и обратно, показывает вкладки.
 */
export function AccountFrame({ tab, lead, children }: FrameProps) {
  const { user, isLoading, isError, refetch } = useMe()
  // Был ли здесь вошедший человек. Если был, а теперь его нет (вышел сам или сессия кончилась), ведём на главную:
  // просьба «войдите, чтобы открыть эту страницу» после намеренного выхода только сбивает с толку.
  const [wasSignedIn, setWasSignedIn] = useState(false)
  if (user && !wasSignedIn) setWasSignedIn(true)

  if (isLoading) {
    return (
      <AuthPage title={t.auth.account.title} wide tabs={<AccountTabs current={tab} />}>
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
    return wasSignedIn ? <Navigate to="/" replace /> : <Navigate to={`/login?next=${tabPaths[tab]}`} state={{ notice: 'need-login' }} replace />
  }

  return (
    <AuthPage title={t.auth.account.title} lead={lead} wide tabs={<AccountTabs current={tab} />}>
      {children(user)}
    </AuthPage>
  )
}

export function AccountPage() {
  return (
    <AccountFrame tab="account" lead={t.auth.account.lead}>
      {(user) => <Account user={user} />}
    </AccountFrame>
  )
}

function Account({ user }: { user: User }) {
  const signOut = useSignOut()
  return (
    <>
      <ProfileSection user={user} />
      <MailSettings />
      <BlockedOrganizations />
      <section className="account-section">
        <SocialSoon title={t.auth.account.linked} note={t.auth.account.linkedText} />
      </section>
      <section className="account-section">
        <Button variant="secondary" loading={signOut.isPending} onClick={() => signOut.mutate()}>
          {t.auth.account.signOut}
        </Button>
      </section>
    </>
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


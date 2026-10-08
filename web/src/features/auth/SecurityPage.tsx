import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { PasswordField } from '../../ui/PasswordField'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import {
  cancelDeletion,
  changePassword,
  deletionPlanQueryKey,
  endSession,
  meQueryKey,
  requestDeletion,
  requestEmailChange,
  revokeOtherSessions,
  sessionsQueryKey,
  setMe,
  useDeletionPlan,
  useSessions,
  type SessionInfo,
  type User,
} from './api'
import { AccountFrame } from './AccountPage'
import { describeError, fieldErrorsOf } from './errors'
import { deviceLabel, formatDay, formatMoment } from './labels'
import { useReauth } from './reauth-context'
import { useForm } from './useForm'
import '../../ui/Deadline.css'
import { checkEmail, checkNewPassword, checkPasswordPresent, compact } from './validation'

/** «Настройки → Безопасность»: пароль, почта для входа, устройства и удаление аккаунта (срез 20). */
export function SecurityPage() {
  return (
    <AccountFrame tab="security" lead={t.auth.account.securityLead}>
      {(user) => (
        <>
          <PasswordSection />
          <EmailSection user={user} />
          <SessionsSection />
          <DeletionSection user={user} />
        </>
      )}
    </AccountFrame>
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

function EmailSection({ user }: { user: User }) {
  const guard = useReauth()
  const formRef = useRef<HTMLFormElement>(null)
  const [sentTo, setSentTo] = useState<string>()
  const change = useMutation({
    mutationFn: (email: string) => guard(() => requestEmailChange(email)),
    onSuccess: (_, email) => setSentTo(email),
  })
  const { values, set, errors, validate } = useForm({ email: '' }, change.error, formRef)

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ email: checkEmail(values.email) }))) change.mutate(values.email.trim())
  }
  const formError = change.error && Object.keys(fieldErrorsOf(change.error)).length === 0 ? describeError(change.error) : undefined

  return (
    <section className="account-section" aria-labelledby="account-email-change">
      <h2 id="account-email-change" className="auth-section-title">
        {t.auth.emailChange.title}
      </h2>
      <p className="account-current">
        <span className="account-current-label">{t.auth.emailChange.current}</span> <span className="account-current-value">{user.email}</span>
      </p>
      {sentTo ? (
        <Alert
          kind="success"
          title={t.auth.emailChange.sentTitle(sentTo)}
          action={
            <Button
              variant="quiet"
              size="sm"
              onClick={() => {
                setSentTo(undefined)
                set('email', '')
                change.reset()
              }}
            >
              {t.auth.emailChange.another}
            </Button>
          }
        >
          {t.auth.emailChange.sentText}
        </Alert>
      ) : (
        <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
          {formError && <Alert kind="error">{formError}</Alert>}
          <TextField
            label={t.auth.emailChange.newEmail}
            type="email"
            name="new-email"
            autoComplete="email"
            inputMode="email"
            autoCapitalize="none"
            spellCheck={false}
            value={values.email}
            onChange={(e) => set('email', e.target.value)}
            error={errors.email}
            required
          />
          <p className="auth-note">{t.auth.emailChange.note}</p>
          <Button type="submit" loading={change.isPending}>
            {t.auth.emailChange.submit}
          </Button>
        </form>
      )}
    </section>
  )
}

function SessionsSection() {
  const sessions = useSessions()
  const client = useQueryClient()
  const toast = useToast()
  const revokeOthers = useMutation({
    mutationFn: revokeOtherSessions,
    onSuccess: async () => {
      toast.show({ kind: 'success', title: t.auth.sessions.revoked })
      await client.invalidateQueries({ queryKey: sessionsQueryKey })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.auth.sessions.revokeFailed, text: describeError(err) }),
  })

  let body
  if (sessions.isPending) {
    body = (
      <div role="status" aria-busy="true" aria-label={t.common.loading} className="session-list">
        <Skeleton width="60%" height="1.25rem" />
        <Skeleton width="45%" height="1.25rem" />
      </div>
    )
  } else if (sessions.isError) {
    body = (
      <Alert kind="error" title={t.auth.sessions.loadError} action={<Button variant="secondary" size="sm" onClick={() => void sessions.refetch()}>{t.common.retry}</Button>}>
        {describeError(sessions.error)}
      </Alert>
    )
  } else {
    const others = sessions.data.filter((s) => !s.current)
    body = (
      <>
        <ul className="session-list">
          {sessions.data.map((s) => (
            <SessionRow key={s.id} session={s} />
          ))}
        </ul>
        {others.length > 0 ? (
          <Button variant="secondary" loading={revokeOthers.isPending} onClick={() => revokeOthers.mutate()}>
            {t.auth.sessions.revokeOthers}
          </Button>
        ) : (
          <p className="auth-note">{t.auth.sessions.onlyThis}</p>
        )}
      </>
    )
  }

  return (
    <section className="account-section" aria-labelledby="account-sessions">
      <h2 id="account-sessions" className="auth-section-title">
        {t.auth.sessions.title}
      </h2>
      <p className="auth-note">{t.auth.sessions.text}</p>
      {body}
    </section>
  )
}

function SessionRow({ session }: { session: SessionInfo }) {
  const client = useQueryClient()
  const toast = useToast()
  const label = deviceLabel(session)
  const end = useMutation({
    mutationFn: () => endSession(session.id),
    onSuccess: async () => {
      toast.show({ kind: 'success', title: t.auth.sessions.ended })
      await client.invalidateQueries({ queryKey: sessionsQueryKey })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.auth.sessions.endFailed, text: describeError(err) }),
  })
  return (
    <li className="session-row">
      <div className="session-main">
        <p className="session-device">
          {label}
          {session.current && <Tag tone="accent">{t.auth.sessions.thisDevice}</Tag>}
        </p>
        <p className="session-meta">
          {t.auth.sessions.signedIn(formatMoment(session.created_at))}
          {' · '}
          {t.auth.sessions.lastSeen(formatMoment(session.last_seen_at))}
        </p>
      </div>
      {!session.current && (
        <Button variant="secondary" size="sm" loading={end.isPending} onClick={() => end.mutate()} aria-label={t.auth.sessions.endLabel(label)}>
          {t.auth.sessions.end}
        </Button>
      )}
    </li>
  )
}

function DeletionSection({ user }: { user: User }) {
  if (user.deletion_due_at) return <PendingDeletion due={user.deletion_due_at} />
  return <RequestDeletion />
}

function PendingDeletion({ due }: { due: string }) {
  const client = useQueryClient()
  const toast = useToast()
  const cancel = useMutation({
    mutationFn: cancelDeletion,
    onSuccess: async () => {
      toast.show({ kind: 'success', title: t.auth.deletion.cancelled })
      await client.invalidateQueries({ queryKey: meQueryKey })
      await client.invalidateQueries({ queryKey: deletionPlanQueryKey })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.auth.deletion.cancelFailed, text: describeError(err) }),
  })
  return (
    <section className="account-section account-danger" aria-labelledby="account-deletion">
      <h2 id="account-deletion" className="auth-section-title">
        {t.auth.deletion.pendingBefore} <span className="mark">{formatDay(due)}</span>
      </h2>
      <p className="auth-note">{t.auth.deletion.pendingText}</p>
      <Button loading={cancel.isPending} onClick={() => cancel.mutate()}>
        {t.auth.deletion.cancel}
      </Button>
    </section>
  )
}

function RequestDeletion() {
  const plan = useDeletionPlan()
  const guard = useReauth()
  const client = useQueryClient()
  const toast = useToast()
  // Дата для окна подтверждения считается в момент нажатия: срок идёт от просьбы.
  const [confirmDue, setConfirmDue] = useState<string | null>(null)
  const remove = useMutation({
    mutationFn: () => guard(requestDeletion),
    onSuccess: async (due) => {
      setConfirmDue(null)
      // Сервер завершил все сеансы: стираем из памяти всё личное, как при выходе.
      client.removeQueries({ predicate: (query) => query.queryKey[0] !== meQueryKey[0] })
      await setMe(client, null)
      toast.show({ kind: 'info', title: t.auth.deletion.scheduledToast(formatDay(due)), text: t.auth.deletion.scheduledToastText })
    },
  })

  let body
  if (plan.isPending) {
    body = (
      <div role="status" aria-busy="true" aria-label={t.common.loading}>
        <Skeleton width="50%" height="2.5rem" />
      </div>
    )
  } else if (plan.isError) {
    body = (
      <Alert kind="error" title={t.auth.deletion.loadError} action={<Button variant="secondary" size="sm" onClick={() => void plan.refetch()}>{t.common.retry}</Button>}>
        {describeError(plan.error)}
      </Alert>
    )
  } else if (plan.data.blocking.length > 0) {
    body = (
      <Alert kind="info" title={t.auth.deletion.blockedTitle}>
        {t.auth.deletion.blocked}
        <OrgLinks orgs={plan.data.blocking} />
      </Alert>
    )
  } else {
    const days = plan.data.grace_days
    body = (
      <>
        <p className="auth-note">{t.auth.deletion.text(days, plural(days, t.auth.deletion.dayForms))}</p>
        <p className="auth-note">{t.auth.deletion.whatGoes}</p>
        {plan.data.orphaned.length > 0 && (
          <div className="auth-note">
            {t.auth.deletion.orphaned}
            <OrgLinks orgs={plan.data.orphaned} />
          </div>
        )}
        <Button variant="danger" onClick={() => setConfirmDue(new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString())}>
          {t.auth.deletion.request}
        </Button>
        <Modal
          open={confirmDue !== null}
          onClose={() => {
            setConfirmDue(null)
            remove.reset()
          }}
          title={t.auth.deletion.confirmTitle}
          actions={
            <>
              <Button variant="secondary" onClick={() => setConfirmDue(null)}>
                {t.common.cancel}
              </Button>
              <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
                {t.auth.deletion.confirm}
              </Button>
            </>
          }
        >
          {remove.error && <Alert kind="error">{describeError(remove.error)}</Alert>}
          {confirmDue && <p>{t.auth.deletion.confirmText(formatDay(confirmDue))}</p>}
        </Modal>
      </>
    )
  }

  return (
    <section className="account-section account-danger" aria-labelledby="account-deletion">
      <h2 id="account-deletion" className="auth-section-title">
        {t.auth.deletion.title}
      </h2>
      {body}
    </section>
  )
}

function OrgLinks({ orgs }: { orgs: { slug: string; name: string }[] }) {
  return (
    <ul className="account-orgs">
      {orgs.map((o) => (
        <li key={o.slug}>
          <Link to={`/my-organization/${encodeURIComponent(o.slug)}/members`} aria-label={t.auth.deletion.manage(o.name)}>
            {o.name}
          </Link>
        </li>
      ))}
    </ul>
  )
}

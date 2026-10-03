import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { FileIcon, PlusIcon } from '../../ui/icons'
import { sizeText } from '../../lib/fileSize'
import { Modal } from '../../ui/Modal'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { checkEmail, compact } from '../auth/validation'
import { refreshNotifications } from '../notifications/refresh'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { isNotFound } from '../orgs/api'
import { longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { RequireUser } from '../orgs/RequireUser'
import { ProfileView } from '../profile/ProfileView'
import { NotFoundPage } from '../status/NotFoundPage'
import {
  MAX_REFEREES,
  addReferee,
  cancelReferee,
  fileUrl,
  refreshApplications,
  resendReferee,
  useApplication,
  withdrawApplication,
  type Detail,
  type FileRef,
  type Reference,
} from './api'
import { InvitationList } from './Invitations'
import { dateText, dateTimeText, refStatusLabel, statusLabel, statusTone } from './labels'
import './applications.css'

/** Отклик глазами соискателя: что отправлено, как идут рекомендации, можно ли отозвать. Для сотрудников организации — другая страница. */
export function ApplicationPage() {
  const { id = '' } = useParams()
  return <RequireUser>{() => <Loader id={id} />}</RequireUser>
}

function Loader({ id }: { id: string }) {
  const query = useApplication(id)
  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <NotFoundPage />
    ) : (
      <LoadFailed title={t.applications.detail.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  if (query.data.viewer.role === 'staff') return <Navigate to={`/candidates/${id}`} replace />
  return <Page app={query.data} />
}

export function FileLink({ appId, file, label }: { appId: string; file: FileRef; label: string }) {
  return (
    <a className="file-link" href={fileUrl(appId, file.id)} download>
      <FileIcon size={18} />
      <span>{label}</span>
    </a>
  )
}

function Page({ app }: { app: Detail }) {
  const d = t.applications.detail
  const client = useQueryClient()
  const toast = useToast()
  const [confirm, setConfirm] = useState(false)
  const refs = app.references as Reference[]

  const withdraw = useMutation({
    mutationFn: () => withdrawApplication(app.id),
    onSuccess: async () => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      setConfirm(false)
      toast.show({ kind: 'success', title: d.withdrawn })
    },
    onError: (err) => {
      setConfirm(false)
      toast.show({ kind: 'error', title: d.withdrawFailed, text: describeError(err) })
    },
  })

  return (
    <article className="page org-page application-page">
      <header className="application-head">
        <h1 data-long={longName(app.vacancy.title)}>
          <Link to={`/vacancies/${app.vacancy.id}`}>{app.vacancy.title}</Link>
        </h1>
        <p className="application-org">
          <Link to={`/organizations/${app.vacancy.org_slug}`}>{app.vacancy.org_name}</Link>
        </p>
        <p className="application-meta">
          <Tag tone={statusTone(app.status)}>{statusLabel(app.status)}</Tag>
          <span>{d.sentAt(dateText(app.created_at))}</span>
        </p>
      </header>

      {(app.status === 'accepted' || app.status === 'rejected') && (
        <section className="application-section" aria-labelledby="app-decision">
          <h2 id="app-decision">{d.decision}</h2>
          <p className="review-final">{app.status === 'accepted' ? d.decisionAccepted : d.decisionRejected}</p>
          {app.decision_note && (
            <p className="review-note">
              <span>{d.organizationNote}: </span>
              {app.decision_note}
            </p>
          )}
        </section>
      )}

      {app.invitations.length > 0 && (
        <section className="application-section" aria-labelledby="app-invitations">
          <h2 id="app-invitations">{t.applications.invitation.titleApplicant}</h2>
          <InvitationList appId={app.id} invitations={app.invitations} role="applicant" />
        </section>
      )}

      <section className="application-section" aria-labelledby="app-letter">
        <h2 id="app-letter">{d.yourLetter}</h2>
        <p className="application-text">{app.cover_letter}</p>
        <p className="application-contact">
          <span>{d.contact}: </span>
          {app.contact_email}
        </p>
      </section>

      <section className="application-section" aria-labelledby="app-files">
        <h2 id="app-files">{d.files}</h2>
        <ul className="file-links">
          {app.cv && (
            <li>
              <FileLink appId={app.id} file={app.cv} label={`${d.cv}, ${d.download(app.cv.name, sizeText(app.cv.size))}`} />
            </li>
          )}
          {app.files.map((f) => (
            <li key={f.id}>
              <FileLink appId={app.id} file={f} label={d.download(f.name, sizeText(f.size))} />
            </li>
          ))}
        </ul>
        {app.files.length === 0 && <p className="application-muted">{d.noExtraFiles}</p>}
      </section>

      <References app={app} refs={refs} />

      <section className="application-section" aria-labelledby="app-profile">
        <h2 id="app-profile">{d.profileTitle}</h2>
        <p className="application-muted">{d.profileNote}</p>
        <details className="application-profile">
          <summary>{d.profileToggle}</summary>
          <ProfileView nested page={{ profile: app.profile, viewer: { is_owner: false, can_see_contacts: true } }} />
        </details>
      </section>

      {app.viewer.can_withdraw && (
        <div className="application-actions">
          <Button variant="quiet" onClick={() => setConfirm(true)}>
            {d.withdraw}
          </Button>
        </div>
      )}
      <ConfirmModal
        open={confirm}
        title={d.withdrawTitle}
        text={d.withdrawText}
        confirmLabel={d.withdraw}
        pending={withdraw.isPending}
        onConfirm={() => withdraw.mutate()}
        onClose={() => setConfirm(false)}
      />
    </article>
  )
}

function References({ app, refs }: { app: Detail; refs: Reference[] }) {
  const d = t.applications.detail
  const [adding, setAdding] = useState(false)
  const open = app.viewer.can_withdraw // пока отклик жив, рекомендации можно менять
  return (
    <section className="application-section" aria-labelledby="app-refs">
      <h2 id="app-refs">{d.referencesTitle}</h2>
      <p className="application-muted">{d.referencesText}</p>
      {refs.length === 0 ? (
        <p className="application-muted">{d.referencesEmpty}</p>
      ) : (
        <ul className="ref-list">
          {refs.map((r) => (
            <RefRow key={r.id} appId={app.id} r={r} editable={open} />
          ))}
        </ul>
      )}
      {open &&
        (refs.length < MAX_REFEREES ? (
          <div>
            <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
              <PlusIcon size={16} />
              {d.addOpen}
            </Button>
          </div>
        ) : (
          <p className="application-muted">{d.addLimit}</p>
        ))}
      {adding && <AddRefereeModal appId={app.id} onClose={() => setAdding(false)} />}
    </section>
  )
}

function RefRow({ appId, r, editable }: { appId: string; r: Reference; editable: boolean }) {
  const d = t.applications.detail
  const client = useQueryClient()
  const toast = useToast()
  const [confirm, setConfirm] = useState(false)
  const refresh = () => Promise.all([refreshApplications(client), refreshNotifications(client)])
  const fail = (err: unknown) => toast.show({ kind: 'error', title: d.actionFailed, text: describeError(err) })

  const resend = useMutation({
    mutationFn: () => resendReferee(appId, r.id),
    onSuccess: async () => {
      await refresh()
      toast.show({ kind: 'success', title: d.resendDone })
    },
    onError: fail,
  })
  const cancel = useMutation({
    mutationFn: () => cancelReferee(appId, r.id),
    onSuccess: async () => {
      await refresh()
      setConfirm(false)
      toast.show({ kind: 'success', title: d.cancelDone })
    },
    onError: (err) => {
      setConfirm(false)
      fail(err)
    },
  })

  return (
    <li className="ref-row">
      <div className="ref-who">
        <p className="ref-name">{r.name}</p>
        <p className="ref-sub">{[r.relation, r.email].filter(Boolean).join(' · ')}</p>
      </div>
      <div className="ref-state">
        <Tag tone={r.status === 'received' ? 'accent' : 'neutral'}>{refStatusLabel(r.status)}</Tag>
        <span className="ref-when">
          {r.status === 'pending' && d.refWaitUntil(dateText(r.expires_at))}
          {r.status !== 'pending' && r.answered_at && d.refAnswered(dateTimeText(r.answered_at))}
        </span>
      </div>
      {editable && r.status === 'pending' && (
        <div className="ref-actions">
          {r.can_resend ? (
            <Button variant="secondary" size="sm" loading={resend.isPending} onClick={() => resend.mutate()}>
              {d.resend}
            </Button>
          ) : (
            r.resend_at && <span className="ref-when">{d.resendLater(dateTimeText(r.resend_at))}</span>
          )}
          <Button variant="quiet" size="sm" onClick={() => setConfirm(true)}>
            {d.cancel}
          </Button>
        </div>
      )}
      <ConfirmModal
        open={confirm}
        title={d.cancelTitle}
        text={d.cancelText(r.name)}
        confirmLabel={d.cancel}
        pending={cancel.isPending}
        onConfirm={() => cancel.mutate()}
        onClose={() => setConfirm(false)}
      />
    </li>
  )
}

function AddRefereeModal({ appId, onClose }: { appId: string; onClose: () => void }) {
  const d = t.applications.detail
  const a = t.applications.apply
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const add = useMutation({
    mutationFn: (v: { name: string; email: string; relation: string }) => addReferee(appId, v),
    onSuccess: async () => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: d.addDone })
      onClose()
    },
  })
  const { values, set, errors, validate } = useForm({ name: '', email: '', relation: '' }, add.error, formRef)
  const formError = add.error && Object.keys(fieldErrorsOf(add.error)).length === 0 ? describeError(add.error) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const found = compact({ name: values.name.trim() === '' ? d.nameRequired : undefined, email: checkEmail(values.email) })
    if (!validate(found)) return
    add.mutate(values)
  }

  return (
    <Modal open onClose={onClose} title={d.addTitle}>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextField
          label={a.refereeName}
          name="name"
          required
          autoComplete="off"
          value={values.name}
          onChange={(ev) => set('name', ev.target.value)}
          error={errors.name}
        />
        <TextField
          label={a.refereeEmail}
          name="email"
          type="email"
          inputMode="email"
          required
          autoComplete="off"
          value={values.email}
          onChange={(ev) => set('email', ev.target.value)}
          error={errors.email}
        />
        <TextField
          label={a.refereeRelation}
          name="relation"
          optional
          hint={a.refereeRelationHint}
          value={values.relation}
          onChange={(ev) => set('relation', ev.target.value)}
          error={errors.relation}
        />
        <div className="form-actions">
          <Button type="submit" loading={add.isPending}>
            {d.addSubmit}
          </Button>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

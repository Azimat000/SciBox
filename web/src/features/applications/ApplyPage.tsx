import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { FilePicker } from '../../ui/FilePicker'
import { PlusIcon } from '../../ui/icons'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { useLeaveGuard } from '../../ui/useLeaveGuard'
import { checkEmail, compact } from '../auth/validation'
import { isNotFound } from '../orgs/api'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { useOwnProfile } from '../profile/api'
import { NotFoundPage } from '../status/NotFoundPage'
import { useVacancy } from '../vacancies/api'
import { refreshNotifications } from '../notifications/refresh'
import { MAX_FILES, MAX_FILE_BYTES, MAX_REFEREES, refreshApplications, sendApplication, useApplyState, type RefereeFields } from './api'
import './applications.css'

const COVER_MIN = 20
const COVER_MAX = 6000

/** Форма отклика: письмо, файлы, рекомендатели. Профиль и резюме подставятся сами. */
export function ApplyPage() {
  const { id = '' } = useParams()
  return <RequireUser>{(user) => <Loader id={id} accountEmail={user.email} />}</RequireUser>
}

function Loader({ id, accountEmail }: { id: string; accountEmail: string }) {
  const vacancy = useVacancy(id)
  const state = useApplyState(id)
  const profile = useOwnProfile()
  const a = t.applications.apply

  if (vacancy.isPending || state.isPending || profile.isPending) return <PageSkeleton />
  if (vacancy.isError) {
    return isNotFound(vacancy.error) ? <NotFoundPage /> : <LoadFailed title={a.loadError} error={vacancy.error} onRetry={() => void vacancy.refetch()} />
  }
  if (state.isError) {
    return isNotFound(state.error) ? <NotFoundPage /> : <LoadFailed title={a.loadError} error={state.error} onRetry={() => void state.refetch()} />
  }
  if (profile.isError) return <LoadFailed title={a.loadError} error={profile.error} onRetry={() => void profile.refetch()} />

  const v = vacancy.data
  const head = (
    <header className="apply-head">
      <h1>{a.title}</h1>
      <p className="apply-vacancy">
        <Link to={`/vacancies/${v.id}`}>{v.title}</Link>
        <span>{v.organization.name}</span>
      </p>
    </header>
  )
  if (!state.data.can_apply) {
    const s = state.data
    return (
      <div className="page page-narrow apply-page">
        {head}
        {s.reason === 'applied' && s.application ? (
          <Alert
            kind="info"
            action={
              <ButtonLink to={`/applications/${s.application.id}`} variant="secondary" size="sm">
                {a.seeApplication}
              </ButtonLink>
            }
          >
            {a.alreadyApplied}
          </Alert>
        ) : (
          <Alert kind="info">{s.reason === 'own_vacancy' ? a.own : s.reason === 'closed' ? a.closed : a.deadlinePassed}</Alert>
        )}
        <p>
          <ButtonLink to={`/vacancies/${v.id}`} variant="quiet">
            {a.back}
          </ButtonLink>
        </p>
      </div>
    )
  }
  const own = profile.data.profile
  return <ApplyForm head={head} vacancyId={v.id} complete={own.headline.trim() !== ''} contact={own.contact_email || accountEmail} />
}

const emptyReferee = (): RefereeFields => ({ name: '', email: '', relation: '' })

function ApplyForm({ head, vacancyId, complete, contact }: { head: React.ReactNode; vacancyId: string; complete: boolean; contact: string }) {
  const a = t.applications.apply
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const [files, setFiles] = useState<File[]>([])
  const [referees, setReferees] = useState<RefereeFields[]>([])

  const send = useMutation({
    mutationFn: (values: { contact_email: string; cover_letter: string }) =>
      sendApplication({ vacancy_id: vacancyId, ...values, referees: referees.map((r) => ({ ...r })) }, files),
    onSuccess: async (saved) => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: a.sent })
      guard.release()
      void navigate(`/applications/${saved.id}`)
    },
  })
  const { values, set, errors, validate, dirty } = useForm({ contact_email: contact, cover_letter: '' }, send.error, formRef)
  const guard = useLeaveGuard(dirty || files.length > 0 || referees.length > 0)
  const serverFields = fieldErrorsOf(send.error)
  const formError = send.error && Object.keys(serverFields).length === 0 ? describeError(send.error) : undefined
  const coverLength = [...values.cover_letter.trim()].length

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const found = compact({
      contact_email: checkEmail(values.contact_email),
      cover_letter: coverLength < COVER_MIN ? a.coverShort : coverLength > COVER_MAX ? a.coverLong : undefined,
    })
    if (!validate(found)) return
    send.mutate(values)
  }
  const setReferee = (i: number, patch: Partial<RefereeFields>) => setReferees((list) => list.map((r, j) => (j === i ? { ...r, ...patch } : r)))

  return (
    <div className="page page-narrow apply-page">
      {guard.prompt}
      {head}
      <section className="apply-sends" aria-labelledby="apply-sends">
        <h2 id="apply-sends">{a.sendsTitle}</h2>
        <p>{a.sendsText}</p>
        <p>
          <Link to="/profile">{a.profileLink}</Link>
        </p>
      </section>
      {!complete && (
        <Alert
          kind="error"
          title={a.incompleteTitle}
          action={
            <ButtonLink to="/profile/edit" variant="secondary" size="sm">
              {a.incompleteAction}
            </ButtonLink>
          }
        >
          {a.incompleteText}
        </Alert>
      )}
      {serverFields.profile && complete && <Alert kind="error">{serverFields.profile}</Alert>}
      <form className="apply-form" onSubmit={submit} ref={formRef} noValidate>
        {formError && (
          <Alert kind="error" title={a.sendFailed}>
            {formError}
          </Alert>
        )}
        <TextField
          label={a.contact}
          name="contact_email"
          type="email"
          inputMode="email"
          autoComplete="email"
          required
          hint={a.contactHint}
          value={values.contact_email}
          onChange={(ev) => set('contact_email', ev.target.value)}
          error={errors.contact_email}
        />
        <div className="apply-cover">
          <TextArea
            label={a.cover}
            name="cover_letter"
            rows={9}
            required
            hint={a.coverHint}
            value={values.cover_letter}
            onChange={(ev) => set('cover_letter', ev.target.value)}
            error={errors.cover_letter}
          />
          <p className="apply-count num" data-over={coverLength > COVER_MAX || undefined}>
            {a.coverCount(coverLength)}
          </p>
        </div>

        <FilePicker
          label={a.files}
          hint={a.filesHint}
          files={files}
          onChange={setFiles}
          addLabel={a.addFile}
          removeLabel={a.removeFile}
          emptyText={a.noFiles}
          max={MAX_FILES}
          maxBytes={MAX_FILE_BYTES}
          messages={{ tooBig: a.fileTooBig, notPdf: a.fileNotPdf, tooMany: a.tooManyFiles }}
          error={errors.files}
        />

        <fieldset className="apply-referees">
          <legend>{a.referees}</legend>
          <p className="field-hint">{a.refereesText}</p>
          {referees.map((r, i) => (
            <div className="apply-referee" key={i} role="group" aria-label={a.refereeN(i + 1)}>
              <div className="apply-referee-head">
                <p className="apply-referee-title">{a.refereeN(i + 1)}</p>
                <Button variant="quiet" size="sm" aria-label={a.removeReferee(i + 1)} onClick={() => setReferees((list) => list.filter((_, j) => j !== i))}>
                  {t.common.remove}
                </Button>
              </div>
              <div className="field-pair">
                <TextField
                  label={a.refereeName}
                  name={`referee-name-${i}`}
                  autoComplete="off"
                  value={r.name}
                  onChange={(ev) => setReferee(i, { name: ev.target.value })}
                />
                <TextField
                  label={a.refereeEmail}
                  name={`referee-email-${i}`}
                  type="email"
                  inputMode="email"
                  autoComplete="off"
                  value={r.email}
                  onChange={(ev) => setReferee(i, { email: ev.target.value })}
                />
              </div>
              <TextField
                label={a.refereeRelation}
                name={`referee-relation-${i}`}
                optional
                hint={a.refereeRelationHint}
                value={r.relation}
                onChange={(ev) => setReferee(i, { relation: ev.target.value })}
              />
            </div>
          ))}
          {serverFields.referees && (
            <p className="field-message" role="alert">
              {serverFields.referees}
            </p>
          )}
          {referees.length < MAX_REFEREES && (
            <div>
              <Button variant="secondary" size="sm" onClick={() => setReferees((list) => [...list, emptyReferee()])}>
                <PlusIcon size={16} />
                {a.addReferee}
              </Button>
            </div>
          )}
        </fieldset>

        <div className="form-actions">
          <Button type="submit" loading={send.isPending} disabled={!complete}>
            {a.submit}
          </Button>
          <ButtonLink to={`/vacancies/${vacancyId}`} variant="quiet">
            {a.back}
          </ButtonLink>
        </div>
      </form>
    </div>
  )
}

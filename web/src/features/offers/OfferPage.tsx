import { usePageTitle } from '../../app/pageTitle'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { Link, useParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { Deadline } from '../../ui/Deadline'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { TextArea } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { dateText } from '../applications/labels'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { refreshNotifications } from '../notifications/refresh'
import { isNotFound } from '../orgs/api'
import { longName } from '../orgs/labels'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { answerOffer, refreshOffers, useOffer, type Offer } from './api'
import { statusLabel, statusTone, vacancyOpen } from './labels'
import '../applications/applications.css'
import './offers.css'

const MAX_NOTE = 1000

/** Приглашение глазами учёного: что написала организация, что за вакансия, ответ «Интересно» или «Не сейчас». */
export function OfferPage() {
  const { id = '' } = useParams()
  return <RequireUser>{() => <Loader id={id} />}</RequireUser>
}

function Loader({ id }: { id: string }) {
  const o = t.offers.one
  const query = useOffer(id)
  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={o.notFoundTitle}
          text={o.notFoundText}
          action={
            <ButtonLink to="/offers" variant="secondary">
              {o.toList}
            </ButtonLink>
          }
        />
      </div>
    ) : (
      <LoadFailed title={o.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  return <Page offer={query.data} />
}

function Page({ offer }: { offer: Offer }) {
  usePageTitle(offer.vacancy.title)
  const o = t.offers.one
  const v = offer.vacancy
  const open = vacancyOpen(offer)
  return (
    <article className="page org-page application-page">
      <header className="application-head">
        <h1 data-long={longName(v.title)}>
          <Link to={`/vacancies/${v.id}`}>{v.title}</Link>
        </h1>
        <p className="application-org">
          {o.from('')}
          <Link to={`/organizations/${v.org_slug}`}>{v.org_name}</Link>
        </p>
        <p className="application-meta">
          <Tag tone={statusTone(offer.status)}>{offer.status === 'pending' ? t.offers.mine.waitsYou : statusLabel(offer.status)}</Tag>
          <span>{o.receivedAt(dateText(offer.created_at))}</span>
        </p>
      </header>

      <section className="application-section" aria-labelledby="offer-message">
        <h2 id="offer-message">{o.messageTitle}</h2>
        {offer.message ? <p className="application-text">{offer.message}</p> : <p className="application-muted">{o.noMessage}</p>}
      </section>

      <section className="application-section" aria-labelledby="offer-vacancy">
        <h2 id="offer-vacancy">{o.vacancyTitle}</h2>
        <p className="offer-vacancy-line">
          {[v.unit_name, v.city].filter(Boolean).join(', ')}
        </p>
        {v.deadline && open !== 'closed' && <Deadline date={v.deadline} />}
        <div className="offer-actions">
          <ButtonLink to={`/vacancies/${v.id}`} variant="secondary">
            {o.openVacancy}
          </ButtonLink>
          {offer.application_id ? (
            <ButtonLink to={`/applications/${offer.application_id}`}>{o.seeApplication}</ButtonLink>
          ) : (
            open === 'open' && <ButtonLink to={`/vacancies/${v.id}/apply`}>{o.apply}</ButtonLink>
          )}
        </div>
        {offer.application_id && <p className="application-muted">{o.alreadyApplied}</p>}
        {!offer.application_id && open === 'closed' && <p className="application-muted">{o.vacancyClosed}</p>}
        {!offer.application_id && open === 'deadline' && <p className="application-muted">{o.deadlinePassed}</p>}
      </section>

      <section className="application-section" aria-labelledby="offer-answer">
        <h2 id="offer-answer">{o.answerTitle}</h2>
        {offer.can_answer ? <AnswerForm offer={offer} /> : <Answered offer={offer} />}
      </section>
    </article>
  )
}

function Answered({ offer }: { offer: Offer }) {
  const o = t.offers.one
  if (offer.status === 'cancelled') return <p className="application-muted">{t.offers.statuses.cancelled}</p>
  return (
    <>
      <p className="review-final">{o.answered(statusLabel(offer.status))}</p>
      {offer.answered_at && <p className="application-muted">{o.answeredAt(dateText(offer.answered_at))}</p>}
      {offer.answer_note && (
        <p className="review-note">
          <span>{o.noteShown}: </span>
          {offer.answer_note}
        </p>
      )}
      {offer.status === 'interested' && !offer.application_id && vacancyOpen(offer) === 'open' && <p className="application-muted">{o.interestedNext}</p>}
    </>
  )
}

function AnswerForm({ offer }: { offer: Offer }) {
  const o = t.offers.one
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const send = useMutation({
    mutationFn: (fields: { action: 'interested' | 'declined'; note: string }) => answerOffer(offer.id, fields),
    onSuccess: async () => {
      await Promise.all([refreshOffers(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: o.sent })
    },
  })
  const { values, set, errors, validate } = useForm({ note: '' }, send.error, formRef)
  const formError = send.error && Object.keys(fieldErrorsOf(send.error)).length === 0 ? describeError(send.error) : undefined

  const submit = (action: 'interested' | 'declined') => {
    if (!validate(values.note.trim().length > MAX_NOTE ? { note: o.noteLong } : {})) return
    send.mutate({ action, note: values.note })
  }

  return (
    <form className="offer-answer" ref={formRef} noValidate onSubmit={(ev) => ev.preventDefault()}>
      <p className="application-muted">{o.answerLead}</p>
      {formError && <Alert kind="error">{formError}</Alert>}
      <TextArea label={o.noteLabel} name="note" optional rows={4} hint={o.noteHint} value={values.note} onChange={(ev) => set('note', ev.target.value)} error={errors.note} />
      <div className="offer-actions">
        <Button loading={send.isPending && send.variables?.action === 'interested'} disabled={send.isPending} onClick={() => submit('interested')}>
          {o.interested}
        </Button>
        <Button variant="secondary" loading={send.isPending && send.variables?.action === 'declined'} disabled={send.isPending} onClick={() => submit('declined')}>
          {o.declined}
        </Button>
      </div>
    </form>
  )
}

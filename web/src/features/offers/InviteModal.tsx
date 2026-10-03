import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Modal } from '../../ui/Modal'
import { Select, type Option, type OptionGroup } from '../../ui/Select'
import { Skeleton } from '../../ui/Skeleton'
import { TextArea } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { refreshNotifications } from '../notifications/refresh'
import { refreshOffers, sendOffer, useOfferTargets, type Target } from './api'
import './offers.css'

const MAX_MESSAGE = 1000

/** Организация зовёт учёного на вакансию: выбор вакансии из тех, что она ведёт, и необязательное сообщение. */
export function InviteModal({ profileId, name, onClose }: { profileId: string; name: string; onClose: () => void }) {
  const i = t.offers.invite
  const targets = useOfferTargets(profileId, true)
  return (
    <Modal open onClose={onClose} title={i.title}>
      {targets.isPending ? (
        <div role="status" aria-busy="true" aria-label={t.common.loading} className="offer-modal-loading">
          <Skeleton width="60%" height="1.1rem" />
          <Skeleton width="100%" height="2.75rem" />
          <Skeleton width="100%" height="6rem" />
        </div>
      ) : targets.isError ? (
        <EmptyState headingLevel={3} tone="error" title={i.loadError} text={describeError(targets.error)} action={<Button onClick={() => void targets.refetch()}>{t.common.retry}</Button>} />
      ) : targets.data.length === 0 ? (
        <EmptyState
          headingLevel={3}
          title={i.noTargetsTitle}
          text={i.noTargetsText}
          action={
            <ButtonLink to="/my-vacancies" variant="secondary">
              {i.noTargetsAction}
            </ButtonLink>
          }
        />
      ) : (
        <Form profileId={profileId} name={name} targets={targets.data} onClose={onClose} />
      )}
    </Modal>
  )
}

function optionOf(x: Target): Option {
  const i = t.offers.invite
  const base = i.vacancyOption(x.title, x.unit_name)
  const label = x.applied ? `${base} — ${i.appliedSuffix}` : x.offered ? `${base} — ${i.offeredSuffix}` : base
  return { value: x.id, label }
}

/** Если человек ведёт вакансии в нескольких организациях, список делится по организациям; иначе он плоский. */
function optionsOf(targets: Target[]): (Option | OptionGroup)[] {
  const orgs = [...new Set(targets.map((x) => x.org_name))]
  if (orgs.length < 2) return targets.map(optionOf)
  return orgs.map((org) => ({ group: org, options: targets.filter((x) => x.org_name === org).map(optionOf) }))
}

function Form({ profileId, name, targets, onClose }: { profileId: string; name: string; targets: Target[]; onClose: () => void }) {
  const i = t.offers.invite
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const free = targets.filter((x) => !x.offered && !x.applied)
  const send = useMutation({
    mutationFn: (fields: { vacancy_id: string; message: string }) => sendOffer({ ...fields, profile_id: profileId }),
    onSuccess: async () => {
      await Promise.all([refreshOffers(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: i.done, text: name })
      onClose()
    },
  })
  // Единственная свободная вакансия выбрана сразу.
  const { values, set, errors, validate } = useForm({ vacancy: free.length === 1 ? free[0].id : '', message: '' }, send.error, formRef)
  const fieldErrors = fieldErrorsOf(send.error)
  const formError = send.error && Object.keys(fieldErrors).length === 0 ? describeError(send.error) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const found: Record<string, string | undefined> = {
      vacancy: values.vacancy ? undefined : i.vacancyRequired,
      message: values.message.trim().length > MAX_MESSAGE ? i.messageLong : undefined,
    }
    if (!validate(compact(found))) return
    send.mutate({ vacancy_id: values.vacancy, message: values.message })
  }

  return (
    <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
      <p className="application-muted">{i.lead}</p>
      {free.length === 0 && <Alert kind="info">{i.allBusy}</Alert>}
      {formError && <Alert kind="error">{formError}</Alert>}
      <Select
        label={i.vacancy}
        name="vacancy"
        required
        placeholder={i.vacancyPlaceholder}
        value={values.vacancy}
        onChange={(ev) => set('vacancy', ev.target.value)}
        options={optionsOf(targets)}
        error={errors.vacancy}
      />
      <TextArea
        label={i.message}
        name="message"
        optional
        rows={5}
        hint={`${i.messageHint} ${i.messageCount(values.message.length)}`}
        value={values.message}
        onChange={(ev) => set('message', ev.target.value)}
        error={errors.message}
      />
      <div className="form-actions">
        <Button type="submit" loading={send.isPending} disabled={free.length === 0}>
          {i.submit}
        </Button>
        <Button variant="quiet" onClick={onClose}>
          {t.common.cancel}
        </Button>
      </div>
    </form>
  )
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { Select } from '../../ui/Select'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { refreshNotifications } from '../notifications/refresh'
import { invitationKinds, refreshApplications, sendInvitation, type InvitationFields, type InvitationKind, type PlaceKind } from './api'
import { kindLabel, mskMoment } from './labels'

/** Организация приглашает кандидата: собеседование, свои контакты или просьба оставить контакты. */
export function InviteModal({ appId, onClose }: { appId: string; onClose: () => void }) {
  const i = t.applications.invitation
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const send = useMutation({
    mutationFn: (fields: InvitationFields) => sendInvitation(appId, fields),
    onSuccess: async () => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: i.inviteDone })
      onClose()
    },
  })
  const { values, set, errors, validate } = useForm(
    { kind: 'interview', date: '', time: '', place_kind: 'online', place: '', message: '', contact_name: '', contact_email: '', contact_phone: '' },
    send.error,
    formRef,
  )
  const kind = values.kind as InvitationKind
  const online = values.place_kind === 'online'
  const formError = send.error && Object.keys(fieldErrorsOf(send.error)).length === 0 ? describeError(send.error) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const found: Record<string, string | undefined> = {}
    if (kind === 'interview') {
      found.date = values.date ? undefined : i.dateRequired
      found.time = values.time ? undefined : i.timeRequired
      found.place = values.place.trim() ? undefined : online ? i.placeRequiredOnline : i.placeRequiredOnsite
    }
    if (kind === 'contacts') found.contact_email = values.contact_email.trim() || values.contact_phone.trim() ? undefined : i.contactRequired
    if (!validate(compact(found))) return
    const fields: InvitationFields = { kind, message: values.message }
    if (kind === 'interview') {
      Object.assign(fields, { starts_at: mskMoment(values.date, values.time), place_kind: values.place_kind as PlaceKind, place: values.place })
    }
    if (kind === 'contacts') {
      Object.assign(fields, { contact_name: values.contact_name, contact_email: values.contact_email, contact_phone: values.contact_phone })
    }
    send.mutate(fields)
  }

  return (
    <Modal open onClose={onClose} title={i.inviteTitle}>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        <p className="application-muted">{i.inviteLead}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <Select
          label={i.kind}
          name="kind"
          value={values.kind}
          onChange={(ev) => set('kind', ev.target.value)}
          options={invitationKinds.map((k) => ({ value: k, label: kindLabel(k) }))}
          error={errors.kind}
        />

        {kind === 'interview' && (
          <>
            <div className="inv-when-fields">
              <TextField label={i.date} name="date" type="date" required value={values.date} onChange={(ev) => set('date', ev.target.value)} error={errors.date ?? errors.starts_at} />
              <TextField label={i.time} name="time" type="time" required value={values.time} onChange={(ev) => set('time', ev.target.value)} error={errors.time} />
            </div>
            <Select
              label={i.format}
              name="place_kind"
              value={values.place_kind}
              onChange={(ev) => set('place_kind', ev.target.value)}
              options={[
                { value: 'online', label: i.online },
                { value: 'onsite', label: i.onsite },
              ]}
              error={errors.place_kind}
            />
            <TextField
              label={online ? i.placeOnline : i.placeOnsite}
              name="place"
              required
              hint={online ? i.placeOnlineHint : i.placeOnsiteHint}
              inputMode={online ? 'url' : undefined}
              value={values.place}
              onChange={(ev) => set('place', ev.target.value)}
              error={errors.place}
            />
          </>
        )}

        {kind === 'contacts' && (
          <>
            <TextField label={i.contactName} name="contact_name" optional value={values.contact_name} onChange={(ev) => set('contact_name', ev.target.value)} error={errors.contact_name} />
            <TextField
              label={i.contactEmail}
              name="contact_email"
              type="email"
              inputMode="email"
              hint={i.contactsHint}
              value={values.contact_email}
              onChange={(ev) => set('contact_email', ev.target.value)}
              error={errors.contact_email}
            />
            <TextField label={i.contactPhone} name="contact_phone" type="tel" inputMode="tel" value={values.contact_phone} onChange={(ev) => set('contact_phone', ev.target.value)} error={errors.contact_phone} />
          </>
        )}

        {kind === 'request_contacts' && <p className="application-muted">{i.requestHint}</p>}

        <TextArea label={i.messageLabel} name="message" optional rows={4} hint={kind === 'interview' ? i.messageHint : undefined} value={values.message} onChange={(ev) => set('message', ev.target.value)} error={errors.message} />

        <div className="form-actions">
          <Button type="submit" loading={send.isPending}>
            {i.inviteSubmit}
          </Button>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

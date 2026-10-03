import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { fieldErrorsOf, describeError } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { refreshNotifications } from '../notifications/refresh'
import { answerInvitation, refreshApplications, type AnswerFields } from './api'
import { mskMoment } from './labels'

type Props = { appId: string; invitationId: string; onClose: () => void }

/** Общая отправка ответа: после успеха всё перечитывается, окно закрывается. */
function useAnswer({ appId, invitationId, onClose }: Props, done: string) {
  const client = useQueryClient()
  const toast = useToast()
  return useMutation({
    mutationFn: (fields: AnswerFields) => answerInvitation(appId, invitationId, fields),
    onSuccess: async () => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: done })
      onClose()
    },
  })
}

const formErrorOf = (error: unknown) => (error && Object.keys(fieldErrorsOf(error)).length === 0 ? describeError(error) : undefined)

/** Соискатель предлагает другое время собеседования. */
export function ProposeModal(props: Props) {
  const i = t.applications.invitation
  const formRef = useRef<HTMLFormElement>(null)
  const answer = useAnswer(props, i.proposeDone)
  const { values, set, errors, validate } = useForm({ date: '', time: '', note: '' }, answer.error, formRef)
  const formError = formErrorOf(answer.error)

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const found = compact({ date: values.date ? undefined : i.dateRequired, time: values.time ? undefined : i.timeRequired })
    if (!validate(found)) return
    answer.mutate({ action: 'propose', proposed_at: mskMoment(values.date, values.time), note: values.note })
  }

  return (
    <Modal open onClose={props.onClose} title={i.proposeTitle}>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        <p className="application-muted">{i.proposeLead}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <div className="inv-when-fields">
          <TextField label={i.date} name="date" type="date" required value={values.date} onChange={(ev) => set('date', ev.target.value)} error={errors.date ?? errors.proposed_at} />
          <TextField label={i.time} name="time" type="time" required value={values.time} onChange={(ev) => set('time', ev.target.value)} error={errors.time} />
        </div>
        <TextArea label={i.noteLabel} name="note" optional rows={3} hint={i.noteHint} value={values.note} onChange={(ev) => set('note', ev.target.value)} error={errors.note} />
        <div className="form-actions">
          <Button type="submit" loading={answer.isPending}>
            {i.proposeSubmit}
          </Button>
          <Button variant="quiet" onClick={props.onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

/** Соискатель отвечает на просьбу оставить контакты. */
export function ReplyModal(props: Props) {
  const i = t.applications.invitation
  const formRef = useRef<HTMLFormElement>(null)
  const answer = useAnswer(props, i.replyDone)
  const { values, set, errors, validate } = useForm({ contact: '', time: '' }, answer.error, formRef)
  const formError = formErrorOf(answer.error)

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    if (!validate(compact({ contact: values.contact.trim() ? undefined : i.replyContactRequired }))) return
    answer.mutate({ action: 'reply', contact: values.contact, time: values.time })
  }

  return (
    <Modal open onClose={props.onClose} title={i.replyTitle}>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        <p className="application-muted">{i.replyLead}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextField label={i.replyContact} name="contact" required hint={i.replyContactHint} value={values.contact} onChange={(ev) => set('contact', ev.target.value)} error={errors.contact} />
        <TextField label={i.replyTime} name="time" optional hint={i.replyTimeHint} value={values.time} onChange={(ev) => set('time', ev.target.value)} error={errors.time} />
        <div className="form-actions">
          <Button type="submit" loading={answer.isPending}>
            {i.replySubmit}
          </Button>
          <Button variant="quiet" onClick={props.onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { TextArea } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { refreshNotifications } from '../notifications/refresh'
import { decideApplication, refreshApplications } from './api'

/** Решение по отклику: принять или отказать, с необязательной запиской кандидату. Решение окончательное. */
export function DecisionModal({ appId, decision, onClose }: { appId: string; decision: 'accepted' | 'rejected'; onClose: () => void }) {
  const r = t.applications.review
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const accepted = decision === 'accepted'
  const decide = useMutation({
    mutationFn: (note: string) => decideApplication(appId, decision, note),
    onSuccess: async () => {
      await Promise.all([refreshApplications(client), refreshNotifications(client)])
      toast.show({ kind: 'success', title: accepted ? r.acceptDone : r.rejectDone })
      onClose()
    },
  })
  const { values, set, errors } = useForm({ note: '' }, decide.error, formRef)
  const formError = decide.error && Object.keys(fieldErrorsOf(decide.error)).length === 0 ? describeError(decide.error) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    decide.mutate(values.note)
  }

  return (
    <Modal open onClose={onClose} title={accepted ? r.acceptTitle : r.rejectTitle}>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        <p>{accepted ? r.acceptText : r.rejectText}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextArea label={r.noteLabel} name="note" optional rows={4} hint={r.noteHint} value={values.note} onChange={(ev) => set('note', ev.target.value)} error={errors.note} />
        <div className="form-actions">
          <Button type="submit" variant={accepted ? 'primary' : 'danger'} loading={decide.isPending}>
            {accepted ? r.acceptSubmit : r.rejectSubmit}
          </Button>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

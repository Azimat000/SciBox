import { useMutation, useQuery } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { FilePicker } from '../../ui/FilePicker'
import { TextArea } from '../../ui/TextField'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { MAX_FILE_BYTES, declineRecommendation, lookupRecommendation, submitRecommendation, type RecommendInfo } from './api'
import './applications.css'

/**
 * Страница рекомендателя по ссылке из письма, без входа. Ссылка срабатывает один раз: после письма или отказа
 * страница только благодарит.
 */
export function RecommendPage() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''
  const r = t.recommend
  const info = useQuery({
    queryKey: ['recommendation', token],
    queryFn: () => lookupRecommendation(token),
    enabled: token !== '',
    retry: false,
  })

  if (token === '') {
    return (
      <div className="page page-narrow">
        <EmptyState headingLevel={2} tone="error" title={r.noToken} text={r.noTokenText} />
      </div>
    )
  }
  if (info.isPending) return <PageSkeleton />
  if (info.isError) {
    // Ссылка недействительна, истекла, отклик отозван: сервер называет причину, её и показываем.
    if (info.error instanceof ApiError && info.error.status >= 400 && info.error.status < 500) {
      return (
        <div className="page page-narrow">
          <EmptyState headingLevel={2} tone="error" title={r.title} text={info.error.message} />
        </div>
      )
    }
    return <LoadFailed title={r.loadError} error={info.error} onRetry={() => void info.refetch()} />
  }
  return <Request token={token} info={info.data} />
}

type Outcome = 'received' | 'declined' | null

function Request({ token, info }: { token: string; info: RecommendInfo }) {
  const r = t.recommend
  const formRef = useRef<HTMLFormElement>(null)
  const [file, setFile] = useState<File[]>([])
  const [outcome, setOutcome] = useState<Outcome>(info.status === 'pending' ? null : info.status)
  const [confirm, setConfirm] = useState(false)

  const send = useMutation({
    mutationFn: (text: string) => submitRecommendation(token, text, file[0] ?? null),
    onSuccess: () => setOutcome('received'),
  })
  const decline = useMutation({
    mutationFn: () => declineRecommendation(token),
    onSuccess: () => {
      setConfirm(false)
      setOutcome('declined')
    },
    onError: () => setConfirm(false),
  })
  const { values, set, errors, validate } = useForm({ text: '' }, send.error, formRef)
  const serverFields = fieldErrorsOf(send.error)
  const failure = send.error ?? decline.error
  const formError = failure && Object.keys(serverFields).length === 0 ? describeError(failure) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    if (!validate(values.text.trim() === '' && file.length === 0 ? { text: t.recommend.textHint } : {})) return
    send.mutate(values.text)
  }

  if (outcome) {
    const received = outcome === 'received'
    const already = info.status !== 'pending'
    return (
      <div className="page page-narrow">
        <EmptyState
          headingLevel={2}
          title={received ? (already ? r.alreadyReceived : r.thanksTitle) : already ? r.alreadyDeclined : r.declinedTitle}
          text={received ? (already ? r.alreadyReceivedText : r.thanksText) : already ? r.alreadyDeclinedText : r.declinedText}
        />
      </div>
    )
  }

  return (
    <div className="page page-narrow recommend-page">
      <h1>{r.ask(info.applicant_name)}</h1>
      <p className="lead">{r.forVacancy(info.vacancy_title, info.org_name)}</p>
      <p className="recommend-hello">{r.hello(info.referee_name, info.relation)}</p>
      <p className="recommend-privacy">{r.privacy}</p>
      <form className="apply-form" onSubmit={submit} ref={formRef} noValidate>
        {formError && (
          <Alert kind="error" title={r.sendFailed}>
            {formError}
          </Alert>
        )}
        <TextArea
          label={r.text}
          name="text"
          rows={12}
          hint={r.textHint}
          value={values.text}
          onChange={(ev) => set('text', ev.target.value)}
          error={errors.text ?? serverFields.text}
        />
        <FilePicker
          label={r.file}
          hint={r.fileHint}
          files={file}
          onChange={setFile}
          addLabel={r.pick}
          removeLabel={() => r.removeFile}
          max={1}
          maxBytes={MAX_FILE_BYTES}
          multiple={false}
          messages={{ tooBig: () => r.fileTooBig, notPdf: () => r.fileNotPdf, tooMany: r.fileTooBig }}
          error={serverFields.file}
        />
        <div className="form-actions">
          <Button type="submit" loading={send.isPending}>
            {r.submit}
          </Button>
          <Button variant="quiet" onClick={() => setConfirm(true)}>
            {r.decline}
          </Button>
        </div>
      </form>
      <ConfirmModal
        open={confirm}
        title={r.declineTitle}
        text={r.declineText}
        confirmLabel={r.decline}
        pending={decline.isPending}
        onConfirm={() => decline.mutate()}
        onClose={() => setConfirm(false)}
      />
    </div>
  )
}

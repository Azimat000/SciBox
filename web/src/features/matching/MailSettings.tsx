import { useMutation, useQueryClient } from '@tanstack/react-query'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Checkbox } from '../../ui/Checkbox'
import { Skeleton } from '../../ui/Skeleton'
import { useToast } from '../../ui/useToast'
import { useMe } from '../auth/api'
import { describeError } from '../auth/errors'
import { keys, saveMailSettings, useMailSettings, type MailSettings as Settings } from './api'
import './matching.css'

/** Раздел «Письма» в настройках аккаунта: какие письма присылать. Меняется сразу, без кнопки «Сохранить». */
export function MailSettings() {
  const m = t.matching.mail
  const { user } = useMe()
  const client = useQueryClient()
  const toast = useToast()
  const query = useMailSettings()

  const save = useMutation({
    mutationFn: saveMailSettings,
    onSuccess: (saved) => {
      client.setQueryData(keys.mail(user?.id), saved)
      toast.show({ kind: 'success', title: m.saved })
    },
    onError: (err) => toast.show({ kind: 'error', title: m.saveFailed, text: describeError(err) }),
  })

  // Пока сервер не ответил, показываем то, что человек выбрал: флажок не «прыгает» назад.
  const shown: Settings | undefined = save.isPending ? save.variables : query.data

  return (
    <section className="account-section" aria-labelledby="account-mail">
      <h2 id="account-mail" className="auth-section-title">
        {m.title}
      </h2>
      <p className="auth-note">{m.lead}</p>
      {query.isPending ? (
        <div role="status" aria-busy="true" aria-label={t.common.loading}>
          <Skeleton width="70%" height="1.25rem" />
        </div>
      ) : query.isError || !shown ? (
        <Alert
          kind="error"
          title={m.loadError}
          action={
            <Button variant="secondary" size="sm" onClick={() => void query.refetch()}>
              {t.common.retry}
            </Button>
          }
        >
          {describeError(query.error)}
        </Alert>
      ) : (
        <div className="mail-options">
          <Checkbox
            checked={shown.email_new_vacancies}
            disabled={save.isPending}
            onChange={(ev) => save.mutate({ ...shown, email_new_vacancies: ev.target.checked })}
            label={
              <>
                {m.newVacancies}
                <span className="mail-hint">{m.newVacanciesHint}</span>
              </>
            }
          />
          <Checkbox
            checked={shown.email_deadlines}
            disabled={save.isPending}
            onChange={(ev) => save.mutate({ ...shown, email_deadlines: ev.target.checked })}
            label={
              <>
                {m.deadlines}
                <span className="mail-hint">{m.deadlinesHint}</span>
              </>
            }
          />
          <p className="auth-note">{m.fixed}</p>
        </div>
      )}
    </section>
  )
}

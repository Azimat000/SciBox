import { useMutation } from '@tanstack/react-query'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { saveFile } from '../../lib/saveFile'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { downloadMyData } from './api'
import { describeError } from './errors'
import { waitLabel } from './labels'
import { useReauth } from './reauth-context'

/** «Ваши данные»: архив со всем, что сервис хранит о человеке (срез 22, 152-ФЗ). Нужен свежий пароль, три архива в сутки. */
export function DataSection() {
  const toast = useToast()
  const guard = useReauth()
  const download = useMutation({
    mutationFn: () => guard(downloadMyData),
    onSuccess: ({ blob, filename }) => {
      saveFile(blob, filename ?? 'scibox-data.zip')
      toast.show({ kind: 'success', title: t.auth.dataExport.downloaded, text: t.auth.dataExport.downloadedText })
    },
  })

  const error = download.error
  const limited = error instanceof ApiError && error.code === 'rate_limited'
  return (
    <section className="account-section" aria-labelledby="account-data">
      <h2 id="account-data" className="auth-section-title">
        {t.auth.dataExport.title}
      </h2>
      <p className="auth-note">{t.auth.dataExport.text}</p>
      <p className="auth-note">{t.auth.dataExport.note}</p>
      {error && (
        <Alert kind={limited ? 'info' : 'error'} title={limited ? undefined : t.auth.dataExport.failed}>
          {limited && error instanceof ApiError && error.retryAfter ? t.auth.dataExport.limitReached(waitLabel(error.retryAfter)) : describeError(error)}
        </Alert>
      )}
      <Button variant="secondary" loading={download.isPending} onClick={() => download.mutate()}>
        {t.auth.dataExport.download}
      </Button>
    </section>
  )
}

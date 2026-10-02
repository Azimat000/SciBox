import { t } from '../i18n'
import { Button } from '../ui/Button'

// Показывается, если страница упала при отрисовке (errorElement роутера).
export function CrashPage() {
  return (
    <main className="page" id="main">
      <h1>{t.crash.title}</h1>
      <p className="lead">{t.crash.text}</p>
      <div className="page-actions">
        <Button onClick={() => window.location.reload()}>{t.common.reload}</Button>
      </div>
    </main>
  )
}

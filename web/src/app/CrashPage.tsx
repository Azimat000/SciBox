import { t } from '../i18n'

// Показывается, если страница упала при отрисовке (errorElement роутера).
export function CrashPage() {
  return (
    <main className="page" id="main">
      <h1>{t.crash.title}</h1>
      <p className="lead">{t.crash.text}</p>
      <button type="button" className="button" onClick={() => window.location.reload()}>
        {t.common.reload}
      </button>
    </main>
  )
}

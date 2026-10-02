import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import './auth.css'

/** Черновик политики конфиденциальности (152-ФЗ). Версия совпадает с той, которую сервер записывает при согласии. */
export function PrivacyPage() {
  const p = t.privacy
  return (
    <div className="page page-narrow">
      <h1>{p.title}</h1>
      <p className="lead">{p.versionLine(p.version)}</p>
      <div className="policy">
        <Alert kind="info">{p.draft}</Alert>
        {p.sections.map((section) => (
          <section key={section.title}>
            <h2>{section.title}</h2>
            {section.text.map((paragraph) => (
              <p key={paragraph}>{paragraph}</p>
            ))}
          </section>
        ))}
      </div>
    </div>
  )
}

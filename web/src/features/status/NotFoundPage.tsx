import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'

export function NotFoundPage() {
  return (
    <div className="page">
      <h1>{t.notFound.title}</h1>
      <p className="lead">{t.notFound.text}</p>
      <div className="page-actions">
        <ButtonLink to="/">{t.common.toHome}</ButtonLink>
      </div>
    </div>
  )
}

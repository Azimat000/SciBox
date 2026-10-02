import { Link } from 'react-router'
import { t } from '../../i18n'

export function NotFoundPage() {
  return (
    <div className="page">
      <h1>{t.notFound.title}</h1>
      <p className="lead">{t.notFound.text}</p>
      <Link className="button" to="/">
        {t.common.toHome}
      </Link>
    </div>
  )
}

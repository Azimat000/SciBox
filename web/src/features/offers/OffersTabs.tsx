import { Link } from 'react-router'
import { t } from '../../i18n'
import '../orgs/orgs.css'

/** Две вкладки раздела: отклики и приглашения. У ищущего это «Мои отклики» и «Приглашения», у нанимающего «Отклики» и «Отправленные». */
export function OffersTabs({ side, current }: { side: 'seeker' | 'employer'; current: 'applications' | 'offers' }) {
  const links =
    side === 'seeker'
      ? { applications: '/applications', offers: '/offers' }
      : { applications: '/candidates', offers: '/sent-offers' }
  return (
    <nav className="manage-tabs" aria-label={t.offers.tabs.label}>
      <Link className="manage-tab" to={links.applications} aria-current={current === 'applications' ? 'page' : undefined}>
        {t.offers.tabs.applications}
      </Link>
      <Link className="manage-tab" to={links.offers} aria-current={current === 'offers' ? 'page' : undefined}>
        {t.offers.tabs.offers}
      </Link>
    </nav>
  )
}

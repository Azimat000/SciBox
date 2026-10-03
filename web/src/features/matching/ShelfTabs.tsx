import { Link } from 'react-router'
import { t } from '../../i18n'
import '../orgs/orgs.css'

export type ShelfTab = 'favorites' | 'matches' | 'searches' | 'deadlines'

const links: Record<ShelfTab, string> = {
  favorites: '/favorites',
  matches: '/matches',
  searches: '/saved-searches',
  deadlines: '/deadlines',
}

/** Четыре вкладки раздела «Избранное»: избранное, подходящие вакансии, сохранённые поиски и сроки подачи. */
export function ShelfTabs({ current }: { current: ShelfTab }) {
  return (
    <nav className="manage-tabs" aria-label={t.matching.tabs.label}>
      {(Object.keys(links) as ShelfTab[]).map((tab) => (
        <Link key={tab} className="manage-tab" to={links[tab]} aria-current={current === tab ? 'page' : undefined}>
          {t.matching.tabs[tab]}
        </Link>
      ))}
    </nav>
  )
}

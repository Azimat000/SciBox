import { Link } from 'react-router'
import { t } from '../../i18n'
import '../orgs/orgs.css'
import { useActiveTabInView } from '../../ui/useActiveTabInView'

export type ShelfTab = 'favorites' | 'matches' | 'searches'

const links: Record<ShelfTab, string> = {
  favorites: '/favorites',
  matches: '/matches',
  searches: '/saved-searches',
}

/** Три вкладки раздела «Избранное»: избранное, подходящие вакансии и сохранённые поиски. Сроки подачи — порядок в избранном. */
export function ShelfTabs({ current }: { current: ShelfTab }) {
  const tabsRef = useActiveTabInView<HTMLElement>()
  return (
    <nav ref={tabsRef} className="manage-tabs" aria-label={t.matching.tabs.label}>
      {(Object.keys(links) as ShelfTab[]).map((tab) => (
        <Link key={tab} className="manage-tab" to={links[tab]} aria-current={current === tab ? 'page' : undefined}>
          {t.matching.tabs[tab]}
        </Link>
      ))}
    </nav>
  )
}

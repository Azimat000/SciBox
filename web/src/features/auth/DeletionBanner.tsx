import { Link, useLocation } from 'react-router'
import { t } from '../../i18n'
import { useMe } from './api'
import { formatDay } from './labels'
import '../../ui/Deadline.css'
import './auth.css'

/** Полоса под шапкой, пока назначено удаление аккаунта: человек вошёл и должен видеть, что срок идёт. */
export function DeletionBanner() {
  const { user } = useMe()
  const { pathname } = useLocation()
  // На странице «Безопасность» есть свой блок с кнопкой отмены: вторая надпись там лишняя.
  if (!user?.deletion_due_at || pathname === '/account/security') return null
  return (
    <div className="deletion-banner" role="status">
      <p className="deletion-banner-inner">
        {t.auth.deletion.bannerBefore} <strong className="mark">{formatDay(user.deletion_due_at)}</strong>. <Link to="/account/security">{t.auth.deletion.bannerLink}</Link>
      </p>
    </div>
  )
}

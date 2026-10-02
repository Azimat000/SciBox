import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'

/** Раздел, который ещё не построен. Каждый следующий срез заменяет такие адреса настоящими страницами. */
export function ComingSoonPage() {
  return (
    <div className="page">
      <EmptyState
        headingLevel={2}
        title={t.comingSoon.title}
        text={t.comingSoon.text}
        action={<ButtonLink to="/">{t.common.toHome}</ButtonLink>}
      />
    </div>
  )
}

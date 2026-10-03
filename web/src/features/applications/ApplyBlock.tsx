import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { useMe } from '../auth/api'
import { useApplyState } from './api'
import './applications.css'

/**
 * Кнопка «Откликнуться» на странице вакансии. Без входа ведёт на вход и потом возвращает к форме; тем, кто вакансию
 * ведёт, кнопки нет; после отклика вместо неё ссылка на отклик.
 */
export function ApplyBlock({ vacancyId }: { vacancyId: string }) {
  const { user } = useMe()
  const state = useApplyState(vacancyId)
  const a = t.applications.apply

  if (user === undefined) return null
  if (user === null) {
    const next = encodeURIComponent(`/vacancies/${vacancyId}/apply`)
    return (
      <div className="apply-block">
        <ButtonLink to={`/login?next=${next}`}>{a.signIn}</ButtonLink>
      </div>
    )
  }
  if (!state.data) return null // загружается или не удалось: страница вакансии от этого не страдает
  const s = state.data
  if (s.can_apply) {
    return (
      <div className="apply-block">
        <ButtonLink to={`/vacancies/${vacancyId}/apply`}>{a.button}</ButtonLink>
      </div>
    )
  }
  if (s.reason === 'applied' && s.application) {
    return (
      <div className="apply-block">
        <p className="apply-block-note">{a.alreadyApplied}</p>
        <ButtonLink to={`/applications/${s.application.id}`} variant="secondary">
          {a.seeApplication}
        </ButtonLink>
      </div>
    )
  }
  if (s.reason === 'deadline_passed') {
    return (
      <div className="apply-block">
        <p className="apply-block-note">{a.deadlinePassed}</p>
      </div>
    )
  }
  return null
}

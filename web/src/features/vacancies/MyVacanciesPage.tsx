import { Link, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { useCandidateVacancies, type VacancyCount } from '../applications/api'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Deadline } from '../../ui/Deadline'
import { Tag } from '../../ui/Tag'
import { RequireUser } from '../orgs/RequireUser'
import { formatDate, longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { PAGE_SIZE, statuses, useMyVacancies, useTargets, type Card, type Status } from './api'
import { statusLabel } from './labels'
import './vacancies.css'

export function MyVacanciesPage() {
  return <RequireUser>{() => <MyVacancies />}</RequireUser>
}

/** Адрес вкладки: пустой статус — «Все». */
const tabSearch = (status: string) => (status ? `?status=${status}` : '')

function MyVacancies() {
  const [params, setParams] = useSearchParams()
  const rawStatus = params.get('status') ?? ''
  const status = (statuses as readonly string[]).includes(rawStatus) ? rawStatus : ''
  const page = Math.max(1, Number(params.get('page')) || 1)
  const mine = useMyVacancies(status, page)
  const targets = useTargets()
  // Числа откликов вспомогательные: если их не удалось получить, страница работает без них.
  const apps = useCandidateVacancies()
  const counted = new Map((apps.data ?? []).map((v) => [v.id, v]))

  if (mine.isPending || targets.isPending) return <PageSkeleton />
  if (mine.isError) return <LoadFailed title={t.vacancies.mine.loadError} error={mine.error} onRetry={() => void mine.refetch()} />
  if (targets.isError) return <LoadFailed title={t.vacancies.mine.loadError} error={targets.error} onRetry={() => void targets.refetch()} />

  const { items, total, counts } = mine.data
  const canCreate = targets.data.length > 0
  const everything = statuses.reduce((n, s) => n + counts[s], 0)
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const go = (nextPage: number) => {
    const next = new URLSearchParams(params)
    if (nextPage > 1) next.set('page', String(nextPage))
    else next.delete('page')
    setParams(next)
  }

  if (!canCreate && everything === 0) {
    return (
      <div className="page org-page">
        <h1>{t.vacancies.mine.title}</h1>
        <EmptyState
          headingLevel={2}
          title={t.vacancies.mine.noTargetsTitle}
          text={t.vacancies.mine.noTargetsText}
          action={<ButtonLink to="/my-organization">{t.vacancies.mine.toOrganizations}</ButtonLink>}
        />
      </div>
    )
  }

  return (
    <div className="page org-page">
      <h1>{t.vacancies.mine.title}</h1>
      <p className="lead">{t.vacancies.mine.lead}</p>
      {canCreate && (
        <div className="org-actions">
          <ButtonLink to="/my-vacancies/new">{t.vacancies.mine.create}</ButtonLink>
        </div>
      )}

      <nav className="manage-tabs" aria-label={t.vacancies.mine.tabs}>
        {['', ...statuses].map((s) => {
          const label = s ? t.vacancies.statusTabs[s as Status] : t.vacancies.statusTabs.all
          const count = s ? counts[s as Status] : everything
          return (
            <Link
              key={s || 'all'}
              className="manage-tab"
              to={`/my-vacancies${tabSearch(s)}`}
              aria-current={s === status ? 'page' : undefined}
              aria-label={`${label}: ${count}`}
            >
              {label}
              <span className="tab-count num" aria-hidden="true">
                {count}
              </span>
            </Link>
          )
        })}
      </nav>

      <div className="manage-body">
        {items.length === 0 ? (
          status === '' ? (
            <EmptyState
              headingLevel={2}
              title={t.vacancies.mine.emptyTitle}
              text={t.vacancies.mine.emptyText}
              action={canCreate ? <ButtonLink to="/my-vacancies/new">{t.vacancies.mine.create}</ButtonLink> : undefined}
            />
          ) : (
            <EmptyState headingLevel={2} title={t.vacancies.mine.emptyTabTitle} text={t.vacancies.mine.emptyTabText} />
          )
        ) : (
          <>
            {items.map((c) => (
              <Row key={c.id} card={c} apps={counted.get(c.id)} />
            ))}
            {pages > 1 && (
              <nav className="pager" aria-label={t.vacancies.mine.pager}>
                <Button variant="secondary" disabled={page <= 1} onClick={() => go(page - 1)}>
                  {t.vacancies.mine.prev}
                </Button>
                <span className="pager-text num">{t.vacancies.mine.page(page, pages)}</span>
                <Button variant="secondary" disabled={page >= pages} onClick={() => go(page + 1)}>
                  {t.vacancies.mine.next}
                </Button>
              </nav>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Row({ card: c, apps }: { card: Card; apps: VacancyCount | undefined }) {
  return (
    <div className="mine-row">
      <div className="mine-row-main">
        <p className="mine-row-title" data-long={longName(c.title)}>
          <Link to={`/vacancies/${c.id}`}>{c.title}</Link>
        </p>
        <p className="mine-row-line">
          <Tag tone={c.status === 'published' ? 'accent' : 'neutral'}>{statusLabel(c.status)}</Tag>
          <span>
            {c.organization.name} · {c.unit?.name ?? t.vacancies.mine.noUnit}
          </span>
          <span>{c.position.name}</span>
          <span className="num">{t.vacancies.mine.updated(formatDate(c.updated_at))}</span>
        </p>
        {c.status === 'published' && c.deadline && <Deadline date={c.deadline} />}
        {apps && apps.total > 0 && (
          <p className="mine-row-apps num">
            <Link to={`/candidates?vacancy=${c.id}`}>{t.vacancies.mine.applications(apps.total, plural(apps.total, t.vacancies.mine.applicationsForms))}</Link>
            {apps.new > 0 && `, ${t.vacancies.mine.applicationsNew(apps.new)}`}
          </p>
        )}
      </div>
      {/* Вакансию открывает её название; синяя кнопка на странице одна: «Создать вакансию». */}
      <div className="mine-row-actions">
        <ButtonLink to={`/my-vacancies/${c.id}/edit`} variant="secondary" size="sm" aria-label={`${t.vacancies.mine.edit}: ${c.title}`}>
          {t.vacancies.mine.edit}
        </ButtonLink>
      </div>
    </div>
  )
}

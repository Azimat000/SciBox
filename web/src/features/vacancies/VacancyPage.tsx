import { usePageTitle } from '../../app/pageTitle'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { Deadline } from '../../ui/Deadline'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { ApplyBlock } from '../applications/ApplyBlock'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { FavoriteButton } from '../matching/FavoriteButton'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { isNotFound } from '../orgs/api'
import { kindLabel, longName } from '../orgs/labels'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { NotFoundPage } from '../status/NotFoundPage'
import { deleteVacancy, refreshVacancies, setVacancyStatus, useVacancy, type Detail, type Status } from './api'
import {
  contractText,
  degreeLabel,
  focusLabel,
  formatLabel,
  fundingLabel,
  housingLabel,
  levelParts,
  placeText,
  salaryLabel,
  salaryText,
  titleLabel,
  transitionLabel,
  doneText,
  statusLabel,
} from './labels'
import { formFieldOrder } from './formValues'
import './vacancies.css'

/** Страница вакансии: оформлена как статья журнала. Тем, кто ведёт вакансию, сверху доступно управление. */
export function VacancyPage() {
  const { id = '' } = useParams()
  const query = useVacancy(id)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <NotFoundPage />
    ) : (
      <LoadFailed title={t.vacancies.page.loadError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  return <Article vacancy={query.data} />
}

function Article({ vacancy: v }: { vacancy: Detail }) {
  usePageTitle(v.title)
  const p = t.vacancies.page
  const level = levelParts(v.career_level)
  const pay = salaryText(v.salary_from, v.salary_to)
  const place = placeText(v)
  const rows: [string, string | null][] = [
    [p.position, v.position.name],
    [p.level, level ? `${level[0]} · ${level[1]}` : null],
    [p.format, v.work_format ? formatLabel(v.work_format) : null],
    [p.place, place || null],
    [p.rate, v.rate_percent !== null ? t.vacancies.rate(v.rate_percent) : null],
    [salaryLabel(v.position.type), pay ? `${pay}, ${t.vacancies.salaryNote}` : null],
    [p.contract, v.contract_type ? contractText(v.contract_type, v.contract_months) : null],
    [p.funding, v.funding_source ? [fundingLabel(v.funding_source), v.funding_note].filter(Boolean).join(', ') : null],
    [p.housing, v.housing !== 'none' ? housingLabel(v.housing) : null],
    [p.degree, v.degree_required !== 'none' ? degreeLabel(v.degree_required) : null],
    [p.academicTitle, v.title_required !== 'none' ? titleLabel(v.title_required) : null],
  ]

  const forReader = (v.status === 'published' || v.status === 'closed') && !v.viewer.can_manage

  return (
    <article className="page vacancy-page">
      <header className="vacancy-head">
        <p className="vacancy-tags">
          {v.is_competition && <Tag tone="accent">{t.vacancy.competition}</Tag>}
          {v.status !== 'published' && <Tag>{statusLabel(v.status)}</Tag>}
        </p>
        <h1 data-long={longName(v.title)}>{v.title}</h1>
        <p className="vacancy-org">
          <Link to={`/organizations/${v.organization.slug}`}>{v.organization.name}</Link>
          <span>{kindLabel(v.organization.kind)}</span>
        </p>
        {(v.unit || place) && (
          <p className="vacancy-where">
            {v.unit && <Link to={`/organizations/${v.organization.slug}/units/${v.unit.id}`}>{v.unit.name}</Link>}
            {place && <span>{place}</span>}
          </p>
        )}
      </header>

      <div className="split">
        {(forReader || v.viewer.can_manage) && (
          <aside className="split-aside vacancy-aside" aria-label={forReader ? p.endActions : t.vacancies.page.manage}>
            {forReader && (
              <>
                {v.status === 'published' &&
                  (v.deadline ? (
                    <Deadline date={v.deadline} />
                  ) : (
                    <p className="deadline deadline-open">
                      <span className="deadline-date">{t.deadline.open}</span>
                      <span className="deadline-rest">{t.deadline.openHint}</span>
                    </p>
                  ))}
                <div className="vacancy-actions">
                  {v.status === 'published' && <ApplyBlock vacancyId={v.id} />}
                  <FavoriteButton vacancyId={v.id} title={v.title} />
                </div>
              </>
            )}
            {v.viewer.can_manage && <ManageBar vacancy={v} />}
          </aside>
        )}

        <div className="split-main vacancy-main">
          {v.status === 'draft' && <Alert kind="info">{p.draftNotice}</Alert>}
          {v.status === 'closed' && <Alert kind="info">{p.closedNotice}</Alert>}
          {v.status === 'archived' && <Alert kind="info">{p.archivedNotice}</Alert>}

          {v.summary && <p className="vacancy-lead">{v.summary}</p>}

          <section className="vacancy-section" aria-labelledby="vacancy-facts">
            <h2 id="vacancy-facts">{p.facts}</h2>
            <dl className="vacancy-facts">
              {rows
                .filter((r): r is [string, string] => r[1] !== null)
                .map(([label, value]) => (
                  <div key={label} className="vacancy-fact">
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              {v.specialties.length > 0 && (
                <div className="vacancy-fact vacancy-fact-wide">
                  <dt>{p.specialties}</dt>
                  <dd>
                    <ul className="vacancy-specialties">
                      {v.specialties.map((s) => (
                        <li key={s.code}>
                          <span className="num vacancy-specialty-code">{s.code}</span>
                          <span>{s.name}</span>
                        </li>
                      ))}
                    </ul>
                  </dd>
                </div>
              )}
              {v.deadline && v.status !== 'published' && (
                <div className="vacancy-fact">
                  <dt>{p.deadline}</dt>
                  <dd className="num">{formatDeadline(v.deadline)}</dd>
                </div>
              )}
            </dl>
          </section>

          {v.focus && (
            <section className="vacancy-section" aria-labelledby="vacancy-focus">
              <h2 id="vacancy-focus">{focusLabel(v.position.type)}</h2>
              <p className="vacancy-text">{v.focus}</p>
            </section>
          )}
          {v.description && (
            <section className="vacancy-section" aria-labelledby="vacancy-description">
              <h2 id="vacancy-description">{p.description}</h2>
              <p className="vacancy-text">{v.description}</p>
            </section>
          )}
          <section className="vacancy-section" aria-labelledby="vacancy-requirements">
            <h2 id="vacancy-requirements">{p.requirements}</h2>
            {v.requirements ? (
              <p className="vacancy-text">{v.requirements}</p>
            ) : (
              <p className="vacancy-text vacancy-muted">
                {v.degree_required !== 'none' || v.title_required !== 'none' ? p.noExtraRequirements : p.noRequirements}
              </p>
            )}
          </section>

          {/* Дочитавшему до конца (на телефоне это три экрана) не нужно листать обратно к кнопке. */}
          {v.status === 'published' && !v.viewer.can_manage && (
            <section className="vacancy-end" aria-label={p.endActions}>
              <ApplyBlock vacancyId={v.id} />
            </section>
          )}
        </div>
      </div>
    </article>
  )
}

const dateFormat = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

/** «14 ноября 2026» из даты вида `2026-11-14` (без пересчёта часовых поясов). */
function formatDeadline(date: string): string {
  const [y, m, d] = date.split('-').map(Number)
  return dateFormat.format(new Date(y, m - 1, d))
}

/** Сообщения сервера о незаполненных полях в том порядке, в каком эти поля стоят в форме. */
function orderedMessages(fields: Record<string, string>): string[] {
  const known = formFieldOrder.filter((name) => name in fields)
  const rest = Object.keys(fields).filter((name) => !(formFieldOrder as readonly string[]).includes(name))
  return [...known, ...rest].map((name) => fields[name])
}

/** Панель для тех, кто ведёт вакансию: правка, смена статуса, удаление черновика. */
function ManageBar({ vacancy: v }: { vacancy: Detail }) {
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const [confirmRemove, setConfirmRemove] = useState(false)

  const change = useMutation({
    mutationFn: (to: Status) => setVacancyStatus(v.id, to),
    onSuccess: async (saved) => {
      await refreshVacancies(client, v.id)
      toast.show({ kind: 'success', title: doneText(saved.status) })
    },
    onError: (err) => {
      if (Object.keys(fieldErrorsOf(err)).length === 0)
        toast.show({
          kind: 'error',
          title: t.vacancies.page.statusFailed,
          text: describeError(err),
        })
    },
  })
  const remove = useMutation({
    mutationFn: () => deleteVacancy(v.id),
    onSuccess: async () => {
      await refreshVacancies(client, v.id)
      toast.show({ kind: 'success', title: t.vacancies.page.removed })
      void navigate('/my-vacancies')
    },
    onError: (err) => {
      setConfirmRemove(false)
      toast.show({
        kind: 'error',
        title: t.vacancies.page.removeFailed,
        text: describeError(err),
      })
    },
  })

  const missing = orderedMessages(fieldErrorsOf(change.error))
  return (
    <section className="vacancy-manage" aria-label={t.vacancies.page.manage}>
      {missing.length > 0 && (
        <Alert
          kind="error"
          title={t.vacancies.page.cannotPublishTitle}
          action={
            <ButtonLink to={`/my-vacancies/${v.id}/edit`} variant="secondary" size="sm">
              {t.vacancies.page.completeIt}
            </ButtonLink>
          }
        >
          {`${t.vacancies.page.cannotPublishText} ${missing.join('; ')}`}
        </Alert>
      )}
      <div className="vacancy-manage-actions">
        <ButtonLink to={`/my-vacancies/${v.id}/edit`} variant="secondary">
          {t.vacancies.page.edit}
        </ButtonLink>
        {v.viewer.transitions.map((to, i) => (
          <Button
            key={to}
            variant={i === 0 ? 'primary' : 'secondary'}
            loading={change.isPending && change.variables === to}
            disabled={change.isPending}
            onClick={() => change.mutate(to)}
          >
            {transitionLabel(v.status, to)}
          </Button>
        ))}
        {v.status === 'draft' && (
          <Button variant="quiet" onClick={() => setConfirmRemove(true)}>
            {t.vacancies.page.remove}
          </Button>
        )}
      </div>
      <ConfirmModal
        open={confirmRemove}
        title={t.vacancies.page.removeTitle}
        text={t.vacancies.page.removeText}
        confirmLabel={t.vacancies.page.remove}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
        onClose={() => setConfirmRemove(false)}
      />
    </section>
  )
}

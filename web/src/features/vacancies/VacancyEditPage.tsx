import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { useToast } from '../../ui/useToast'
import { isNotFound } from '../orgs/api'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import {
  createVacancy,
  refreshVacancies,
  setVacancyStatus,
  updateVacancy,
  useReference,
  useTargets,
  useVacancy,
  type Detail,
  type Target,
  type VacancyFields,
} from './api'
import { emptyValues, type FormValues, type Intent } from './formValues'
import { VacancyForm } from './VacancyForm'
import './vacancies.css'

export function VacancyEditPage() {
  return <RequireUser>{() => <Editor />}</RequireUser>
}

/** Значения формы по сохранённой вакансии. */
function valuesOf(v: Detail): FormValues {
  const text = (n: number | null) => (n === null ? '' : String(n))
  return {
    organization: v.organization.slug,
    position_type: v.position.type,
    unit_id: v.unit?.id ?? '',
    position_code: v.position.code,
    title: v.title,
    summary: v.summary,
    description: v.description,
    requirements: v.requirements,
    focus: v.focus,
    specialties: v.specialties.map((s) => s.code).join(','),
    career_level: text(v.career_level),
    degree_required: v.degree_required,
    title_required: v.title_required,
    work_format: v.work_format,
    region_code: v.region?.code ?? '',
    city: v.city,
    rate_percent: text(v.rate_percent),
    salary_from: text(v.salary_from),
    salary_to: text(v.salary_to),
    contract_type: v.contract_type,
    contract_months: text(v.contract_months),
    funding_source: v.funding_source,
    funding_note: v.funding_note,
    housing: v.housing,
    is_competition: v.is_competition,
    deadline: v.deadline,
  }
}

/** Начальные значения новой вакансии: организация и подразделение из адреса (?org=…&unit=…) или единственные доступные. */
function newValues(targets: readonly Target[], orgParam: string | null, unitParam: string | null): FormValues {
  const target = targets.find((x) => x.organization.slug === orgParam) ?? (targets.length === 1 ? targets[0] : undefined)
  if (!target) return emptyValues
  const unitKnown = target.units.some((u) => u.id === unitParam)
  const unit = unitKnown ? (unitParam as string) : target.whole_org ? '' : (target.units[0]?.id ?? '')
  return { ...emptyValues, organization: target.organization.slug, unit_id: unit }
}

function Editor() {
  const { id } = useParams()
  const [params] = useSearchParams()
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const reference = useReference()
  const targets = useTargets()
  const vacancy = useVacancy(id ?? '', id !== undefined)
  // Черновик, который уже создан этой формой: если публикация не прошла, повторная отправка правит его, а не создаёт второй.
  const created = useRef<string | null>(null)

  const save = useMutation({
    mutationFn: async ({ organization, fields, intent }: { organization: string; fields: VacancyFields; intent: Intent }) => {
      const existing = id ?? created.current
      let saved = existing ? await updateVacancy(existing, fields) : await createVacancy(organization, fields)
      created.current = saved.id
      if (intent === 'publish') saved = await setVacancyStatus(saved.id, 'published')
      return { saved, intent }
    },
    onSuccess: async ({ saved, intent }) => {
      await refreshVacancies(client, saved.id)
      const f = t.vacancies.form
      toast.show({ kind: 'success', title: intent === 'publish' ? f.published : id ? f.saved : f.savedDraft })
      void navigate(`/vacancies/${saved.id}`)
    },
  })

  if (reference.isPending || targets.isPending || (id !== undefined && vacancy.isPending)) return <PageSkeleton />
  if (reference.isError) return <LoadFailed title={t.vacancies.form.loadError} error={reference.error} onRetry={() => void reference.refetch()} />
  if (targets.isError) return <LoadFailed title={t.vacancies.form.loadError} error={targets.error} onRetry={() => void targets.refetch()} />
  if (id !== undefined && vacancy.isError) {
    return isNotFound(vacancy.error) ? (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={t.vacancies.form.notFoundTitle}
          text={t.vacancies.form.notFoundText}
          action={<ButtonLink to="/my-vacancies">{t.vacancies.form.backToList}</ButtonLink>}
        />
      </div>
    ) : (
      <LoadFailed title={t.vacancies.form.loadError} error={vacancy.error} onRetry={() => void vacancy.refetch()} />
    )
  }

  const current = id !== undefined ? vacancy.data : undefined
  if (current && !current.viewer.can_manage) {
    return (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={t.vacancies.form.noAccessTitle}
          text={t.vacancies.form.noAccessText}
          action={<ButtonLink to={`/vacancies/${current.id}`}>{t.vacancies.form.back}</ButtonLink>}
        />
      </div>
    )
  }
  if (!current && targets.data.length === 0) {
    return (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={t.vacancies.form.noTargetsTitle}
          text={t.vacancies.form.noTargetsText}
          action={<ButtonLink to="/my-organization">{t.vacancies.mine.toOrganizations}</ButtonLink>}
        />
      </div>
    )
  }

  const draft = current ? current.status === 'draft' : true
  return (
    <div className="page vacancy-edit">
      <p className="manage-note">
        {current ? <Link to={`/vacancies/${current.id}`}>{t.vacancies.form.back}</Link> : <Link to="/my-vacancies">{t.vacancies.form.backToList}</Link>}
      </p>
      <h1>{current ? t.vacancies.form.editTitle : t.vacancies.form.newTitle}</h1>
      <VacancyForm
        key={current?.id ?? 'new'}
        initial={current ? valuesOf(current) : newValues(targets.data, params.get('org'), params.get('unit'))}
        reference={reference.data}
        targets={targets.data}
        organizationLocked={current !== undefined}
        draft={draft}
        pending={save.isPending ? save.variables.intent : null}
        error={save.error}
        onSubmit={(organization, fields, intent) => save.mutate({ organization, fields, intent })}
        onCancel={() => void navigate(current ? `/vacancies/${current.id}` : '/my-vacancies')}
      />
    </div>
  )
}

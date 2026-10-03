import { useMemo, useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Checkbox } from '../../ui/Checkbox'
import { Combobox } from '../../ui/Combobox'
import { Select, type Option } from '../../ui/Select'
import { FilterChip } from '../../ui/Tag'
import { TextArea, TextField } from '../../ui/TextField'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { positionTypes, type PositionType, type Reference, type Target, type VacancyFields } from './api'
import {
  contractOptions,
  degreeOptions,
  formatOptions,
  fundingOptions,
  housingOptions,
  levelHint,
  levelOptions,
  positionTypeLabel,
  rateOptions,
  titleOptions,
} from './labels'
import { SpecialtiesPicker } from './SpecialtiesPicker'
import { typeOfPosition, toFields, type FormValues, type Intent } from './formValues'

type Props = {
  initial: FormValues
  reference: Reference
  targets: readonly Target[]
  /** Организацию нельзя менять у уже созданной вакансии. */
  organizationLocked: boolean
  draft: boolean
  pending: Intent | null
  error: unknown
  onSubmit: (organization: string, fields: VacancyFields, intent: Intent) => void
  onCancel: () => void
}

/** Форма вакансии: и для новой, и для правки. Поля зависят от типа позиции. */
export function VacancyForm({ initial, reference, targets, organizationLocked, draft, pending, error, onSubmit, onCancel }: Props) {
  const f = t.vacancies.form
  const formRef = useRef<HTMLFormElement>(null)
  const intent = useRef<Intent>('save')
  const { values, set, errors, validate } = useForm<FormValues>(initial, error, formRef)

  const type = values.position_type as PositionType | ''
  const target = targets.find((x) => x.organization.slug === values.organization)
  const remote = values.work_format === 'remote'
  const management = type === 'management'
  const early = type === 'early_career'

  const positionOptions: Option[] = useMemo(
    () => reference.positions.filter((p) => p.type === type).map((p) => ({ value: p.code, label: p.name })),
    [reference, type],
  )
  const regionOptions: Option[] = useMemo(() => reference.regions.map((r) => ({ value: r.code, label: r.name })), [reference])
  const organizationOptions: Option[] = targets.map((x) => ({ value: x.organization.slug, label: x.organization.name }))
  const unitOptions: Option[] = [
    ...(target?.whole_org ? [{ value: '', label: f.wholeOrg }] : []),
    ...(target?.units ?? []).map((u) => ({ value: u.id, label: u.name })),
  ]

  function pickOrganization(slug: string) {
    set('organization', slug)
    const next = targets.find((x) => x.organization.slug === slug)
    // Если «всей организации» у человека нет, подразделение выбирается сразу: пустое значение не годится.
    set('unit_id', next && !next.whole_org ? (next.units[0]?.id ?? '') : '')
  }

  function pickType(next: PositionType) {
    set('position_type', next)
    if (typeOfPosition(reference, values.position_code) !== next) set('position_code', '')
    if (next !== 'teaching') set('title_required', 'none')
    if (next !== 'research' && next !== 'teaching') set('is_competition', false)
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({
      organization: values.organization === '' ? f.errors.organizationRequired : undefined,
      title: values.title.trim() === '' ? f.errors.titleRequired : undefined,
      position_code: values.position_code === '' ? f.errors.positionRequired : undefined,
    })
    if (!validate(found)) return
    onSubmit(values.organization, toFields(values), intent.current)
  }
  const formError = error && Object.keys(fieldErrorsOf(error)).length === 0 ? describeError(error) : undefined
  const busy = pending !== null

  return (
    <form className="vacancy-form" onSubmit={submit} ref={formRef} noValidate>
      {formError && <Alert kind="error">{formError}</Alert>}
      {draft && <p className="manage-note">{t.vacancies.form.draftNote}</p>}

      <fieldset className="form-block">
        <legend>{f.sections.what}</legend>
        {!organizationLocked && targets.length > 1 && (
          <Select label={f.organization} name="organization" options={organizationOptions} value={values.organization} placeholder="" onChange={(e) => pickOrganization(e.target.value)} error={errors.organization} required />
        )}
        {/* Единственное «вся организация» выбирать не из чего; единственное подразделение показываем: человек должен видеть, куда попадёт вакансия. */}
        {(unitOptions.length > 1 || (unitOptions.length === 1 && !target?.whole_org)) && (
          <Select label={f.unit} name="unit_id" options={unitOptions} value={values.unit_id} onChange={(e) => set('unit_id', e.target.value)} error={errors.unit_id} />
        )}
        <div className="field">
          <span className="field-label" id="position-type-label">
            {f.positionType}
            <span className="field-flag"> · {t.ui.required}</span>
          </span>
          <div className="chip-row" role="group" aria-labelledby="position-type-label">
            {positionTypes.map((p) => (
              <FilterChip key={p} pressed={type === p} onClick={() => pickType(p)}>
                {positionTypeLabel(p)}
              </FilterChip>
            ))}
          </div>
          {type !== '' && <p className="field-hint">{t.vacancies.positionTypeHints[type]}</p>}
        </div>
        {type !== '' && (
          <Select
            label={f.position}
            name="position_code"
            placeholder={f.positionPlaceholder}
            options={positionOptions}
            value={values.position_code}
            onChange={(e) => set('position_code', e.target.value)}
            error={errors.position_code}
            required
          />
        )}
        <TextField label={f.title} name="title" hint={f.titleHint} value={values.title} onChange={(e) => set('title', e.target.value)} error={errors.title} required />
      </fieldset>

      <fieldset className="form-block">
        <legend>{f.sections.about}</legend>
        <TextArea label={f.summary} name="summary" hint={f.summaryHint} rows={3} value={values.summary} onChange={(e) => set('summary', e.target.value)} error={errors.summary} />
        <TextArea label={f.description} name="description" hint={f.descriptionHint} rows={8} value={values.description} onChange={(e) => set('description', e.target.value)} error={errors.description} />
        <TextArea label={f.requirements} name="requirements" optional rows={4} value={values.requirements} onChange={(e) => set('requirements', e.target.value)} error={errors.requirements} />
        {type !== '' && (
          <TextField label={f.focusLabels[type]} name="focus" optional={type === 'research' || type === 'management'} value={values.focus} onChange={(e) => set('focus', e.target.value)} error={errors.focus} />
        )}
      </fieldset>

      <fieldset className="form-block">
        <legend>{f.sections.science}</legend>
        <SpecialtiesPicker
          science={reference.science}
          value={values.specialties === '' ? [] : values.specialties.split(',')}
          onChange={(codes) => set('specialties', codes.join(','))}
          error={errors.specialties}
          optional={management}
        />
        <Select label={f.level} hint={levelHint(values.career_level)} name="career_level" placeholder={f.levelPlaceholder} options={levelOptions} value={values.career_level} onChange={(e) => set('career_level', e.target.value)} error={errors.career_level} optional={management} />
        <Select label={f.degree} name="degree_required" options={degreeOptions} value={values.degree_required} onChange={(e) => set('degree_required', e.target.value)} error={errors.degree_required} />
        {type === 'teaching' && (
          <Select label={f.academicTitle} name="title_required" options={titleOptions} value={values.title_required} onChange={(e) => set('title_required', e.target.value)} error={errors.title_required} />
        )}
      </fieldset>

      <fieldset className="form-block">
        <legend>{f.sections.terms}</legend>
        <Select label={f.format} name="work_format" placeholder={f.formatPlaceholder} options={formatOptions} value={values.work_format} onChange={(e) => set('work_format', e.target.value)} error={errors.work_format} />
        <Combobox label={f.region} placeholder={f.regionPlaceholder} options={regionOptions} value={values.region_code === '' ? null : values.region_code} onChange={(code) => set('region_code', code ?? '')} error={errors.region_code} optional={remote} />
        <TextField label={f.city} name="city" value={values.city} onChange={(e) => set('city', e.target.value)} error={errors.city} optional={remote} />
        <Select label={f.rate} name="rate_percent" placeholder={f.ratePlaceholder} options={rateOptions} value={values.rate_percent} onChange={(e) => set('rate_percent', e.target.value)} error={errors.rate_percent} optional={early} />
        <div className="field-pair">
          <TextField label={early ? f.stipendFrom : f.salaryFrom} name="salary_from" type="number" inputMode="numeric" min={1} step={1000} value={values.salary_from} onChange={(e) => set('salary_from', e.target.value)} error={errors.salary_from} optional />
          <TextField label={early ? f.stipendTo : f.salaryTo} name="salary_to" type="number" inputMode="numeric" min={1} step={1000} value={values.salary_to} onChange={(e) => set('salary_to', e.target.value)} error={errors.salary_to} optional />
        </div>
        <p className="field-hint">{f.salaryHint}</p>
        <Select label={f.contract} name="contract_type" placeholder={f.contractPlaceholder} options={contractOptions} value={values.contract_type} onChange={(e) => set('contract_type', e.target.value)} error={errors.contract_type} />
        {values.contract_type === 'fixed' && (
          <TextField label={f.months} name="contract_months" type="number" inputMode="numeric" min={1} max={120} value={values.contract_months} onChange={(e) => set('contract_months', e.target.value)} error={errors.contract_months} />
        )}
        <Select label={f.funding} name="funding_source" placeholder={f.fundingPlaceholder} options={fundingOptions} value={values.funding_source} onChange={(e) => set('funding_source', e.target.value)} error={errors.funding_source} optional />
        {values.funding_source !== '' && (
          <TextField label={f.fundingNote} name="funding_note" hint={f.fundingNoteHint} value={values.funding_note} onChange={(e) => set('funding_note', e.target.value)} error={errors.funding_note} optional />
        )}
        <Select label={f.housing} name="housing" options={housingOptions} value={values.housing} onChange={(e) => set('housing', e.target.value)} error={errors.housing} />
      </fieldset>

      <fieldset className="form-block">
        <legend>{f.sections.deadline}</legend>
        {(type === 'research' || type === 'teaching') && (
          <Checkbox label={f.competition} checked={values.is_competition} onChange={(e) => set('is_competition', e.target.checked)} error={errors.is_competition} />
        )}
        {(type === 'research' || type === 'teaching') && <p className="field-hint">{f.competitionHint}</p>}
        <TextField label={f.deadline} name="deadline" type="date" hint={f.deadlineHint} value={values.deadline} onChange={(e) => set('deadline', e.target.value)} error={errors.deadline} optional={!values.is_competition} required={values.is_competition} />
      </fieldset>

      <div className="form-actions">
        {draft ? (
          <>
            <Button type="submit" variant="secondary" loading={pending === 'save'} disabled={busy} onClick={() => (intent.current = 'save')}>
              {organizationLocked ? f.save : f.saveDraft}
            </Button>
            <Button type="submit" loading={pending === 'publish'} disabled={busy} onClick={() => (intent.current = 'publish')}>
              {f.saveAndPublish}
            </Button>
          </>
        ) : (
          <Button type="submit" loading={pending === 'save'} disabled={busy} onClick={() => (intent.current = 'save')}>
            {f.save}
          </Button>
        )}
        <Button variant="quiet" disabled={busy} onClick={onCancel}>
          {f.cancel}
        </Button>
      </div>
    </form>
  )
}

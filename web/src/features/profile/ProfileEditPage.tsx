import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useRef, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { Combobox } from '../../ui/Combobox'
import { Select, type Option } from '../../ui/Select'
import { SkillsInput } from '../../ui/SkillsInput'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { useLeaveGuard } from '../../ui/useLeaveGuard'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { SpecialtiesPicker } from '../vacancies/SpecialtiesPicker'
import { useReference } from '../vacancies/api'
import { MAX_SKILL_LENGTH, MAX_SKILLS, refreshProfile, saveCore, skillsOf, useOwnProfile, type CoreFields, type Profile } from './api'
import { degreeOptions, titleOptions } from './labels'
import '../vacancies/vacancies.css'
import './profile.css'

/** Форма основных сведений: кто вы, степень и звание, специальности, идентификаторы, h-index, контакты. */
export function ProfileEditPage() {
  return <RequireUser>{() => <Loader />}</RequireUser>
}

function Loader() {
  const profile = useOwnProfile()
  const reference = useReference()
  if (profile.isPending || reference.isPending) return <PageSkeleton />
  if (profile.isError) return <LoadFailed title={t.profile.loadError} error={profile.error} onRetry={() => void profile.refetch()} />
  if (reference.isError) return <LoadFailed title={t.profile.loadError} error={reference.error} onRetry={() => void reference.refetch()} />
  return <EditForm profile={profile.data.profile} reference={reference.data} />
}

type Values = Record<'headline' | 'city' | 'region_code' | 'about' | 'degree' | 'degree_specialty_code' | 'degree_year' | 'degree_institution' | 'dissertation_title' | 'academic_title' | 'academic_title_year' | 'orcid' | 'spin' | 'scopus_id' | 'wos_id' | 'h_rsci' | 'h_scopus' | 'h_wos' | 'h_scholar' | 'contact_email' | 'specialties' | 'research_skills' | 'general_skills', string>

const str = (n: number | null | undefined) => (n === null || n === undefined ? '' : String(n))
const num = (s: string): number | null => (s.trim() === '' ? null : Number(s))
// Навыки в форме лежат одной строкой через перевод строки: в самих навыках переводов строк не бывает.
const list = (s: string): string[] => (s === '' ? [] : s.split('\n'))

function initialOf(p: Profile): Values {
  return {
    headline: p.headline,
    city: p.city,
    region_code: p.region?.code ?? '',
    about: p.about,
    degree: p.degree.level,
    degree_specialty_code: p.degree.specialty?.code ?? '',
    degree_year: str(p.degree.year),
    degree_institution: p.degree.institution,
    dissertation_title: p.degree.dissertation,
    academic_title: p.academic_title,
    academic_title_year: str(p.academic_title_year),
    orcid: p.identifiers.orcid,
    spin: p.identifiers.spin,
    scopus_id: p.identifiers.scopus_id,
    wos_id: p.identifiers.wos_id,
    h_rsci: str(p.h_index.rsci),
    h_scopus: str(p.h_index.scopus),
    h_wos: str(p.h_index.wos),
    h_scholar: str(p.h_index.scholar),
    contact_email: p.contact_email ?? '',
    specialties: p.specialties.map((s) => s.code).join(','),
    research_skills: skillsOf(p).research.join('\n'),
    general_skills: skillsOf(p).general.join('\n'),
  }
}

function fieldsOf(v: Values): CoreFields {
  return {
    headline: v.headline,
    city: v.city,
    region_code: v.region_code,
    about: v.about,
    degree: v.degree,
    degree_specialty_code: v.degree_specialty_code,
    degree_year: num(v.degree_year),
    degree_institution: v.degree_institution,
    dissertation_title: v.dissertation_title,
    academic_title: v.academic_title,
    academic_title_year: num(v.academic_title_year),
    orcid: v.orcid,
    spin: v.spin,
    scopus_id: v.scopus_id,
    wos_id: v.wos_id,
    h_rsci: num(v.h_rsci),
    h_scopus: num(v.h_scopus),
    h_wos: num(v.h_wos),
    h_scholar: num(v.h_scholar),
    contact_email: v.contact_email,
    specialties: v.specialties === '' ? [] : v.specialties.split(','),
    research_skills: list(v.research_skills),
    general_skills: list(v.general_skills),
  }
}

function EditForm({ profile, reference }: { profile: Profile; reference: NonNullable<ReturnType<typeof useReference>['data']> }) {
  const e = t.profile.edit
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const save = useMutation({
    mutationFn: (fields: CoreFields) => saveCore(fields),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: e.saved })
      guard.release()
      void navigate('/profile')
    },
  })
  const { values, set, errors, validate, dirty } = useForm<Values>(initialOf(profile), save.error, formRef)
  const guard = useLeaveGuard(dirty)
  const formError = save.error && Object.keys(fieldErrorsOf(save.error)).length === 0 ? describeError(save.error) : undefined

  const regionOptions: Option[] = useMemo(() => reference.regions.map((r) => ({ value: r.code, label: r.name })), [reference])
  const specialtyOptions: Option[] = useMemo(
    () => reference.science.flatMap((f) => f.groups.flatMap((g) => g.specialties.map((s) => ({ value: s.code, label: `${s.code} ${s.name}` })))),
    [reference],
  )
  const hasDegree = values.degree !== 'none'
  const hasTitle = values.academic_title !== 'none'

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    validate({})
    save.mutate(fieldsOf(values))
  }
  const text = (name: keyof Values, label: string, extra: { hint?: string; type?: string; inputMode?: 'numeric' | 'email' } = {}) => (
    <TextField label={label} name={name} optional hint={extra.hint} type={extra.type} inputMode={extra.inputMode} value={values[name]} onChange={(ev) => set(name, ev.target.value)} error={errors[name]} />
  )
  const number = (name: keyof Values, label: string, min: number, max: number) => (
    <TextField label={label} name={name} optional type="number" inputMode="numeric" min={min} max={max} step={1} value={values[name]} onChange={(ev) => set(name, ev.target.value)} error={errors[name]} />
  )

  return (
    <div className="page page-narrow profile-edit">
      {guard.prompt}
      <h1>{e.title}</h1>
      <p className="lead">{e.lead}</p>
      <form className="profile-form" onSubmit={submit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}

        <fieldset className="form-block">
          <legend>{e.groups.main}</legend>
          {text('headline', e.headline, { hint: e.headlineHint })}
          <div className="field-pair">
            {text('city', e.city)}
            <Combobox label={e.region} placeholder={e.regionPlaceholder} options={regionOptions} value={values.region_code === '' ? null : values.region_code} onChange={(code) => set('region_code', code ?? '')} error={errors.region_code} optional />
          </div>
          <TextArea label={e.about} name="about" optional hint={e.aboutHint} rows={6} value={values.about} onChange={(ev) => set('about', ev.target.value)} error={errors.about} />
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.skills}</legend>
          <p className="field-hint">{e.skillsLead}</p>
          <SkillsInput label={e.researchSkills} hint={e.researchSkillsHint} placeholder={e.researchSkillsPlaceholder} value={list(values.research_skills)} onChange={(next) => set('research_skills', next.join('\n'))} error={errors.research_skills} max={MAX_SKILLS} maxLength={MAX_SKILL_LENGTH} />
          <SkillsInput label={e.generalSkills} hint={e.generalSkillsHint} placeholder={e.generalSkillsPlaceholder} value={list(values.general_skills)} onChange={(next) => set('general_skills', next.join('\n'))} error={errors.general_skills} max={MAX_SKILLS} maxLength={MAX_SKILL_LENGTH} />
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.degree}</legend>
          <Select label={e.degree} name="degree" options={degreeOptions} value={values.degree} onChange={(ev) => set('degree', ev.target.value)} error={errors.degree} />
          {hasDegree && (
            <>
              <Combobox label={e.degreeSpecialty} placeholder={e.degreeSpecialtyPlaceholder} options={specialtyOptions} value={values.degree_specialty_code === '' ? null : values.degree_specialty_code} onChange={(code) => set('degree_specialty_code', code ?? '')} error={errors.degree_specialty_code} optional />
              <div className="field-pair">
                {number('degree_year', e.degreeYear, 1950, 2100)}
                {text('degree_institution', e.degreeInstitution)}
              </div>
              {text('dissertation_title', e.dissertation)}
            </>
          )}
          <Select label={e.academicTitle} name="academic_title" options={titleOptions} value={values.academic_title} onChange={(ev) => set('academic_title', ev.target.value)} error={errors.academic_title} />
          {hasTitle && number('academic_title_year', e.academicTitleYear, 1950, 2100)}
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.specialties}</legend>
          <SpecialtiesPicker science={reference.science} value={values.specialties === '' ? [] : values.specialties.split(',')} onChange={(codes) => set('specialties', codes.join(','))} error={errors.specialties} optional />
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.identifiers}</legend>
          {text('orcid', e.orcid, { hint: e.orcidHint })}
          <div className="field-pair">
            {text('spin', e.spin, { hint: e.spinHint })}
            {text('scopus_id', e.scopus, { hint: e.scopusHint, inputMode: 'numeric' })}
          </div>
          {text('wos_id', e.wos, { hint: e.wosHint })}
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.metrics}</legend>
          <div className="field-pair">
            {number('h_rsci', t.profile.identifiers.h.rsci, 0, 300)}
            {number('h_scopus', t.profile.identifiers.h.scopus, 0, 300)}
          </div>
          <div className="field-pair">
            {number('h_wos', t.profile.identifiers.h.wos, 0, 300)}
            {number('h_scholar', t.profile.identifiers.h.scholar, 0, 300)}
          </div>
          <p className="field-hint">{e.hHint}</p>
        </fieldset>

        <fieldset className="form-block">
          <legend>{e.groups.contacts}</legend>
          {text('contact_email', e.contactEmail, { hint: e.contactEmailHint, type: 'email', inputMode: 'email' })}
        </fieldset>

        <div className="form-actions">
          <Button type="submit" loading={save.isPending}>
            {e.save}
          </Button>
          <ButtonLink to="/profile" variant="quiet">
            {e.cancel}
          </ButtonLink>
        </div>
      </form>
    </div>
  )
}

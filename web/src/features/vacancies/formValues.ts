import type { Reference, VacancyFields } from './api'

/** Значения полей формы: имена совпадают с полями сервера, чтобы его ошибки попадали точно в нужное поле. */
export type FormValues = {
  organization: string
  position_type: string
  unit_id: string
  position_code: string
  title: string
  summary: string
  description: string
  requirements: string
  focus: string
  specialties: string // коды через запятую
  career_level: string
  degree_required: string
  title_required: string
  work_format: string
  region_code: string
  city: string
  rate_percent: string
  salary_from: string
  salary_to: string
  contract_type: string
  contract_months: string
  funding_source: string
  funding_note: string
  housing: string
  is_competition: boolean
  deadline: string
}

export const emptyValues: FormValues = {
  organization: '',
  position_type: '',
  unit_id: '',
  position_code: '',
  title: '',
  summary: '',
  description: '',
  requirements: '',
  focus: '',
  specialties: '',
  career_level: '',
  degree_required: 'none',
  title_required: 'none',
  work_format: '',
  region_code: '',
  city: '',
  rate_percent: '',
  salary_from: '',
  salary_to: '',
  contract_type: '',
  contract_months: '',
  funding_source: '',
  funding_note: '',
  housing: 'none',
  is_competition: false,
  deadline: '',
}

/** Поля сервера в том порядке, в каком они стоят в форме. */
export const formFieldOrder = [
  'unit_id',
  'position_code',
  'title',
  'summary',
  'description',
  'requirements',
  'focus',
  'specialties',
  'career_level',
  'degree_required',
  'title_required',
  'work_format',
  'region_code',
  'city',
  'rate_percent',
  'salary_from',
  'salary_to',
  'contract_type',
  'contract_months',
  'funding_source',
  'funding_note',
  'housing',
  'is_competition',
  'deadline',
] as const

export type Intent = 'save' | 'publish'

const numberOrNull = (s: string) => (s.trim() === '' ? null : Number(s))

/** Превращает значения формы в то, что ждёт сервер. Пустые поля идут пустыми: проверяет их сервер. */
export function toFields(v: FormValues): VacancyFields {
  return {
    title: v.title,
    position_code: v.position_code,
    unit_id: v.unit_id === '' ? null : v.unit_id,
    summary: v.summary,
    description: v.description,
    requirements: v.requirements,
    focus: v.focus,
    career_level: numberOrNull(v.career_level),
    work_format: v.work_format,
    region_code: v.region_code,
    city: v.city,
    housing: v.housing,
    rate_percent: numberOrNull(v.rate_percent),
    salary_from: numberOrNull(v.salary_from),
    salary_to: numberOrNull(v.salary_to),
    contract_type: v.contract_type,
    contract_months: v.contract_type === 'fixed' ? numberOrNull(v.contract_months) : null,
    funding_source: v.funding_source,
    funding_note: v.funding_note,
    degree_required: v.degree_required,
    title_required: v.position_type === 'teaching' ? v.title_required : 'none',
    is_competition: v.position_type === 'research' || v.position_type === 'teaching' ? v.is_competition : false,
    deadline: v.deadline,
    specialties: v.specialties === '' ? [] : v.specialties.split(','),
  }
}

/** Тип позиции по коду должности; пусто, если должности нет в справочнике. */
export const typeOfPosition = (reference: Reference, code: string): string => reference.positions.find((p) => p.code === code)?.type ?? ''


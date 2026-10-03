import { t } from '../../i18n'
import { kindLabel, kindOptions } from '../orgs/labels'
import type { Option } from '../../ui/Select'
import { positionTypes, type Reference } from '../vacancies/api'
import {
  degreeOptions,
  formatLabel,
  fundingOptions,
  levelOptions,
  levelParts,
  positionTypeLabel,
  rateOptions,
  degreeLabel,
  fundingLabel,
} from '../vacancies/labels'
import { terms, type FilterRef, type MultiKey, type Search } from './params'

export const typeOptions: readonly Option[] = positionTypes.map((v) => ({ value: v, label: positionTypeLabel(v) }))
export const termOptions: readonly Option[] = terms.map((v) => ({ value: v, label: t.find.terms[v] }))

/** Варианты для каждого фильтра «один из нескольких», кроме области науки (она берётся из справочника). */
export const optionsFor: Record<Exclude<MultiKey, 'field'>, readonly Option[]> = {
  type: typeOptions,
  level: levelOptions,
  format: ['onsite', 'hybrid', 'remote'].map((v) => ({ value: v, label: formatLabel(v) })),
  degree: degreeOptions,
  org_kind: kindOptions,
  funding: fundingOptions,
  rate: rateOptions,
  term: termOptions,
}

/** Названия областей науки по коду: «1.4 Химические науки», «1.4.3 Органическая химия». */
export function fieldNames(reference: Reference | undefined): Map<string, string> {
  const out = new Map<string, string>()
  for (const field of reference?.science ?? []) {
    out.set(field.code, field.name)
    for (const group of field.groups) {
      out.set(group.code, group.name)
      for (const s of group.specialties) out.set(s.code, s.name)
    }
  }
  return out
}

export const salaryPresets = [50000, 80000, 100000, 150000, 200000] as const

const money = new Intl.NumberFormat('ru-RU')
export const salaryChip = (v: number) => t.find.salaryFrom(money.format(v))

/** Подпись фильтра в чипе над выдачей. Пока справочник не загрузился, область науки показывается кодом. */
export function filterLabel(ref: FilterRef, search: Search, names: Map<string, string>, regionName: (code: string) => string): string {
  switch (ref.kind) {
    case 'multi': {
      const v = ref.value
      switch (ref.key) {
        case 'field': {
          const name = names.get(v)
          return name ? `${v} ${name}` : v
        }
        case 'type':
          return positionTypeLabel(v)
        case 'level': {
          const parts = levelParts(Number(v))
          return parts ? `${parts[0]} · ${parts[1]}` : v
        }
        case 'format':
          return formatLabel(v)
        case 'degree':
          return `${t.find.groups.degree}: ${degreeLabel(v).toLowerCase()}`
        case 'org_kind':
          return kindLabel(v)
        case 'funding':
          return fundingLabel(v)
        case 'rate':
          return t.vacancies.rate(Number(v))
        case 'term':
          return t.find.terms[v as keyof typeof t.find.terms] ?? v
      }
      return v
    }
    case 'region':
      return regionName(search.region)
    case 'salary':
      return salaryChip(search.salaryMin)
    case 'housing':
      return t.find.housing
    case 'competition':
      return t.find.competition
    case 'deadline':
      return t.find.deadlines[search.deadline as keyof typeof t.find.deadlines] ?? search.deadline
  }
}

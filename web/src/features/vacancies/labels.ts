import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import type { Option } from '../../ui/Select'
import {
  academicTitles,
  careerLevels,
  contractTypes,
  degrees,
  fundingSources,
  housings,
  positionTypes,
  rates,
  rulesOf,
  workFormats,
  type Card,
  type PositionType,
  type Status,
} from './api'

// Сервер может прислать значение, которого в словаре ещё нет (после обновления сервера раньше сайта):
// тогда показываем само значение, а не пустое место.

const lookup = (dict: Record<string, string>, key: string) => dict[key] ?? key

export const positionTypeLabel = (type: string) => lookup(t.vacancies.positionTypes, type)
export const formatLabel = (v: string) => lookup(t.vacancies.formats, v)
export const housingLabel = (v: string) => lookup(t.vacancies.housings, v)
export const fundingLabel = (v: string) => lookup(t.vacancies.fundings, v)
export const degreeLabel = (v: string) => lookup(t.vacancies.degrees, v)
export const titleLabel = (v: string) => lookup(t.vacancies.titles, v)
export const statusLabel = (v: string) => lookup(t.vacancies.statuses, v)

/** Уровень R1–R4: [«R3», «самостоятельный исследователь»]; null, если уровня нет или он незнакомый. */
export function levelParts(level: number | null): readonly [string, string] | null {
  const l = t.vacancies.levels[level as 1 | 2 | 3 | 4]
  return l ? [l.short, l.name] : null
}

const toOptions = <T extends string | number>(values: readonly T[], label: (v: T) => string): readonly Option[] =>
  values.map((v) => ({ value: String(v), label: label(v) }))

export const positionTypeOptions = positionTypes
export const formatOptions = toOptions(workFormats, formatLabel)
export const housingOptions = toOptions(housings, housingLabel)
export const contractOptions = toOptions(contractTypes, (v) => lookup(t.vacancies.contracts, v))
export const fundingOptions = toOptions(fundingSources, fundingLabel)
export const degreeOptions = toOptions(degrees, degreeLabel)
export const titleOptions = toOptions(academicTitles, titleLabel)
export const rateOptions = toOptions(rates, (v) => t.vacancies.rate(v))
export const levelOptions = toOptions(careerLevels, (v) => {
  const l = t.vacancies.levels[v]
  return `${l.short} · ${l.name}`
})

/** Пояснение к выбранному уровню (что он значит); без выбора — общее. */
export const levelHint = (level: string) => t.vacancies.levels[Number(level) as 1 | 2 | 3 | 4]?.hint ?? t.vacancies.form.levelHint

const money = new Intl.NumberFormat('ru-RU')

/** «от 80 000 ₽», «до 120 000 ₽», «80 000 – 120 000 ₽»; null, если зарплата не указана. */
export function salaryText(from: number | null, to: number | null): string | null {
  if (from !== null && to !== null) return t.vacancies.salaryRange(money.format(from), money.format(to))
  if (from !== null) return t.vacancies.salaryFrom(money.format(from))
  if (to !== null) return t.vacancies.salaryTo(money.format(to))
  return null
}

/** Срок договора словами: 36 → «3 года», 18 → «18 месяцев». */
export function termText(months: number): string {
  if (months % 12 === 0) {
    const years = months / 12
    return `${years} ${plural(years, t.vacancies.yearForms)}`
  }
  return t.vacancies.months(months, plural(months, t.vacancies.monthForms))
}

/** «Бессрочный договор» или «Срочный договор, 3 года». */
export function contractText(type: string, months: number | null): string {
  if (type === 'fixed') {
    return months === null ? t.vacancies.contracts.fixed : t.vacancies.contractFixed(termText(months))
  }
  return type === '' ? '' : lookup(t.vacancies.contracts, type)
}

/** Стипендия у аспирантуры, магистратуры и стажировки, зарплата у остальных. */
export const salaryLabel = (type: PositionType | string) => (rulesOf(type).stipend ? t.vacancies.page.stipend : t.vacancies.page.salary)

export const focusLabel = (type: string) => lookup(t.vacancies.page.focusLabels, type)

/** Факты для строки вакансии в списке: должность, ставка, договор, формат, деньги. */
export function entryFacts(c: Card): string[] {
  const facts: string[] = [c.position.name]
  if (c.rate_percent !== null) facts.push(t.vacancies.rate(c.rate_percent))
  const contract = contractText(c.contract_type, c.contract_months)
  if (contract) facts.push(contract)
  if (c.work_format) facts.push(formatLabel(c.work_format))
  const pay = salaryText(c.salary_from, c.salary_to)
  if (pay) facts.push(`${salaryLabel(c.position.type)} ${pay}`)
  return facts
}

/** Научные специальности вакансии одной строкой: первая и «ещё N»; пусто, если их нет. */
export function entryTopics(c: Card): string {
  const [first, ...rest] = c.specialties
  if (!first) return ''
  return rest.length > 0 ? `${first.name} ${t.vacancies.page.moreSpecialties(rest.length)}` : first.name
}

/** Город или регион, как показать у вакансии; пусто для удалённой работы без места. */
export function placeText(c: Pick<Card, 'city' | 'region'>): string {
  return t.vacancies.place(c.city, c.region?.name ?? '')
}

export const transitionLabel = (from: Status | string, to: Status | string) =>
  (t.vacancies.page.actions as Record<string, string>)[`${from}_${to}`] ?? to

export const doneText = (to: string) => (t.vacancies.page.done as Record<string, string>)[to] ?? t.vacancies.page.done.draft


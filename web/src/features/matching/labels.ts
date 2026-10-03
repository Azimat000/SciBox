import { t } from '../../i18n'
import type { Option } from '../../ui/Select'
import { fieldNames, filterLabel } from '../search/labels'
import { activeFilters, parseSearch, type Search } from '../search/params'
import type { Reference } from '../vacancies/api'
import { frequencies } from './api'

export const MAX_NAME = 120

export const frequencyLabel = (f: string): string => t.matching.frequencies[f] ?? f
export const frequencyHint = (f: string): string => t.matching.frequencyHints[f] ?? ''
export const frequencyOptions: readonly Option[] = frequencies.map((value) => ({ value, label: frequencyLabel(value) }))

/** Причина, по которой вакансия попала в подборку, словами. Неизвестный код показывается как есть. */
export const reasonLabel = (code: string): string => t.matching.matches.reasons[code] ?? code

/** Условия поиска словами: сначала слова, затем подписи включённых фильтров. */
export function conditionsOfSearch(search: Search, reference: Reference | undefined): string[] {
  const names = fieldNames(reference)
  const regionName = (code: string) => reference?.regions.find((r) => r.code === code)?.name ?? code
  const out: string[] = []
  if (search.q) out.push(t.matching.searches.words(search.q))
  for (const ref of activeFilters(search)) out.push(filterLabel(ref, search, names, regionName))
  return out
}

/** То же для сохранённого поиска: условия лежат строкой запроса. */
export const conditionsOfQuery = (query: string, reference: Reference | undefined): string[] =>
  conditionsOfSearch(parseSearch(new URLSearchParams(query)), reference)

/** Название для нового поиска: слова и подписи фильтров через « · », не длиннее 120 знаков. */
export function suggestName(search: Search, reference: Reference | undefined): string {
  const parts = conditionsOfSearch({ ...search, q: '' }, reference)
  const name = [search.q, ...parts].filter(Boolean).join(' · ') || t.matching.save.defaultName
  return name.length > MAX_NAME ? `${name.slice(0, MAX_NAME - 1).trimEnd()}…` : name
}

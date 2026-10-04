import { t } from '../../i18n'
import type { Option } from '../../ui/Select'
import { degrees, titles, type CatalogSearch, type FilterRef } from './params'

const lookup = (dict: Record<string, string>, key: string) => dict[key] ?? key

export const degreeLabel = (v: string) => lookup(t.catalog.degrees, v)
export const titleLabel = (v: string) => lookup(t.catalog.titles, v)

export const degreeOptions: readonly Option[] = degrees.map((v) => ({ value: v, label: degreeLabel(v) }))
export const titleOptions: readonly Option[] = titles.map((v) => ({ value: v, label: titleLabel(v) }))

/** Подпись фильтра в чипе над выдачей. Пока справочник не загрузился, область науки показывается кодом. */
export function filterLabel(ref: FilterRef, search: CatalogSearch, names: Map<string, string>, regionName: (code: string) => string): string {
  switch (ref.kind) {
    case 'list':
      if (ref.key === 'field') {
        const name = names.get(ref.value)
        return name ? `${ref.value} ${name}` : ref.value
      }
      return ref.key === 'degree' ? degreeLabel(ref.value) : titleLabel(ref.value)
    case 'region':
      return regionName(search.region)
    case 'open':
      return t.catalog.openOnly
    case 'hIndex':
      return t.catalog.hIndexFrom(search.hMin)
    case 'q12':
      return t.catalog.q12From(search.q12Min)
  }
}

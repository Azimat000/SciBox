import { t } from '../../i18n'
import { Checkbox } from '../../ui/Checkbox'
import { FilterGroup, ScienceFilter } from '../../ui/FilterGroup'
import { Select } from '../../ui/Select'
import type { Reference } from '../vacancies/api'
import { degreeOptions, titleOptions } from './labels'
import { change, hIndexPresets, toggleList, type CatalogSearch, type ListKey } from './params'

type Props = { search: CatalogSearch; onChange: (next: CatalogSearch) => void; reference: Reference | undefined }

function Checks({ search, onChange, name, options }: Pick<Props, 'search' | 'onChange'> & { name: Exclude<ListKey, 'field'>; options: typeof degreeOptions }) {
  return (
    <div className="filter-checks">
      {options.map((o) => (
        <Checkbox key={o.value} label={o.label} checked={search[name].includes(o.value)} onChange={() => onChange(toggleList(search, name, o.value))} />
      ))}
    </div>
  )
}

/** Все фильтры каталога. Каждое изменение сразу попадает в адрес (родитель решает, как). */
export function CatalogFilters({ search, onChange, reference }: Props) {
  const g = t.catalog.groups
  const presets = [...hIndexPresets] as number[]
  if (search.hMin > 0 && !presets.includes(search.hMin)) presets.push(search.hMin)
  presets.sort((a, b) => a - b)
  return (
    <div className="filter-panel">
      <FilterGroup title={g.offers} selected={search.open ? 1 : 0} defaultOpen>
        <Checkbox label={t.catalog.openOnly} checked={search.open} onChange={(e) => onChange(change(search, { open: e.target.checked }))} />
      </FilterGroup>
      <FilterGroup title={g.field} selected={search.field.length}>
        {reference && <ScienceFilter fields={reference.science} selected={search.field} onToggle={(code) => onChange(toggleList(search, 'field', code))} />}
      </FilterGroup>
      <FilterGroup title={g.degree} selected={search.degree.length}>
        <Checks search={search} onChange={onChange} name="degree" options={degreeOptions} />
      </FilterGroup>
      <FilterGroup title={g.title} selected={search.title.length}>
        <Checks search={search} onChange={onChange} name="title" options={titleOptions} />
      </FilterGroup>
      <FilterGroup title={g.hIndex} selected={search.hMin > 0 ? 1 : 0}>
        <Select
          label={t.catalog.hIndexLabel}
          hint={t.catalog.hIndexNote}
          placeholder={t.catalog.hIndexAny}
          value={search.hMin > 0 ? String(search.hMin) : ''}
          options={presets.map((v) => ({ value: String(v), label: t.catalog.hIndexFrom(v) }))}
          onChange={(e) => onChange(change(search, { hMin: Number(e.target.value) || 0 }))}
        />
      </FilterGroup>
    </div>
  )
}

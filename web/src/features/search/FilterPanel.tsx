import { useState, type ReactNode } from 'react'
import { t } from '../../i18n'
import { Checkbox } from '../../ui/Checkbox'
import { Select } from '../../ui/Select'
import { FilterChip } from '../../ui/Tag'
import type { Reference } from '../vacancies/api'
import { optionsFor, salaryChip, salaryPresets } from './labels'
import { change, toggleMulti, type DeadlineFilter, type MultiKey, type Search } from './params'

type BaseProps = {
  search: Search
  onChange: (next: Search) => void
}

type PanelProps = BaseProps & { reference: Reference | undefined }

/** Раскрывающаяся группа фильтров. Открыта, если в ней что-то выбрано; сама раскрывается, когда выбор появляется со стороны. */
function Group({ title, selected, defaultOpen = false, children }: { title: string; selected: number; defaultOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(defaultOpen || selected > 0)
  const [seen, setSeen] = useState(selected)
  if (selected !== seen) {
    setSeen(selected)
    if (selected > 0 && seen === 0) setOpen(true)
  }
  return (
    <details className="filter-group" open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
      <summary>
        <span className="filter-group-title">{title}</span>
        {selected > 0 && <span className="filter-group-count num">{t.find.selected(selected)}</span>}
      </summary>
      <div className="filter-group-body">{children}</div>
    </details>
  )
}

/** Флажки одного фильтра. */
function Checks({ search, onChange, name }: BaseProps & { name: Exclude<MultiKey, 'field'> }) {
  return (
    <div className="filter-checks">
      {optionsFor[name].map((o) => (
        <Checkbox
          key={o.value}
          label={o.label}
          checked={search.multi[name].includes(o.value)}
          onChange={() => onChange(toggleMulti(search, name, o.value))}
        />
      ))}
    </div>
  )
}

/** Короткие значения лежат рядом, как переключатели. */
function Chips({ search, onChange, name, label }: BaseProps & { name: 'format' | 'rate'; label: string }) {
  return (
    <div className="filter-chips" role="group" aria-label={label}>
      {optionsFor[name].map((o) => (
        <FilterChip key={o.value} pressed={search.multi[name].includes(o.value)} onClick={() => onChange(toggleMulti(search, name, o.value))}>
          {o.label}
        </FilterChip>
      ))}
    </div>
  )
}

/** Области науки: пять разделов, в каждом группы специальностей. Выбирается группа, она находит все свои специальности. */
function Science({ search, onChange, reference }: PanelProps) {
  if (!reference) return null
  return (
    <div className="filter-science">
      {reference.science.map((field) => {
        const chosen = field.groups.filter((g) => search.multi.field.includes(g.code)).length
        return (
          <Group key={field.code} title={field.name} selected={chosen}>
            <div className="filter-checks">
              {field.groups.map((g) => (
                <Checkbox
                  key={g.code}
                  label={
                    <>
                      <span className="filter-code num">{g.code}</span> {g.name}
                    </>
                  }
                  checked={search.multi.field.includes(g.code)}
                  onChange={() => onChange(toggleMulti(search, 'field', g.code))}
                />
              ))}
            </div>
          </Group>
        )
      })}
    </div>
  )
}

const deadlineOrder: readonly DeadlineFilter[] = ['week', 'month', 'none']

/** Все фильтры поиска. Каждое изменение сразу попадает в адрес (родитель решает, как). */
export function FilterPanel({ search, onChange, reference }: PanelProps) {
  const n = (key: MultiKey) => search.multi[key].length
  const presets = [...salaryPresets] as number[]
  if (search.salaryMin > 0 && !presets.includes(search.salaryMin)) presets.push(search.salaryMin)
  presets.sort((a, b) => a - b)
  const g = t.find.groups

  return (
    <div className="filter-panel">
      <Group title={g.field} selected={n('field')}>
        <Science search={search} onChange={onChange} reference={reference} />
      </Group>
      <Group title={g.type} selected={n('type') + (search.competition ? 1 : 0)} defaultOpen>
        <Checks search={search} onChange={onChange} name="type" />
        <Checkbox label={t.find.competition} checked={search.competition} onChange={(e) => onChange(change(search, { competition: e.target.checked }))} />
      </Group>
      <Group title={g.level} selected={n('level')}>
        <Checks search={search} onChange={onChange} name="level" />
      </Group>
      <Group title={g.format} selected={n('format')} defaultOpen>
        <Chips search={search} onChange={onChange} name="format" label={g.format} />
      </Group>
      <Group title={g.salary} selected={(search.salaryMin > 0 ? 1 : 0) + (search.housing ? 1 : 0)}>
        <Select
          label={t.find.salaryLabel}
          hint={t.find.salaryNote}
          placeholder={t.find.salaryAny}
          value={search.salaryMin > 0 ? String(search.salaryMin) : ''}
          options={presets.map((v) => ({ value: String(v), label: salaryChip(v) }))}
          onChange={(e) => onChange(change(search, { salaryMin: Number(e.target.value) || 0 }))}
        />
        <Checkbox label={t.find.housing} checked={search.housing} onChange={(e) => onChange(change(search, { housing: e.target.checked }))} />
      </Group>
      <Group title={g.rate} selected={n('rate')}>
        <Chips search={search} onChange={onChange} name="rate" label={g.rate} />
      </Group>
      <Group title={g.term} selected={n('term')}>
        <Checks search={search} onChange={onChange} name="term" />
      </Group>
      <Group title={g.funding} selected={n('funding')}>
        <Checks search={search} onChange={onChange} name="funding" />
      </Group>
      <Group title={g.degree} selected={n('degree')}>
        <Checks search={search} onChange={onChange} name="degree" />
      </Group>
      <Group title={g.org_kind} selected={n('org_kind')}>
        <Checks search={search} onChange={onChange} name="org_kind" />
      </Group>
      <Group title={g.deadline} selected={search.deadline ? 1 : 0}>
        <div className="filter-chips" role="group" aria-label={g.deadline}>
          {deadlineOrder.map((d) => (
            <FilterChip key={d} pressed={search.deadline === d} onClick={() => onChange(change(search, { deadline: search.deadline === d ? '' : d }))}>
              {t.find.deadlines[d]}
            </FilterChip>
          ))}
        </div>
      </Group>
    </div>
  )
}

import { useState, type ReactNode } from 'react'
import { t } from '../i18n'
import { Checkbox } from './Checkbox'

/** Раскрывающаяся группа фильтров. Открыта, если в ней что-то выбрано; сама раскрывается, когда выбор появляется со стороны. */
export function FilterGroup({ title, selected, defaultOpen = false, children }: { title: string; selected: number; defaultOpen?: boolean; children: ReactNode }) {
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

/** Одна область науки: раздел справочника ВАК с группами специальностей (код и название). */
export type ScienceField = { code: string; name: string; groups: readonly { code: string; name: string }[] }

/** Области науки: разделы, в каждом группы специальностей. Выбирается группа, она находит все свои специальности. */
export function ScienceFilter({ fields, selected, onToggle }: { fields: readonly ScienceField[]; selected: readonly string[]; onToggle: (code: string) => void }) {
  return (
    <div className="filter-science">
      {fields.map((field) => {
        const chosen = field.groups.filter((g) => selected.includes(g.code)).length
        return (
          <FilterGroup key={field.code} title={field.name} selected={chosen}>
            <div className="filter-checks">
              {field.groups.map((g) => (
                <Checkbox
                  key={g.code}
                  label={
                    <>
                      <span className="filter-code num">{g.code}</span> {g.name}
                    </>
                  }
                  checked={selected.includes(g.code)}
                  onChange={() => onToggle(g.code)}
                />
              ))}
            </div>
          </FilterGroup>
        )
      })}
    </div>
  )
}

import { useMemo } from 'react'
import { t } from '../../i18n'
import { Combobox } from '../../ui/Combobox'
import { CloseIcon } from '../../ui/icons'
import type { Option } from '../../ui/Select'
import type { ScienceField } from './api'

export const MAX_SPECIALTIES = 5

type Props = {
  science: readonly ScienceField[]
  /** Коды выбранных специальностей. */
  value: readonly string[]
  onChange: (codes: string[]) => void
  error?: string
  /** Необязательно ли поле для этого типа позиции. */
  optional?: boolean
}

/** Выбор до пяти научных специальностей: поиск по названию и коду, выбранные стоят списком под полем. */
export function SpecialtiesPicker({ science, value, onChange, error, optional }: Props) {
  const all = useMemo(
    () => science.flatMap((f) => f.groups.flatMap((g) => g.specialties.map((s) => ({ code: s.code, name: s.name })))),
    [science],
  )
  const names = useMemo(() => new Map(all.map((s) => [s.code, s.name])), [all])
  const options: Option[] = useMemo(
    () => all.filter((s) => !value.includes(s.code)).map((s) => ({ value: s.code, label: `${s.code} ${s.name}` })),
    [all, value],
  )
  const full = value.length >= MAX_SPECIALTIES

  return (
    <div className="specialties">
      {!full && (
        <Combobox
          label={t.vacancies.form.specialties}
          hint={t.vacancies.form.specialtiesHint}
          placeholder={t.vacancies.form.specialtyPlaceholder}
          options={options}
          value={null}
          onChange={(code) => code && onChange([...value, code])}
          error={error}
          optional={optional}
        />
      )}
      {full && (
        <p className="field-label" id="specialties-full">
          {t.vacancies.form.specialties}
          <span className="field-hint"> {t.vacancies.form.specialtiesHint}</span>
        </p>
      )}
      {value.length === 0 ? (
        <p className="specialties-none">{t.vacancies.form.specialtiesNone}</p>
      ) : (
        <ul className="specialties-chosen">
          {value.map((code) => {
            const name = names.get(code) ?? code
            return (
              <li key={code} className="specialty-chip">
                <span>
                  <span className="num">{code}</span> {name}
                </span>
                <button type="button" className="specialty-remove" aria-label={t.vacancies.form.specialtyRemove(name)} onClick={() => onChange(value.filter((c) => c !== code))}>
                  <CloseIcon size={16} />
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

import type { SelectHTMLAttributes } from 'react'
import { t } from '../i18n'
import { Field } from './Field'
import { ChevronDownIcon } from './icons'
import './Field.css'

export type Option = { value: string; label: string }

type SelectProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'id' | 'children'> & {
  label: string
  options: readonly Option[]
  hint?: string
  error?: string
  optional?: boolean
  /** Текст пустого пункта; без него пустого пункта нет. */
  placeholder?: string
}

/** Короткий список (до десятка пунктов): родной select, на телефоне открывается системным выбором. */
export function Select({ label, options, hint, error, optional, required, placeholder, className, ...rest }: SelectProps) {
  return (
    <Field label={label} hint={hint} error={error} optional={optional} required={required} className={className}>
      {(control) => (
        <div className="select-wrap">
          <select className="control" {...control} required={required} {...rest}>
            {placeholder !== undefined && <option value="">{placeholder || t.ui.selectPlaceholder}</option>}
            {options.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <ChevronDownIcon className="select-chevron" size={18} />
        </div>
      )}
    </Field>
  )
}

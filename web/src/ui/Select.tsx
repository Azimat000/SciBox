import type { SelectHTMLAttributes } from 'react'
import { t } from '../i18n'
import { Field } from './Field'
import { ChevronDownIcon } from './icons'
import './Field.css'

export type Option = { value: string; label: string }

/** Группа пунктов: в родном списке выбора она подписана и отделена (например, вакансии по организациям). */
export type OptionGroup = { group: string; options: readonly Option[] }

type SelectProps = Omit<SelectHTMLAttributes<HTMLSelectElement>, 'id' | 'children'> & {
  label: string
  options: readonly (Option | OptionGroup)[]
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
            {options.map((o) =>
              'group' in o ? (
                <optgroup key={o.group} label={o.group}>
                  {o.options.map((item) => (
                    <option key={item.value} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </optgroup>
              ) : (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ),
            )}
          </select>
          <ChevronDownIcon className="select-chevron" size={18} />
        </div>
      )}
    </Field>
  )
}

import { useId, useMemo, useRef, useState, type ChangeEvent, type KeyboardEvent } from 'react'
import { t } from '../i18n'
import { normalize } from '../lib/normalize'
import { Field } from './Field'
import { ChevronDownIcon } from './icons'
import type { Option } from './Select'
import './Combobox.css'
import './Field.css'

type ComboboxProps = {
  label: string
  options: readonly Option[]
  /** Выбранное значение или null, если ничего не выбрано. */
  value: string | null
  onChange: (value: string | null) => void
  hint?: string
  error?: string
  optional?: boolean
  required?: boolean
  placeholder?: string
  className?: string
}

/**
 * Длинный список с поиском по набору букв (специальности, города, организации).
 * Шаблон WAI-ARIA 1.2: поле ввода со списком; фокус остаётся в поле, текущий пункт
 * сообщается через aria-activedescendant.
 */
export function Combobox({ label, options, value, onChange, hint, error, optional, required, placeholder, className }: ComboboxProps) {
  const listId = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const selected = options.find((o) => o.value === value) ?? null
  // null: человек ничего не печатает, в поле показан выбранный пункт
  const [draft, setDraft] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)

  const text = draft ?? selected?.label ?? ''
  const shown = useMemo(() => {
    if (draft === null) return options
    const q = normalize(draft)
    return q === '' ? options : options.filter((o) => normalize(o.label).includes(q))
  }, [options, draft])
  const activeIndex = Math.min(active, Math.max(shown.length - 1, 0))

  function close() {
    setOpen(false)
    setDraft(null)
  }

  function choose(option: Option) {
    onChange(option.value)
    close()
  }

  function onInput(e: ChangeEvent<HTMLInputElement>) {
    const next = e.target.value
    setDraft(next)
    setActive(0)
    setOpen(true)
    // Очистили поле целиком: сбрасываем и сам выбор
    if (next === '' && value !== null) onChange(null)
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        if (!open) setOpen(true)
        else setActive(Math.min(activeIndex + 1, shown.length - 1))
        break
      case 'ArrowUp':
        e.preventDefault()
        if (!open) setOpen(true)
        else setActive(Math.max(activeIndex - 1, 0))
        break
      case 'Home':
        if (open) {
          e.preventDefault()
          setActive(0)
        }
        break
      case 'End':
        if (open) {
          e.preventDefault()
          setActive(shown.length - 1)
        }
        break
      case 'Enter':
        if (open && shown[activeIndex]) {
          e.preventDefault()
          choose(shown[activeIndex])
        }
        break
      case 'Escape':
        if (open) {
          e.preventDefault()
          close()
        }
        break
    }
  }

  const optionId = (i: number) => `${listId}-o${i}`

  return (
    <Field label={label} hint={hint} error={error} optional={optional} required={required} className={className}>
      {(control) => (
        <div className="combo">
          <input
            ref={inputRef}
            className="control combo-input"
            type="text"
            role="combobox"
            autoComplete="off"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={open && shown.length > 0 ? optionId(activeIndex) : undefined}
            placeholder={placeholder}
            value={text}
            onChange={onInput}
            onKeyDown={onKeyDown}
            onBlur={close}
            {...control}
          />
          <button
            type="button"
            className="combo-toggle"
            tabIndex={-1}
            aria-label={t.ui.comboboxToggle}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setOpen(!open)
              inputRef.current?.focus()
            }}
          >
            <ChevronDownIcon size={18} />
          </button>
          <ul id={listId} role="listbox" aria-label={label} className="combo-list" hidden={!open}>
            {shown.map((o, i) => (
              <li
                key={o.value}
                id={optionId(i)}
                role="option"
                aria-selected={o.value === value}
                data-active={i === activeIndex || undefined}
                className="combo-option"
                // mousedown, а не click: иначе поле потеряет фокус раньше выбора
                onMouseDown={(e) => {
                  e.preventDefault()
                  choose(o)
                }}
                onMouseMove={() => setActive(i)}
              >
                {o.label}
              </li>
            ))}
            {shown.length === 0 && <li className="combo-empty">{t.ui.comboboxEmpty}</li>}
          </ul>
          <span className="visually-hidden" role="status">
            {open ? (shown.length === 0 ? t.ui.comboboxEmpty : t.ui.comboboxFound(shown.length)) : ''}
          </span>
        </div>
      )}
    </Field>
  )
}

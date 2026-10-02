import { useId, type ReactNode } from 'react'
import { t } from '../i18n'
import './Field.css'

export type FieldControlProps = {
  id: string
  'aria-describedby'?: string
  'aria-invalid'?: true
  'aria-required'?: true
}

type FieldProps = {
  label: string
  hint?: string
  error?: string
  /** Поле обязательное: подпись получает пометку «обязательно». */
  required?: boolean
  /** Поле необязательное: подпись получает пометку «необязательно». */
  optional?: boolean
  className?: string
  /** Рисует сам элемент ввода; ему нужно передать выданные свойства. */
  children: (control: FieldControlProps) => ReactNode
}

/** Подпись, подсказка и ошибка вокруг любого элемента ввода. */
export function Field({ label, hint, error, required, optional, className, children }: FieldProps) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`
  const describedBy = [error ? errorId : null, hint ? hintId : null].filter(Boolean).join(' ')

  const control: FieldControlProps = { id }
  if (describedBy) control['aria-describedby'] = describedBy
  if (error) control['aria-invalid'] = true
  if (required) control['aria-required'] = true

  return (
    <div className={['field', error ? 'field-error' : '', className ?? ''].filter(Boolean).join(' ')}>
      <label className="field-label" htmlFor={id}>
        {label}
        {required && <span className="field-flag"> · {t.ui.required}</span>}
        {optional && !required && <span className="field-flag"> · {t.ui.optional}</span>}
      </label>
      {children(control)}
      {error && (
        <p className="field-message" id={errorId}>
          {error}
        </p>
      )}
      {hint && (
        <p className="field-hint" id={hintId}>
          {hint}
        </p>
      )}
    </div>
  )
}

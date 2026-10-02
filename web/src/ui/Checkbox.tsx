import { useId, type InputHTMLAttributes, type ReactNode } from 'react'
import './Checkbox.css'

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'type' | 'children'> & {
  /** Подпись справа от флажка; может содержать ссылку. */
  label: ReactNode
  error?: string
}

/** Флажок с подписью. Вся строка нажимается, ошибка стоит под ней. */
export function Checkbox({ label, error, className, ...rest }: CheckboxProps) {
  const id = useId()
  const errorId = `${id}-error`
  return (
    <div className={['checkbox', error ? 'checkbox-error' : '', className ?? ''].filter(Boolean).join(' ')}>
      <div className="checkbox-row">
        <input
          id={id}
          type="checkbox"
          className="checkbox-input"
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errorId : undefined}
          {...rest}
        />
        <label htmlFor={id} className="checkbox-label">
          {label}
        </label>
      </div>
      {error && (
        <p className="field-message" id={errorId}>
          {error}
        </p>
      )}
    </div>
  )
}

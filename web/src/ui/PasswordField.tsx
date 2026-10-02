import { useState, type InputHTMLAttributes } from 'react'
import { t } from '../i18n'
import { Field } from './Field'
import './PasswordField.css'

type PasswordFieldProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'type'> & {
  label: string
  hint?: string
  error?: string
}

/** Поле пароля с кнопкой «Показать»: на телефоне опечатку в пароле иначе не заметить. */
export function PasswordField({ label, hint, error, required, className, ...rest }: PasswordFieldProps) {
  const [shown, setShown] = useState(false)
  return (
    <Field label={label} hint={hint} error={error} required={required} className={className}>
      {(control) => (
        <div className="password-wrap">
          <input
            className="control password-input"
            {...control}
            type={shown ? 'text' : 'password'}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            {...rest}
          />
          <button
            type="button"
            className="password-toggle"
            aria-pressed={shown}
            aria-label={shown ? t.auth.hidePassword : t.auth.showPassword}
            onClick={() => setShown((v) => !v)}
          >
            {shown ? t.auth.hideShort : t.auth.showShort}
          </button>
        </div>
      )}
    </Field>
  )
}

import type { InputHTMLAttributes, TextareaHTMLAttributes } from 'react'
import { Field } from './Field'
import './Field.css'

type Shared = { label: string; hint?: string; error?: string; optional?: boolean }

type TextFieldProps = Shared & Omit<InputHTMLAttributes<HTMLInputElement>, 'id'>

export function TextField({ label, hint, error, optional, required, className, ...rest }: TextFieldProps) {
  return (
    <Field label={label} hint={hint} error={error} optional={optional} required={required} className={className}>
      {(control) => <input className="control" {...control} required={required} {...rest} />}
    </Field>
  )
}

type TextAreaProps = Shared & Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id'>

export function TextArea({ label, hint, error, optional, required, className, rows = 5, ...rest }: TextAreaProps) {
  return (
    <Field label={label} hint={hint} error={error} optional={optional} required={required} className={className}>
      {(control) => <textarea className="control" rows={rows} {...control} required={required} {...rest} />}
    </Field>
  )
}

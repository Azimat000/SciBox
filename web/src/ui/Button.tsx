import type { ButtonHTMLAttributes } from 'react'
import { Link, type LinkProps } from 'react-router'
import { t } from '../i18n'
import './Button.css'

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'danger'
export type ButtonSize = 'md' | 'sm'

type Look = { variant?: ButtonVariant; size?: ButtonSize }

function classes({ variant = 'primary', size = 'md' }: Look, extra?: string) {
  return ['btn', `btn-${variant}`, size === 'sm' ? 'btn-sm' : '', extra ?? ''].filter(Boolean).join(' ')
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & Look & { loading?: boolean }

export function Button({ variant, size, loading = false, className, children, disabled, type = 'button', ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      className={classes({ variant, size }, className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      data-loading={loading || undefined}
      {...rest}
    >
      {loading && <span className="btn-spinner" aria-hidden="true" />}
      <span className="btn-label">{children}</span>
      {loading && <span className="visually-hidden">{t.ui.loadingButton}</span>}
    </button>
  )
}

export function ButtonLink({ variant, size, className, ...rest }: LinkProps & Look) {
  return <Link className={classes({ variant, size }, className)} {...rest} />
}

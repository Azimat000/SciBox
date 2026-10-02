import type { ReactNode } from 'react'
import { AlertIcon, CheckIcon, InfoIcon } from './icons'
import './Alert.css'

export type AlertKind = 'error' | 'success' | 'info'

const icons: Record<AlertKind, ReactNode> = {
  error: <AlertIcon />,
  success: <CheckIcon />,
  info: <InfoIcon />,
}

type AlertProps = {
  kind?: AlertKind
  title?: string
  children?: ReactNode
  /** Действие, которое исправляет ситуацию (кнопка или ссылка). */
  action?: ReactNode
  className?: string
}

/** Сообщение внутри страницы или формы. Ошибка читается сразу (role=alert), остальное вежливо (role=status). */
export function Alert({ kind = 'info', title, children, action, className }: AlertProps) {
  return (
    <div className={['alert', className ?? ''].filter(Boolean).join(' ')} data-kind={kind} role={kind === 'error' ? 'alert' : 'status'}>
      <span className="alert-icon">{icons[kind]}</span>
      <div className="alert-body">
        {title && <p className="alert-title">{title}</p>}
        {children && <p className="alert-text">{children}</p>}
        {action && <div className="alert-action">{action}</div>}
      </div>
    </div>
  )
}

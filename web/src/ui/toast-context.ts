import { createContext } from 'react'

export type ToastKind = 'info' | 'success' | 'error'

export type ToastInput = {
  kind?: ToastKind
  title: string
  text?: string
  /** Через сколько миллисекунд скрыть. Для ошибок по умолчанию не скрывается. */
  duration?: number
}

export type ToastApi = {
  show: (toast: ToastInput) => number
  dismiss: (id: number) => void
}

export const ToastContext = createContext<ToastApi | null>(null)

export const DEFAULT_DURATION = 6000

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { t } from '../i18n'
import { AlertIcon, CheckIcon, CloseIcon, InfoIcon } from './icons'
import { DEFAULT_DURATION, ToastContext, type ToastInput, type ToastKind } from './toast-context'
import './Toast.css'

type Item = ToastInput & { id: number; kind: ToastKind }

const MAX_VISIBLE = 3

const icons: Record<ToastKind, ReactNode> = {
  info: <InfoIcon />,
  success: <CheckIcon />,
  error: <AlertIcon />,
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Item[]>([])
  const nextId = useRef(1)
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const dismiss = useCallback((id: number) => {
    const timer = timers.current.get(id)
    if (timer) clearTimeout(timer)
    timers.current.delete(id)
    setItems((list) => list.filter((i) => i.id !== id))
  }, [])

  const show = useCallback(
    (input: ToastInput) => {
      const id = nextId.current++
      const kind = input.kind ?? 'info'
      setItems((list) => [...list, { ...input, id, kind }].slice(-MAX_VISIBLE))
      // Ошибка остаётся, пока её не закроют: её нужно успеть прочитать и понять
      const duration = input.duration ?? (kind === 'error' ? 0 : DEFAULT_DURATION)
      if (duration > 0) timers.current.set(id, setTimeout(() => dismiss(id), duration))
      return id
    },
    [dismiss],
  )

  useEffect(() => {
    const pending = timers.current
    return () => {
      pending.forEach(clearTimeout)
      pending.clear()
    }
  }, [])

  const api = useMemo(() => ({ show, dismiss }), [show, dismiss])

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-region" role="region" aria-label={t.ui.toastRegion}>
        {items.map((item) => (
          <div key={item.id} className="toast" data-kind={item.kind} role={item.kind === 'error' ? 'alert' : 'status'}>
            <span className="toast-icon">{icons[item.kind]}</span>
            <div className="toast-body">
              <p className="toast-title">{item.title}</p>
              {item.text && <p className="toast-text">{item.text}</p>}
            </div>
            <button type="button" className="toast-close" onClick={() => dismiss(item.id)} aria-label={t.ui.toastDismiss}>
              <CloseIcon size={18} />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

import { useCallback, useEffect, useMemo, useState, type RefObject } from 'react'
import { fieldErrorsOf } from './errors'
import type { FieldErrors } from './validation'

/**
 * Значения полей и ошибки формы. Ошибка поля (и от браузера, и от сервера) пропадает, как только
 * человек начал это поле править; после следующей отправки сервер может показать её снова.
 * После каждой отправки с ошибками фокус переходит на первое неверное поле: клавиатуре и экранному диктору
 * не надо искать его самим. Во время обычной правки фокус не трогаем.
 */
export function useForm<V extends Record<string, string | boolean>>(initial: V, serverError: unknown, form: RefObject<HTMLElement | null>) {
  const [values, setValues] = useState<V>(initial)
  const [attempts, setAttempts] = useState(0)
  const [clientErrors, setClientErrors] = useState<FieldErrors>({})
  const [edited, setEdited] = useState<ReadonlySet<string>>(() => new Set())

  const set = useCallback(<K extends keyof V & string>(name: K, value: V[K]) => {
    setValues((v) => ({ ...v, [name]: value }))
    setClientErrors((e) => {
      if (!(name in e)) return e
      const rest = { ...e }
      delete rest[name]
      return rest
    })
    setEdited((s) => (s.has(name) ? s : new Set(s).add(name)))
  }, [])

  /** Принимает результат проверки перед отправкой; true, если ошибок нет. */
  const validate = useCallback((found: FieldErrors) => {
    setClientErrors(found)
    setEdited(new Set())
    setAttempts((n) => n + 1)
    return Object.keys(found).length === 0
  }, [])

  const errors = useMemo<FieldErrors>(() => {
    const fromServer = Object.fromEntries(Object.entries(fieldErrorsOf(serverError)).filter(([name]) => !edited.has(name)))
    return { ...fromServer, ...clientErrors }
  }, [serverError, clientErrors, edited])

  useEffect(() => {
    if (attempts === 0) return
    form.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, [form, attempts, serverError])

  return { values, set, errors, validate }
}

import { useMutation } from '@tanstack/react-query'
import { useCallback, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { PasswordField } from '../../ui/PasswordField'
import { reauth } from './api'
import { describeError, fieldErrorsOf } from './errors'
import { ReauthContext, type ReauthGuard } from './reauth-context'
import { checkPasswordPresent } from './validation'

type Waiting = { resolve: () => void; reject: () => void }

/** Окно «Подтвердите, что это вы» для всего сайта: одно на страницу, его открывает useReauth. */
export function ReauthProvider({ children }: { children: ReactNode }) {
  const [waiting, setWaiting] = useState<Waiting | null>(null)

  const guard = useCallback<ReauthGuard>(async (action) => {
    try {
      return await action()
    } catch (err) {
      if (!(err instanceof ApiError && err.code === 'reauth_required')) throw err
      // Ждём пароль; закрыли окно — действие кончается той же ошибкой сервера.
      await new Promise<void>((resolve, reject) => setWaiting({ resolve, reject: () => reject(err) }))
      return action()
    }
  }, [])

  return (
    <ReauthContext value={guard}>
      {children}
      {waiting && (
        <ReauthModal
          onDone={() => {
            waiting.resolve()
            setWaiting(null)
          }}
          onCancel={() => {
            waiting.reject()
            setWaiting(null)
          }}
        />
      )}
    </ReauthContext>
  )
}

function ReauthModal({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [password, setPassword] = useState('')
  const [localError, setLocalError] = useState<string>()
  const inputRef = useRef<HTMLInputElement>(null)
  const check = useMutation({
    mutationFn: (p: string) => reauth(p),
    onSuccess: onDone,
    onError: () => inputRef.current?.focus(),
  })
  const fieldError = localError ?? fieldErrorsOf(check.error).password
  const formError = check.error && !fieldErrorsOf(check.error).password ? describeError(check.error) : undefined

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    const missing = checkPasswordPresent(password)
    setLocalError(missing)
    if (missing) {
      inputRef.current?.focus()
      return
    }
    check.mutate(password)
  }

  return (
    <Modal
      open
      onClose={onCancel}
      title={t.auth.reauth.title}
      actions={
        <>
          <Button variant="secondary" onClick={onCancel}>
            {t.common.cancel}
          </Button>
          <Button type="submit" form="reauth-form" loading={check.isPending}>
            {t.auth.reauth.submit}
          </Button>
        </>
      }
    >
      <form id="reauth-form" className="auth-form" onSubmit={onSubmit} noValidate>
        <p className="auth-note">{t.auth.reauth.text}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <PasswordField
          ref={inputRef}
          label={t.auth.fields.password}
          name="current-password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => {
            setPassword(e.target.value)
            setLocalError(undefined)
          }}
          error={fieldError}
          required
        />
      </form>
    </Modal>
  )
}

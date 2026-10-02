import { useMutation } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { PasswordField } from '../../ui/PasswordField'
import { resetPassword } from './api'
import { AuthPage } from './AuthPage'
import { describeError, errorCode, fieldErrorsOf } from './errors'
import { useForm } from './useForm'
import { checkNewPassword, compact } from './validation'

export function ResetPasswordPage() {
  const token = useSearchParams()[0].get('token') ?? ''
  const navigate = useNavigate()
  const formRef = useRef<HTMLFormElement>(null)
  const save = useMutation({
    mutationFn: (password: string) => resetPassword(token, password),
    onSuccess: () => void navigate('/login', { replace: true, state: { notice: 'reset-done' } }),
  })
  const { values, set, errors, validate } = useForm({ password: '' }, save.error, formRef)

  const requestNew = <ButtonLink to="/forgot-password">{t.auth.reset.requestNew}</ButtonLink>

  if (!token) {
    return (
      <AuthPage title={t.auth.reset.missingTitle} lead={t.auth.reset.missingText}>
        <div className="auth-status">{requestNew}</div>
      </AuthPage>
    )
  }
  if (errorCode(save.error) === 'invalid_token') {
    return (
      <AuthPage title={t.auth.reset.invalidTitle} lead={t.auth.reset.invalidText}>
        <div className="auth-status">{requestNew}</div>
      </AuthPage>
    )
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ password: checkNewPassword(values.password) }))) save.mutate(values.password)
  }
  const formError = save.error && Object.keys(fieldErrorsOf(save.error)).length === 0 ? describeError(save.error) : undefined

  return (
    <AuthPage title={t.auth.reset.title} lead={t.auth.reset.lead}>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <PasswordField
          label={t.auth.fields.newPassword}
          hint={t.auth.passwordHint}
          name="password"
          autoComplete="new-password"
          value={values.password}
          onChange={(e) => set('password', e.target.value)}
          error={errors.password}
          required
        />
        <Button type="submit" loading={save.isPending}>
          {t.auth.reset.submit}
        </Button>
      </form>
    </AuthPage>
  )
}

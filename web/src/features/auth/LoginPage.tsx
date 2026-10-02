import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, type FormEvent } from 'react'
import { Link, Navigate, useLocation, useNavigate, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { PasswordField } from '../../ui/PasswordField'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { login, resendConfirmation, setMe, useMe } from './api'
import { AuthPage } from './AuthPage'
import { describeError, errorCode, fieldErrorsOf } from './errors'
import { SocialSoon } from './SocialSoon'
import { useForm } from './useForm'
import { checkEmail, checkPasswordPresent, compact, safeNext } from './validation'

/** Сообщение, которое другая страница просит показать над формой входа (через состояние перехода). */
export type LoginNotice = 'reset-done' | 'need-login'

export function LoginPage() {
  const next = safeNext(useSearchParams()[0].get('next'))
  const notice = (useLocation().state as { notice?: LoginNotice } | null)?.notice
  const navigate = useNavigate()
  const client = useQueryClient()
  const toast = useToast()
  const { user } = useMe()
  const formRef = useRef<HTMLFormElement>(null)

  const signIn = useMutation({
    mutationFn: (v: { email: string; password: string }) => login(v.email.trim(), v.password),
    onSuccess: async (u) => {
      await setMe(client, u)
      void navigate(next, { replace: true })
    },
  })
  const resend = useMutation({
    mutationFn: (email: string) => resendConfirmation(email.trim()),
    onSuccess: () => toast.show({ kind: 'success', title: t.auth.login.resent }),
    onError: (err) => toast.show({ kind: 'error', title: describeError(err) }),
  })
  const { values, set, errors, validate } = useForm({ email: '', password: '' }, signIn.error, formRef)

  // Уже вошли: незачем показывать форму входа
  if (user) return <Navigate to={next} replace />

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ email: checkEmail(values.email), password: checkPasswordPresent(values.password) }))) signIn.mutate(values)
  }

  const formError = signIn.error && Object.keys(fieldErrorsOf(signIn.error)).length === 0 ? describeError(signIn.error) : undefined

  return (
    <AuthPage title={t.auth.login.title} lead={t.auth.login.lead}>
      {notice === 'reset-done' && <Alert kind="success">{t.auth.login.resetDone}</Alert>}
      {notice === 'need-login' && <Alert kind="info">{t.auth.login.needLogin}</Alert>}
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {formError && (
          <Alert
            kind="error"
            action={
              errorCode(signIn.error) === 'email_not_confirmed' ? (
                <Button variant="secondary" size="sm" loading={resend.isPending} onClick={() => resend.mutate(values.email)}>
                  {t.auth.login.resend}
                </Button>
              ) : undefined
            }
          >
            {formError}
          </Alert>
        )}
        <TextField
          label={t.auth.fields.email}
          type="email"
          name="email"
          autoComplete="username"
          inputMode="email"
          autoCapitalize="none"
          spellCheck={false}
          value={values.email}
          onChange={(e) => set('email', e.target.value)}
          error={errors.email}
          required
        />
        <PasswordField
          label={t.auth.fields.password}
          name="password"
          autoComplete="current-password"
          value={values.password}
          onChange={(e) => set('password', e.target.value)}
          error={errors.password}
          required
        />
        <Button type="submit" loading={signIn.isPending}>
          {t.auth.login.submit}
        </Button>
      </form>
      <div className="auth-links">
        <Link to="/forgot-password">{t.auth.login.forgot}</Link>
        <span className="auth-alt">
          {t.auth.login.noAccount} <Link to="/register">{t.auth.login.toRegister}</Link>
        </span>
      </div>
      <SocialSoon />
    </AuthPage>
  )
}

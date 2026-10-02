import { useMutation } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Link, Navigate } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Checkbox } from '../../ui/Checkbox'
import { PasswordField } from '../../ui/PasswordField'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { register, resendConfirmation, useMe, type RegisterInput } from './api'
import { AuthPage } from './AuthPage'
import { describeError, fieldErrorsOf } from './errors'
import { SocialSoon } from './SocialSoon'
import { useForm } from './useForm'
import { checkEmail, checkName, checkNewPassword, compact } from './validation'

type Values = { name: string; email: string; password: string; consent: boolean }

export function RegisterPage() {
  const toast = useToast()
  const { user } = useMe()
  const formRef = useRef<HTMLFormElement>(null)
  const [sentTo, setSentTo] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: (v: RegisterInput) => register({ ...v, name: v.name.trim(), email: v.email.trim() }),
    onSuccess: (res) => setSentTo(res.email),
  })
  const resend = useMutation({
    mutationFn: (address: string) => resendConfirmation(address),
    onSuccess: () => toast.show({ kind: 'success', title: t.auth.login.resent }),
    onError: (err) => toast.show({ kind: 'error', title: describeError(err) }),
  })
  const { values, set, errors, validate } = useForm<Values>({ name: '', email: '', password: '', consent: false }, create.error, formRef)

  if (user) return <Navigate to="/" replace />

  if (sentTo) {
    return (
      <AuthPage title={t.auth.register.sentTitle} lead={t.auth.register.sentText(sentTo)}>
        <div className="auth-status">
          <p className="auth-note">{t.auth.register.sentHint}</p>
          <Button variant="secondary" loading={resend.isPending} onClick={() => resend.mutate(sentTo)}>
            {t.auth.register.resend}
          </Button>
          <Button variant="quiet" onClick={() => setSentTo(null)}>
            {t.auth.register.otherAddress}
          </Button>
        </div>
      </AuthPage>
    )
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({
      name: checkName(values.name),
      email: checkEmail(values.email),
      password: checkNewPassword(values.password),
      consent: values.consent ? undefined : t.auth.errors.consentRequired,
    })
    if (validate(found)) create.mutate(values)
  }

  const formError = create.error && Object.keys(fieldErrorsOf(create.error)).length === 0 ? describeError(create.error) : undefined

  return (
    <AuthPage title={t.auth.register.title} lead={t.auth.register.lead}>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextField
          label={t.auth.fields.name}
          name="name"
          autoComplete="name"
          value={values.name}
          onChange={(e) => set('name', e.target.value)}
          error={errors.name}
          required
        />
        <TextField
          label={t.auth.fields.email}
          type="email"
          name="email"
          autoComplete="email"
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
          hint={t.auth.passwordHint}
          name="password"
          autoComplete="new-password"
          value={values.password}
          onChange={(e) => set('password', e.target.value)}
          error={errors.password}
          required
        />
        <Checkbox
          name="consent"
          checked={values.consent}
          onChange={(e) => set('consent', e.target.checked)}
          error={errors.consent}
          label={
            <>
              {t.auth.register.consentLead}{' '}
              <Link to="/privacy" target="_blank" rel="noopener">
                {t.auth.register.consentLink}
              </Link>
              .
            </>
          }
        />
        <Button type="submit" loading={create.isPending}>
          {t.auth.register.submit}
        </Button>
      </form>
      <p className="auth-alt">
        {t.auth.register.haveAccount} <Link to="/login">{t.auth.register.toLogin}</Link>
      </p>
      <SocialSoon />
    </AuthPage>
  )
}

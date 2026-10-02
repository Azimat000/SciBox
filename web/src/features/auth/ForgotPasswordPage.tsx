import { useMutation } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { TextField } from '../../ui/TextField'
import { forgotPassword } from './api'
import { AuthPage } from './AuthPage'
import { describeError } from './errors'
import { useForm } from './useForm'
import { checkEmail, compact } from './validation'

export function ForgotPasswordPage() {
  const formRef = useRef<HTMLFormElement>(null)
  const [sentTo, setSentTo] = useState<string | null>(null)
  const send = useMutation({
    mutationFn: (email: string) => forgotPassword(email),
    onSuccess: (_, email) => setSentTo(email),
  })
  const { values, set, errors, validate } = useForm({ email: '' }, send.error, formRef)

  if (sentTo) {
    return (
      <AuthPage title={t.auth.forgot.sentTitle} lead={t.auth.forgot.sentText(sentTo)}>
        <div className="auth-status">
          <p className="auth-note">{t.auth.forgot.sentHint}</p>
          <ButtonLink to="/login" variant="secondary">
            {t.auth.forgot.back}
          </ButtonLink>
          <Button variant="quiet" onClick={() => setSentTo(null)}>
            {t.auth.forgot.again}
          </Button>
        </div>
      </AuthPage>
    )
  }

  const onSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (validate(compact({ email: checkEmail(values.email) }))) send.mutate(values.email.trim())
  }

  return (
    <AuthPage title={t.auth.forgot.title} lead={t.auth.forgot.lead}>
      <form className="auth-form" onSubmit={onSubmit} ref={formRef} noValidate>
        {send.error && <Alert kind="error">{describeError(send.error)}</Alert>}
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
        <Button type="submit" loading={send.isPending}>
          {t.auth.forgot.submit}
        </Button>
      </form>
      <p className="auth-alt">
        <Link to="/login">{t.auth.forgot.back}</Link>
      </p>
    </AuthPage>
  )
}

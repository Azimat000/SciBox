import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Select } from '../../ui/Select'
import { TextArea, TextField } from '../../ui/TextField'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import type { OrgFields } from './api'
import { kindOptions } from './labels'

type Props = {
  initial: OrgFields
  submitLabel: string
  pending: boolean
  /** Ошибка последней отправки: поля подсвечиваются, общая ошибка показывается над формой. */
  error: unknown
  onSubmit: (fields: OrgFields) => void
}

/** Форма данных организации: и для создания, и для правки. */
export function OrgForm({ initial, submitLabel, pending, error, onSubmit }: Props) {
  const formRef = useRef<HTMLFormElement>(null)
  const { values, set, errors, validate } = useForm(initial, error, formRef)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({
      name: values.name.trim() === '' ? t.orgs.errors.nameRequired : undefined,
      kind: values.kind === '' ? t.orgs.errors.kindRequired : undefined,
      city: values.city.trim() === '' ? t.orgs.errors.cityRequired : undefined,
    })
    if (validate(found)) onSubmit(values)
  }
  const formError = error && Object.keys(fieldErrorsOf(error)).length === 0 ? describeError(error) : undefined

  return (
    <form className="manage-form" onSubmit={submit} ref={formRef} noValidate>
      {formError && <Alert kind="error">{formError}</Alert>}
      <TextField
        label={t.orgs.fields.name}
        hint={t.orgs.fields.nameHint}
        name="name"
        autoComplete="organization"
        value={values.name}
        onChange={(e) => set('name', e.target.value)}
        error={errors.name}
        required
      />
      <Select
        label={t.orgs.fields.kind}
        name="kind"
        placeholder={t.orgs.fields.kindPlaceholder}
        options={kindOptions}
        value={values.kind}
        onChange={(e) => set('kind', e.target.value)}
        error={errors.kind}
        required
      />
      <TextField
        label={t.orgs.fields.city}
        name="city"
        autoComplete="address-level2"
        value={values.city}
        onChange={(e) => set('city', e.target.value)}
        error={errors.city}
        required
      />
      <TextField
        label={t.orgs.fields.website}
        hint={t.orgs.fields.websiteHint}
        name="website"
        type="url"
        inputMode="url"
        autoCapitalize="none"
        spellCheck={false}
        value={values.website}
        onChange={(e) => set('website', e.target.value)}
        error={errors.website}
        optional
      />
      <TextArea
        label={t.orgs.fields.description}
        hint={t.orgs.fields.descriptionHint}
        name="description"
        rows={7}
        value={values.description}
        onChange={(e) => set('description', e.target.value)}
        error={errors.description}
        optional
      />
      <Button type="submit" loading={pending}>
        {submitLabel}
      </Button>
    </form>
  )
}

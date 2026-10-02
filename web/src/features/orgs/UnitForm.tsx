import { useRef, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Select, type Option } from '../../ui/Select'
import { TextArea, TextField } from '../../ui/TextField'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import type { UnitFields } from './api'
import { unitKindOptions } from './labels'

export type UnitFormValues = { fields: UnitFields; head: string }

type Props = {
  initial: { name: string; kind: string; description: string; topics: string[]; head: string }
  submitLabel: string
  pending: boolean
  error: unknown
  /** Кого можно назначить руководителем; без списка поля нет (назначает только владелец). */
  headOptions?: readonly Option[]
  /** Имя текущего руководителя для того, кто назначать не может. */
  headName?: string | null
  onSubmit: (v: UnitFormValues) => void
}

/** Форма подразделения: и для добавления, и для правки. Темы вводятся по одной в строке. */
export function UnitForm({ initial, submitLabel, pending, error, headOptions, headName, onSubmit }: Props) {
  const formRef = useRef<HTMLFormElement>(null)
  const { values, set, errors, validate } = useForm({ ...initial, topics: initial.topics.join('\n') }, error, formRef)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({
      name: values.name.trim() === '' ? t.orgs.errors.nameRequired : undefined,
      kind: values.kind === '' ? t.orgs.errors.unitKindRequired : undefined,
    })
    if (!validate(found)) return
    onSubmit({
      fields: { name: values.name, kind: values.kind, description: values.description, topics: values.topics.split('\n') },
      head: values.head,
    })
  }
  const formError = error && Object.keys(fieldErrorsOf(error)).length === 0 ? describeError(error) : undefined

  return (
    <form className="manage-form" onSubmit={submit} ref={formRef} noValidate>
      {formError && <Alert kind="error">{formError}</Alert>}
      <TextField label={t.orgs.units.name} name="name" value={values.name} onChange={(e) => set('name', e.target.value)} error={errors.name} required />
      <Select
        label={t.orgs.units.kind}
        name="kind"
        placeholder={t.orgs.units.kindPlaceholder}
        options={unitKindOptions}
        value={values.kind}
        onChange={(e) => set('kind', e.target.value)}
        error={errors.kind}
        required
      />
      <TextArea
        label={t.orgs.units.description}
        hint={t.orgs.units.descriptionHint}
        name="description"
        rows={5}
        value={values.description}
        onChange={(e) => set('description', e.target.value)}
        error={errors.description}
        optional
      />
      <TextArea
        label={t.orgs.units.topics}
        hint={t.orgs.units.topicsHint}
        name="topics"
        rows={5}
        value={values.topics}
        onChange={(e) => set('topics', e.target.value)}
        error={errors.topics}
        optional
      />
      {headOptions ? (
        <Select
          label={t.orgs.units.head}
          hint={t.orgs.units.headHint}
          name="head"
          placeholder={t.orgs.units.headNone}
          options={headOptions}
          value={values.head}
          onChange={(e) => set('head', e.target.value)}
          error={errors.head ?? errors.user_id}
          optional
        />
      ) : (
        <p className="manage-note">
          {headName ? `${t.orgs.page.head}: ${headName}. ` : ''}
          {t.orgs.units.headOnlyOwner}
        </p>
      )}
      <Button type="submit" loading={pending}>
        {submitLabel}
      </Button>
    </form>
  )
}

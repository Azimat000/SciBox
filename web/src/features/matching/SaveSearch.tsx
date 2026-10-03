import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useId, useRef, useState, type FormEvent } from 'react'
import { useLocation } from 'react-router'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { BellIcon } from '../../ui/icons'
import { Modal } from '../../ui/Modal'
import { Select } from '../../ui/Select'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { useMe } from '../auth/api'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { filterCount, toParams, type Search } from '../search/params'
import type { Reference } from '../vacancies/api'
import { useRole } from '../shell/useRole'
import { createSearch, DEFAULT_FREQUENCY, type Frequency } from './api'
import { conditionsOfSearch, frequencyHint, frequencyLabel, frequencyOptions, MAX_NAME, suggestName } from './labels'
import './matching.css'

/**
 * «Сохранить поиск» в строке результатов. Только ищущему и только если что-то задано (слова или фильтры): поиск «всех
 * вакансий» сохранять незачем. Не вошедшего ведёт на вход и обратно.
 */
export function SaveSearchButton({ search, reference }: { search: Search; reference: Reference | undefined }) {
  const { role } = useRole()
  const { user } = useMe()
  const location = useLocation()
  const [open, setOpen] = useState(false)
  const s = t.matching.save

  if (role === 'employer' || user === undefined) return null
  if (!search.q && filterCount(search) === 0) return null
  if (user === null) {
    const next = encodeURIComponent(location.pathname + location.search)
    return (
      <ButtonLink to={`/login?next=${next}`} state={{ notice: 'need-login' }} variant="quiet" size="sm" className="save-search">
        <BellIcon size={18} />
        {s.signIn}
      </ButtonLink>
    )
  }
  return (
    <>
      <Button variant="quiet" size="sm" className="save-search" onClick={() => setOpen(true)}>
        <BellIcon size={18} />
        {s.button}
      </Button>
      {open && <SaveSearchModal search={search} reference={reference} onClose={() => setOpen(false)} />}
    </>
  )
}

function SaveSearchModal({ search, reference, onClose }: { search: Search; reference: Reference | undefined; onClose: () => void }) {
  const s = t.matching.save
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const formId = useId()
  const conditions = conditionsOfSearch(search, reference)

  const save = useMutation({
    mutationFn: (v: { name: string; frequency: Frequency }) => createSearch({ ...v, query: toParams({ ...search, page: 1 }).toString() }),
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: ['matching', 'searches'] })
      toast.show({ kind: 'success', title: s.done, text: s.doneText(frequencyLabel(saved.frequency)) })
      onClose()
    },
  })
  const { values, set, errors, validate } = useForm({ name: suggestName(search, reference), frequency: DEFAULT_FREQUENCY as string }, save.error, formRef)
  // Сервер может отвергнуть и сами условия (поле query): отдельного поля для них нет, показываем общей ошибкой.
  const fieldErrors = fieldErrorsOf(save.error)
  const formError = save.error ? (fieldErrors.query ?? (Object.keys(fieldErrors).length === 0 ? describeError(save.error) : undefined)) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const name = values.name.trim()
    const found = { name: !name ? s.nameRequired : name.length > MAX_NAME ? s.nameLong : undefined }
    if (!validate(compact(found))) return
    save.mutate({ name, frequency: values.frequency as Frequency })
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={s.title}
      actions={
        <>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" form={formId} loading={save.isPending}>
            {s.submit}
          </Button>
        </>
      }
    >
      <form id={formId} className="save-form" onSubmit={submit} ref={formRef} noValidate>
        <p className="shelf-muted">{s.lead}</p>
        {formError && <Alert kind="error">{formError}</Alert>}
        <div className="save-conditions">
          <p className="save-conditions-label">{s.conditions}</p>
          <ul>
            {conditions.map((c) => (
              <li key={c}>
                <Tag>{c}</Tag>
              </li>
            ))}
          </ul>
        </div>
        <TextField
          label={s.name}
          name="name"
          hint={s.nameHint}
          value={values.name}
          onChange={(ev) => set('name', ev.target.value)}
          error={errors.name}
          required
        />
        <Select
          label={s.frequency}
          name="frequency"
          options={frequencyOptions}
          hint={frequencyHint(values.frequency)}
          value={values.frequency}
          onChange={(ev) => set('frequency', ev.target.value)}
          error={errors.frequency}
        />
      </form>
    </Modal>
  )
}

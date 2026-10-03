import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { Select } from '../../ui/Select'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { ApiError } from '../../api/client'
import { addItem, lookupDoi, refreshProfile, updateItem, type Item, type ItemKind, type Work } from './api'
import { bodyOf, sectionDefs, valuesOf } from './sections'
import type { FieldDef } from './sections'

type Props = { kind: ItemKind; item: Item | null; onClose: () => void }

/** Окно добавления или правки записи раздела. Поля описаны в sections.ts, окно у всех видов записей одно. */
export function ItemModal({ kind, item, onClose }: Props) {
  const def = sectionDefs[kind]
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const [source, setSource] = useState<string>(item?.source ?? 'manual')
  const save = useMutation({
    mutationFn: (fields: Record<string, unknown>) => (item ? updateItem(item.id, kind, fields) : addItem(kind, fields)),
    onSuccess: async () => {
      await refreshProfile(client)
      toast.show({ kind: 'success', title: item ? t.profile.item.saved : t.profile.item.added })
      onClose()
    },
  })
  const { values, set, errors, validate } = useForm<Record<string, string>>(valuesOf(def, item), save.error, formRef)
  const formError = save.error && Object.keys(fieldErrorsOf(save.error)).length === 0 ? describeError(save.error) : undefined

  const submit = (e: FormEvent) => {
    e.preventDefault()
    validate({})
    const body = bodyOf(def, values)
    if (kind === 'publication') body.source = source
    save.mutate(body)
  }

  function fill(work: Work) {
    set('title', work.title)
    set('authors', work.authors)
    set('venue', work.venue)
    set('year', work.year ? String(work.year) : '')
    set('volume', work.volume)
    set('issue', work.issue)
    set('pages', work.pages)
    set('doi', work.doi)
    if (work.type) set('pub_type', work.type)
    setSource('crossref')
  }

  const title = item ? t.profile.item.editTitle(def.heading) : t.profile.item.addTitle(def.heading)
  const fields = kind === 'publication' ? def.fields.filter((f) => f.name !== 'doi') : def.fields

  return (
    <Modal
      open
      onClose={onClose}
      title={title}
      actions={
        <>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" form="profile-item-form" loading={save.isPending}>
            {t.profile.item.save}
          </Button>
        </>
      }
    >
      <form id="profile-item-form" className="profile-form" ref={formRef} onSubmit={submit} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        {kind === 'publication' && <DoiLookup value={values.doi} onChange={(v) => set('doi', v)} error={errors.doi} onFound={fill} />}
        <div className="profile-fields">
          {fields.map((field) => (
            <ItemField key={field.name} field={field} value={values[field.name]} error={errors[field.name]} onChange={(v) => set(field.name, v)} />
          ))}
        </div>
      </form>
    </Modal>
  )
}

function ItemField({ field, value, error, onChange }: { field: FieldDef; value: string; error?: string; onChange: (v: string) => void }) {
  const common = { label: field.label, hint: field.hint, error, required: field.required, optional: !field.required, name: field.name }
  const wide = field.type === 'area' || field.name === 'title' || field.name === 'authors' || field.name === 'venue'
  const cls = wide ? 'profile-field-wide' : undefined
  switch (field.type) {
    case 'select':
      return <Select {...common} className={cls} options={field.options ?? []} placeholder={t.profile.fields.select} value={value} onChange={(e) => onChange(e.target.value)} />
    case 'area':
      return <TextArea {...common} className={cls} rows={4} value={value} onChange={(e) => onChange(e.target.value)} />
    case 'year':
      return <TextField {...common} type="number" inputMode="numeric" min={1900} max={2100} step={1} value={value} onChange={(e) => onChange(e.target.value)} />
    case 'url':
      return <TextField {...common} className="profile-field-wide" type="url" inputMode="url" value={value} onChange={(e) => onChange(e.target.value)} />
    default:
      return <TextField {...common} className={cls} value={value} onChange={(e) => onChange(e.target.value)} />
  }
}

/** Поле DOI с кнопкой «Найти»: подставляет поля из Crossref. Если Crossref молчит, запись остаётся ручной. */
function DoiLookup({ value, onChange, error, onFound }: { value: string; onChange: (v: string) => void; error?: string; onFound: (w: Work) => void }) {
  const [problem, setProblem] = useState<string | null>(null)
  const [found, setFound] = useState(false)
  const find = useMutation({
    mutationFn: () => lookupDoi(value.trim()),
    onMutate: () => {
      setProblem(null)
      setFound(false)
    },
    onSuccess: (work) => {
      onFound(work)
      setFound(true)
    },
    onError: (err) => setProblem(err instanceof ApiError ? (err.fields.doi ?? err.message) : describeError(err)),
  })
  const run = () => {
    if (value.trim() === '') {
      setProblem(t.profile.doi.empty)
      return
    }
    find.mutate()
  }
  return (
    <div className="profile-doi">
      <div className="profile-doi-row">
        <TextField
          label={t.profile.doi.title}
          hint={t.profile.doi.hint}
          name="doi"
          optional
          value={value}
          error={problem ?? error}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              run()
            }
          }}
        />
        <Button variant="secondary" loading={find.isPending} onClick={run}>
          {t.profile.doi.button}
        </Button>
      </div>
      {found && <Alert kind="success">{t.profile.doi.found}</Alert>}
    </div>
  )
}

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useId, useRef, useState, type FormEvent } from 'react'
import { Link, Navigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Modal } from '../../ui/Modal'
import { Select } from '../../ui/Select'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { dateText } from '../applications/labels'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { compact } from '../auth/validation'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { isNotFound } from '../orgs/api'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { longName } from '../orgs/labels'
import { useReference } from '../vacancies/api'
import { deleteSearch, updateSearch, useSavedSearch, useSavedSearches, type Frequency, type SavedSearch } from './api'
import { conditionsOfQuery, frequencyHint, frequencyOptions, MAX_NAME } from './labels'
import { ShelfTabs } from './ShelfTabs'
import '../../ui/ResultsHeader.css'
import './matching.css'

/** «Поиски»: сохранённые поиски человека. Частоту сообщений можно сменить прямо в списке. */
export function SavedSearchesPage() {
  return <RequireUser>{() => <Searches />}</RequireUser>
}

function Searches() {
  const m = t.matching.searches
  const query = useSavedSearches()
  const reference = useReference()

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={m.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const items = query.data
  return (
    <div className="page shelf-page">
      <h1>{m.title}</h1>
      <p className="lead">{m.lead}</p>
      <ShelfTabs current="searches" />
      {items.length === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.emptyTitle}
          text={m.emptyText}
          action={<ButtonLink to="/vacancies">{m.emptyAction}</ButtonLink>}
        />
      ) : (
        <>
          <p className="results-header num">{m.count(items.length, plural(items.length, m.countForms))}</p>
          <ul className="saved-list">
            {items.map((s) => (
              <Entry key={s.id} search={s} conditions={conditionsOfQuery(s.query, reference.data)} />
            ))}
          </ul>
          <p className="shelf-muted">
            {m.mailNote} <Link to="/account">{m.mailLink}</Link>
          </p>
        </>
      )}
    </div>
  )
}

function Entry({ search, conditions }: { search: SavedSearch; conditions: string[] }) {
  const m = t.matching.searches
  const client = useQueryClient()
  const toast = useToast()
  const [renaming, setRenaming] = useState(false)
  const [removing, setRemoving] = useState(false)

  const refresh = () => client.invalidateQueries({ queryKey: ['matching', 'searches'] })
  const changeFrequency = useMutation({
    mutationFn: (frequency: Frequency) => updateSearch(search.id, { name: search.name, frequency }),
    onSuccess: async () => {
      await refresh()
      toast.show({ kind: 'success', title: m.frequencySaved })
    },
    onError: (err) => toast.show({ kind: 'error', title: m.saveFailed, text: describeError(err) }),
  })
  const remove = useMutation({
    mutationFn: () => deleteSearch(search.id),
    onSuccess: async () => {
      setRemoving(false)
      await refresh()
      toast.show({ kind: 'success', title: m.removed })
    },
    onError: (err) => {
      setRemoving(false)
      toast.show({ kind: 'error', title: m.removeFailed, text: describeError(err) })
    },
  })

  return (
    <li className="saved-entry">
      <h2 className="saved-title" data-long={longName(search.name)}>
        <Link to={`/saved-searches/${search.id}`}>{search.name}</Link>
      </h2>
      {conditions.length > 0 && (
        <ul className="saved-conditions" aria-label={m.conditions}>
          {conditions.map((c) => (
            <li key={c}>
              <Tag>{c}</Tag>
            </li>
          ))}
        </ul>
      )}
      <Select
        className="saved-frequency"
        label={m.frequency}
        options={frequencyOptions}
        value={search.frequency}
        hint={frequencyHint(search.frequency)}
        disabled={changeFrequency.isPending}
        onChange={(ev) => changeFrequency.mutate(ev.target.value as Frequency)}
      />
      <p className="saved-meta">
        <span>{m.createdAt(dateText(search.created_at))}</span>
        <span>{search.last_sent_at ? m.lastSent(dateText(search.last_sent_at)) : m.neverSent}</span>
      </p>
      <div className="saved-actions">
        <ButtonLink to={`/saved-searches/${search.id}`} variant="secondary" size="sm" aria-label={`${m.open}: ${search.name}`}>
          {m.open}
        </ButtonLink>
        <Button variant="quiet" size="sm" onClick={() => setRenaming(true)} aria-label={`${m.rename}: ${search.name}`}>
          {m.rename}
        </Button>
        <Button variant="quiet" size="sm" onClick={() => setRemoving(true)} aria-label={`${m.remove}: ${search.name}`}>
          {m.remove}
        </Button>
      </div>
      {renaming && <RenameModal search={search} onClose={() => setRenaming(false)} />}
      <ConfirmModal
        open={removing}
        title={m.removeTitle}
        text={m.removeText(search.name)}
        confirmLabel={m.removeSubmit}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
        onClose={() => setRemoving(false)}
      />
    </li>
  )
}

function RenameModal({ search, onClose }: { search: SavedSearch; onClose: () => void }) {
  const m = t.matching.searches
  const s = t.matching.save
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const formId = useId()
  const rename = useMutation({
    mutationFn: (name: string) => updateSearch(search.id, { name, frequency: search.frequency }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['matching', 'searches'] })
      toast.show({ kind: 'success', title: m.renamed })
      onClose()
    },
  })
  const { values, set, errors, validate } = useForm({ name: search.name }, rename.error, formRef)
  const fieldErrors = fieldErrorsOf(rename.error)
  const formError = rename.error && Object.keys(fieldErrors).length === 0 ? describeError(rename.error) : undefined

  const submit = (ev: FormEvent) => {
    ev.preventDefault()
    const name = values.name.trim()
    if (!validate(compact({ name: !name ? s.nameRequired : name.length > MAX_NAME ? s.nameLong : undefined }))) return
    rename.mutate(name)
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={m.renameTitle}
      actions={
        <>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button type="submit" form={formId} loading={rename.isPending}>
            {m.renameSubmit}
          </Button>
        </>
      }
    >
      <form id={formId} className="save-form" onSubmit={submit} ref={formRef} noValidate>
        {formError && <Alert kind="error">{formError}</Alert>}
        <TextField label={s.name} name="name" hint={s.nameHint} value={values.name} onChange={(ev) => set('name', ev.target.value)} error={errors.name} required />
      </form>
    </Modal>
  )
}

/** Адрес из уведомления: открывает поиск, который сохранил человек, новыми вакансиями вперёд. */
export function OpenSavedSearchPage() {
  return <RequireUser>{() => <Open />}</RequireUser>
}

function Open() {
  const m = t.matching.searches
  const { id = '' } = useParams()
  const query = useSavedSearch(id)

  if (query.isPending) return <PageSkeleton />
  if (query.isError) {
    return isNotFound(query.error) ? (
      <div className="page">
        <EmptyState
          headingLevel={2}
          title={m.notFoundTitle}
          text={m.notFoundText}
          action={<ButtonLink to="/saved-searches">{m.toList}</ButtonLink>}
        />
      </div>
    ) : (
      <LoadFailed title={m.loadOneError} error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  const params = new URLSearchParams(query.data.query)
  if (!params.has('sort')) params.set('sort', 'new')
  return <Navigate to={`/vacancies?${params}`} replace />
}

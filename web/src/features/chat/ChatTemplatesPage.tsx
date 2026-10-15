import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { useLeaveGuard } from '../../ui/useLeaveGuard'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { useManageContext } from '../orgs/manageContext'
import { MAX_TEXT } from './api'
import { TEMPLATE_DEFAULT_LIMIT, TEMPLATE_TITLE_MAX, useTemplateActions, useTemplates, type Template } from './templates'
import { textLength } from './text'
import './chat-tools.css'

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' })

/** Вкладка «Шаблоны ответов» в разделе организации (срез 36): правка для владельца и кадровика, чтение для остальных. */
export function ChatTemplatesPage() {
  const p = t.chat.templatesPage
  const { view } = useManageContext()
  const orgId = view.organization.id
  const query = useTemplates(orgId)
  const actions = useTemplateActions(orgId)
  const toast = useToast()
  // 'new' — форма добавления, иначе номер шаблона, который правят
  const [editing, setEditing] = useState<string | null>(null)
  const [removing, setRemoving] = useState<Template | null>(null)
  // Форма закрылась: фокус возвращается на кнопку, с которой она открывалась
  const returnTo = useRef<string | null>(null)
  const finish = () => {
    returnTo.current = editing
    setEditing(null)
  }
  useEffect(() => {
    if (editing !== null || returnTo.current === null) return
    const selector = returnTo.current === 'new' ? '[data-tpl-add]' : `[data-tpl-edit="${CSS.escape(returnTo.current)}"]`
    document.querySelector<HTMLElement>(selector)?.focus()
    returnTo.current = null
  }, [editing])

  if (query.isPending) {
    return (
      <div role="status" aria-busy="true" aria-label={t.common.loading} className="tpl-skeleton">
        <Skeleton width="45%" height="1.25rem" />
        <Skeleton width="90%" height="3.5rem" />
        <Skeleton width="90%" height="3.5rem" />
      </div>
    )
  }
  if (query.isError) {
    const gone = query.error instanceof ApiError && (query.error.status === 403 || query.error.status === 404)
    return gone ? (
      <EmptyState headingLevel={2} title={p.noAccess.title} text={p.noAccess.text} />
    ) : (
      <EmptyState
        headingLevel={2}
        tone="error"
        title={p.loadError}
        text={describeError(query.error)}
        action={<Button onClick={() => void query.refetch()}>{t.common.retry}</Button>}
      />
    )
  }

  const { items, can_edit: canEdit } = query.data
  const limit = query.data.limit || TEMPLATE_DEFAULT_LIMIT
  const full = items.length >= limit

  return (
    <>
      <h2 className="visually-hidden">{p.title}</h2>
      <p className="manage-intro">{p.lead}</p>
      {!canEdit && <p className="manage-note">{p.readOnly}</p>}
      <div className="tpl-bar">
        <p className="tpl-count num">{p.count(items.length, limit)}</p>
        {canEdit && editing !== 'new' && (
          <Button disabled={full} data-tpl-add onClick={() => setEditing('new')}>
            {p.add}
          </Button>
        )}
      </div>
      {canEdit && full && <p className="manage-note">{p.limitReached(limit)}</p>}
      {editing === 'new' && (
        <TemplateForm
          submitLabel={p.create}
          pending={actions.create.isPending}
          error={actions.create.error}
          onCancel={() => {
            finish()
            actions.create.reset()
          }}
          onSubmit={(input) =>
            actions.create.mutate(input, {
              onSuccess: () => {
                finish()
                toast.show({ kind: 'success', title: p.created })
              },
            })
          }
        />
      )}
      {items.length === 0 && editing !== 'new' ? (
        <EmptyState
          headingLevel={2}
          title={canEdit ? p.empty.title : p.emptyReader.title}
          text={canEdit ? p.empty.text : p.emptyReader.text}
          action={canEdit ? <Button onClick={() => setEditing('new')}>{p.add}</Button> : undefined}
        />
      ) : (
        <ul className="tpl-list">
          {items.map((tpl) => (
            <li key={tpl.id} className="tpl-row">
              {editing === tpl.id ? (
                <TemplateForm
                  initial={tpl}
                  submitLabel={p.save}
                  pending={actions.update.isPending}
                  error={actions.update.error}
                  onCancel={() => {
                    finish()
                    actions.update.reset()
                  }}
                  onSubmit={(input) =>
                    actions.update.mutate(
                      { id: tpl.id, ...input },
                      {
                        onSuccess: () => {
                          finish()
                          toast.show({ kind: 'success', title: p.saved })
                        },
                      },
                    )
                  }
                />
              ) : (
                <>
                  <div className="tpl-main">
                    <h3 className="tpl-title">{tpl.title}</h3>
                    <p className="tpl-text">{tpl.text}</p>
                    <p className="tpl-date num">{p.updated(dateFormat.format(new Date(tpl.updated_at)))}</p>
                  </div>
                  {canEdit && (
                    <div className="tpl-actions">
                      <Button
                        size="sm"
                        variant="secondary"
                        data-tpl-edit={tpl.id}
                        aria-label={p.editLabel(tpl.title)}
                        onClick={() => {
                          actions.update.reset()
                          setEditing(tpl.id)
                        }}
                      >
                        {p.edit}
                      </Button>
                      <Button size="sm" variant="quiet" aria-label={p.removeLabel(tpl.title)} onClick={() => setRemoving(tpl)}>
                        {p.remove}
                      </Button>
                    </div>
                  )}
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmModal
        open={removing !== null}
        title={p.deleteTitle}
        text={removing ? p.deleteText(removing.title) : ''}
        confirmLabel={p.remove}
        pending={actions.remove.isPending}
        onConfirm={() =>
          removing &&
          actions.remove.mutate(removing.id, {
            onSuccess: () => toast.show({ kind: 'success', title: p.deleted }),
            onError: (err) => toast.show({ kind: 'error', title: p.deleteFailed, text: describeError(err) }),
            onSettled: () => setRemoving(null),
          })
        }
        onClose={() => setRemoving(null)}
      />
    </>
  )
}

type FormProps = {
  initial?: Template
  submitLabel: string
  pending: boolean
  error: unknown
  onSubmit: (input: { title: string; text: string }) => void
  onCancel: () => void
}

/** Форма шаблона на месте строки или над списком: название и текст, счётчик знаков, ошибки сервера по полям. */
function TemplateForm({ initial, submitLabel, pending, error, onSubmit, onCancel }: FormProps) {
  const p = t.chat.templatesPage
  const [title, setTitle] = useState(initial?.title ?? '')
  const [text, setText] = useState(initial?.text ?? '')
  const [local, setLocal] = useState<{ title?: string; text?: string }>({})
  const box = useRef<HTMLFormElement>(null)

  // Форма появилась: фокус в первое поле, чтобы можно было сразу печатать
  useEffect(() => {
    box.current?.querySelector<HTMLElement>('input')?.focus()
  }, [])

  const textLen = textLength(text.trim())
  const server = fieldErrorsOf(error)
  const generic = error && Object.keys(server).length === 0 ? describeError(error) : ''
  // Начатый, но не сохранённый шаблон не пропадает молча при уходе со страницы
  const dirty = title !== (initial?.title ?? '') || text !== (initial?.text ?? '')
  const { prompt } = useLeaveGuard(dirty && !pending)
  // Ошибка полей: фокус на первое поле с ошибкой
  const errorKey = JSON.stringify([local, server])
  useEffect(() => {
    if (errorKey !== '[{},{}]') box.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, [errorKey])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const errors: { title?: string; text?: string } = {}
    const name = title.trim()
    if (!name) errors.title = p.titleRequired
    else if ([...name].length > TEMPLATE_TITLE_MAX) errors.title = p.titleTooLong(TEMPLATE_TITLE_MAX)
    if (!text.trim()) errors.text = p.textRequired
    else if (textLen > MAX_TEXT) errors.text = t.chat.composer.tooLong(textLen - MAX_TEXT)
    setLocal(errors)
    if (Object.keys(errors).length === 0) onSubmit({ title: name, text: text.trim() })
  }

  return (
    <form className="tpl-form" ref={box} noValidate onSubmit={submit}>
      {generic && (
        <Alert kind="error" title={p.saveFailed}>
          {generic}
        </Alert>
      )}
      <TextField
        label={p.titleLabel}
        name="title"
        autoComplete="off"
        required
        maxLength={TEMPLATE_TITLE_MAX + 20}
        value={title}
        hint={p.titleHint}
        error={local.title ?? server.title}
        onChange={(e) => setTitle(e.target.value)}
      />
      <TextArea
        label={p.textLabel}
        name="text"
        autoComplete="off"
        required
        rows={6}
        value={text}
        hint={p.textHint}
        error={local.text ?? server.text}
        onChange={(e) => setText(e.target.value)}
      />
      <p className="tpl-counter num" data-over={textLen > MAX_TEXT || undefined}>
        {p.textCounter(textLen, MAX_TEXT)}
      </p>
      <div className="tpl-form-actions">
        <Button type="submit" loading={pending}>
          {submitLabel}
        </Button>
        <Button variant="quiet" onClick={onCancel} disabled={pending}>
          {p.cancel}
        </Button>
      </div>
      {prompt}
    </form>
  )
}

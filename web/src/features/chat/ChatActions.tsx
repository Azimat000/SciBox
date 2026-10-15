import { useMutation } from '@tanstack/react-query'
import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { Select } from '../../ui/Select'
import { TextArea } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { blockOrganization, complaintReasons, MAX_COMPLAINT_NOTE, sendComplaint, type ChatHeader, type ComplaintReason } from './api'
import { useArchiveToggle, useRefreshChats } from './useArchive'
import { textLength } from './text'
import './chat-tools.css'

/**
 * Действия шапки переписки. Если кроме архива есть что предложить (соискателю: пожаловаться, заблокировать), всё лежит
 * в одном «Ещё»: на телефоне шапка не растёт лишним рядом кнопок. Организации «В архив» остаётся кнопкой.
 */
export function ChatHeaderActions({ chat }: { chat: ChatHeader }) {
  const archive = useArchiveToggle()
  const a = t.chat.archive
  if (chat.viewer.can_complain || chat.viewer.can_block) return <MoreMenu chat={chat} />
  return (
    <Button size="sm" variant="quiet" loading={archive.isPending} onClick={() => archive.mutate({ id: chat.id, archived: !chat.archived })}>
      {chat.archived ? a.back : a.to}
    </Button>
  )
}

type Dialog = 'complain' | 'block' | null

/** «Ещё»: раскрывающийся список из двух действий. Escape и щелчок снаружи закрывают, фокус возвращается на кнопку. */
function MoreMenu({ chat }: { chat: ChatHeader }) {
  const s = t.chat.safety
  const a = t.chat.archive
  const archive = useArchiveToggle()
  const [open, setOpen] = useState(false)
  const [dialog, setDialog] = useState<Dialog>(null)
  const root = useRef<HTMLDivElement>(null)
  const listId = useId()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      setOpen(false)
      root.current?.querySelector<HTMLElement>('.chat-more-trigger')?.focus()
    }
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDown)
    }
  }, [open])

  // Окно закрылось: фокус возвращается на «Ещё» (кнопка, с которой оно открывалось, к этому времени уже убрана)
  const hadDialog = useRef(false)
  useEffect(() => {
    if (dialog === null && hadDialog.current) root.current?.querySelector<HTMLElement>('.chat-more-trigger')?.focus()
    hadDialog.current = dialog !== null
  }, [dialog])

  const choose = (d: Dialog) => {
    setOpen(false)
    setDialog(d)
  }

  return (
    <div className="chat-more" ref={root}>
      <Button className="chat-more-trigger" size="sm" variant="quiet" aria-expanded={open} aria-controls={open ? listId : undefined} aria-label={s.moreLabel} title={s.moreLabel} onClick={() => setOpen((v) => !v)}>
        {s.more}
      </Button>
      {open && (
        <ul className="chat-more-list" id={listId}>
          <li>
            <button
              type="button"
              onClick={() => {
                setOpen(false)
                archive.mutate({ id: chat.id, archived: !chat.archived })
              }}
            >
              {chat.archived ? a.back : a.to}
            </button>
          </li>
          {chat.viewer.can_complain && (
            <li>
              <button type="button" onClick={() => choose('complain')}>
                {s.complain}
              </button>
            </li>
          )}
          {chat.viewer.can_block && (
            <li>
              <button type="button" className="chat-more-danger" onClick={() => choose('block')}>
                {s.block}
              </button>
            </li>
          )}
        </ul>
      )}
      <BlockDialog open={dialog === 'block'} chat={chat} onClose={() => setDialog(null)} />
      <ComplaintDialog open={dialog === 'complain'} chat={chat} onClose={() => setDialog(null)} />
    </div>
  )
}

function BlockDialog({ open, chat, onClose }: { open: boolean; chat: ChatHeader; onClose: () => void }) {
  const s = t.chat.safety
  const refresh = useRefreshChats()
  const toast = useToast()
  const block = useMutation({
    mutationFn: () => blockOrganization(chat.id),
    onSuccess: () => {
      void refresh()
      toast.show({ kind: 'success', title: s.blockDone, text: s.blockDoneText })
      onClose()
    },
  })
  const close = () => {
    if (block.isPending) return
    block.reset()
    onClose()
  }
  return (
    <Modal
      open={open}
      onClose={close}
      title={s.blockTitle}
      actions={
        <>
          <Button variant="quiet" onClick={close}>
            {t.common.cancel}
          </Button>
          <Button variant="danger" loading={block.isPending} onClick={() => block.mutate()}>
            {s.blockConfirm}
          </Button>
        </>
      }
    >
      <div className="chat-dialog">
        <p>{s.blockLead(chat.vacancy.org_name)}</p>
        <ul className="chat-dialog-list">
          {s.blockItems.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
        {block.isError && (
          <Alert kind="error" title={s.blockFailed}>
            {describeError(block.error)}
          </Alert>
        )}
      </div>
    </Modal>
  )
}

function ComplaintDialog({ open, chat, onClose }: { open: boolean; chat: ChatHeader; onClose: () => void }) {
  const s = t.chat.safety
  const toast = useToast()
  const [reason, setReason] = useState<ComplaintReason | ''>('')
  const [note, setNote] = useState('')
  const [local, setLocal] = useState<{ reason?: string; note?: string }>({})
  const send = useMutation({
    mutationFn: () => sendComplaint(chat.id, reason as ComplaintReason, note.trim()),
    onSuccess: () => {
      toast.show({ kind: 'success', title: s.complainDone, text: s.complainDoneText })
      reset()
      onClose()
    },
  })
  const reset = () => {
    setReason('')
    setNote('')
    setLocal({})
    send.reset()
  }
  const close = () => {
    if (send.isPending) return
    reset()
    onClose()
  }

  const server = fieldErrorsOf(send.error)
  const generic = send.isError && Object.keys(server).length === 0 ? describeError(send.error) : ''
  const length = textLength(note.trim())

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const errors: { reason?: string; note?: string } = {}
    if (!reason) errors.reason = s.reasonRequired
    if (reason === 'other' && note.trim() === '') errors.note = s.noteRequired
    if (length > MAX_COMPLAINT_NOTE) errors.note = t.chat.composer.tooLong(length - MAX_COMPLAINT_NOTE)
    setLocal(errors)
    if (Object.keys(errors).length === 0) send.mutate()
  }
  const formId = `complaint-${chat.id}`
  // Ошибка полей: фокус на первое поле с ошибкой (окно рисуется в портале, поэтому ищем по номеру формы)
  const errorKey = JSON.stringify([local, server])
  useEffect(() => {
    if (errorKey === '[{},{}]') return
    document.getElementById(formId)?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, [errorKey, formId])

  return (
    <Modal
      open={open}
      onClose={close}
      title={s.complainTitle}
      actions={
        <>
          <Button variant="quiet" onClick={close}>
            {t.common.cancel}
          </Button>
          <Button type="submit" form={formId} loading={send.isPending}>
            {s.send}
          </Button>
        </>
      }
    >
      <form id={formId} className="chat-dialog" noValidate onSubmit={submit}>
        <p>{s.complainLead}</p>
        {generic && (
          <Alert kind="error" title={s.complainFailed}>
            {generic}
          </Alert>
        )}
        <Select
          label={s.reasonLabel}
          name="reason"
          required
          value={reason}
          placeholder={s.reasonPlaceholder}
          error={local.reason ?? server.reason}
          onChange={(e) => setReason(e.target.value as ComplaintReason | '')}
          options={complaintReasons.map((r) => ({ value: r, label: s.reasons[r] }))}
        />
        <TextArea
          label={s.noteLabel}
          name="note"
          autoComplete="off"
          rows={4}
          maxLength={MAX_COMPLAINT_NOTE + 200}
          required={reason === 'other'}
          optional={reason !== 'other'}
          value={note}
          hint={reason === 'other' ? s.noteHintOther : s.noteHint}
          error={local.note ?? server.note}
          onChange={(e) => setNote(e.target.value)}
        />
        {length >= MAX_COMPLAINT_NOTE - 200 && <p className="chat-dialog-count num">{s.noteCounter(length, MAX_COMPLAINT_NOTE)}</p>}
      </form>
    </Modal>
  )
}

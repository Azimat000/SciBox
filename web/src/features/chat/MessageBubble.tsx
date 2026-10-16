import { useEffect, useId, useRef, useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { MAX_TEXT, type ChatHeader, type Message } from './api'
import { canModify, delivery, messageTime, type PendingMessage } from './conversation'
import { authorName } from './labels'
import { MessageText } from './MessageText'
import { textLength } from './text'

type Actions = {
  onEdit: (seq: number, text: string) => Promise<void>
  onRemove: (seq: number) => Promise<void>
  onRetry: (pending: PendingMessage) => void
  onDiscard: (clientId: string) => void
}

function Check({ double }: { double: boolean }) {
  return (
    <svg className="chat-tick" data-double={double || undefined} viewBox="0 0 18 12" width="16" height="11" fill="none" aria-hidden="true" focusable="false">
      <path d="M1.5 6.5l3.5 3.5L11 2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      {double && <path d="M7 9.2l1 .8L16.5 2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />}
    </svg>
  )
}

/**
 * Сообщение глазами смотрящего: своя сторона справа (своё и коллег по организации), собеседник слева.
 * У чужого и у коллеги над первым в серии имя автора и организация.
 */
export function MessageBubble({ message, chat, now, fresh, actions }: { message: Message; chat: ChatHeader; now: number; fresh: boolean; actions: Actions }) {
  const m = t.chat.msg
  const mine = message.author?.is_me === true
  const name = message.author ? authorName(message.author) : ''
  const [editing, setEditing] = useState(false)
  const [menu, setMenu] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [removing, setRemoving] = useState(false)
  const toast = useToast()
  const modifiable = chat.can_write && canModify(message, now)
  const colleague = !mine && message.author !== null && message.author.side === chat.side
  // У организации подпись «имя, организация» (CHAT.md): сотрудник виден собеседнику вместе с местом работы.
  const orgLine = message.author?.side === 'org' ? chat.vacancy.org_name : ''

  const remove = async () => {
    setRemoving(true)
    try {
      await actions.onRemove(message.seq)
      toast.show({ kind: 'success', title: m.deleteDone })
      setConfirm(false)
    } catch (err) {
      setConfirm(false)
      toast.show({ kind: 'error', title: m.deleteFailed, text: describeError(err) })
    } finally {
      setRemoving(false)
    }
  }

  return (
    <div className="chat-msg" data-mine={mine || undefined} data-ours={colleague || undefined} data-deleted={message.deleted || undefined} data-fresh={fresh || undefined} data-seq={message.seq}>
      {(!mine || colleague) && name && (
        <p className="chat-msg-author">
          <span className="chat-msg-name">{name}</span>
          {orgLine && <span className="chat-msg-org">{orgLine}</span>}
        </p>
      )}
      <div className="chat-bubble">
        {message.deleted ? (
          <p className="chat-msg-text chat-msg-gone">{m.deleted}</p>
        ) : editing ? (
          <Editor
            initial={message.text}
            onCancel={() => setEditing(false)}
            onSave={async (text) => {
              await actions.onEdit(message.seq, text)
              setEditing(false)
            }}
          />
        ) : (
          <p className="chat-msg-text">
            <MessageText text={message.text} />
          </p>
        )}
        <div className="chat-msg-meta">
          {message.edited_at && !message.deleted && <span className="chat-msg-edited">{m.edited}</span>}
          <time className="num" dateTime={message.created_at}>
            {messageTime(message.created_at)}
          </time>
          {mine && !message.deleted && (
            <span className="chat-msg-state">
              <Check double={delivery(message, chat.read.theirs) === 'read'} />
              <span className="visually-hidden">{delivery(message, chat.read.theirs) === 'read' ? m.read : m.delivered}</span>
            </span>
          )}
          {modifiable && !editing && (
            <button type="button" className="chat-msg-menu" aria-expanded={menu} aria-label={m.actions} title={m.actions} onClick={() => setMenu((v) => !v)}>
              <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
                <circle cx="3" cy="8" r="1.4" fill="currentColor" />
                <circle cx="8" cy="8" r="1.4" fill="currentColor" />
                <circle cx="13" cy="8" r="1.4" fill="currentColor" />
              </svg>
            </button>
          )}
        </div>
      </div>
      {modifiable && menu && !editing && (
        <div className="chat-msg-actions">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setMenu(false)
              setEditing(true)
            }}
          >
            {m.edit}
          </Button>
          <Button
            size="sm"
            variant="quiet"
            onClick={() => {
              setMenu(false)
              setConfirm(true)
            }}
          >
            {m.remove}
          </Button>
        </div>
      )}
      <ConfirmModal open={confirm} title={m.deleteTitle} text={m.deleteText} confirmLabel={m.remove} pending={removing} onConfirm={() => void remove()} onClose={() => setConfirm(false)} />
    </div>
  )
}

/** Правка своего сообщения на месте: Enter сохраняет (Shift+Enter переносит), Escape отменяет. */
function Editor({ initial, onSave, onCancel }: { initial: string; onSave: (text: string) => Promise<void>; onCancel: () => void }) {
  const m = t.chat.msg
  const [value, setValue] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const ref = useRef<HTMLTextAreaElement>(null)
  const id = useId()
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [])
  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [value])

  const text = value.trim()
  const tooLong = textLength(text) > MAX_TEXT
  const save = async () => {
    if (busy || !text || tooLong) return
    if (text === initial.trim()) return onCancel()
    setBusy(true)
    setError('')
    try {
      await onSave(text)
    } catch (err) {
      setError(describeError(err))
      setBusy(false)
    }
  }

  return (
    <form
      className="chat-edit"
      onSubmit={(e) => {
        e.preventDefault()
        void save()
      }}
    >
      <label className="visually-hidden" htmlFor={id}>
        {m.editLabel}
      </label>
      <textarea
        id={id}
        ref={ref}
        className="control chat-edit-input"
        rows={1}
        value={value}
        disabled={busy}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          } else if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            void save()
          }
        }}
      />
      {tooLong && <p className="chat-edit-error">{t.chat.composer.tooLong(textLength(text) - MAX_TEXT)}</p>}
      {error && (
        <p className="chat-edit-error" role="alert">
          {m.editFailed}: {error}
        </p>
      )}
      <div className="chat-edit-actions">
        <Button type="submit" size="sm" loading={busy} disabled={!text || tooLong}>
          {m.save}
        </Button>
        <Button size="sm" variant="quiet" onClick={onCancel} disabled={busy}>
          {m.cancelEdit}
        </Button>
      </div>
    </form>
  )
}

/** Своё сообщение, которое сервер ещё не подтвердил: «Отправляется» или «Не отправлено» с повтором. */
export function PendingBubble({ pending, actions, fresh, canRetry }: { pending: PendingMessage; actions: Pick<Actions, 'onRetry' | 'onDiscard'>; fresh: boolean; canRetry: boolean }) {
  const m = t.chat.msg
  const failed = pending.status === 'failed'
  return (
    <div className="chat-msg" data-mine="true" data-pending={pending.status} data-fresh={fresh || undefined}>
      <div className="chat-bubble">
        <p className="chat-msg-text">
          <MessageText text={pending.text} />
        </p>
        <div className="chat-msg-meta">
          <span className="chat-msg-sending" role={failed ? undefined : 'status'}>
            {failed ? m.failed : m.sending}
          </span>
        </div>
      </div>
      {failed && (
        <div className="chat-msg-actions" role="group" aria-label={m.failed}>
          {pending.error && <p className="chat-msg-error">{pending.error}</p>}
          {canRetry && (
            <Button size="sm" onClick={() => actions.onRetry(pending)}>
              {m.retry}
            </Button>
          )}
          <Button size="sm" variant="quiet" onClick={() => actions.onDiscard(pending.client_id)}>
            {m.discard}
          </Button>
        </div>
      )}
    </div>
  )
}

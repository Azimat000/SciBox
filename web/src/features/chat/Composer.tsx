import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { useMe } from '../auth/api'
import { MAX_TEXT, sendTyping, type ClosedReason } from './api'
import { loadDraft, saveDraft } from './drafts'
import { textLength } from './text'

const COUNTER_FROM = 3500
const DRAFT_DELAY_MS = 300
const TYPING_EVERY_MS = 3000

/** Экран без мыши и без точной указки: там Enter переносит строку, отправка только кнопкой. */
const touchLike = () => typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(hover: none), (pointer: coarse)').matches

type Props = {
  chatId: string
  canWrite: boolean
  closedReason: ClosedReason | null
  onSend: (text: string) => void
}

/** Поле ввода (срез 35): растёт по высоте, Enter отправляет, Shift+Enter переносит; черновик сохраняется; пока пишете, собеседнику уходит «печатает». */
export function Composer({ chatId, canWrite, closedReason, onSend }: Props) {
  const c = t.chat.composer
  const { user } = useMe()
  const userId = user?.id ?? ''
  const [value, setValue] = useState(() => loadDraft(userId, chatId))
  const ref = useRef<HTMLTextAreaElement>(null)
  const inputId = useId()
  const hintId = useId()
  const lastTyping = useRef(0)
  const latest = useRef(value)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [value])

  // Черновик: после паузы в наборе и при уходе со страницы
  useEffect(() => {
    latest.current = value
    const timer = setTimeout(() => saveDraft(userId, chatId, value), DRAFT_DELAY_MS)
    return () => clearTimeout(timer)
  }, [value, userId, chatId])
  useEffect(() => () => saveDraft(userId, chatId, latest.current), [userId, chatId])

  if (!canWrite) {
    return (
      <p className="chat-closed" role="status">
        {closedReason ? t.chat.pane.closed[closedReason] : c.cannotWrite}
      </p>
    )
  }

  const text = value.trim()
  const length = textLength(text)
  const over = length - MAX_TEXT
  const canSend = text !== '' && over <= 0

  const submit = () => {
    if (!canSend) return
    onSend(text)
    setValue('')
    saveDraft(userId, chatId, '')
    lastTyping.current = 0
    ref.current?.focus()
  }

  const change = (next: string) => {
    setValue(next)
    const now = Date.now()
    if (next.trim() !== '' && now - lastTyping.current >= TYPING_EVERY_MS) {
      lastTyping.current = now
      // «Печатает» — удобство, не данные: промах не показываем
      void sendTyping(chatId).catch(() => undefined)
    }
  }

  return (
    <form
      className="chat-composer"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <div className="chat-composer-row">
        <label className="visually-hidden" htmlFor={inputId}>
          {c.label}
        </label>
        <textarea
          id={inputId}
          ref={ref}
          className="control chat-composer-input"
          rows={1}
          value={value}
          placeholder={c.placeholder}
          aria-describedby={hintId}
          aria-invalid={over > 0 || undefined}
          enterKeyHint="enter"
          onChange={(e) => change(e.target.value)}
          onKeyDown={(e) => {
            if (e.key !== 'Enter' || e.nativeEvent.isComposing) return
            const withModifier = e.metaKey || e.ctrlKey
            if (withModifier || (!e.shiftKey && !touchLike())) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <Button type="submit" disabled={!canSend}>
          {c.send}
        </Button>
      </div>
      <p id={hintId} className="chat-composer-hint" data-over={over > 0 || undefined}>
        {over > 0 ? c.tooLong(over) : length >= COUNTER_FROM ? <span className="num">{c.counter(length, MAX_TEXT)}</span> : touchLike() ? c.hintTouch : c.hintKeys}
      </p>
    </form>
  )
}

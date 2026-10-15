import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { useMe } from '../auth/api'
import { MAX_TEXT, sendTyping, type ClosedReason, type Side } from './api'
import { loadDraft, saveDraft } from './drafts'
import { TemplateMenu } from './TemplateMenu'
import type { Template } from './templates'
import { textLength } from './text'
import './chat-tools.css'

const COUNTER_FROM = 3500
const DRAFT_DELAY_MS = 300
const TYPING_EVERY_MS = 3000

/** Экран без мыши и без точной указки: там Enter переносит строку, отправка только кнопкой. */
const touchLike = () => typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(hover: none), (pointer: coarse)').matches

type Props = {
  chatId: string
  side: Side
  canWrite: boolean
  closedReason: ClosedReason | null
  /** Шаблоны организации: номер организации и адрес правки (null, если править нельзя). Нет у учёного и у тех, кому шаблоны недоступны. */
  templates: { orgId: string; manageTo: string | null } | null
  onSend: (text: string) => void
}

/** Почему писать нельзя, словами для этой стороны. */
function closedText(side: Side, reason: ClosedReason | null): string {
  if (!reason) return t.chat.composer.cannotWrite
  const mine = (t.chat.pane.closedMine as Partial<Record<ClosedReason, string>>)[reason]
  return (side === 'scientist' && mine) || t.chat.pane.closed[reason]
}

/** Поле ввода (срез 35): растёт по высоте, Enter отправляет, Shift+Enter переносит; черновик сохраняется; пока пишете, собеседнику уходит «печатает». */
export function Composer({ chatId, side, canWrite, closedReason, templates, onSend }: Props) {
  const c = t.chat.composer
  const { user } = useMe()
  const userId = user?.id ?? ''
  const [value, setValue] = useState(() => loadDraft(userId, chatId))
  const ref = useRef<HTMLTextAreaElement>(null)
  const inputId = useId()
  const hintId = useId()
  const lastTyping = useRef(0)
  const latest = useRef(value)
  const [menu, setMenu] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  // Куда поставить курсор после вставки шаблона (когда значение поля уже обновилось)
  const caret = useRef<number | null>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
    if (caret.current !== null) {
      el.focus({ preventScroll: true })
      el.setSelectionRange(caret.current, caret.current)
      caret.current = null
    }
  }, [value])

  // Черновик: после паузы в наборе и при уходе со страницы
  useEffect(() => {
    latest.current = value
    const timer = setTimeout(() => saveDraft(userId, chatId, value), DRAFT_DELAY_MS)
    return () => clearTimeout(timer)
  }, [value, userId, chatId])
  useEffect(() => () => saveDraft(userId, chatId, latest.current), [userId, chatId])

  if (!canWrite) {
    // Переписку закрыли, пока человек печатал: текст не пропадает, его можно скопировать
    const unsent = value.trim()
    return (
      <div className="chat-closed-wrap">
        <p className="chat-closed" role="status">
          {closedText(side, closedReason)}
        </p>
        {unsent !== '' && (
          <div className="chat-unsent">
            <label htmlFor={`${inputId}-unsent`}>{c.unsent}</label>
            <textarea id={`${inputId}-unsent`} className="control" readOnly rows={3} value={value} />
          </div>
        )}
      </div>
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

  const pick = (tpl: Template) => {
    const el = ref.current
    const from = el?.selectionStart ?? value.length
    const to = el?.selectionEnd ?? value.length
    caret.current = from + tpl.text.length
    change(value.slice(0, from) + tpl.text + value.slice(to))
    setMenu(false)
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
          name="message"
          autoComplete="off"
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
      <div className="chat-composer-foot">
        {templates && (
          <button
            type="button"
            ref={trigger}
            className="chat-templates-trigger"
            data-templates-trigger
            aria-expanded={menu}
            aria-haspopup="dialog"
            onClick={() => setMenu((v) => !v)}
          >
            {t.chat.templates.open}
          </button>
        )}
        <p id={hintId} className="chat-composer-hint" data-over={over > 0 || undefined}>
          {over > 0 ? c.tooLong(over) : length >= COUNTER_FROM ? <span className="num">{c.counter(length, MAX_TEXT)}</span> : touchLike() ? c.hintTouch : c.hintKeys}
        </p>
      </div>
      {menu && templates && (
        <TemplateMenu
          orgId={templates.orgId}
          manageTo={templates.manageTo}
          onPick={pick}
          onClose={() => {
            setMenu(false)
            trigger.current?.focus()
          }}
        />
      )}
    </form>
  )
}

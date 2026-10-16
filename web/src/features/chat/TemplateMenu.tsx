import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Skeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'
import { matchTemplate, useTemplates, type Template } from './templates'
import './chat-tools.css'

/** Сколько шаблонов должно быть, чтобы показать поиск: короткий список проще просмотреть глазами. */
const SEARCH_FROM = 6

type Props = {
  orgId: string
  /** Адрес страницы правки шаблонов; null, если у человека нет права их править. */
  manageTo: string | null
  onPick: (template: Template) => void
  onClose: () => void
}

/**
 * Меню шаблонов у поля ввода организации (срез 36): поиск, список, вставка в поле.
 * Не окно с запертым фокусом: человек может вернуться к полю. Escape и щелчок снаружи закрывают.
 */
export function TemplateMenu({ orgId, manageTo, onPick, onClose }: Props) {
  const m = t.chat.templates
  const query = useTemplates(orgId)
  const [q, setQ] = useState('')
  const panel = useRef<HTMLDivElement>(null)
  const onCloseRef = useRef(onClose)
  const searchId = useId()
  useEffect(() => {
    onCloseRef.current = onClose
  })

  const all = useMemo(() => query.data?.items ?? [], [query.data])
  const shown = useMemo(() => all.filter((tpl) => matchTemplate(tpl, q)), [all, q])
  const searchable = all.length >= SEARCH_FROM

  // Первый шаг: фокус внутрь, к поиску, к первому шаблону или к самой панели
  useEffect(() => {
    if (query.isPending) return
    const el = panel.current
    const first = el?.querySelector<HTMLElement>('input, button')
    ;(first ?? el)?.focus({ preventScroll: true })
    // Страница могла быть прокручена так, что верх панели ушёл под шапку сайта: показываем её целиком
    el?.scrollIntoView?.({ block: 'nearest' })
  }, [query.isPending])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onCloseRef.current()
      }
    }
    const onDown = (e: MouseEvent | TouchEvent) => {
      const el = panel.current
      const target = e.target as Node
      // Кнопка «Шаблоны» сама переключает меню: её нажатие здесь не считаем «снаружи»
      if (el && !el.contains(target) && !(target as HTMLElement).closest?.('[data-templates-trigger]')) onCloseRef.current()
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDown)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDown)
    }
  }, [])

  return (
    <div className="chat-templates" role="dialog" aria-modal="false" aria-label={m.title} ref={panel} tabIndex={-1}>
      {query.isPending ? (
        <div role="status" aria-busy="true" aria-label={m.loading} className="chat-templates-skeleton">
          <Skeleton width="60%" height="1rem" />
          <Skeleton width="85%" height="0.875rem" />
          <Skeleton width="50%" height="1rem" />
        </div>
      ) : query.isError ? (
        <div className="chat-templates-note" role="alert">
          <p>
            {m.loadError}: {describeError(query.error)}
          </p>
          <Button size="sm" variant="secondary" onClick={() => void query.refetch()}>
            {t.common.retry}
          </Button>
        </div>
      ) : (
        <>
          {searchable && (
            <div className="chat-templates-search">
              <label className="visually-hidden" htmlFor={searchId}>
                {m.searchLabel}
              </label>
              <input
                id={searchId}
                name="q"
                autoComplete="off"
                className="control"
                type="search"
                maxLength={100}
                value={q}
                placeholder={m.searchPlaceholder}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  // Меню лежит внутри формы сообщения: Enter в поиске не должен её отправлять
                  if (e.key === 'Enter') e.preventDefault()
                }}
              />
            </div>
          )}
          {all.length === 0 ? (
            <p className="chat-templates-note">{manageTo ? m.empty : m.emptyReader}</p>
          ) : shown.length === 0 ? (
            <p className="chat-templates-note" role="status">
              {m.noMatch}
            </p>
          ) : (
            <ul className="chat-templates-list">
              {shown.map((tpl) => (
                <li key={tpl.id}>
                  <button type="button" className="chat-templates-item" onClick={() => onPick(tpl)}>
                    <span className="chat-templates-name">{tpl.title}</span>
                    <span className="chat-templates-text">{tpl.text}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {manageTo && (
        <p className="chat-templates-foot">
          <Link to={manageTo}>{query.isSuccess && all.length === 0 ? m.add : m.manage}</Link>
        </p>
      )}
    </div>
  )
}

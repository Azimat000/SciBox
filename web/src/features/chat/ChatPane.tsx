import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { usePageTitle } from '../../app/pageTitle'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import { describeError } from '../auth/errors'
import { useMe } from '../auth/api'
import { isNotFound } from '../orgs/api'
import { ApiError } from '../../api/client'
import { isGone, keys, markRead, useChat, type ChatHeader, type Message } from './api'
import { ChatHeaderActions } from './ChatActions'
import { Composer } from './Composer'
import { buildRows } from './conversation'
import { useChatEvents } from './hub'
import { counterpartName, statusText } from './labels'
import { MessageBubble, PendingBubble } from './MessageBubble'
import { SystemLine } from './SystemLine'
import { useConversation } from './useConversation'
import './chat-window.css'

/** Справа, пока переписка не выбрана (на телефоне не видно: там только список). */
export function ChoosePane() {
  return (
    <div className="chat-choose">
      <svg className="chat-choose-art" viewBox="0 0 64 56" width="64" height="56" fill="none" aria-hidden="true" focusable="false">
        <path d="M6 6h52v32H24L10 50V38H6z" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
        <path d="M16 18h32M16 27h20" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </svg>
      <p>{t.chat.pane.choose}</p>
    </div>
  )
}

/** Где открыть отклик или приглашение этой переписки глазами смотрящего. */
function sourceLink(c: ChatHeader): { to: string; label: string } | null {
  const p = t.chat.pane
  if (c.application_id) {
    const base = c.side === 'org' ? '/candidates/' : '/applications/'
    return { to: base + encodeURIComponent(c.application_id), label: p.openApplication }
  }
  if (c.offer_id) return { to: c.side === 'org' ? '/sent-offers' : `/offers/${encodeURIComponent(c.offer_id)}`, label: p.openOffer }
  return null
}

/** Окно открытой переписки: шапка, лента, поле ввода (срез 35). */
export function ChatPane({ id }: { id: string }) {
  const chat = useChat(id)
  const [params] = useSearchParams()
  const qs = params.toString()
  const back = (
    <Link className="chat-back" to={qs ? `/chats?${qs}` : '/chats'}>
      {t.chat.pane.back}
    </Link>
  )

  if (chat.isPending) {
    return (
      <div className="chat-window" role="status" aria-busy="true" aria-label={t.common.loading}>
        {back}
        <div className="chat-window-head">
          <Skeleton width="50%" height="1.5rem" />
          <Skeleton width="70%" height="1rem" />
        </div>
      </div>
    )
  }
  // Перечитывание шапки споткнулось о сеть, а прежняя есть: окно остаётся, ошибка не стирает переписку.
  // Пропавший доступ (404, 403) окно закрывает всегда: чужое показывать нельзя.
  if (chat.isError && (!chat.data || isGone(chat.error))) {
    return (
      <div className="chat-window">
        {back}
        {chat.data ? (
          <EmptyState title={t.chat.pane.accessLost} text={t.chat.pane.accessLostText} />
        ) : isNotFound(chat.error) ? (
          <EmptyState title={t.chat.pane.notFound} text={t.chat.pane.notFoundText} />
        ) : (
          <EmptyState tone="error" title={t.chat.pane.loadError} text={describeError(chat.error)} action={<Button onClick={() => void chat.refetch()}>{t.common.retry}</Button>} />
        )}
      </div>
    )
  }
  return (
    <div className="chat-window">
      {back}
      <Header chat={chat.data} />
      <Thread chat={chat.data} />
    </div>
  )
}

/** Переписка открыта поверх списка (узкий экран) или список недоступен: фокус не должен остаться на скрытом. */
const listHidden = () => typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(max-width: 62rem)').matches

function Header({ chat }: { chat: ChatHeader }) {
  const name = counterpartName(chat.counterpart)
  usePageTitle(`${name} — ${t.chat.title}`)
  const link = sourceLink(chat)
  const title = useRef<HTMLHeadingElement>(null)
  // Открыли переписку: фокус на заголовок, чтобы чтение с экрана начало с неё, а клавиатура продолжила отсюда.
  // На компьютере, пока человек листает список клавишей, фокус остаётся в списке.
  useEffect(() => {
    const active = document.activeElement
    if (!active || active === document.body || listHidden()) title.current?.focus({ preventScroll: true })
  }, [chat.id])
  return (
    <header className="chat-window-head">
      <div className="chat-window-top">
        <h2 className="chat-window-title" ref={title} tabIndex={-1} data-anonymous={chat.counterpart.anonymous ? 'true' : undefined}>
          {name}
        </h2>
        <div className="chat-window-actions">
          <ChatHeaderActions chat={chat} />
        </div>
      </div>
      <p className="chat-window-vacancy">
        <Link to={`/vacancies/${encodeURIComponent(chat.vacancy.id)}`}>{chat.vacancy.title}</Link>
        {chat.vacancy.unit_name && <span className="chat-window-unit">{chat.vacancy.unit_name}</span>}
      </p>
      <div className="chat-window-meta">
        {chat.needs_answer ? <Tag tone="accent">{t.chat.needsAnswer}</Tag> : <Tag>{statusText(chat.status)}</Tag>}
        {link && (
          <ButtonLink to={link.to} variant="secondary" size="sm">
            {link.label}
          </ButtonLink>
        )}
      </div>
    </header>
  )
}

const NEAR_BOTTOM_PX = 72
const NEAR_TOP_PX = 160
const TYPING_SHOWN_MS = 5000
const SEEN_DELAY_MS = 250
/** Несколько сообщений подряд читаются с экрана одной фразой: каждое по отдельности было бы шумом. */
const ANNOUNCE_DELAY_MS = 400

const nearBottom = (el: HTMLElement) => el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_BOTTOM_PX

/** Кто сейчас печатает (из потока): имя пропадает через 5 секунд тишины или с новым сообщением от него. */
function useTyping(chatId: string): string | null {
  const [who, setWho] = useState<string | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useChatEvents((e) => {
    if (e.type === 'chat.typing' && e.chat_id === chatId) {
      setWho(e.author.anonymous || !e.author.name ? t.chat.anonymous : e.author.name)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setWho(null), TYPING_SHOWN_MS)
    } else if (e.type === 'message.created' && e.chat_id === chatId && !e.message.author?.is_me) {
      clearTimeout(timer.current)
      setWho(null)
    }
  })
  useEffect(() => () => clearTimeout(timer.current), [])
  return who
}

/** Текущее время, обновляется раз в минуту: от него зависит, показана ли правка своего сообщения. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000)
    return () => clearInterval(timer)
  }, [])
  return now
}

function Thread({ chat }: { chat: ChatHeader }) {
  const f = t.chat.feed
  const client = useQueryClient()
  const { user } = useMe()
  const userId = user?.id ?? ''
  const feedRef = useRef<HTMLDivElement>(null)
  const stuck = useRef(true)
  const anchor = useRef<{ height: number; top: number } | null>(null)
  const placed = useRef(false)
  const lastTail = useRef('')
  const [freshAfter, setFreshAfter] = useState<number | null>(null)
  const seen = useRef(chat.read.mine)
  const seenTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [atBottom, setAtBottom] = useState(true)
  const [below, setBelow] = useState(0)
  const [announce, setAnnounce] = useState('')
  const incoming = useRef<{ who: string; text: string }[]>([])
  const announceTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const typing = useTyping(chat.id)
  const now = useNow()

  const refreshHeader = useCallback(() => client.invalidateQueries({ queryKey: keys.one(userId, chat.id) }), [client, userId, chat.id])
  /** Сервер сказал «переписки нет» или «закрыта»: шапка перечитывается и окно показывает, что случилось (потеря прав, закрытие). */
  const afterFailure = useCallback(
    (error: unknown) => {
      if (isGone(error) || (error instanceof ApiError && error.code === 'chat_closed')) void refreshHeader()
    },
    [refreshHeader],
  )
  const conv = useConversation(chat.id, chat.read.mine, {
    onIncoming: (m: Message) => {
      const who = m.author ? (m.author.anonymous || !m.author.name ? t.chat.anonymous : m.author.name) : ''
      incoming.current.push({ who, text: m.text.slice(0, 160) })
      if (announceTimer.current === undefined) {
        announceTimer.current = setTimeout(() => {
          announceTimer.current = undefined
          const batch = incoming.current.splice(0)
          setAnnounce(batch.length === 1 ? t.chat.msg.incoming(batch[0].who, batch[0].text) : t.chat.msg.incomingMany(batch.length))
        }, ANNOUNCE_DELAY_MS)
      }
      if (!stuck.current) setBelow((n) => n + 1)
    },
    onSendFailed: () => void refreshHeader(),
  })
  const { state } = conv

  const rows = useMemo(() => buildRows(state, new Date(now), { today: f.today, yesterday: f.yesterday }), [state, now, f.today, f.yesterday])

  /** Что человек увидел, о том и говорим серверу: по нижнему краю ленты; вкладка в фоне ничего не читает. */
  const checkSeen = useCallback(() => {
    const el = feedRef.current
    if (!el || state.status !== 'ready' || document.visibilityState === 'hidden') return
    const bottom = el.scrollTop + el.clientHeight
    const nodes = el.querySelectorAll<HTMLElement>('li[data-seq]')
    let top = 0
    for (let i = nodes.length - 1; i >= 0; i -= 1) {
      if (nodes[i].offsetTop <= bottom) {
        top = Number(nodes[i].dataset.seq)
        break
      }
    }
    if (top <= seen.current) return
    seen.current = top
    clearTimeout(seenTimer.current)
    seenTimer.current = setTimeout(() => {
      markRead(chat.id, seen.current)
        .then(() => {
          client.setQueryData<ChatHeader>(keys.one(userId, chat.id), (h) => (h && h.read.mine < seen.current ? { ...h, read: { ...h.read, mine: seen.current } } : h))
          void client.invalidateQueries({ queryKey: ['chats', 'list'] })
          void client.invalidateQueries({ queryKey: keys.unread(userId) })
        })
        .catch((error: unknown) => {
          // Не дошло: в следующий раз отметим заново
          seen.current = Math.min(seen.current, chat.read.mine)
          afterFailure(error)
        })
    }, SEEN_DELAY_MS)
  }, [afterFailure, chat.id, chat.read.mine, client, state.status, userId])

  useEffect(
    () => () => {
      clearTimeout(seenTimer.current)
      clearTimeout(announceTimer.current)
    },
    [],
  )

  // Положение ленты после каждого изменения сообщений: подгрузка вверх держит место, своё и «у нижнего края» едет вниз.
  useLayoutEffect(() => {
    const el = feedRef.current
    if (!el || state.status !== 'ready') return
    // Подгрузка не удалась: запомненное место больше не нужно, иначе оно собьёт следующую прокрутку
    if (state.olderFailed) anchor.current = null
    if (anchor.current) {
      el.scrollTop = anchor.current.top + (el.scrollHeight - anchor.current.height)
      anchor.current = null
      return
    }
    if (!placed.current) {
      placed.current = true
      setFreshAfter(state.items.length > 0 ? state.items[state.items.length - 1].seq : 0)
      const line = el.querySelector<HTMLElement>('[data-unread-line]')
      if (line) el.scrollTop = Math.max(0, line.offsetTop - 24)
      else el.scrollTop = el.scrollHeight
      stuck.current = nearBottom(el)
      lastTail.current = tailKey(state.items, state.pending)
      checkSeen()
      return
    }
    const tail = tailKey(state.items, state.pending)
    if (tail !== lastTail.current) {
      lastTail.current = tail
      const mineLast = state.pending.length > 0 || state.items[state.items.length - 1]?.author?.is_me === true
      if (stuck.current || mineLast) {
        el.scrollTop = el.scrollHeight
        stuck.current = true
      }
    }
    checkSeen()
  }, [state.items, state.pending, state.status, state.olderFailed, checkSeen])

  // Поле ввода выросло или экран повернули: у нижнего края остаёмся у нижнего.
  useEffect(() => {
    const el = feedRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (stuck.current) el.scrollTop = el.scrollHeight
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [state.status])

  useEffect(() => {
    const onVisible = () => checkSeen()
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('focus', onVisible)
    }
  }, [checkSeen])

  const onScroll = () => {
    const el = feedRef.current
    if (!el) return
    const bottom = nearBottom(el)
    stuck.current = bottom
    setAtBottom(bottom)
    if (bottom) setBelow(0)
    if (el.scrollTop < NEAR_TOP_PX && state.status === 'ready' && state.hasMore && !state.olderLoading && !state.olderFailed) {
      anchor.current = { height: el.scrollHeight, top: el.scrollTop }
      void conv.loadOlder()
    }
    checkSeen()
  }

  const toBottom = () => {
    const el = feedRef.current
    if (!el) return
    const calm = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    if (typeof el.scrollTo === 'function') el.scrollTo({ top: el.scrollHeight, behavior: calm ? 'auto' : 'smooth' })
    else el.scrollTop = el.scrollHeight
    setBelow(0)
  }

  const retryOlder = () => {
    const el = feedRef.current
    if (el) anchor.current = { height: el.scrollHeight, top: el.scrollTop }
    void conv.loadOlder()
  }

  const actions = {
    onEdit: async (seq: number, text: string) => {
      try {
        await conv.edit(seq, text)
      } catch (error) {
        afterFailure(error)
        throw error
      }
    },
    onRemove: async (seq: number) => {
      try {
        await conv.remove(seq)
      } catch (error) {
        afterFailure(error)
        throw error
      }
    },
    onRetry: conv.retry,
    onDiscard: conv.discard,
  }
  const templates =
    chat.viewer.can_use_templates && chat.viewer.org_id
      ? { orgId: chat.viewer.org_id, manageTo: chat.viewer.can_edit_templates ? `/my-organization/${encodeURIComponent(chat.vacancy.org_slug)}/chat-templates` : null }
      : null
  return (
    <>
      <div className="chat-feed-wrap">
        {state.status === 'loading' && (
          <div className="chat-feed" role="status" aria-busy="true" aria-label={t.common.loading}>
            <FeedSkeleton />
          </div>
        )}
        {state.status === 'error' && (
          <div className="chat-feed">
            <EmptyState tone="error" title={f.loadError} text={describeError(state.error)} action={<Button onClick={conv.reload}>{t.common.retry}</Button>} />
          </div>
        )}
        {state.status === 'ready' && (
          <div className="chat-feed" ref={feedRef} onScroll={onScroll} tabIndex={0} role="region" aria-label={f.label}>
            {state.hasMore && (
              <div className="chat-older">
                {state.olderFailed ? (
                  <>
                    <p role="alert">{f.olderFailed}</p>
                    <Button size="sm" variant="secondary" onClick={retryOlder}>
                      {f.olderRetry}
                    </Button>
                  </>
                ) : (
                  <span role="status">{state.olderLoading ? f.olderLoading : ''}</span>
                )}
              </div>
            )}
            {rows.length === 0 && <p className="chat-feed-empty">{chat.can_write ? f.empty : f.emptyClosed}</p>}
            <ol className="chat-thread">
              {rows.map((r) => {
                switch (r.kind) {
                  case 'day':
                    return (
                      <li key={r.key} className="chat-day">
                        <span>{r.label}</span>
                      </li>
                    )
                  case 'unread':
                    return (
                      <li key={r.key} className="chat-unread-line" data-unread-line>
                        <span>{f.newMessages}</span>
                      </li>
                    )
                  case 'pending':
                    return (
                      <li key={r.key} className="chat-item">
                        <PendingBubble pending={r.pending} actions={actions} fresh canRetry={chat.can_write} />
                      </li>
                    )
                  case 'message': {
                    const m = r.message
                    const fresh = freshAfter !== null && m.seq > freshAfter
                    return m.kind === 'system' ? (
                      <li key={r.key} className="chat-item chat-item-event" data-seq={m.seq} data-fresh={fresh || undefined}>
                        <SystemLine message={m} items={state.items} chat={chat} onChanged={() => void Promise.all([refreshHeader(), conv.refresh()])} />
                      </li>
                    ) : (
                      <li key={r.key} className="chat-item" data-seq={m.seq} data-author={r.showAuthor || undefined}>
                        <MessageBubble message={m} chat={chat} now={now} fresh={fresh} actions={actions} />
                      </li>
                    )
                  }
                }
              })}
            </ol>
          </div>
        )}
        {state.status === 'ready' && !atBottom && (
          <button type="button" className="chat-to-bottom" onClick={toBottom} aria-label={f.toNew(below)}>
            <svg viewBox="0 0 16 16" width="16" height="16" fill="none" aria-hidden="true" focusable="false">
              <path d="M3 6l5 5 5-5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            {below > 0 && <span className="chat-to-bottom-count num">{below > 99 ? '99+' : below}</span>}
          </button>
        )}
      </div>
      <p className="chat-typing" aria-hidden={typing ? undefined : 'true'} data-on={typing ? 'true' : undefined}>
        {typing && (
          <>
            <span className="chat-typing-dots" aria-hidden="true">
              <i />
              <i />
              <i />
            </span>
            {f.typing(typing)}
          </>
        )}
      </p>
      <p className="visually-hidden" role="status" aria-live="polite">
        {announce}
      </p>
      <Composer chatId={chat.id} side={chat.side} canWrite={chat.can_write} closedReason={chat.closed_reason} templates={templates} onSend={conv.send} />
    </>
  )
}

/** Метка конца ленты: меняется, когда пришло новое сообщение или добавилось своё неподтверждённое. */
function tailKey(items: readonly Message[], pending: readonly { client_id: string }[]): string {
  return `${items.length > 0 ? items[items.length - 1].seq : 0}:${pending.map((p) => p.client_id).join(',')}`
}

function FeedSkeleton() {
  return (
    <div className="chat-thread-skeleton" aria-hidden="true">
      <Skeleton width="55%" height="3.5rem" />
      <Skeleton width="40%" height="2.5rem" className="chat-skel-mine" />
      <Skeleton width="62%" height="4.5rem" />
    </div>
  )
}

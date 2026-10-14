import { Link, useSearchParams } from 'react-router'
import { usePageTitle } from '../../app/pageTitle'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Skeleton } from '../../ui/Skeleton'
import { Tag } from '../../ui/Tag'
import { describeError } from '../auth/errors'
import { isNotFound } from '../orgs/api'
import { useChat, type ChatHeader } from './api'
import { counterpartName, statusText } from './labels'

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

/** Окно открытой переписки. В срезе 34 только шапка; лента сообщений и поле ввода — срез 35. */
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
  if (chat.isError) {
    return (
      <div className="chat-window">
        {back}
        {isNotFound(chat.error) ? (
          <EmptyState title={t.chat.pane.notFound} text={t.chat.pane.notFoundText} />
        ) : (
          <EmptyState tone="error" title={t.chat.pane.loadError} text={describeError(chat.error)} action={<Button onClick={() => void chat.refetch()}>{t.common.retry}</Button>} />
        )}
      </div>
    )
  }
  return <Header chat={chat.data} back={back} />
}

function Header({ chat, back }: { chat: ChatHeader; back: React.ReactNode }) {
  const name = counterpartName(chat.counterpart)
  usePageTitle(`${name} — ${t.chat.title}`)
  const link = sourceLink(chat)
  return (
    <div className="chat-window">
      {back}
      <header className="chat-window-head">
        <h2 className="chat-window-title" data-anonymous={chat.counterpart.anonymous ? 'true' : undefined}>
          {name}
        </h2>
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
      {chat.closed_reason && <p className="chat-window-closed">{t.chat.pane.closed[chat.closed_reason]}</p>}
      <p className="chat-window-soon">{t.chat.pane.soon}</p>
    </div>
  )
}

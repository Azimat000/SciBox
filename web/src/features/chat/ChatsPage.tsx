import { useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { t } from '../../i18n'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { SearchIcon } from '../../ui/icons'
import { Select } from '../../ui/Select'
import { Skeleton } from '../../ui/Skeleton'
import { describeError } from '../auth/errors'
import { RequireUser } from '../orgs/RequireUser'
import { useRole } from '../shell/useRole'
import { useChatList, useChatVacancies, useUnreadChats, type Box, type ChatQuery, type Side } from './api'
import { ChatPane, ChoosePane } from './ChatPane'
import { ChatRow } from './ChatRow'
import '../orgs/orgs.css'
import './chats.css'

const boxes: readonly Box[] = ['all', 'unread', 'archive']
const SEARCH_DELAY_MS = 300

/** «Сообщения»: список переписок стороны текущего режима и окно открытой переписки (D-217, CHAT.md). */
export function ChatsPage() {
  return <RequireUser>{() => <Chats />}</RequireUser>
}

function Chats() {
  const { id } = useParams()
  const { role } = useRole()
  const side: Side = role === 'employer' ? 'org' : 'scientist'
  const [params] = useSearchParams()
  const box = (boxes as readonly string[]).includes(params.get('box') ?? '') ? (params.get('box') as Box) : 'all'
  const query: ChatQuery = { side, box, q: (params.get('q') ?? '').trim(), vacancy: side === 'org' ? (params.get('vacancy') ?? '') : '' }

  return (
    <div className="page chats-page" data-open={id ? 'true' : undefined}>
      <div className="chats-head">
        <h1>{t.chat.title}</h1>
        <p className="lead">{t.chat.lead[side]}</p>
        <OtherSide side={side} />
      </div>
      <div className="chats">
        <section className="chats-side" aria-label={t.chat.listLabel}>
          <ListControls query={query} />
          {/* Ключ по стороне: при смене режима поле поиска начинается заново */}
          <ChatList key={side} query={query} selected={id} />
        </section>
        <section className="chats-pane">{id ? <ChatPane key={id} id={id} /> : <ChoosePane />}</section>
      </div>
    </div>
  )
}

/** Непрочитанные в другом режиме: человек мог их пропустить, пока смотрит другую сторону. */
function OtherSide({ side }: { side: Side }) {
  const unread = useUnreadChats()
  const { setRole } = useRole()
  const other = side === 'org' ? (unread.data?.scientist ?? 0) : (unread.data?.org ?? 0)
  if (other === 0) return null
  return (
    <p className="chats-other">
      {t.chat.otherSide[side](other)}{' '}
      <Button variant="quiet" size="sm" onClick={() => setRole(side === 'org' ? 'seeker' : 'employer')}>
        {t.chat.otherSide.switch}
      </Button>
    </p>
  )
}

function useParamsPatch() {
  const [params, setParams] = useSearchParams()
  return (patch: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [k, v] of Object.entries(patch)) {
      if (v) next.set(k, v)
      else next.delete(k)
    }
    setParams(next, { replace: true })
  }
}

function ListControls({ query }: { query: ChatQuery }) {
  const [params] = useSearchParams()
  const patch = useParamsPatch()
  const list = useChatList(query)
  const counts = list.data?.pages[0]?.counts
  const vacancies = useChatVacancies(query.side === 'org')
  const tabHref = (b: Box) => {
    const next = new URLSearchParams(params)
    if (b === 'all') next.delete('box')
    else next.set('box', b)
    const qs = next.toString()
    return qs ? `/chats?${qs}` : '/chats'
  }

  return (
    <div className="chats-controls">
      <nav className="manage-tabs chats-boxes" aria-label={t.chat.boxesLabel}>
        {boxes.map((b) => {
          const count = b === 'unread' ? counts?.unread : b === 'archive' ? counts?.archive : undefined
          return (
            <Link
              key={b}
              className="manage-tab"
              to={tabHref(b)}
              aria-current={b === query.box ? 'page' : undefined}
              aria-label={count ? t.chat.boxLabel(t.chat.boxes[b], count) : undefined}
            >
              {t.chat.boxes[b]}
              {count ? (
                <span className="tab-count num" aria-hidden="true">
                  {count}
                </span>
              ) : null}
            </Link>
          )
        })}
      </nav>
      <div className="chats-filters">
        <SearchField key={query.side} side={query.side} initial={query.q} onSearch={(q) => patch({ q })} />
        {query.side === 'org' && vacancies.data && vacancies.data.items.length > 0 && (
          <Select
            className="chats-vacancy"
            label={t.chat.vacancyFilter}
            name="vacancy"
            value={query.vacancy}
            placeholder={t.chat.allVacancies}
            onChange={(ev) => patch({ vacancy: ev.target.value })}
            options={vacancies.data.items.map((v) => ({ value: v.id, label: t.chat.vacancyOption(v.title, v.chats) }))}
          />
        )}
      </div>
    </div>
  )
}

/** Поиск применяется сам, когда человек перестал печатать; Enter применяет сразу. */
function SearchField({ side, initial, onSearch }: { side: Side; initial: string; onSearch: (q: string) => void }) {
  const [value, setValue] = useState(initial)
  // Поиск сбросили снаружи («Сбросить поиск», назад в браузере): поле показывает то, что в адресе.
  const [seen, setSeen] = useState(initial)
  if (initial !== seen) {
    setSeen(initial)
    if (initial !== value.trim()) setValue(initial)
  }
  useEffect(() => {
    if (value.trim() === initial) return
    const timer = setTimeout(() => onSearch(value.trim()), SEARCH_DELAY_MS)
    return () => clearTimeout(timer)
  }, [value, initial, onSearch])
  return (
    <form
      className="chats-search"
      role="search"
      onSubmit={(e) => {
        e.preventDefault()
        onSearch(value.trim())
      }}
    >
      <label htmlFor="chats-q" className="visually-hidden">
        {t.chat.searchLabel}
      </label>
      <SearchIcon className="chats-search-icon" size={18} />
      <input id="chats-q" className="control chats-search-input" type="search" maxLength={100} value={value} placeholder={t.chat.searchPlaceholder[side]} onChange={(e) => setValue(e.target.value)} />
    </form>
  )
}

function ChatList({ query, selected }: { query: ChatQuery; selected: string | undefined }) {
  const list = useChatList(query)
  const patch = useParamsPatch()
  const [params] = useSearchParams()
  const qs = params.toString()

  if (list.isPending) return <ListSkeleton />
  if (list.isError && !list.data) {
    return (
      <EmptyState tone="error" title={t.chat.loadError} text={describeError(list.error)} action={<Button onClick={() => void list.refetch()}>{t.common.retry}</Button>} />
    )
  }
  const items = list.data.pages.flatMap((p) => p.items)
  if (items.length === 0) return <Empty query={query} onReset={() => patch({ q: '', vacancy: '' })} />

  return (
    <>
      <ul className="chat-list" aria-busy={list.isPlaceholderData || undefined}>
        {items.map((c) => (
          <ChatRow key={c.id} chat={c} selected={c.id === selected} href={`/chats/${encodeURIComponent(c.id)}${qs ? `?${qs}` : ''}`} />
        ))}
      </ul>
      {list.isFetchNextPageError && (
        <p className="chats-more-error" role="alert">
          {t.chat.moreError}
        </p>
      )}
      {list.hasNextPage && (
        <div className="chats-more">
          <Button variant="secondary" loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
            {list.isFetchNextPageError ? t.common.retry : t.chat.more}
          </Button>
        </div>
      )}
    </>
  )
}

function Empty({ query, onReset }: { query: ChatQuery; onReset: () => void }) {
  const e = t.chat.empty
  if (query.q || query.vacancy) {
    return (
      <EmptyState
        title={e.search.title}
        text={e.search.text}
        action={
          <Button variant="secondary" onClick={onReset}>
            {e.search.reset}
          </Button>
        }
      />
    )
  }
  if (query.box === 'unread') return <EmptyState title={e.unread.title} text={e.unread.text} />
  if (query.box === 'archive') return <EmptyState title={e.archive.title} text={e.archive.text} />
  const own = e[query.side]
  return (
    <EmptyState
      title={own.title}
      text={own.text}
      action={
        <ButtonLink to={query.side === 'org' ? '/scientists' : '/vacancies'} variant="secondary">
          {own.action}
        </ButtonLink>
      }
    />
  )
}

/** Заготовки строк списка той же высоты, что и настоящие. */
function ListSkeleton() {
  return (
    <div className="chat-list" role="status" aria-busy="true" aria-label={t.common.loading}>
      {[0, 1, 2, 3, 4].map((i) => (
        <div key={i} className="chat-row chat-row-skeleton" aria-hidden="true">
          <Skeleton width="58%" height="1.05rem" />
          <Skeleton width="80%" height="0.875rem" />
          <Skeleton width="68%" height="0.875rem" />
        </div>
      ))}
    </div>
  )
}

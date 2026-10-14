import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'
import { useMe } from '../auth/api'

// Типы ответов API чата. Точные примеры: contracts/chat/*.json (срез 34, D-215); правила: docs/CHAT.md.

export type Side = 'scientist' | 'org'
export type Box = 'all' | 'unread' | 'archive'

export type ChatVacancy = { id: string; title: string; org_name: string; org_slug: string; unit_name: string }

/** Собеседник глазами смотрящего: у учёного организация, у организации учёный (имя пусто, если он анонимен). */
export type Counterpart = { kind: Side; name: string; anonymous: boolean; profile_id: string | null }

export type ChatStatus = {
  source: 'application' | 'offer'
  /** Статус отклика (sent…withdrawn) или приглашения на вакансию (pending…cancelled). */
  value: string
}

export type ClosedReason = 'rejected' | 'withdrawn' | 'offer_declined' | 'offer_cancelled' | 'blocked'

export type Author = { side: Side; name: string; is_me: boolean; anonymous: boolean; former: boolean }

export type LastMessage = {
  seq: number
  kind: 'text' | 'system'
  /** У системной строки автора нет. */
  author: Author | null
  text: string
  event_type: string | null
  deleted: boolean
  created_at: string
}

export type ChatSummary = {
  id: string
  side: Side
  vacancy: ChatVacancy
  counterpart: Counterpart
  status: ChatStatus
  /** Ждут ответа смотрящего: приглашение без ответа у учёного, предложенное время у организации. */
  needs_answer: boolean
  last_message: LastMessage | null
  unread: number
  archived: boolean
  can_write: boolean
  closed_reason: ClosedReason | null
  updated_at: string
}

export type ChatList = { items: ChatSummary[]; next_cursor: string | null; counts: { unread: number; archive: number } }

export type UnreadCount = { total: number; scientist: number; org: number }

export type ChatVacancyOption = { id: string; title: string; org_name: string; unit_name: string; chats: number }

export type Viewer = {
  side: Side
  can_use_templates: boolean
  can_edit_templates: boolean
  org_id: string | null
  can_block: boolean
  can_complain: boolean
}

export type ChatHeader = {
  id: string
  side: Side
  vacancy: ChatVacancy & { status: string; deadline: string | null }
  counterpart: Counterpart
  application_id: string | null
  offer_id: string | null
  status: ChatStatus
  needs_answer: boolean
  can_write: boolean
  closed_reason: ClosedReason | null
  archived: boolean
  viewer: Viewer
  /** Докуда прочитано (seq): mine — смотрящим, theirs — другой стороной. */
  read: { mine: number; theirs: number }
}

export type ChatEvent = { type: string; data: Record<string, unknown> }

export type Message = {
  id: string
  seq: number
  kind: 'text' | 'system'
  author: Author | null
  text: string
  event: ChatEvent | null
  created_at: string
  edited_at: string | null
  deleted: boolean
  editable_until: string | null
  client_id: string | null
}

export type MessagePage = { items: Message[]; has_more: boolean }

/** Поле `chat` у отклика и приглашения на вакансию: переписка этой пары глазами смотрящего. */
export type ChatRef = { id: string; unread: number; can_write: boolean; started: boolean }

export const PAGE_SIZE = 30
/** Как часто спрашиваем число непрочитанных и обновляем список, пока сайт открыт (живой поток появится в срезе 35). */
export const POLL_MS = 60_000

export type ChatQuery = { side: Side; box: Box; q: string; vacancy: string }

const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  all: ['chats'] as const,
  unread: (userId: string | null | undefined) => ['chats', 'unread', who(userId)] as const,
  list: (userId: string | null | undefined, q: ChatQuery) => ['chats', 'list', who(userId), q.side, q.box, q.q, q.vacancy] as const,
  vacancies: (userId: string | null | undefined) => ['chats', 'vacancies', who(userId)] as const,
  one: (userId: string | null | undefined, id: string) => ['chats', 'one', who(userId), id] as const,
}

export function listPath(q: ChatQuery, cursor: string | null) {
  const qs = new URLSearchParams({ side: q.side, box: q.box, limit: String(PAGE_SIZE) })
  if (q.q) qs.set('q', q.q)
  if (q.vacancy && q.side === 'org') qs.set('vacancy', q.vacancy)
  if (cursor) qs.set('cursor', cursor)
  return `/api/chats?${qs}`
}

/** Число переписок с непрочитанным для значка в шапке; обновляется раз в минуту. */
export function useUnreadChats() {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.unread(user?.id),
    queryFn: ({ signal }) => apiGet<UnreadCount>('/api/chats/unread-count', { signal }),
    enabled: user !== undefined && user !== null,
    refetchInterval: POLL_MS,
    retry: false,
  })
}

/** Список переписок одной стороны: страницы по ключу, «Показать ещё» дочитывает следующую. */
export function useChatList(q: ChatQuery) {
  const { user } = useMe()
  return useInfiniteQuery({
    queryKey: keys.list(user?.id, q),
    queryFn: ({ signal, pageParam }) => apiGet<ChatList>(listPath(q, pageParam), { signal }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next_cursor,
    enabled: user !== undefined && user !== null,
    refetchInterval: POLL_MS,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Вакансии с переписками для отбора у организации. */
export function useChatVacancies(enabled: boolean) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.vacancies(user?.id),
    queryFn: ({ signal }) => apiGet<{ items: ChatVacancyOption[] }>('/api/chats/vacancies', { signal }),
    enabled: enabled && user !== undefined && user !== null,
    retry: false,
  })
}

/** Шапка одной переписки. */
export function useChat(id: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.one(user?.id, id),
    queryFn: async ({ signal }) => (await apiGet<{ chat: ChatHeader }>(`/api/chats/${encodeURIComponent(id)}`, { signal })).chat,
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

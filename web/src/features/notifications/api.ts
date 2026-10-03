import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'

export type Notice = {
  id: string
  kind: string
  title: string
  body: string
  link: string
  created_at: string
  read: boolean
}

export type NoticeList = { items: Notice[]; total: number; unread: number }

export const PAGE_SIZE = 20
/** Как часто спрашиваем число непрочитанных, пока сайт открыт. */
export const POLL_MS = 60_000

const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  all: ['notifications'] as const,
  unread: (userId: string | null | undefined) => ['notifications', 'unread', who(userId)] as const,
  recent: (userId: string | null | undefined) => ['notifications', 'recent', who(userId)] as const,
  page: (userId: string | null | undefined, page: number) => ['notifications', 'page', who(userId), page] as const,
}

/** Число непрочитанных для колокольчика; сайт сам обновляет его раз в минуту. */
export function useUnreadCount() {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.unread(user?.id),
    queryFn: async ({ signal }) => (await apiGet<{ unread: number }>('/api/notifications/unread-count', { signal })).unread,
    enabled: user !== undefined && user !== null,
    refetchInterval: POLL_MS,
    retry: false,
  })
}

/** Последние уведомления для выпадающего окна колокольчика. */
export function useRecent(enabled: boolean) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.recent(user?.id),
    queryFn: ({ signal }) => apiGet<NoticeList>('/api/notifications?limit=6', { signal }),
    enabled: enabled && user !== undefined && user !== null,
    retry: false,
  })
}

export function useNoticePage(page: number) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.page(user?.id, page),
    queryFn: ({ signal }) => apiGet<NoticeList>(`/api/notifications?limit=${PAGE_SIZE}&offset=${(page - 1) * PAGE_SIZE}`, { signal }),
    enabled: user !== undefined && user !== null,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Отметки о прочтении: после них все списки и цифра на колокольчике перечитываются. */
export function useMarkRead() {
  const client = useQueryClient()
  const refresh = () => client.invalidateQueries({ queryKey: keys.all })
  const one = useMutation({ mutationFn: (id: string) => apiSend('POST', `/api/notifications/${id}/read`), onSuccess: refresh })
  const all = useMutation({ mutationFn: () => apiSend('POST', '/api/notifications/read-all'), onSuccess: refresh })
  return { one, all }
}

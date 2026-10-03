import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'
import type { Card } from '../vacancies/api'

export const PAGE_SIZE = 20

// ---- избранное ----

/** open: можно откликнуться; expired: срок подачи прошёл; closed: набор закончен. */
export type FavoriteState = 'open' | 'expired' | 'closed'

export type FavoriteItem = {
  vacancy: Card
  added_at: string
  state: FavoriteState
  /** Живой отклик человека на эту вакансию. */
  application_id: string | null
}
export type FavoriteList = { items: FavoriteItem[]; total: number }

// ---- подбор ----

export type MatchItem = { vacancy: Card; score: number; reasons: string[] }
export type MatchBasis = { specialties: number; level: number; has_region: boolean; has_degree: boolean }
export type MatchList = { items: MatchItem[]; total: number; ready: boolean; basis: MatchBasis }

// ---- сроки ----

export type DeadlineItem = { vacancy: Card; days_left: number; application_id: string | null }
export type Calendar = { items: DeadlineItem[]; without_deadline: number }

// ---- сохранённые поиски ----

export const frequencies = ['instant', 'daily', 'weekly', 'off'] as const
export type Frequency = (typeof frequencies)[number]
export const DEFAULT_FREQUENCY: Frequency = 'daily'

export type SavedSearch = {
  id: string
  name: string
  /** Условия в виде строки запроса страницы поиска, без знака «?». */
  query: string
  frequency: Frequency
  created_at: string
  last_sent_at: string | null
}

// ---- письма ----

export type MailSettings = { email_new_vacancies: boolean; email_deadlines: boolean }

// Что видно, зависит от того, кто смотрит: кеш лежит под ключом с номером человека.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  all: ['matching'] as const,
  ids: (userId: string | null | undefined) => ['matching', 'ids', who(userId)] as const,
  favorites: (userId: string | null | undefined, page: number) => ['matching', 'favorites', who(userId), page] as const,
  matches: (userId: string | null | undefined, page: number) => ['matching', 'matches', who(userId), page] as const,
  calendar: (userId: string | null | undefined) => ['matching', 'calendar', who(userId)] as const,
  searches: (userId: string | null | undefined) => ['matching', 'searches', who(userId)] as const,
  search: (userId: string | null | undefined, id: string) => ['matching', 'search', who(userId), id] as const,
  mail: (userId: string | null | undefined) => ['matching', 'mail', who(userId)] as const,
}

function useSignedIn() {
  const { user } = useMe()
  return { user, enabled: user !== undefined && user !== null }
}

/** Номера избранных вакансий: ими отмечены закладки в списках. Для не вошедшего не спрашивается. */
export function useFavoriteIds() {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.ids(user?.id),
    queryFn: async ({ signal }) => new Set((await apiGet<{ ids: string[] }>('/api/favorites/ids', { signal })).ids),
    enabled,
    retry: false,
  })
}

export function useFavorites(page: number) {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.favorites(user?.id, page),
    queryFn: ({ signal }) => apiGet<FavoriteList>(`/api/favorites?${pageQuery(page)}`, { signal }),
    enabled,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

export function useMatches(page: number) {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.matches(user?.id, page),
    queryFn: ({ signal }) => apiGet<MatchList>(`/api/matches?${pageQuery(page)}`, { signal }),
    enabled,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

export function useCalendar() {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.calendar(user?.id),
    queryFn: ({ signal }) => apiGet<Calendar>('/api/deadlines', { signal }),
    enabled,
    retry: false,
  })
}

export function useSavedSearches() {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.searches(user?.id),
    queryFn: async ({ signal }) => (await apiGet<{ items: SavedSearch[] }>('/api/saved-searches', { signal })).items,
    enabled,
    retry: false,
  })
}

export function useSavedSearch(id: string) {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.search(user?.id, id),
    queryFn: async ({ signal }) => (await apiGet<{ search: SavedSearch }>(`/api/saved-searches/${encodeURIComponent(id)}`, { signal })).search,
    enabled,
    retry: false,
  })
}

export function useMailSettings() {
  const { user, enabled } = useSignedIn()
  return useQuery({
    queryKey: keys.mail(user?.id),
    queryFn: ({ signal }) => apiGet<MailSettings>('/api/notification-settings', { signal }),
    enabled,
    retry: false,
  })
}

function pageQuery(page: number): string {
  return new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) }).toString()
}

// ---- действия ----

export const addFavorite = (vacancyId: string) => apiSend('PUT', `/api/favorites/${vacancyId}`)
export const removeFavorite = (vacancyId: string) => apiSend('DELETE', `/api/favorites/${vacancyId}`)

export async function createSearch(fields: { name: string; query: string; frequency: Frequency }): Promise<SavedSearch> {
  return (await apiSend<{ search: SavedSearch }>('POST', '/api/saved-searches', fields)).search
}

export async function updateSearch(id: string, fields: { name: string; frequency: Frequency }): Promise<SavedSearch> {
  return (await apiSend<{ search: SavedSearch }>('PATCH', `/api/saved-searches/${id}`, fields)).search
}

export const deleteSearch = (id: string) => apiSend('DELETE', `/api/saved-searches/${id}`)

export const saveMailSettings = (fields: MailSettings) => apiSend<MailSettings>('PUT', '/api/notification-settings', fields)

/** Избранное изменилось: закладки, список и календарь сроков перечитаются. */
export function refreshFavorites(client: QueryClient) {
  return Promise.all(
    (['ids', 'favorites', 'calendar'] as const).map((part) => client.invalidateQueries({ queryKey: ['matching', part] })),
  )
}

/** Человек откликнулся на вакансию: она уходит из подборки, а в избранном и сроках получает отметку об отклике. */
export function refreshAfterApplying(client: QueryClient) {
  return Promise.all(
    (['matches', 'favorites', 'calendar'] as const).map((part) => client.invalidateQueries({ queryKey: ['matching', part] })),
  )
}

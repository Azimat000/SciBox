import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'

export const statuses = ['pending', 'interested', 'declined', 'cancelled'] as const
export type OfferStatus = (typeof statuses)[number]

export const PAGE_SIZE = 20

/** Вакансия в приглашении. */
export type VacancyRef = {
  id: string
  title: string
  status: string
  org_name: string
  org_slug: string
  unit_name: string
  city: string
  deadline: string | null
}

/** Приглашение: учёный видит организацию и вакансию, организация видит учёного. Флаги can_* зависят от того, кто смотрит. */
export type Offer = {
  id: string
  status: OfferStatus
  message: string
  answer_note: string
  answered_at: string | null
  created_at: string
  vacancy: VacancyRef
  scientist?: { profile_id: string; name: string }
  /** Только учёному: его живой отклик на эту вакансию. */
  application_id: string | null
  can_answer: boolean
  can_cancel: boolean
}

export type MineList = { items: Offer[]; total: number; counts: Record<Exclude<OfferStatus, 'cancelled'>, number>; pending: number }
export type SentList = { items: Offer[]; total: number; counts: Record<OfferStatus, number> }

/** Вакансия, на которую можно пригласить учёного. offered — уже приглашён, applied — уже откликнулся. */
export type Target = {
  id: string
  title: string
  org_name: string
  org_slug: string
  unit_name: string
  deadline: string | null
  offered: boolean
  applied: boolean
}

// Что видно, зависит от того, кто смотрит: кеш лежит под ключом с номером человека.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  all: ['offers'] as const,
  mine: (userId: string | null | undefined, page: number) => ['offers', 'mine', who(userId), page] as const,
  one: (userId: string | null | undefined, id: string) => ['offers', 'one', who(userId), id] as const,
  sent: (userId: string | null | undefined, status: string, page: number) => ['offers', 'sent', who(userId), status, page] as const,
  targets: (userId: string | null | undefined, profileId: string) => ['offers', 'targets', who(userId), profileId] as const,
}

export function useMyOffers(page: number) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.mine(user?.id, page),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) })
      return apiGet<MineList>(`/api/offers?${qs}`, { signal })
    },
    enabled: user !== undefined && user !== null,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

export function useOffer(id: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.one(user?.id, id),
    queryFn: async ({ signal }) => (await apiGet<{ offer: Offer }>(`/api/offers/${encodeURIComponent(id)}`, { signal })).offer,
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

export function useSentOffers(status: string, page: number) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.sent(user?.id, status, page),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) })
      if (status) qs.set('status', status)
      return apiGet<SentList>(`/api/my/sent-offers?${qs}`, { signal })
    },
    enabled: user !== undefined && user !== null,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Вакансии, на которые вошедший может пригласить этого учёного; спрашивается, только когда окно открыто. */
export function useOfferTargets(profileId: string, enabled: boolean) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.targets(user?.id, profileId),
    queryFn: async ({ signal }) => (await apiGet<{ items: Target[] }>(`/api/scientists/${encodeURIComponent(profileId)}/offer-targets`, { signal })).items,
    enabled: enabled && user !== undefined && user !== null,
    retry: false,
  })
}

// ---- действия ----

export async function sendOffer(fields: { vacancy_id: string; profile_id: string; message: string }): Promise<Offer> {
  const res = await apiSend<{ offer: Offer }>('POST', '/api/offers', fields)
  return res.offer
}

export function answerOffer(id: string, fields: { action: 'interested' | 'declined'; note: string }) {
  return apiSend('POST', `/api/offers/${id}/answer`, fields)
}

export function cancelOffer(id: string) {
  return apiSend('POST', `/api/offers/${id}/cancel`)
}

/** Приглашение изменилось: списки, карточка и окно «куда пригласить» перечитаются. */
export function refreshOffers(client: QueryClient) {
  return client.invalidateQueries({ queryKey: keys.all })
}

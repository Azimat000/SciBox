import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'

export const itemKinds = ['education', 'experience', 'publication', 'grant', 'patent', 'teaching'] as const
export type ItemKind = (typeof itemKinds)[number]

export const visibilities = ['hidden', 'orgs', 'public'] as const
export type Visibility = (typeof visibilities)[number]

export const degreeLevels = ['none', 'candidate', 'doctor'] as const
export const academicTitles = ['none', 'docent', 'professor'] as const
export const pubTypes = ['article', 'book', 'chapter', 'conference', 'preprint', 'thesis', 'other'] as const
export const grantRoles = ['lead', 'participant'] as const
export const patentTypes = ['invention', 'utility_model', 'software', 'database', 'other'] as const
export const teachLevels = ['bachelor', 'master', 'postgraduate', 'continuing', 'other'] as const

export type Code = { code: string; name: string }

/** Запись любого раздела: у каждого вида заполнены свои поля, остальных в ответе нет. */
export type Item = {
  id: string
  kind: ItemKind
  institution?: string
  program?: string
  organization?: string
  position?: string
  title?: string
  authors?: string
  venue?: string
  pub_type?: string
  doi?: string
  url?: string
  volume?: string
  issue?: string
  pages?: string
  source?: string
  funder?: string
  role?: string
  number?: string
  patent_type?: string
  office?: string
  course?: string
  level?: string
  description?: string
  year?: number
  year_from?: number
  year_to?: number
}

export type Profile = {
  id: string
  name: string
  /** Только у владельца. */
  visibility?: Visibility
  open_to_offers: boolean
  headline: string
  city: string
  region: Code | null
  about: string
  degree: { level: string; specialty: Code | null; year: number | null; institution: string; dissertation: string }
  academic_title: string
  academic_title_year: number | null
  identifiers: { orcid: string; spin: string; scopus_id: string; wos_id: string }
  h_index: { rsci: number | null; scopus: number | null; wos: number | null; scholar: number | null }
  specialties: Code[]
  /** Только владельцу и сотрудникам организаций. */
  contact_email?: string
  sections: {
    education: Item[]
    experience: Item[]
    publications: Item[]
    grants: Item[]
    patents: Item[]
    teaching: Item[]
  }
  updated_at: string
}

export type ProfilePage = { profile: Profile; viewer: { is_owner: boolean; can_see_contacts: boolean } }

/** Основные поля, как их принимает сервер. */
export type CoreFields = {
  headline: string
  city: string
  region_code: string
  about: string
  degree: string
  degree_specialty_code: string
  degree_year: number | null
  degree_institution: string
  dissertation_title: string
  academic_title: string
  academic_title_year: number | null
  orcid: string
  spin: string
  scopus_id: string
  wos_id: string
  h_rsci: number | null
  h_scopus: number | null
  h_wos: number | null
  h_scholar: number | null
  contact_email: string
  specialties: string[]
}

export type Work = {
  doi: string
  title: string
  authors: string
  venue: string
  year?: number
  type: string
  volume: string
  issue: string
  pages: string
}

/** Раздел профиля на странице → вид записи (в ответе разделы названы во множественном числе). */
export const sectionOf: Record<ItemKind, keyof Profile['sections']> = {
  education: 'education',
  experience: 'experience',
  publication: 'publications',
  grant: 'grants',
  patent: 'patents',
  teaching: 'teaching',
}

// Что видно, зависит от того, кто смотрит: кеш лежит под ключом с номером человека.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  own: (userId: string | null | undefined) => ['profile', 'own', who(userId)] as const,
  one: (id: string, userId: string | null | undefined) => ['profile', 'scientist', id, who(userId)] as const,
}

export function useOwnProfile() {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.own(user?.id),
    queryFn: ({ signal }) => apiGet<ProfilePage>('/api/profile', { signal }),
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

export function useScientist(id: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.one(id, user?.id),
    queryFn: ({ signal }) => apiGet<ProfilePage>(`/api/scientists/${encodeURIComponent(id)}`, { signal }),
    enabled: user !== undefined,
    retry: false,
  })
}

// ---- действия ----

export const saveCore = (fields: CoreFields) => apiSend<ProfilePage>('PUT', '/api/profile', fields)

export const savePrivacy = (visibility: Visibility, openToOffers: boolean) =>
  apiSend<ProfilePage>('PUT', '/api/profile/privacy', { visibility, open_to_offers: openToOffers })

export async function addItem(kind: ItemKind, fields: Record<string, unknown>): Promise<Item> {
  return (await apiSend<{ item: Item }>('POST', '/api/profile/items', { kind, ...fields })).item
}

export async function updateItem(id: string, kind: ItemKind, fields: Record<string, unknown>): Promise<Item> {
  return (await apiSend<{ item: Item }>('PUT', `/api/profile/items/${id}`, { kind, ...fields })).item
}

export const deleteItem = (id: string) => apiSend('DELETE', `/api/profile/items/${id}`)

export async function lookupDoi(doi: string): Promise<Work> {
  return (await apiGet<{ work: Work }>(`/api/profile/doi?doi=${encodeURIComponent(doi)}`)).work
}

/** Профиль изменился: своя страница и чужие просмотры перечитаются. */
export function refreshProfile(client: QueryClient) {
  return client.invalidateQueries({ queryKey: ['profile'] })
}

export const ownCvPath = '/api/profile/cv'
export const cvPath = (id: string) => `/api/scientists/${encodeURIComponent(id)}/cv`

import { useQuery, type QueryClient } from '@tanstack/react-query'
import { ApiError, apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'

export const orgKinds = ['university', 'institute', 'science_center', 'rd_company', 'technopark', 'other'] as const
export type OrgKind = (typeof orgKinds)[number]

export const unitKinds = ['department', 'laboratory', 'division', 'shared_facility'] as const
export type UnitKind = (typeof unitKinds)[number]

export const roles = ['owner', 'hr', 'unit_head'] as const
export type Role = (typeof roles)[number]

export type Organization = {
  id: string
  slug: string
  name: string
  kind: OrgKind
  city: string
  website: string
  description: string
  created_at: string
}

export type Unit = {
  id: string
  name: string
  kind: UnitKind
  description: string
  topics: string[]
  /** Имя руководителя; null, если не назначен. */
  head_name: string | null
  /** Номер аккаунта руководителя: сервер отдаёт его только сотрудникам организации. */
  head_user_id?: string | null
}

/** Что вошедший человек может в организации (пустая роль: вошёл, но не сотрудник). */
export type Viewer = {
  role: Role | ''
  can_edit_organization: boolean
  can_manage_members: boolean
  can_manage_units: boolean
  editable_units: string[]
}

export type OrgView = { organization: Organization; units: Unit[]; viewer: Viewer | null }

export type UnitView = {
  organization: { slug: string; name: string; kind: OrgKind; city: string }
  unit: Unit
  viewer: Viewer | null
}

export type OrgSummary = {
  id: string
  slug: string
  name: string
  kind: OrgKind
  city: string
  summary: string
  unit_count: number
}

export type OrgList = { items: OrgSummary[]; total: number }

export type MyOrganization = { id: string; slug: string; name: string; kind: OrgKind; city: string; role: Role }

export type MyInvitation = {
  id: string
  organization: { slug: string; name: string }
  role: Role
  unit_name: string | null
  expires_at: string
}

export type Member = { user_id: string; name: string; email: string; role: Role; joined_at: string }

export type PendingInvitation = {
  id: string
  email: string
  role: Role
  unit_id: string | null
  unit_name: string | null
  created_at: string
  expires_at: string
}

export type MembersView = { members: Member[]; invitations: PendingInvitation[] }

export type InvitationPreview = {
  organization: { slug: string; name: string }
  role: Role
  unit_name: string | null
  email: string
  /** Совпадает ли почта приглашения с почтой вошедшего; null, если никто не вошёл. */
  email_matches: boolean | null
}

export type Joined = { slug: string; name: string; role: Role }

export const PAGE_SIZE = 20

export type CatalogParams = { q: string; kind: string; page: number }

// Данные, зависящие от того, кто смотрит (права, номера руководителей), лежат в кеше под ключом с номером человека:
// после входа или выхода страница не покажет чужие права из старого кеша.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  catalog: (p: CatalogParams) => ['organizations', 'catalog', p] as const,
  org: (slug: string, userId: string | null | undefined) => ['organization', slug, who(userId)] as const,
  orgAll: (slug: string) => ['organization', slug] as const,
  unit: (slug: string, id: string, userId: string | null | undefined) => ['organization', slug, who(userId), 'unit', id] as const,
  members: (slug: string) => ['members', slug] as const,
  mine: ['my-organizations'] as const,
  invitation: (token: string, userId: string | null | undefined) => ['invitation', token, who(userId)] as const,
}

export function useCatalog(p: CatalogParams) {
  return useQuery({
    queryKey: keys.catalog(p),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((p.page - 1) * PAGE_SIZE) })
      if (p.q) qs.set('q', p.q)
      if (p.kind) qs.set('kind', p.kind)
      return apiGet<OrgList>(`/api/organizations?${qs}`, { signal })
    },
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Страница организации. Ждёт, пока станет известно, кто смотрит. */
export function useOrganization(slug: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.org(slug, user?.id),
    queryFn: ({ signal }) => apiGet<OrgView>(`/api/organizations/${encodeURIComponent(slug)}`, { signal }),
    enabled: user !== undefined,
    retry: false,
  })
}

export function useUnit(slug: string, id: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.unit(slug, id, user?.id),
    queryFn: ({ signal }) => apiGet<UnitView>(`/api/organizations/${encodeURIComponent(slug)}/units/${encodeURIComponent(id)}`, { signal }),
    enabled: user !== undefined,
    retry: false,
  })
}

export function useMembers(slug: string, enabled = true) {
  return useQuery({
    queryKey: keys.members(slug),
    queryFn: ({ signal }) => apiGet<MembersView>(`/api/organizations/${encodeURIComponent(slug)}/members`, { signal }),
    enabled,
    retry: false,
  })
}

export function useMine(enabled: boolean) {
  return useQuery({
    queryKey: keys.mine,
    queryFn: ({ signal }) => apiGet<{ organizations: MyOrganization[]; invitations: MyInvitation[] }>('/api/my/organizations', { signal }),
    enabled,
    retry: false,
  })
}

export function useInvitation(token: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.invitation(token, user?.id),
    enabled: token !== '' && user !== undefined,
    retry: false,
    // Страница приглашения читает ссылку, не принимая её; кеш не нужен, пусть всегда спрашивает заново.
    staleTime: 0,
    gcTime: 0,
    queryFn: () => apiSend<InvitationPreview>('POST', '/api/invitations/lookup', { token }),
  })
}

// ---- действия ----

export type OrgFields = { name: string; kind: string; city: string; website: string; description: string }
export type UnitFields = { name: string; kind: string; description: string; topics: string[] }

const org = (slug: string) => `/api/organizations/${encodeURIComponent(slug)}`

export async function createOrganization(fields: OrgFields): Promise<Organization> {
  const res = await apiSend<{ organization: Organization }>('POST', '/api/organizations', fields)
  return res.organization
}

export async function updateOrganization(slug: string, fields: OrgFields): Promise<Organization> {
  const res = await apiSend<{ organization: Organization }>('PATCH', org(slug), fields)
  return res.organization
}

export async function createUnit(slug: string, fields: UnitFields): Promise<Unit> {
  const res = await apiSend<{ unit: Unit }>('POST', `${org(slug)}/units`, fields)
  return res.unit
}

export async function updateUnit(slug: string, id: string, fields: UnitFields): Promise<Unit> {
  const res = await apiSend<{ unit: Unit }>('PATCH', `${org(slug)}/units/${id}`, fields)
  return res.unit
}

export async function setUnitHead(slug: string, id: string, userId: string | null): Promise<Unit> {
  const res = await apiSend<{ unit: Unit }>('PUT', `${org(slug)}/units/${id}/head`, { user_id: userId })
  return res.unit
}

export function deleteUnit(slug: string, id: string) {
  return apiSend('DELETE', `${org(slug)}/units/${id}`)
}

export function changeRole(slug: string, userId: string, role: string) {
  return apiSend('PATCH', `${org(slug)}/members/${userId}`, { role })
}

export function removeMember(slug: string, userId: string) {
  return apiSend('DELETE', `${org(slug)}/members/${userId}`)
}

export async function invite(slug: string, input: { email: string; role: string; unit_id: string | null }): Promise<PendingInvitation> {
  const res = await apiSend<{ invitation: PendingInvitation }>('POST', `${org(slug)}/invitations`, input)
  return res.invitation
}

export function revokeInvitation(slug: string, id: string) {
  return apiSend('DELETE', `${org(slug)}/invitations/${id}`)
}

export function acceptInvitation(token: string) {
  return apiSend<Joined>('POST', '/api/invitations/accept', { token })
}

export function acceptInvitationById(id: string) {
  return apiSend<Joined>('POST', `/api/invitations/${id}/accept`)
}

/** Данные организации поменялись: страницы, каталог и «мои организации» перечитаются. */
export function refreshOrganizations(client: QueryClient, slug: string) {
  return Promise.all([
    client.invalidateQueries({ queryKey: ['organizations'] }),
    client.invalidateQueries({ queryKey: keys.mine }),
    client.invalidateQueries({ queryKey: keys.orgAll(slug) }),
    client.invalidateQueries({ queryKey: keys.members(slug) }),
  ])
}

/** Сервер ответил «такой страницы нет». */
export const isNotFound = (error: unknown) => error instanceof ApiError && error.code === 'not_found'

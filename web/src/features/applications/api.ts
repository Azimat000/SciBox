import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend, apiSendForm } from '../../api/client'
import { useMe } from '../auth/api'
import type { Profile } from '../profile/api'

export const statuses = ['sent', 'viewed', 'invited', 'rejected', 'accepted', 'withdrawn'] as const
export type AppStatus = (typeof statuses)[number]

export const PAGE_SIZE = 20

export const MAX_FILES = 5
export const MAX_REFEREES = 3
/** Самый большой файл, байт (то же число проверяет сервер). */
export const MAX_FILE_BYTES = 10 * 1024 * 1024

export type VacancyRef = {
  id: string
  title: string
  status: string
  org_name: string
  org_slug: string
  deadline: string | null
}

export type FileRef = { id: string; name: string; size: number }

export type Summary = {
  id: string
  status: AppStatus
  created_at: string
  status_changed_at: string
  vacancy: VacancyRef
  references: { total: number; received: number }
  /** Приглашения, которые ждут ответа соискателя. */
  pending_invitations: number
}

export type AppList = { items: Summary[]; total: number }

export type RefStatus = 'pending' | 'received' | 'declined'

/** Просьба о рекомендации глазами соискателя: письма здесь нет. */
export type Reference = {
  id: string
  name: string
  email: string
  relation: string
  status: RefStatus
  created_at: string
  expires_at: string
  last_sent_at: string
  answered_at: string | null
  can_resend: boolean
  resend_at: string | null
}

/** То же глазами организации, с письмом. */
export type StaffReference = Reference & { letter: { text: string; file: FileRef | null } | null }

export const invitationKinds = ['interview', 'contacts', 'request_contacts'] as const
export type InvitationKind = (typeof invitationKinds)[number]
export type InvitationStatus = 'pending' | 'confirmed' | 'proposed' | 'answered' | 'shared' | 'cancelled'
export type PlaceKind = 'online' | 'onsite'

/** Ответ соискателя на приглашение. */
export type InvitationAnswer = { proposed_at: string | null; note: string; contact: string; time: string; answered_at: string }

/** Приглашение: обе стороны видят его целиком, флаги can_* зависят от того, кто смотрит. */
export type Invitation = {
  id: string
  kind: InvitationKind
  status: InvitationStatus
  message: string
  starts_at: string | null
  place_kind: PlaceKind | ''
  place: string
  contact_name: string
  contact_email: string
  contact_phone: string
  answer: InvitationAnswer | null
  created_at: string
  can_answer: boolean
  can_cancel: boolean
  can_accept_proposal: boolean
}

export type Detail = {
  id: string
  status: AppStatus
  created_at: string
  status_changed_at: string
  vacancy: VacancyRef
  applicant_name: string
  contact_email: string
  cover_letter: string
  profile: Profile
  cv: FileRef | null
  files: FileRef[]
  decision_note: string
  invitations: Invitation[]
  viewer: { role: 'applicant' | 'staff'; can_withdraw: boolean; decisions: ('accepted' | 'rejected')[]; can_invite: boolean }
  references: Reference[] | StaffReference[]
}

/** Отклик в списке организации. */
export type Candidate = {
  id: string
  status: AppStatus
  created_at: string
  status_changed_at: string
  applicant_name: string
  headline: string
  vacancy: VacancyRef
  unit_name: string
  references: { total: number; received: number }
  pending_invitations: number
  proposed_invitations: number
}

export type CandidateList = { items: Candidate[]; total: number; counts: Record<AppStatus, number> }

/** Вакансия с числом откликов (без отозванных) и числом непросмотренных. */
export type VacancyCount = { id: string; title: string; status: string; org_name: string; org_slug: string; total: number; new: number }

export type CandidateQuery = { vacancy: string; status: string; page: number }

export type ApplyState = {
  can_apply: boolean
  reason?: 'own_vacancy' | 'closed' | 'deadline_passed' | 'applied'
  application: { id: string; status: AppStatus } | null
}

export type RefereeFields = { name: string; email: string; relation: string }

export type ApplyFields = {
  vacancy_id: string
  contact_email: string
  cover_letter: string
  referees: RefereeFields[]
}

// Что видно, зависит от того, кто смотрит: кеш лежит под ключом с номером человека.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  all: ['applications'] as const,
  mine: (userId: string | null | undefined, page: number) => ['applications', 'mine', who(userId), page] as const,
  one: (userId: string | null | undefined, id: string) => ['applications', 'one', who(userId), id] as const,
  state: (userId: string | null | undefined, vacancyId: string) => ['applications', 'state', who(userId), vacancyId] as const,
  candidates: (userId: string | null | undefined, q: CandidateQuery) => ['applications', 'candidates', who(userId), q.vacancy, q.status, q.page] as const,
  candidateVacancies: (userId: string | null | undefined) => ['applications', 'candidate-vacancies', who(userId)] as const,
}

export function useMyApplications(page: number) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.mine(user?.id, page),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) })
      return apiGet<AppList>(`/api/applications?${qs}`, { signal })
    },
    enabled: user !== undefined && user !== null,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Отклики на вакансии, которые человек разбирает: с отбором по вакансии и статусу. */
export function useCandidates(q: CandidateQuery) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.candidates(user?.id, q),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((q.page - 1) * PAGE_SIZE) })
      if (q.vacancy) qs.set('vacancy', q.vacancy)
      if (q.status) qs.set('status', q.status)
      return apiGet<CandidateList>(`/api/my/candidates?${qs}`, { signal })
    },
    enabled: user !== undefined && user !== null,
    placeholderData: (previous) => previous,
    retry: false,
  })
}

/** Вакансии с откликами: для отбора на странице откликов и для чисел в «Моих вакансиях». */
export function useCandidateVacancies() {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.candidateVacancies(user?.id),
    queryFn: async ({ signal }) => (await apiGet<{ items: VacancyCount[] }>('/api/my/candidate-vacancies', { signal })).items,
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

export function useApplication(id: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.one(user?.id, id),
    queryFn: async ({ signal }) => (await apiGet<{ application: Detail }>(`/api/applications/${encodeURIComponent(id)}`, { signal })).application,
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

/** Можно ли вошедшему откликнуться на вакансию; без входа не спрашивается. */
export function useApplyState(vacancyId: string) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.state(user?.id, vacancyId),
    queryFn: ({ signal }) => apiGet<ApplyState>(`/api/applications/for-vacancy/${encodeURIComponent(vacancyId)}`, { signal }),
    enabled: user !== undefined && user !== null,
    retry: false,
  })
}

// ---- действия ----

export async function sendApplication(fields: ApplyFields, files: readonly File[]): Promise<Detail> {
  const res = await apiSendForm<{ application: Detail }>('POST', '/api/applications', fields, files)
  return res.application
}

export function withdrawApplication(id: string) {
  return apiSend('POST', `/api/applications/${id}/withdraw`)
}

export async function addReferee(appId: string, fields: RefereeFields): Promise<Reference> {
  const res = await apiSend<{ reference: Reference }>('POST', `/api/applications/${appId}/references`, fields)
  return res.reference
}

export async function resendReferee(appId: string, refId: string): Promise<Reference> {
  const res = await apiSend<{ reference: Reference }>('POST', `/api/applications/${appId}/references/${refId}/resend`)
  return res.reference
}

export function cancelReferee(appId: string, refId: string) {
  return apiSend('DELETE', `/api/applications/${appId}/references/${refId}`)
}

// ---- разбор откликов ----

/** Решение организации: принят или отказ, с запиской, которую увидит соискатель. */
export function decideApplication(id: string, status: 'accepted' | 'rejected', note: string) {
  return apiSend('POST', `/api/applications/${id}/status`, { status, note })
}

/** Поля приглашения; лишнее для вида сервер отбрасывает. starts_at — момент со смещением +03:00. */
export type InvitationFields = {
  kind: InvitationKind
  message: string
  starts_at?: string
  place_kind?: PlaceKind
  place?: string
  contact_name?: string
  contact_email?: string
  contact_phone?: string
}

export async function sendInvitation(appId: string, fields: InvitationFields): Promise<Invitation> {
  const res = await apiSend<{ invitation: Invitation }>('POST', `/api/applications/${appId}/invitations`, fields)
  return res.invitation
}

export function cancelInvitation(appId: string, invId: string) {
  return apiSend('DELETE', `/api/applications/${appId}/invitations/${invId}`)
}

export function acceptProposal(appId: string, invId: string) {
  return apiSend('POST', `/api/applications/${appId}/invitations/${invId}/accept-proposal`)
}

export type AnswerFields = { action: 'confirm' | 'propose' | 'reply'; proposed_at?: string; note?: string; contact?: string; time?: string }

export function answerInvitation(appId: string, invId: string, fields: AnswerFields) {
  return apiSend('POST', `/api/applications/${appId}/invitations/${invId}/answer`, fields)
}

/** Отклик изменился: карточка, список и состояние кнопки на вакансии перечитаются. */
export function refreshApplications(client: QueryClient) {
  return client.invalidateQueries({ queryKey: keys.all })
}

/** Адрес файла отклика (скачивается как PDF). */
export const fileUrl = (appId: string, fileId: string) => `/api/applications/${appId}/files/${fileId}`

// ---- страница рекомендателя (без входа) ----

export type RecommendInfo = {
  referee_name: string
  relation: string
  applicant_name: string
  vacancy_title: string
  org_name: string
  status: RefStatus
  expires_at: string
}

export async function lookupRecommendation(token: string, signal?: AbortSignal): Promise<RecommendInfo> {
  void signal
  const res = await apiSend<{ request: RecommendInfo }>('POST', '/api/recommendations/lookup', { token })
  return res.request
}

export function submitRecommendation(token: string, text: string, file: File | null) {
  return apiSendForm('POST', '/api/recommendations/submit', { token, text }, file ? [file] : [])
}

export function declineRecommendation(token: string) {
  return apiSend('POST', '/api/recommendations/decline', { token })
}

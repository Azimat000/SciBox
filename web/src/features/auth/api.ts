import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiDownload, apiGet, apiSend } from '../../api/client'

/** Человек, как его отдаёт сервер. */
export type User = {
  id: string
  email: string
  name: string
  email_confirmed: boolean
  created_at: string
  /** Когда аккаунт будет удалён (ISO); null — удаление не назначено. */
  deletion_due_at?: string | null
}

type UserResponse = { user: User | null }

export const meQueryKey = ['me'] as const

/** Кто сейчас вошёл: null, если никто. */
export async function fetchMe(signal?: AbortSignal): Promise<User | null> {
  const res = await apiGet<UserResponse>('/api/auth/me', { signal })
  return res.user ?? null
}

/** Текущий человек. user === undefined, пока ответ не пришёл; null, если не вошёл. */
export function useMe() {
  const query = useQuery({
    queryKey: meQueryKey,
    queryFn: ({ signal }) => fetchMe(signal),
    retry: false,
    staleTime: 60_000,
  })
  return { user: query.data, isLoading: query.isPending, isError: query.isError, refetch: query.refetch }
}

/**
 * Запоминает, кто вошёл (или что никого нет), без лишнего запроса.
 * Сначала отменяет «кто я?», который ещё в пути: он мог уйти до входа, и его запоздавший ответ «никого»
 * затёр бы только что полученного человека.
 */
export async function setMe(client: QueryClient, user: User | null) {
  await client.cancelQueries({ queryKey: meQueryKey })
  client.setQueryData(meQueryKey, user)
}

export type RegisterInput = { name: string; email: string; password: string; consent: boolean }

export function register(input: RegisterInput) {
  return apiSend<{ email: string }>('POST', '/api/auth/register', input)
}

export async function confirmEmail(token: string): Promise<User> {
  const res = await apiSend<UserResponse>('POST', '/api/auth/confirm-email', { token })
  return res.user as User
}

export function resendConfirmation(email: string) {
  return apiSend('POST', '/api/auth/resend-confirmation', { email })
}

export async function login(email: string, password: string): Promise<User> {
  const res = await apiSend<UserResponse>('POST', '/api/auth/login', { email, password })
  return res.user as User
}

export function logout() {
  return apiSend('POST', '/api/auth/logout')
}

export function forgotPassword(email: string) {
  return apiSend('POST', '/api/auth/forgot-password', { email })
}

export function resetPassword(token: string, password: string) {
  return apiSend('POST', '/api/auth/reset-password', { token, password })
}

export async function updateName(name: string): Promise<User> {
  const res = await apiSend<UserResponse>('PATCH', '/api/account', { name })
  return res.user as User
}

export function changePassword(currentPassword: string, newPassword: string) {
  return apiSend('POST', '/api/account/password', { current_password: currentPassword, new_password: newPassword })
}

export function revokeOtherSessions() {
  return apiSend('POST', '/api/account/sessions/revoke-others')
}

/** Ввести пароль ещё раз: следующие 10 минут в этом сеансе разрешены опасные действия. */
export function reauth(password: string) {
  return apiSend('POST', '/api/account/reauth', { password })
}

/** Сеанс: устройство по строке браузера (коды), когда вошли и когда заходили. */
export type SessionInfo = {
  id: string
  browser: string
  os: string
  mobile: boolean
  created_at: string
  last_seen_at: string
  current: boolean
}

export const sessionsQueryKey = ['account', 'sessions'] as const

export function useSessions() {
  return useQuery({
    queryKey: sessionsQueryKey,
    retry: false,
    queryFn: async ({ signal }) => (await apiGet<{ sessions: SessionInfo[] }>('/api/account/sessions', { signal })).sessions,
  })
}

export function endSession(id: string) {
  return apiSend('DELETE', `/api/account/sessions/${encodeURIComponent(id)}`)
}

export function requestEmailChange(email: string) {
  return apiSend('POST', '/api/account/email', { email })
}

export async function confirmEmailChange(token: string): Promise<User> {
  const res = await apiSend<UserResponse>('POST', '/api/auth/confirm-email-change', { token })
  return res.user as User
}

export function revertEmailChange(token: string) {
  return apiSend('POST', '/api/auth/revert-email-change', { token })
}

export type OrgRef = { slug: string; name: string }

/** Что будет при удалении: организации, где нужно сначала назначить владельца, и те, что останутся без владельца. */
export type DeletionPlan = { due_at: string | null; grace_days: number; blocking: OrgRef[]; orphaned: OrgRef[] }

export const deletionPlanQueryKey = ['account', 'deletion'] as const

export function useDeletionPlan() {
  return useQuery({
    queryKey: deletionPlanQueryKey,
    retry: false,
    queryFn: ({ signal }) => apiGet<DeletionPlan>('/api/account/deletion', { signal }),
  })
}

export async function requestDeletion(): Promise<string> {
  return (await apiSend<{ due_at: string }>('POST', '/api/account/deletion')).due_at
}

export function cancelDeletion() {
  return apiSend('POST', '/api/account/deletion/cancel')
}

/** Архив со всеми данными человека (ZIP): сервер просит свежий пароль и пускает не больше трёх раз в сутки. */
export function downloadMyData() {
  return apiDownload('/api/account/export')
}

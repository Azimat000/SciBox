import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'

/** Человек, как его отдаёт сервер. */
export type User = {
  id: string
  email: string
  name: string
  email_confirmed: boolean
  created_at: string
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

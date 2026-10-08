import { createContext, useContext } from 'react'

/** Выполняет действие; если сервер просит пароль ещё раз (reauth_required), спрашивает его и повторяет. */
export type ReauthGuard = <T>(action: () => Promise<T>) => Promise<T>

export const ReauthContext = createContext<ReauthGuard | null>(null)

const passThrough: ReauthGuard = (action) => action()

/**
 * Опасные действия (смена почты, удаление аккаунта, назначение владельца) сервер разрешает только вскоре после
 * ввода пароля. Оберните запрос: `guard(() => requestDeletion())`. Если человек закроет окно пароля, действие
 * завершится исходной ошибкой сервера («Для этого действия введите пароль ещё раз»).
 */
export function useReauth(): ReauthGuard {
  return useContext(ReauthContext) ?? passThrough
}

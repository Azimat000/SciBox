import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { ROLE_STORAGE_KEY, RoleContext, parseRole, type Role } from './role-context'

function readStored(): Role {
  try {
    return parseRole(window.localStorage.getItem(ROLE_STORAGE_KEY))
  } catch {
    // Хранилище закрыто (приватный режим): работаем без памяти между визитами
    return 'seeker'
  }
}

/** Режим «Ищу работу / Нанимаю» (D-004, D-038). Запоминается в браузере. */
export function RoleProvider({ children }: { children: ReactNode }) {
  const [role, setRoleState] = useState<Role>(readStored)

  const setRole = useCallback((next: Role) => {
    setRoleState(next)
    try {
      window.localStorage.setItem(ROLE_STORAGE_KEY, next)
    } catch {
      // см. readStored
    }
  }, [])

  const api = useMemo(() => ({ role, setRole }), [role, setRole])
  return <RoleContext.Provider value={api}>{children}</RoleContext.Provider>
}

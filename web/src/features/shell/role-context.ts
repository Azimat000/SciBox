import { createContext } from 'react'

export type Role = 'seeker' | 'employer'

export type RoleApi = { role: Role; setRole: (role: Role) => void }

export const ROLE_STORAGE_KEY = 'scibox.role'

export const RoleContext = createContext<RoleApi | null>(null)

export function parseRole(value: string | null): Role {
  return value === 'employer' ? 'employer' : 'seeker'
}

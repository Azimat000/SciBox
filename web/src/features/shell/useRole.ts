import { useContext } from 'react'
import { RoleContext, type RoleApi } from './role-context'

export function useRole(): RoleApi {
  const api = useContext(RoleContext)
  if (!api) throw new Error('useRole нужно вызывать внутри RoleProvider')
  return api
}

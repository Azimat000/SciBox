import { useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export type Health = {
  status: 'ok'
  product: string
  version: string
  database: { schema_version: number; server_version: string }
}

export const healthQueryKey = ['health'] as const

export function useHealth() {
  return useQuery({
    queryKey: healthQueryKey,
    queryFn: ({ signal }) => apiGet<Health>('/api/health', { signal }),
    retry: false,
  })
}

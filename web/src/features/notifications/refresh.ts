import type { QueryClient } from '@tanstack/react-query'
import { keys } from './api'

/** Появились новые уведомления (или прочитаны старые): колокольчик и список перечитаются. */
export function refreshNotifications(client: QueryClient) {
  return client.invalidateQueries({ queryKey: keys.all })
}

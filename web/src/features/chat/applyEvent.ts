import type { QueryClient } from '@tanstack/react-query'
import { keys, type ChatHeader, type ChatStreamEvent, type UnreadCount } from './api'

/** Новое событие меняет списки и значок: кэш правится сразу, перечитывание списков собирается в один запрос. */
export function applyEvent(client: QueryClient, userId: string, e: ChatStreamEvent, schedule: () => void) {
  switch (e.type) {
    case 'unread.count': {
      const counts: UnreadCount = { total: e.total, scientist: e.scientist, org: e.org }
      client.setQueryData(keys.unread(userId), counts)
      schedule()
      break
    }
    case 'chat.read':
      client.setQueryData<ChatHeader>(keys.one(userId, e.chat_id), (h) => {
        if (!h) return h
        // reader_side совпадает со стороной смотрящего: он сам прочитал в другой вкладке
        const field = e.reader_side === h.side ? 'mine' : 'theirs'
        return h.read[field] >= e.up_to ? h : { ...h, read: { ...h.read, [field]: e.up_to } }
      })
      break
    case 'chat.state':
      client.setQueryData<ChatHeader>(keys.one(userId, e.chat_id), (h) =>
        h ? { ...h, status: e.status, needs_answer: e.needs_answer, can_write: e.can_write, closed_reason: e.closed_reason } : h,
      )
      schedule()
      break
    case 'message.created':
    case 'message.edited':
    case 'message.deleted':
      schedule()
      break
    default:
      break
  }
}


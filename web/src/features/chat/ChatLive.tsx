import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useMe } from '../auth/api'
import { applyEvent } from './applyEvent'
import { chatHub } from './hub'
import { runStream } from './live'

const INVALIDATE_DELAY_MS = 300

/**
 * Один живой поток на сайт для вошедшего человека (срез 35). Правит кэш (значок, списки, шапка переписки) и раздаёт
 * события открытым окнам через chatHub. Ничего не рисует.
 */
export function ChatLive() {
  const { user } = useMe()
  const client = useQueryClient()
  const userId = user?.id ?? null

  useEffect(() => {
    if (userId === null) return
    const ctl = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const schedule = () => {
      if (timer !== undefined) return
      timer = setTimeout(() => {
        timer = undefined
        void client.invalidateQueries({ queryKey: ['chats', 'list'] })
        void client.invalidateQueries({ queryKey: ['chats', 'vacancies'] })
      }, INVALIDATE_DELAY_MS)
    }
    void runStream({
      signal: ctl.signal,
      onEvent: (e) => {
        applyEvent(client, userId, e, schedule)
        chatHub.emit(e)
      },
      onConnected: () => chatHub.emit({ type: 'reconnected' }),
    })
    return () => {
      ctl.abort()
      clearTimeout(timer)
    }
  }, [client, userId])

  return null
}

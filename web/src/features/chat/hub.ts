import { useEffect, useRef } from 'react'
import type { ChatStreamEvent } from './api'

// Раздача событий живого потока окнам переписок (срез 35). Поток открывает ChatLive один на сайт, окно подписывается здесь.

type Listener = (e: ChatStreamEvent) => void

const listeners = new Set<Listener>()

export const chatHub = {
  emit(e: ChatStreamEvent) {
    for (const l of [...listeners]) l(e)
  },
  subscribe(l: Listener): () => void {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
}

/** Подписка на время жизни компонента; обработчик можно менять между отрисовками. */
export function useChatEvents(handler: Listener) {
  const ref = useRef(handler)
  useEffect(() => {
    ref.current = handler
  })
  useEffect(() => chatHub.subscribe((e) => ref.current(e)), [])
}

import { useCallback, useEffect, useReducer, useRef } from 'react'
import { describeError } from '../auth/errors'
import { deleteMessage, editMessage, fetchMessages, sendMessage, type Message } from './api'
import { convReducer, initialConv, newClientId, type ConvAction, type ConvState, type PendingMessage } from './conversation'
import { useChatEvents } from './hub'

/**
 * Лента одной переписки: последние сообщения при открытии, подгрузка вверх, свои неподтверждённые, события потока.
 * readAtOpen — докуда прочитано в момент открытия (от него линия «Новые сообщения»).
 */
export function useConversation(chatId: string, readAtOpen: number, hooks: { onIncoming?: (m: Message) => void; onSendFailed?: (error: unknown) => void } = {}) {
  const [state, dispatchRaw] = useReducer(convReducer, readAtOpen, initialConv)
  // Копия состояния, всегда свежая: обработчики потока и дочитывание решают по ней, а не по последней отрисовке.
  // Редьюсер чистый, поэтому применить его здесь и в React одинаково безопасно.
  const stateRef = useRef<ConvState>(initialConv(readAtOpen))
  const dispatch = useCallback((action: ConvAction) => {
    stateRef.current = convReducer(stateRef.current, action)
    dispatchRaw(action)
  }, [])
  const hooksRef = useRef(hooks)
  useEffect(() => {
    hooksRef.current = hooks
  })
  // События, пришедшие, пока лента грузилась: применяются после загрузки.
  const buffered = useRef<Message[]>([])
  const connectedWhileLoading = useRef(false)
  const [retryToken, bump] = useReducer((n: number) => n + 1, 0)
  const reload = useCallback(() => {
    dispatch({ type: 'reload' })
    bump()
  }, [dispatch])

  const catchUp = useCallback(async () => {
    // Пока есть новее последнего известного: дочитываем страницами
    for (let guard = 0; guard < 20; guard += 1) {
      const items = stateRef.current.items
      const last = items.length > 0 ? items[items.length - 1].seq : 0
      const page = await fetchMessages(chatId, { after: last })
      if (page.items.length > 0) dispatch({ type: 'merge', items: page.items })
      if (!page.has_more || page.items.length === 0) return
    }
  }, [chatId, dispatch])

  useEffect(() => {
    const ctl = new AbortController()
    fetchMessages(chatId, {}, ctl.signal)
      .then(async (page) => {
        dispatch({ type: 'loaded', items: page.items, hasMore: page.has_more })
        if (buffered.current.length > 0) dispatch({ type: 'merge', items: buffered.current.splice(0) })
        if (connectedWhileLoading.current) {
          connectedWhileLoading.current = false
          await catchUp().catch(() => undefined)
        }
      })
      .catch((error: unknown) => {
        if (ctl.signal.aborted) return
        dispatch({ type: 'failed', error })
      })
    return () => ctl.abort()
  }, [chatId, retryToken, catchUp, dispatch])

  useChatEvents((e) => {
    if (e.type === 'reconnected') {
      if (stateRef.current.status === 'ready') void catchUp().catch(() => undefined)
      else connectedWhileLoading.current = true
      return
    }
    if (e.type !== 'message.created' && e.type !== 'message.edited' && e.type !== 'message.deleted') return
    if (e.chat_id !== chatId) return
    if (stateRef.current.status !== 'ready') {
      buffered.current.push(e.message)
      return
    }
    const known = stateRef.current.items.some((m) => m.seq === e.message.seq)
    dispatch({ type: 'merge', items: [e.message] })
    if (e.type === 'message.created' && !known && !e.message.author?.is_me && e.message.kind === 'text') hooksRef.current.onIncoming?.(e.message)
  })

  const loadOlder = useCallback(async () => {
    const s = stateRef.current
    if (s.status !== 'ready' || !s.hasMore || s.olderLoading || s.items.length === 0) return
    dispatch({ type: 'olderStart' })
    try {
      const page = await fetchMessages(chatId, { before: s.items[0].seq })
      dispatch({ type: 'older', items: page.items, hasMore: page.has_more })
    } catch {
      dispatch({ type: 'olderFailed' })
    }
  }, [chatId, dispatch])

  const deliver = useCallback(
    async (text: string, clientId: string) => {
      try {
        const message = await sendMessage(chatId, text, clientId)
        dispatch({ type: 'merge', items: [message] })
        dispatch({ type: 'pendingDrop', clientId })
      } catch (error) {
        dispatch({ type: 'pendingState', clientId, status: 'failed', error: describeError(error) })
        hooksRef.current.onSendFailed?.(error)
      }
    },
    [chatId, dispatch],
  )

  /** Отправка: сообщение сразу в ленте как «отправляется», затем подтверждение сервера или «не отправлено». */
  const send = useCallback(
    (text: string) => {
      const clientId = newClientId()
      dispatch({ type: 'pendingAdd', pending: { client_id: clientId, text, created_at: new Date().toISOString(), status: 'sending' } })
      void deliver(text, clientId)
    },
    [deliver, dispatch],
  )

  const retry = useCallback(
    (p: PendingMessage) => {
      dispatch({ type: 'pendingState', clientId: p.client_id, status: 'sending' })
      void deliver(p.text, p.client_id)
    },
    [deliver, dispatch],
  )

  const discard = useCallback((clientId: string) => dispatch({ type: 'pendingDrop', clientId }), [dispatch])

  const edit = useCallback(
    async (seq: number, text: string) => {
      const message = await editMessage(chatId, seq, text)
      dispatch({ type: 'merge', items: [message] })
    },
    [chatId, dispatch],
  )

  const remove = useCallback(
    async (seq: number) => {
      await deleteMessage(chatId, seq)
      dispatch({ type: 'deleted', seq })
    },
    [chatId, dispatch],
  )

  return { state, reload, refresh: () => catchUp().catch(() => undefined), loadOlder, send, retry, discard, edit, remove }
}

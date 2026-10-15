import type { Message } from './api'

// Состояние ленты одной переписки и чистые правила вокруг неё (срез 35). Хук useConversation.ts только подключает это к сети.

/** Своё сообщение, которое ещё не подтвердил сервер. */
export type PendingMessage = {
  client_id: string
  text: string
  created_at: string
  status: 'sending' | 'failed'
  /** Почему не ушло (текст сервера), если он сказал. */
  error?: string
}

export type ConvState = {
  status: 'loading' | 'ready' | 'error'
  error: unknown
  /** Сообщения сервера по возрастанию seq, без дыр между первым и последним. */
  items: Message[]
  /** Есть ли старее первого. */
  hasMore: boolean
  olderLoading: boolean
  olderFailed: boolean
  pending: PendingMessage[]
  /** Докуда было прочитано, когда окно открыли: от него считается линия «Новые сообщения». */
  readAtOpen: number
}

export const initialConv = (readAtOpen: number): ConvState => ({
  status: 'loading',
  error: null,
  items: [],
  hasMore: false,
  olderLoading: false,
  olderFailed: false,
  pending: [],
  readAtOpen,
})

export type ConvAction =
  | { type: 'loaded'; items: Message[]; hasMore: boolean }
  | { type: 'failed'; error: unknown }
  | { type: 'reload' }
  | { type: 'olderStart' }
  | { type: 'olderFailed' }
  | { type: 'older'; items: Message[]; hasMore: boolean }
  | { type: 'merge'; items: Message[] }
  | { type: 'deleted'; seq: number }
  | { type: 'pendingAdd'; pending: PendingMessage }
  | { type: 'pendingState'; clientId: string; status: PendingMessage['status']; error?: string }
  | { type: 'pendingDrop'; clientId: string }

/**
 * Какая из двух версий одного сообщения новее: удалённое побеждает всё, иначе позднее изменение. Событие потока
 * и ответ на запрос могут прийти в любом порядке, старая версия не должна затереть новую.
 */
function newer(a: Message, b: Message): Message {
  if (a.deleted !== b.deleted) return a.deleted ? a : b
  const ta = a.edited_at ? Date.parse(a.edited_at) : 0
  const tb = b.edited_at ? Date.parse(b.edited_at) : 0
  return tb >= ta ? b : a
}

/** Склеивает сообщения по seq: повтор не даёт дубля, порядок по seq. */
export function mergeMessages(items: readonly Message[], incoming: readonly Message[]): Message[] {
  const bySeq = new Map<number, Message>()
  for (const m of items) bySeq.set(m.seq, m)
  for (const m of incoming) {
    const have = bySeq.get(m.seq)
    bySeq.set(m.seq, have ? newer(have, m) : m)
  }
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq)
}

export function convReducer(state: ConvState, action: ConvAction): ConvState {
  switch (action.type) {
    case 'loaded': {
      const items = mergeMessages(state.items, action.items)
      return { ...state, status: 'ready', error: null, items, hasMore: action.hasMore, pending: dropAcked(state.pending, items) }
    }
    case 'failed':
      return { ...state, status: 'error', error: action.error }
    case 'reload':
      return { ...state, status: 'loading', error: null }
    case 'olderStart':
      return { ...state, olderLoading: true, olderFailed: false }
    case 'olderFailed':
      return { ...state, olderLoading: false, olderFailed: true }
    case 'older':
      return { ...state, olderLoading: false, olderFailed: false, hasMore: action.hasMore, items: mergeMessages(state.items, action.items) }
    case 'merge': {
      const items = mergeMessages(state.items, action.items)
      return { ...state, items, pending: dropAcked(state.pending, items) }
    }
    case 'deleted':
      return { ...state, items: state.items.map((m) => (m.seq === action.seq ? { ...m, deleted: true, text: '', editable_until: null } : m)) }
    case 'pendingAdd':
      return { ...state, pending: [...state.pending.filter((p) => p.client_id !== action.pending.client_id), action.pending] }
    case 'pendingState':
      return { ...state, pending: state.pending.map((p) => (p.client_id === action.clientId ? { ...p, status: action.status, error: action.error } : p)) }
    case 'pendingDrop':
      return { ...state, pending: state.pending.filter((p) => p.client_id !== action.clientId) }
  }
}

/** Отправленное, что уже пришло с сервера (по client_id), из очереди убирается. */
function dropAcked(pending: PendingMessage[], items: readonly Message[]): PendingMessage[] {
  if (pending.length === 0) return pending
  const known = new Set(items.map((m) => m.client_id).filter((c): c is string => c !== null))
  const left = pending.filter((p) => !known.has(p.client_id))
  return left.length === pending.length ? pending : left
}

/**
 * Первое сообщение, пришедшее после того, как окно открыли с прочитанным до readAtOpen, и не своё: перед ним линия
 * «Новые сообщения». Системные строки считаются: это события по отклику, которые стоит заметить.
 */
export function firstUnreadSeq(items: readonly Message[], readAtOpen: number): number | null {
  const m = items.find((x) => x.seq > readAtOpen && !x.author?.is_me)
  return m ? m.seq : null
}

/** Своё сообщение можно исправить или удалить, пока не вышел срок (24 часа с отправки) и оно не удалено. */
export function canModify(m: Message, now: number): boolean {
  if (m.kind !== 'text' || m.deleted || !m.author?.is_me || m.editable_until === null) return false
  return now < Date.parse(m.editable_until)
}

/** Одна галочка «доставлено», две «прочитано» (номер прочитанного собеседником берётся из шапки и потока). */
export function delivery(m: Message, readTheirs: number): 'delivered' | 'read' {
  return m.seq <= readTheirs ? 'read' : 'delivered'
}

export type Row =
  | { kind: 'day'; key: string; label: string }
  | { kind: 'unread'; key: string }
  | { kind: 'message'; key: string; message: Message; showAuthor: boolean }
  | { kind: 'pending'; key: string; pending: PendingMessage }

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`

const MONTH_DAY = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' })
const FULL_DAY = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })

/** Подпись дня в ленте по часам человека: «Сегодня», «Вчера», «12 октября», «12 октября 2025 г.». */
export function dayLabel(iso: string, now: Date, labels: { today: string; yesterday: string }): string {
  const d = new Date(iso)
  if (dayKey(d) === dayKey(now)) return labels.today
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (dayKey(d) === dayKey(yesterday)) return labels.yesterday
  return d.getFullYear() === now.getFullYear() ? MONTH_DAY.format(d) : FULL_DAY.format(d)
}

const SAME_AUTHOR_GAP_MS = 5 * 60 * 1000

/** Подписывать ли автора: у первого сообщения подряд от одного человека (не позже 5 минут друг от друга, в один день). */
function startsRun(prev: Message | null, m: Message, sameDay: boolean): boolean {
  if (!prev || !sameDay || prev.kind !== 'text') return true
  const a = prev.author
  const b = m.author
  if (!a || !b || a.side !== b.side || a.name !== b.name || a.is_me !== b.is_me || a.former !== b.former) return true
  return Date.parse(m.created_at) - Date.parse(prev.created_at) > SAME_AUTHOR_GAP_MS
}

/** Строки ленты: разделители дней, линия непрочитанного, сообщения сервера и неподтверждённые свои. */
export function buildRows(state: Pick<ConvState, 'items' | 'pending' | 'readAtOpen'>, now: Date, labels: { today: string; yesterday: string }): Row[] {
  const rows: Row[] = []
  const unread = firstUnreadSeq(state.items, state.readAtOpen)
  let prev: Message | null = null
  let prevDay = ''
  for (const m of state.items) {
    const key = dayKey(new Date(m.created_at))
    const sameDay = key === prevDay
    if (!sameDay) rows.push({ kind: 'day', key: `day-${key}`, label: dayLabel(m.created_at, now, labels) })
    if (m.seq === unread) rows.push({ kind: 'unread', key: 'unread' })
    rows.push({ kind: 'message', key: m.id, message: m, showAuthor: startsRun(prev, m, sameDay) })
    prev = m
    prevDay = key
  }
  for (const p of state.pending) rows.push({ kind: 'pending', key: `pending-${p.client_id}`, pending: p })
  return rows
}

/** Время сообщения в ленте: «14:20». */
const TIME = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' })
export const messageTime = (iso: string) => TIME.format(new Date(iso))

/** Новый идентификатор отправки; без crypto.randomUUID (старый браузер, тест) собирается из случайных чисел. */
export function newClientId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const hex = (n: number) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('')
  return `${hex(8)}-${hex(4)}-4${hex(3)}-8${hex(3)}-${hex(12)}`
}

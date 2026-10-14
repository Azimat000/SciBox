import type { ChatHeader, ChatStatus, ClosedReason, Counterpart, Message, Side, Viewer } from '../api'

/** Сообщение в памяти подставного сервера: from — чья сторона его вызвала (у системной строки тоже), наружу не уходит. */
export type MockMessage = Message & { from: Side }

export type MockChat = {
  id: string
  side: Side
  vacancy: ChatHeader['vacancy']
  counterpart: Counterpart
  application_id: string | null
  offer_id: string | null
  status: ChatStatus
  needs_answer: boolean
  can_write: boolean
  closed_reason: ClosedReason | null
  archived: boolean
  viewer: Viewer
  messages: MockMessage[]
  read_mine: number
  read_theirs: number
  updated_at: string
  /** Чем переписка привязана к настоящему отклику или приглашению (подставляется в их ответы). */
  link?: string
}

export type MockTemplate = { id: string; title: string; text: string; updated_at: string }
export type MockBlock = { org_id: string; org_name: string; org_slug: string; created_at: string }
export type MockComplaint = { id: string; chat_id: string; reason: string; note: string; created_at: string }

export type MockState = {
  version: 1
  chats: MockChat[]
  templates: Record<string, MockTemplate[]>
  blocks: MockBlock[]
  complaints: MockComplaint[]
  email_messages: boolean
  next_id: number
}

export type StreamEvent = { id: number; event: string; data: Record<string, unknown> }

export type MockRequest = { method: string; url: URL; body: unknown; headers?: Headers }
export type MockReply = { status: number; body?: unknown; stream?: ReadableStream<Uint8Array> }

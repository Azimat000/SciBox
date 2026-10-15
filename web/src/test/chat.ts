// Подставные ответы чата для тестов сайта берутся прямо из контракта (contracts/chat, срез 34): разошлись с ним — тест увидит.
import chatFile from '../../../contracts/chat/chat.json'
import messagesFile from '../../../contracts/chat/messages.json'
import linksFile from '../../../contracts/chat/links.json'
import listFile from '../../../contracts/chat/list.json'
import unreadFile from '../../../contracts/chat/unread-count.json'
import vacanciesFile from '../../../contracts/chat/vacancies.json'
import type { ChatHeader, ChatList, ChatRef, ChatSummary, ChatVacancyOption, Message, MessagePage, UnreadCount } from '../features/chat/api'

const clone = <T>(v: unknown): T => structuredClone(v) as T

export const chatLists = {
  scientistAll: () => clone<ChatList>(listFile.examples.scientist_all.response),
  scientistUnread: () => clone<ChatList>(listFile.examples.scientist_unread.response),
  orgAll: () => clone<ChatList>(listFile.examples.org_all.response),
  orgArchive: () => clone<ChatList>(listFile.examples.org_archive.response),
  empty: () => clone<ChatList>(listFile.examples.empty.response),
}

export const chatHeaders = {
  scientistInvited: () => clone<{ chat: ChatHeader }>(chatFile.examples.scientist_invited.response).chat,
  scientistClosed: () => clone<{ chat: ChatHeader }>(chatFile.examples.scientist_closed.response).chat,
  orgAnonymousOffer: () => clone<{ chat: ChatHeader }>(chatFile.examples.org_anonymous_offer.response).chat,
  orgApplication: () => clone<{ chat: ChatHeader }>(chatFile.examples.org_offer_then_application.response).chat,
}

export const unreadCounts = {
  some: () => clone<UnreadCount>(unreadFile.examples.some.response),
  none: () => clone<UnreadCount>(unreadFile.examples.none.response),
}

export const chatVacancies = () => clone<{ items: ChatVacancyOption[] }>(vacanciesFile.examples.some.response)

export const chatRefs = {
  openWithUnread: () => clone<{ chat: ChatRef }>(linksFile.examples.open_with_unread.response).chat,
  notStarted: () => clone<{ chat: ChatRef }>(linksFile.examples.not_started.response).chat,
  closed: () => clone<{ chat: ChatRef }>(linksFile.examples.closed.response).chat,
}

/** Список из одной переписки с правками поверх примера. */
export const oneChat = (base: ChatSummary, over: Partial<ChatSummary> = {}): ChatList => ({ items: [{ ...base, ...over }], next_cursor: null, counts: { unread: 0, archive: 0 } })

/** Страницы ленты из примеров контракта и сборка своих сообщений для проверок окна. */
export const chatMessages = {
  scientistHistory: () => clone<MessagePage>(messagesFile.examples.scientist_history.response),
  orgSystemLines: () => clone<MessagePage>(messagesFile.examples.org_all_system_lines.response),
  nothingNewer: () => clone<MessagePage>(messagesFile.examples.nothing_newer.response),
}

const FAR = '2100-01-01T00:00:00Z'

export function textMessage(seq: number, over: Partial<Message> = {}): Message {
  return {
    id: `00000000-0000-4000-8000-${String(seq).padStart(12, '0')}`,
    seq,
    kind: 'text',
    author: { side: 'org', name: 'Елена Орлова', is_me: false, anonymous: false, former: false },
    text: `Сообщение ${seq}`,
    event: null,
    created_at: `2026-10-14T10:${String(seq % 60).padStart(2, '0')}:00Z`,
    edited_at: null,
    deleted: false,
    editable_until: null,
    client_id: null,
    ...over,
  }
}

/** Моё сообщение, которое ещё можно править. */
export const mineMessage = (seq: number, over: Partial<Message> = {}): Message =>
  textMessage(seq, { author: { side: 'scientist', name: 'Дмитрий Королёв', is_me: true, anonymous: false, former: false }, editable_until: FAR, ...over })

export function systemMessage(seq: number, type: string, data: Record<string, unknown> = {}, over: Partial<Message> = {}): Message {
  return { ...textMessage(seq), kind: 'system', author: null, text: '', event: { type, data }, ...over }
}

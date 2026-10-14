// Подставные ответы чата для тестов сайта берутся прямо из контракта (contracts/chat, срез 34): разошлись с ним — тест увидит.
import chatFile from '../../../contracts/chat/chat.json'
import linksFile from '../../../contracts/chat/links.json'
import listFile from '../../../contracts/chat/list.json'
import unreadFile from '../../../contracts/chat/unread-count.json'
import vacanciesFile from '../../../contracts/chat/vacancies.json'
import type { ChatHeader, ChatList, ChatRef, ChatSummary, ChatVacancyOption, UnreadCount } from '../features/chat/api'

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

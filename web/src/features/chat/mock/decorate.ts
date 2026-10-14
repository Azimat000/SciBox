import type { ChatHeader, ClosedReason, Side } from '../api'
import { orgViewer, scientistViewer } from './data'
import type { ChatServer } from './server'
import type { MockChat } from './types'

// До сервера чата (срез 38) настоящие ответы откликов и приглашений не знают поля chat. Подставной сервер дописывает его сам:
// находит или заводит переписку для записи и сверяет её состояние со статусом записи. Только добавляет поле, остальное не трогает.

type Json = Record<string, unknown>
type VacancyLike = { id: string; title: string; status?: string; org_name: string; org_slug: string; unit_name?: string; deadline?: string | null }

const closedBy: Record<string, ClosedReason> = {
  rejected: 'rejected',
  withdrawn: 'withdrawn',
  declined: 'offer_declined',
  cancelled: 'offer_cancelled',
}

function vacancyOf(v: VacancyLike): ChatHeader['vacancy'] {
  return {
    id: v.id,
    title: v.title,
    org_name: v.org_name,
    org_slug: v.org_slug,
    unit_name: v.unit_name ?? '',
    status: v.status ?? 'published',
    deadline: v.deadline ?? null,
  }
}

type Record_ = {
  link: string
  side: Side
  source: 'application' | 'offer'
  id: string
  status: string
  created_at: string
  vacancy: VacancyLike
  counterpart: { name: string; anonymous: boolean; profile_id: string | null }
  first: { type: string; data: Json }
  /** Учёный откликнулся после приглашения: та же переписка, статус отклика главнее. */
  applicationId?: string | null
}

function sync(server: ChatServer, r: Record_): MockChat {
  const c = server.linked(r.link, () => ({
    id: server.nextId(),
    side: r.side,
    vacancy: vacancyOf(r.vacancy),
    counterpart: r.side === 'scientist' ? { kind: 'org', name: r.vacancy.org_name, anonymous: false, profile_id: null } : { kind: 'scientist', ...r.counterpart },
    application_id: r.source === 'application' ? r.id : (r.applicationId ?? null),
    offer_id: r.source === 'offer' ? r.id : null,
    status: { source: r.source, value: r.status },
    needs_answer: false,
    can_write: true,
    closed_reason: null,
    archived: false,
    viewer: r.side === 'scientist' ? scientistViewer : orgViewer,
    first: { ...r.first, from: r.source === 'application' ? 'scientist' : 'org', at: r.created_at },
  }))
  // Состояние переписки следует за статусом записи (кроме блокировки, её ставит сам чат).
  if (r.source === 'application') c.application_id = r.id
  else c.offer_id = r.id
  if (r.source === 'application' || c.status.source === 'offer') {
    c.status = { source: r.source, value: r.status }
    if (c.closed_reason !== 'blocked') {
      const reason = closedBy[r.status] ?? null
      c.closed_reason = reason
      c.can_write = reason === null
    }
  }
  return c
}

const ref = (server: ChatServer, r: Record_) => server.chatRef(sync(server, r))

function applicationRecord(a: Json, side: Side): Record_ {
  const vacancy = a.vacancy as VacancyLike
  return {
    link: side === 'scientist' ? `sci-vac:${vacancy.id}` : `org-app:${String(a.id)}`,
    side,
    source: 'application',
    id: String(a.id),
    status: String(a.status),
    created_at: String(a.created_at),
    vacancy: { ...vacancy, unit_name: (a.unit_name as string | undefined) ?? vacancy.unit_name },
    counterpart: { name: String(a.applicant_name ?? ''), anonymous: false, profile_id: null },
    first: { type: 'application.sent', data: { application_id: String(a.id), cover_letter: typeof a.cover_letter === 'string' ? a.cover_letter : '' } },
  }
}

function offerRecord(o: Json): Record_ {
  const vacancy = o.vacancy as VacancyLike
  const scientist = o.scientist as { name: string; anonymous?: boolean; profile_id: string } | undefined
  const side: Side = scientist ? 'org' : 'scientist'
  return {
    link: side === 'scientist' ? `sci-vac:${vacancy.id}` : `org-offer:${String(o.id)}`,
    side,
    source: 'offer',
    id: String(o.id),
    status: String(o.status),
    created_at: String(o.created_at),
    vacancy,
    counterpart: { name: scientist?.anonymous ? '' : (scientist?.name ?? ''), anonymous: scientist?.anonymous ?? false, profile_id: scientist?.anonymous ? null : (scientist?.profile_id ?? null) },
    first: { type: 'offer.created', data: { offer_id: String(o.id), message: String(o.message ?? ''), status: String(o.status), can_answer: Boolean(o.can_answer) } },
    applicationId: (o.application_id as string | null | undefined) ?? null,
  }
}

const withChat = (item: Json, chat: unknown) => ({ ...item, chat })

/** Дописать поле chat в ответ настоящего сервера; адрес не про отклики и приглашения — ответ как есть. */
export function decorate(server: ChatServer, method: string, path: string, body: unknown): unknown {
  if (method !== 'GET' || typeof body !== 'object' || body === null) return body
  const b = body as Json
  const items = (map: (item: Json) => Json) => ({ ...b, items: (b.items as Json[]).map(map) })
  // Отклик удалённого аккаунта обезличен: переписок у него нет (стираются вместе с откликом).
  const app = (side: Side) => (a: Json) => (a.anonymized ? withChat(a, null) : withChat(a, ref(server, applicationRecord(a, side))))
  if (path === '/api/applications' && Array.isArray(b.items)) return items(app('scientist'))
  if (path === '/api/my/candidates' && Array.isArray(b.items)) return items(app('org'))
  if (/^\/api\/applications\/[^/]+$/.test(path) && b.application) {
    const a = b.application as Json
    const side: Side = (a.viewer as { role?: string } | undefined)?.role === 'staff' ? 'org' : 'scientist'
    return { ...b, application: app(side)(a) }
  }
  if ((path === '/api/offers' || path === '/api/my/sent-offers') && Array.isArray(b.items)) return items((o) => withChat(o, ref(server, offerRecord(o))))
  if (/^\/api\/offers\/[^/]+$/.test(path) && b.offer) return { ...b, offer: withChat(b.offer as Json, ref(server, offerRecord(b.offer as Json))) }
  return body
}

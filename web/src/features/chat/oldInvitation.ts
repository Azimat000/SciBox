import { t } from '../../i18n'

// Приглашения внутри отклика (собеседование, контакты организации, просьба оставить контакты) были до D-256. Новых нет:
// о времени и контактах договариваются сообщениями. Старые строки о них остались в переписках как история и
// показываются только для чтения, без кнопок (InvitationHistory.tsx).

export const oldInvitationKinds = ['interview', 'contacts', 'request_contacts'] as const
export type OldInvitationKind = (typeof oldInvitationKinds)[number]
export type OldInvitationStatus = 'pending' | 'confirmed' | 'proposed' | 'answered' | 'shared' | 'cancelled'

export type OldInvitation = {
  id: string
  kind: OldInvitationKind
  status: OldInvitationStatus
  message: string
  starts_at: string | null
  place_kind: 'online' | 'onsite' | ''
  place: string
  contact_name: string
  contact_email: string
  contact_phone: string
  answer: { proposed_at: string | null; note: string; contact: string; time: string } | null
}

const momentFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' })

/** «15 октября 2026 г. в 14:00 (МСК)»: время приглашений было московским (D-089). */
export const momentText = (iso: string) => `${momentFormat.format(new Date(iso))} (${t.chat.oldInvitation.msk})`

export const oldKindLabel = (k: string) => (t.chat.oldInvitation.kinds as Record<string, string>)[k] ?? k
export const oldStatusLabel = (s: string) => (t.chat.oldInvitation.statuses as Record<string, string>)[s] ?? s

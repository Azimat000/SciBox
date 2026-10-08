import { t } from '../../i18n'
import { roleLabel } from './labels'
import type { AuditEntry } from './api'

const dayFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'Europe/Moscow' })
const timeFormat = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' })

/** День записи по Москве: «9 октября 2026». Время везде московское (D-089). */
export function auditDay(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : dayFormat.format(d).replace(/\s*г\.$/, '')
}

/** Время записи по Москве: «14:32». */
export function auditTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : timeFormat.format(d)
}

export type AuditLine = {
  /** Что произошло, одной фразой без рода («Вакансия опубликована»). */
  what: string
  /** Над чем: подпись объекта и, если у него есть страница, адрес внутри сайта. */
  target: { label: string; to: string | null } | null
  /** Кого касается («Сотрудник: Борис Кадров»). */
  subject: string | null
  /** Подробности из кода: роль до и после, вид приглашения или файла. */
  detail: string | null
}

const family = (action: string) => action.split('.')[0] ?? ''

/** Адрес страницы объекта внутри сайта; null — у объекта своей страницы нет. */
function targetLink(e: AuditEntry, slug: string): string | null {
  const id = e.target.id
  if (!id) return null
  switch (family(e.action)) {
    case 'vacancy':
      return `/vacancies/${encodeURIComponent(id)}`
    case 'application':
      return `/candidates/${encodeURIComponent(id)}`
    case 'offer':
      return '/sent-offers'
    case 'unit':
      return `/organizations/${encodeURIComponent(slug)}/units/${encodeURIComponent(id)}`
    default:
      return null
  }
}

function detailOf(e: AuditEntry): string | null {
  const d = e.details
  const a = t.orgs.audit
  switch (e.action) {
    case 'member.role_changed':
      return d.from && d.to ? a.roleChange(roleLabel(d.from), roleLabel(d.to)) : null
    case 'member.removed':
    case 'member.joined':
      return d.role ? roleLabel(d.role) : null
    case 'invitation.sent':
      return d.role ? a.invitedAs(roleLabel(d.role), d.email ?? '').replace(/, $/, '') : null
    case 'application.invited':
    case 'application.invitation_canceled':
    case 'application.proposal_accepted':
      return d.kind ? (a.invitationKinds[d.kind as keyof typeof a.invitationKinds] ?? d.kind) : null
    case 'application.file_downloaded':
      return d.kind ? (a.fileKinds[d.kind as keyof typeof a.fileKinds] ?? d.kind) : null
    default:
      return null
  }
}

/** Собирает строку журнала из записи сервера. Код действия, которого нет в словаре, показывается как есть. */
export function describeAudit(e: AuditEntry, slug: string): AuditLine {
  const a = t.orgs.audit
  // Руководителя подразделения могут и снять: тогда в записи нет человека, и фраза другая.
  const code = e.action === 'unit.head_changed' && !e.subject ? 'unit.head_cleared' : e.action
  const known = a.actions as Record<string, string>
  const subjectLabel = (a.subjects as Record<string, string>)[family(e.action)]
  return {
    what: known[code] ?? a.unknownAction(e.action),
    target: e.target.label ? { label: e.target.label, to: targetLink(e, slug) } : null,
    subject: e.subject && subjectLabel ? `${subjectLabel}: ${e.subject.name}` : null,
    detail: detailOf(e),
  }
}

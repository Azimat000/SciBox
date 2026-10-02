import { t } from '../../i18n'
import { orgKinds, roles, unitKinds, type OrgKind, type Role, type UnitKind } from './api'
import type { Option } from '../../ui/Select'

// Сервер может прислать значение, которого в словаре ещё нет (например, после обновления сервера раньше сайта).
// Тогда показываем само значение, а не пустое место.

export const kindLabel = (kind: string) => t.orgs.kinds[kind as OrgKind] ?? kind
export const unitKindLabel = (kind: string) => t.orgs.unitKinds[kind as UnitKind] ?? kind
export const unitKindShort = (kind: string) => t.orgs.unitKindsShort[kind as UnitKind] ?? kind
export const roleLabel = (role: string) => t.orgs.roles[role as Role] ?? role

export const kindOptions: readonly Option[] = orgKinds.map((k) => ({ value: k, label: t.orgs.kinds[k] }))
export const unitKindOptions: readonly Option[] = unitKinds.map((k) => ({ value: k, label: t.orgs.unitKinds[k] }))
export const roleOptions: readonly Option[] = roles.map((r) => ({ value: r, label: t.orgs.roles[r] }))

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })

/** «9 октября 2026». Для даты, которую сервер прислал строкой времени. */
export function formatDate(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : dateFormat.format(d)
}

/** Название длиннее обычного (юридические названия вузов бывают за 150 знаков): заголовок набирается мельче. */
export const LONG_NAME = 80
export const longName = (name: string): true | undefined => (name.length > LONG_NAME ? true : undefined)

/** Сайт для показа: только домен (полный адрес может быть в сотни знаков, а ссылка всё равно ведёт на него целиком). */
export function siteLabel(website: string): string {
  try {
    return new URL(website).host
  } catch {
    return website
  }
}

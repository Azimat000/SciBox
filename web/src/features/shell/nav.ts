import { t } from '../../i18n'
import type { Role } from './role-context'

export type NavItem = { to: string; label: string; /** Другие разделы, на которых этот пункт тоже считается текущим. */ also?: readonly string[] }

/** Пункты меню по режимам. */
export function navFor(role: Role): NavItem[] {
  if (role === 'employer') {
    return [
      { to: '/my-vacancies', label: t.nav.myVacancies },
      { to: '/candidates', label: t.nav.applications, also: ['/sent-offers'] },
      { to: '/scientists', label: t.nav.candidates },
      { to: '/my-organization', label: t.nav.myOrganization },
    ]
  }
  return [
    { to: '/vacancies', label: t.nav.vacancies },
    { to: '/scientists', label: t.nav.scientists },
    { to: '/organizations', label: t.nav.organizations },
    { to: '/favorites', label: t.nav.favorites, also: ['/matches', '/saved-searches', '/deadlines'] },
    { to: '/applications', label: t.nav.myApplications, also: ['/offers'] },
  ]
}

/** Пункт меню текущий, если открыт его раздел: сам адрес, страницы внутри него и связанные разделы (`also`). */
export function isCurrent(item: NavItem, pathname: string): boolean {
  return [item.to, ...(item.also ?? [])].some((to) => pathname === to || pathname.startsWith(`${to}/`))
}

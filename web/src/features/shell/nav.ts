import { t } from '../../i18n'
import type { Role } from './role-context'

export type NavItem = { to: string; label: string }

/** Пункты меню по режимам. Разделы, которых ещё нет, открывают страницу «Раздел готовится». */
export function navFor(role: Role): NavItem[] {
  if (role === 'employer') {
    return [
      { to: '/my-vacancies', label: t.nav.myVacancies },
      { to: '/applications', label: t.nav.applications },
      { to: '/candidates', label: t.nav.candidates },
      { to: '/my-organization', label: t.nav.myOrganization },
    ]
  }
  return [
    { to: '/vacancies', label: t.nav.vacancies },
    { to: '/scientists', label: t.nav.scientists },
    { to: '/organizations', label: t.nav.organizations },
    { to: '/favorites', label: t.nav.favorites },
  ]
}

/** Адреса разделов-заглушек (то же, что в navFor). */
export const comingSoonPaths = ['/vacancies', '/scientists', '/favorites', '/my-vacancies', '/applications', '/candidates'] as const

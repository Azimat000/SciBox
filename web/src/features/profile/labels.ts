import { t } from '../../i18n'
import type { Option } from '../../ui/Select'
import { academicTitles, degreeLevels, grantRoles, patentTypes, pubTypes, teachLevels, visibilities, type Item } from './api'

const lookup = (dict: Record<string, string>, key: string) => dict[key] ?? key

export const degreeLabel = (v: string) => lookup(t.profile.degreeLevels, v)
export const titleLabel = (v: string) => lookup(t.profile.titles, v)
export const pubTypeLabel = (v: string) => lookup(t.profile.pubTypes, v)
export const grantRoleLabel = (v: string) => lookup(t.profile.grantRoles, v)
export const patentTypeLabel = (v: string) => lookup(t.profile.patentTypes, v)
export const teachLevelLabel = (v: string) => lookup(t.profile.teachLevels, v)

const toOptions = (values: readonly string[], label: (v: string) => string): readonly Option[] => values.map((v) => ({ value: v, label: label(v) }))

export const degreeOptions = toOptions(degreeLevels, degreeLabel)
export const titleOptions = toOptions(academicTitles, titleLabel)
export const pubTypeOptions = toOptions(pubTypes, pubTypeLabel)
export const grantRoleOptions = toOptions(grantRoles, grantRoleLabel)
export const patentTypeOptions = toOptions(patentTypes, patentTypeLabel)
export const teachLevelOptions = toOptions(teachLevels, teachLevelLabel)
export const visibilityOptions = visibilities

export const years = (from?: number | null, to?: number | null) => t.profile.years.range(from ?? null, to ?? null)

/** «Орлова Е. А. Журнал, 2023. Т. 5, № 2, С. 10–20.» без названия: оно выводится отдельной строкой. */
export function publicationMeta(it: Item): string {
  const dot = (s: string) => (s === '' || s.endsWith('.') ? s : `${s}.`)
  const biblio = [(it.venue ?? '').replace(/\.+$/, ''), it.year].filter(Boolean).join(', ')
  const vol = [it.volume && `Т. ${it.volume}`, it.issue && `№ ${it.issue}`, it.pages && `С. ${it.pages}`].filter(Boolean).join(', ')
  return [dot(it.authors ?? ''), dot(biblio), dot(vol)].filter(Boolean).join(' ')
}

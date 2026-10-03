import { t } from '../../i18n'
import type { Option } from '../../ui/Select'
import type { Item, ItemKind } from './api'
import {
  grantRoleLabel,
  grantRoleOptions,
  patentTypeLabel,
  patentTypeOptions,
  pubTypeLabel,
  pubTypeOptions,
  publicationMeta,
  teachLevelLabel,
  teachLevelOptions,
  years,
} from './labels'

export type FieldDef = {
  name: string
  label: string
  type: 'text' | 'area' | 'year' | 'select' | 'url'
  required?: boolean
  hint?: string
  options?: readonly Option[]
}

/** Как запись выглядит в списке: слева метка (годы), справа заголовок и пояснения. */
export type Summary = { label: string; lead: string; lines: string[]; doi?: string; url?: string }

export type SectionDef = {
  kind: ItemKind
  heading: string
  empty: string
  fields: FieldDef[]
  /** Значения формы для новой записи. */
  defaults: Record<string, string>
  summary: (it: Item) => Summary
}

const f = t.profile.fields
const join = (parts: (string | undefined)[], sep = ', ') => parts.filter(Boolean).join(sep)
const text = (name: string, label: string, extra: Partial<FieldDef> = {}): FieldDef => ({ name, label, type: 'text', ...extra })
const area = (name: string, label: string): FieldDef => ({ name, label, type: 'area' })
const year = (name: string, label: string, extra: Partial<FieldDef> = {}): FieldDef => ({ name, label, type: 'year', ...extra })
const select = (name: string, label: string, options: readonly Option[], required = true): FieldDef => ({ name, label, type: 'select', options, required })

const period = [year('year_from', f.yearFrom, { required: true }), year('year_to', f.yearTo, { hint: f.yearToHint })]

export const sectionDefs: Record<ItemKind, SectionDef> = {
  education: {
    kind: 'education',
    heading: t.profile.sections.education,
    empty: t.profile.empty.education,
    fields: [text('institution', f.institution, { required: true }), text('program', f.program), ...period, area('description', f.description)],
    defaults: {},
    summary: (it) => ({ label: years(it.year_from, it.year_to), lead: it.institution ?? '', lines: [it.program, it.description].filter(Boolean) as string[] }),
  },
  experience: {
    kind: 'experience',
    heading: t.profile.sections.experience,
    empty: t.profile.empty.experience,
    fields: [text('position', f.position, { required: true }), text('organization', f.organization, { required: true }), ...period, area('description', f.description)],
    defaults: {},
    summary: (it) => ({ label: years(it.year_from, it.year_to), lead: it.position ?? '', lines: [it.organization, it.description].filter(Boolean) as string[] }),
  },
  publication: {
    kind: 'publication',
    heading: t.profile.sections.publication,
    empty: t.profile.empty.publication,
    fields: [
      text('title', f.title, { required: true }),
      text('authors', f.authors, { required: true, hint: f.authorsHint }),
      text('venue', f.venue),
      select('pub_type', f.pubType, pubTypeOptions),
      year('year', f.year, { required: true }),
      text('volume', f.volume),
      text('issue', f.issue),
      text('pages', f.pages),
      text('doi', f.doi),
      { name: 'url', label: f.url, type: 'url' },
    ],
    defaults: { pub_type: 'article' },
    summary: (it) => ({
      label: it.year ? String(it.year) : '',
      lead: it.title ?? '',
      lines: [publicationMeta(it), it.pub_type && it.pub_type !== 'article' ? pubTypeLabel(it.pub_type) : ''].filter(Boolean),
      doi: it.doi,
      url: it.url,
    }),
  },
  grant: {
    kind: 'grant',
    heading: t.profile.sections.grant,
    empty: t.profile.empty.grant,
    fields: [
      text('title', f.title, { required: true }),
      text('funder', f.funder, { required: true, hint: f.funderHint }),
      text('number', f.number),
      select('role', f.role, grantRoleOptions),
      ...period,
      area('description', f.description),
    ],
    defaults: {},
    summary: (it) => ({
      label: years(it.year_from, it.year_to),
      lead: it.title ?? '',
      lines: [join([it.funder, it.number && `№ ${it.number}`, it.role && grantRoleLabel(it.role)]), it.description].filter(Boolean) as string[],
    }),
  },
  patent: {
    kind: 'patent',
    heading: t.profile.sections.patent,
    empty: t.profile.empty.patent,
    fields: [
      text('title', f.title, { required: true }),
      select('patent_type', f.patentType, patentTypeOptions),
      text('number', f.number, { required: true }),
      text('office', f.office),
      text('authors', f.authors),
      year('year', f.year, { required: true }),
    ],
    defaults: {},
    summary: (it) => ({
      label: it.year ? String(it.year) : '',
      lead: it.title ?? '',
      lines: [join([it.patent_type && patentTypeLabel(it.patent_type), it.number && `№ ${it.number}`, it.office]), it.authors].filter(Boolean) as string[],
    }),
  },
  teaching: {
    kind: 'teaching',
    heading: t.profile.sections.teaching,
    empty: t.profile.empty.teaching,
    fields: [
      text('course', f.course, { required: true }),
      text('institution', f.institution, { required: true }),
      select('level', f.level, teachLevelOptions),
      ...period,
      area('description', f.description),
    ],
    defaults: {},
    summary: (it) => ({
      label: years(it.year_from, it.year_to),
      lead: it.course ?? '',
      lines: [join([it.institution, it.level && teachLevelLabel(it.level)]), it.description].filter(Boolean) as string[],
    }),
  },
}

/** Значения формы из сохранённой записи (всё строками, как в полях ввода). */
export function valuesOf(def: SectionDef, item: Item | null): Record<string, string> {
  const out: Record<string, string> = {}
  for (const field of def.fields) {
    const raw = item ? (item as unknown as Record<string, string | number | undefined>)[field.name] : def.defaults[field.name]
    out[field.name] = raw === undefined ? '' : String(raw)
  }
  return out
}

/** Тело запроса из значений формы: пустое не отправляем, годы переводим в числа. */
export function bodyOf(def: SectionDef, values: Record<string, string | boolean>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const field of def.fields) {
    const v = String(values[field.name] ?? '').trim()
    if (v === '') continue
    out[field.name] = field.type === 'year' ? Number(v) : v
  }
  return out
}

/** Что показать в вопросе «удалить?»: заголовок записи. */
export const itemTitle = (it: Item) => sectionDefs[it.kind].summary(it).lead

// Состояние каталога учёных и его запись в адресе. Как в поиске вакансий (D-012): всё выбранное лежит в адресной строке,
// неизвестное в адресе молча отбрасывается.

export const PAGE_SIZE = 20

export const degrees = ['none', 'candidate', 'doctor'] as const
export const titles = ['none', 'docent', 'professor'] as const
export const sorts = ['relevance', 'updated', 'h_index', 'name'] as const
export type Sort = (typeof sorts)[number]

/** Значения «h-index от» в выпадающем списке. */
export const hIndexPresets = [5, 10, 20, 30, 50] as const

const fieldCode = /^[1-9][0-9]*(\.[0-9]+){0,2}$/
const regionCode = /^[0-9]{2}$/
const MAX_H = 300
const MAX_VALUES = 30
const MAX_QUERY = 200

export type CatalogSearch = {
  q: string
  region: string
  field: string[]
  degree: string[]
  title: string[]
  open: boolean
  /** h-index от; 0 — без ограничения. */
  hMin: number
  /** Явно выбранный порядок; пусто — порядок по умолчанию (см. effectiveSort). */
  sort: Sort | ''
  page: number
}

export function emptyCatalog(): CatalogSearch {
  return { q: '', region: '', field: [], degree: [], title: [], open: false, hMin: 0, sort: '', page: 1 }
}

const clean = (raw: string[], ok: (v: string) => boolean) => [...new Set(raw.filter(ok))].slice(0, MAX_VALUES)

/** Читает адрес. Всё неизвестное отбрасывается. */
export function parseCatalog(params: URLSearchParams): CatalogSearch {
  const s = emptyCatalog()
  s.q = (params.get('q') ?? '').replace(/\s+/g, ' ').trim().slice(0, MAX_QUERY)
  const region = params.get('region') ?? ''
  s.region = regionCode.test(region) ? region : ''
  s.field = clean(params.getAll('field'), (v) => fieldCode.test(v))
  s.degree = clean(params.getAll('degree'), (v) => (degrees as readonly string[]).includes(v))
  s.title = clean(params.getAll('title'), (v) => (titles as readonly string[]).includes(v))
  const open = params.get('open')
  s.open = open === '1' || open === 'true'
  const h = Number.parseInt(params.get('h_min') ?? '', 10)
  s.hMin = Number.isFinite(h) && h > 0 && h <= MAX_H ? h : 0
  const sort = params.get('sort') ?? ''
  s.sort = (sorts as readonly string[]).includes(sort) ? (sort as Sort) : ''
  s.page = Math.max(1, Number.parseInt(params.get('page') ?? '', 10) || 1)
  return s
}

/** Порядок по умолчанию: со словами — по совпадению, без слов — недавно обновлённые. */
export const defaultSort = (s: Pick<CatalogSearch, 'q'>): Sort => (s.q ? 'relevance' : 'updated')

/** Порядок, по которому выдача выстроена на самом деле. «По совпадению» без слов значит «недавно обновлённые». */
export function effectiveSort(s: Pick<CatalogSearch, 'q' | 'sort'>): Sort {
  if (s.sort === 'relevance' && !s.q) return 'updated'
  return s.sort || defaultSort(s)
}

function filterParams(s: CatalogSearch): URLSearchParams {
  const out = new URLSearchParams()
  if (s.q) out.set('q', s.q)
  for (const v of s.field) out.append('field', v)
  for (const v of s.degree) out.append('degree', v)
  for (const v of s.title) out.append('title', v)
  if (s.region) out.set('region', s.region)
  if (s.open) out.set('open', '1')
  if (s.hMin > 0) out.set('h_min', String(s.hMin))
  return out
}

/** Адрес страницы: только то, что отличается от умолчаний. */
export function toParams(s: CatalogSearch): URLSearchParams {
  const out = filterParams(s)
  if (s.sort && s.sort !== defaultSort(s)) out.set('sort', s.sort)
  if (s.page > 1) out.set('page', String(s.page))
  return out
}

/** Запрос к серверу. */
export function toApiParams(s: CatalogSearch): URLSearchParams {
  const out = filterParams(s)
  if (s.sort) out.set('sort', s.sort)
  out.set('limit', String(PAGE_SIZE))
  if (s.page > 1) out.set('offset', String((s.page - 1) * PAGE_SIZE))
  return out
}

export type ListKey = 'field' | 'degree' | 'title'

/** Фильтры, которые можно убрать по одному: области, степени, звания, регион, «открыт к предложениям», h-index. */
export type FilterRef = { kind: 'list'; key: ListKey; value: string } | { kind: 'region' } | { kind: 'open' } | { kind: 'hIndex' }

/** Сколько фильтров включено (слова поиска и порядок не считаются). */
export const filterCount = (s: CatalogSearch) => activeFilters(s).length

/** Тот же поиск без фильтров, со словами. */
export const withoutFilters = (s: CatalogSearch): CatalogSearch => ({ ...emptyCatalog(), q: s.q, sort: s.sort })

/** Любое изменение условий возвращает на первую страницу. */
export const change = (s: CatalogSearch, patch: Partial<CatalogSearch>): CatalogSearch => ({ ...s, ...patch, page: patch.page ?? 1 })

/** Включает или выключает одно значение в списке «один из нескольких». */
export function toggleList(s: CatalogSearch, key: ListKey, value: string): CatalogSearch {
  const next = s[key].includes(value) ? s[key].filter((v) => v !== value) : [...s[key], value]
  return change(s, { [key]: next })
}

export function removeFilter(s: CatalogSearch, ref: FilterRef): CatalogSearch {
  switch (ref.kind) {
    case 'list':
      return toggleList(s, ref.key, ref.value)
    case 'region':
      return change(s, { region: '' })
    case 'open':
      return change(s, { open: false })
    case 'hIndex':
      return change(s, { hMin: 0 })
  }
}

/** Все включённые фильтры по порядку показа: чипами над выдачей. */
export function activeFilters(s: CatalogSearch): FilterRef[] {
  const out: FilterRef[] = []
  for (const key of ['field', 'degree', 'title'] as const) for (const value of s[key]) out.push({ kind: 'list', key, value })
  if (s.region) out.push({ kind: 'region' })
  if (s.open) out.push({ kind: 'open' })
  if (s.hMin > 0) out.push({ kind: 'hIndex' })
  return out
}

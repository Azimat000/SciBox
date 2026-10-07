// Состояние поиска и его запись в адресе. Всё, что человек выбрал, живёт в адресной строке (D-012): ссылку можно
// отправить, а кнопка «Назад» возвращает прежнюю выдачу. Неизвестное в адресе молча отбрасывается: ссылку могли
// набрать руками или получить от более старой версии сайта.

export const PAGE_SIZE = 20

/** Фильтры «один из нескольких»: ключ — имя параметра в адресе и на сервере. */
export const multiKeys = ['field', 'type', 'level', 'format', 'degree', 'org_kind', 'funding', 'rate', 'term'] as const
export type MultiKey = (typeof multiKeys)[number]

export const sorts = ['relevance', 'new', 'deadline', 'salary'] as const
export type Sort = (typeof sorts)[number]

export const deadlines = ['week', 'month', 'none'] as const
export type DeadlineFilter = (typeof deadlines)[number]

export const terms = ['permanent', 'short', 'medium', 'long'] as const

/** Допустимые значения фильтров с фиксированным набором. Область науки проверяется по форме кода. */
export const allowed: Record<Exclude<MultiKey, 'field'>, readonly string[]> = {
  type: ['research', 'teaching', 'admin', 'phd', 'masters', 'project', 'internship'],
  level: ['1', '2', '3', '4'],
  format: ['onsite', 'hybrid', 'remote'],
  degree: ['none', 'candidate', 'doctor'],
  org_kind: ['university', 'institute', 'science_center', 'rd_company', 'technopark', 'other'],
  funding: ['budget', 'grant', 'contract', 'own'],
  rate: ['25', '50', '75', '100'],
  term: terms,
}

const fieldCode = /^[1-9][0-9]*(\.[0-9]+){0,2}$/
const regionCode = /^[0-9]{2}$/
const MAX_SALARY = 100_000_000
const MAX_VALUES = 30
const MAX_QUERY = 200

export type Search = {
  q: string
  region: string
  multi: Record<MultiKey, string[]>
  /** Зарплата от, рублей в месяц; 0 — без ограничения. */
  salaryMin: number
  housing: boolean
  competition: boolean
  deadline: DeadlineFilter | ''
  /** Явно выбранный порядок; пусто — порядок по умолчанию (см. effectiveSort). */
  sort: Sort | ''
  page: number
}

export function emptySearch(): Search {
  return {
    q: '',
    region: '',
    multi: { field: [], type: [], level: [], format: [], degree: [], org_kind: [], funding: [], rate: [], term: [] },
    salaryMin: 0,
    housing: false,
    competition: false,
    deadline: '',
    sort: '',
    page: 1,
  }
}

const isFlag = (v: string | null) => v === '1' || v === 'true'

function cleanMulti(key: MultiKey, raw: string[]): string[] {
  const seen = new Set<string>()
  for (const v of raw) {
    const ok = key === 'field' ? fieldCode.test(v) : allowed[key].includes(v)
    if (ok) seen.add(v)
  }
  return [...seen].slice(0, MAX_VALUES)
}

/** Читает адрес. Всё неизвестное отбрасывается. */
export function parseSearch(params: URLSearchParams): Search {
  const s = emptySearch()
  s.q = (params.get('q') ?? '').replace(/\s+/g, ' ').trim().slice(0, MAX_QUERY)
  const region = params.get('region') ?? ''
  s.region = regionCode.test(region) ? region : ''
  for (const key of multiKeys) s.multi[key] = cleanMulti(key, params.getAll(key))
  const salary = Number.parseInt(params.get('salary_min') ?? '', 10)
  s.salaryMin = Number.isFinite(salary) && salary > 0 && salary <= MAX_SALARY ? salary : 0
  s.housing = isFlag(params.get('housing'))
  s.competition = isFlag(params.get('competition'))
  const deadline = params.get('deadline') ?? ''
  s.deadline = (deadlines as readonly string[]).includes(deadline) ? (deadline as DeadlineFilter) : ''
  const sort = params.get('sort') ?? ''
  s.sort = (sorts as readonly string[]).includes(sort) ? (sort as Sort) : ''
  s.page = Math.max(1, Number.parseInt(params.get('page') ?? '', 10) || 1)
  return s
}

/** Порядок, который сервер применит, если человек ничего не выбирал: со словами — по совпадению, без слов — новые. */
export const defaultSort = (s: Pick<Search, 'q'>): Sort => (s.q ? 'relevance' : 'new')

/** Порядок, по которому выдача выстроена на самом деле. «По совпадению» без слов поиска значит «новые». */
export function effectiveSort(s: Pick<Search, 'q' | 'sort'>): Sort {
  if (s.sort === 'relevance' && !s.q) return 'new'
  return s.sort || defaultSort(s)
}

/** Фильтры без слов поиска, порядка и страницы: общая часть адреса страницы и запроса к серверу. */
function filterParams(s: Search): URLSearchParams {
  const out = new URLSearchParams()
  if (s.q) out.set('q', s.q)
  for (const key of multiKeys) for (const v of s.multi[key]) out.append(key, v)
  if (s.region) out.set('region', s.region)
  if (s.salaryMin > 0) out.set('salary_min', String(s.salaryMin))
  if (s.housing) out.set('housing', '1')
  if (s.competition) out.set('competition', '1')
  if (s.deadline) out.set('deadline', s.deadline)
  return out
}

/** Адрес страницы: только то, что отличается от умолчаний. */
export function toParams(s: Search): URLSearchParams {
  const out = filterParams(s)
  if (s.sort && s.sort !== defaultSort(s)) out.set('sort', s.sort)
  if (s.page > 1) out.set('page', String(s.page))
  return out
}

/** Запрос к серверу. */
export function toApiParams(s: Search): URLSearchParams {
  const out = filterParams(s)
  if (s.sort) out.set('sort', s.sort)
  out.set('limit', String(PAGE_SIZE))
  if (s.page > 1) out.set('offset', String((s.page - 1) * PAGE_SIZE))
  return out
}

/** Сколько фильтров включено (слова поиска и порядок не считаются). */
export function filterCount(s: Search): number {
  let n = multiKeys.reduce((sum, key) => sum + s.multi[key].length, 0)
  if (s.region) n += 1
  if (s.salaryMin > 0) n += 1
  if (s.housing) n += 1
  if (s.competition) n += 1
  if (s.deadline) n += 1
  return n
}

/** Тот же поиск без фильтров, со словами. */
export const withoutFilters = (s: Search): Search => ({ ...emptySearch(), q: s.q, sort: s.sort })

/** Любое изменение условий возвращает на первую страницу. */
export function change(s: Search, patch: Partial<Search>): Search {
  return { ...s, ...patch, page: patch.page ?? 1 }
}

/** Включает или выключает одно значение в фильтре «один из нескольких». */
export function toggleMulti(s: Search, key: MultiKey, value: string): Search {
  const has = s.multi[key].includes(value)
  const next = has ? s.multi[key].filter((v) => v !== value) : [...s.multi[key], value]
  return change(s, { multi: { ...s.multi, [key]: next } })
}

/** Убирает значение из фильтра; для фильтров не из списка очищает их. */
export type FilterRef =
  | { kind: 'multi'; key: MultiKey; value: string }
  | { kind: 'region' }
  | { kind: 'salary' }
  | { kind: 'housing' }
  | { kind: 'competition' }
  | { kind: 'deadline' }

export function removeFilter(s: Search, ref: FilterRef): Search {
  switch (ref.kind) {
    case 'multi':
      return toggleMulti(s, ref.key, ref.value)
    case 'region':
      return change(s, { region: '' })
    case 'salary':
      return change(s, { salaryMin: 0 })
    case 'housing':
      return change(s, { housing: false })
    case 'competition':
      return change(s, { competition: false })
    case 'deadline':
      return change(s, { deadline: '' })
  }
}

/** Все включённые фильтры по порядку показа: чипами над выдачей. */
export function activeFilters(s: Search): FilterRef[] {
  const out: FilterRef[] = []
  for (const key of multiKeys) for (const value of s.multi[key]) out.push({ kind: 'multi', key, value })
  if (s.region) out.push({ kind: 'region' })
  if (s.salaryMin > 0) out.push({ kind: 'salary' })
  if (s.housing) out.push({ kind: 'housing' })
  if (s.competition) out.push({ kind: 'competition' })
  if (s.deadline) out.push({ kind: 'deadline' })
  return out
}

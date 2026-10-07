import { useQuery, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiSend } from '../../api/client'
import { useMe } from '../auth/api'

export const statuses = ['draft', 'published', 'closed', 'archived'] as const
export type Status = (typeof statuses)[number]

export const positionTypes = ['research', 'teaching', 'admin', 'phd', 'masters', 'project', 'internship'] as const
export type PositionType = (typeof positionTypes)[number]

/** Что требуется от вакансии каждого вида и что для неё допустимо (как в internal/vacancies/validate.go, D-134). */
export type TypeRules = {
  rate: boolean // ставка обязательна
  level: boolean // уровень R1–R4 обязателен
  specialties: boolean // нужна хотя бы одна научная специальность
  focus: boolean // нужно поле «что предстоит делать»
  competition: boolean // можно отметить «конкурс»
  title: boolean // можно требовать учёное звание
  stipend: boolean // деньги называются стипендией
}

const none: TypeRules = { rate: false, level: false, specialties: false, focus: false, competition: false, title: false, stipend: false }

export const typeRules: Record<PositionType, TypeRules> = {
  research: { ...none, rate: true, level: true, specialties: true, competition: true },
  teaching: { ...none, rate: true, level: true, specialties: true, focus: true, competition: true, title: true },
  admin: { ...none, rate: true },
  phd: { ...none, specialties: true, focus: true, stipend: true },
  masters: { ...none, specialties: true, focus: true, stipend: true },
  project: { ...none, focus: true },
  internship: { ...none, focus: true, stipend: true },
}

/** Правила вида; для пустого или незнакомого вида ничего не обязательно. */
export const rulesOf = (type: string): TypeRules => typeRules[type as PositionType] ?? none

export const workFormats = ['onsite', 'hybrid', 'remote'] as const
export const housings = ['none', 'dormitory', 'service', 'compensation'] as const
export const contractTypes = ['permanent', 'fixed'] as const
export const fundingSources = ['budget', 'grant', 'contract', 'own'] as const
export const degrees = ['none', 'candidate', 'doctor'] as const
export const academicTitles = ['none', 'docent', 'professor'] as const
export const rates = [25, 50, 75, 100] as const
export const careerLevels = [1, 2, 3, 4] as const

// ---- справочники ----

export type Specialty = { code: string; name: string }
export type ScienceGroup = { code: string; name: string; specialties: Specialty[] }
export type ScienceField = { code: string; name: string; groups: ScienceGroup[] }
export type Region = { code: string; name: string }
export type Position = { code: string; type: PositionType; name: string }
export type ReferenceSource = { catalog: string; title: string; url: string; edition: string; checked_on: string }
export type Reference = { science: ScienceField[]; regions: Region[]; positions: Position[]; sources: ReferenceSource[] }

/** Справочники меняются только вместе с сайтом, поэтому читаем один раз и держим в памяти. */
export function useReference() {
  return useQuery({
    queryKey: ['reference'],
    queryFn: ({ signal }) => apiGet<Reference>('/api/reference', { signal }),
    staleTime: Infinity,
    retry: false,
  })
}

// ---- вакансии ----

export type PositionRef = { code: string; name: string; type: PositionType }
export type OrgRef = { slug: string; name: string; kind: string; city: string }
export type UnitRef = { id: string; name: string }

export type Card = {
  id: string
  status: Status
  title: string
  summary: string
  position: PositionRef
  organization: OrgRef
  unit: UnitRef | null
  city: string
  region: { code: string; name: string } | null
  work_format: string
  career_level: number | null
  rate_percent: number | null
  salary_from: number | null
  salary_to: number | null
  contract_type: string
  contract_months: number | null
  is_competition: boolean
  /** Последний день приёма, `2026-11-14`; пусто, если срока нет. */
  deadline: string
  specialties: Specialty[]
  published_at: string | null
  updated_at: string
}

export type Detail = Card & {
  description: string
  requirements: string
  focus: string
  housing: string
  funding_source: string
  funding_note: string
  degree_required: string
  title_required: string
  created_at: string
  viewer: { can_manage: boolean; transitions: Status[] }
}

export type VacancyList = { items: Card[]; total: number }
export type MineList = VacancyList & { counts: Record<Status, number> }
export type Target = { organization: OrgRef; whole_org: boolean; units: UnitRef[] }

export const PAGE_SIZE = 50

/** Поля формы вакансии: так их принимает сервер. */
export type VacancyFields = {
  title: string
  position_code: string
  unit_id: string | null
  summary: string
  description: string
  requirements: string
  focus: string
  career_level: number | null
  work_format: string
  region_code: string
  city: string
  housing: string
  rate_percent: number | null
  salary_from: number | null
  salary_to: number | null
  contract_type: string
  contract_months: number | null
  funding_source: string
  funding_note: string
  degree_required: string
  title_required: string
  is_competition: boolean
  deadline: string
  specialties: string[]
}

// Что видно человеку, зависит от того, кто смотрит (черновик видят только сотрудники): кеш лежит под ключом с номером человека.
const who = (userId: string | null | undefined) => userId ?? 'anonymous'

export const keys = {
  one: (id: string, userId: string | null | undefined) => ['vacancy', id, who(userId)] as const,
  oneAll: (id: string) => ['vacancy', id] as const,
  ofOrg: (slug: string, unitId: string | null) => ['vacancies', 'org', slug, unitId] as const,
  mine: (status: string, page: number) => ['vacancies', 'mine', status, page] as const,
  targets: ['vacancies', 'targets'] as const,
}

export function useVacancy(id: string, enabled = true) {
  const { user } = useMe()
  return useQuery({
    queryKey: keys.one(id, user?.id),
    queryFn: async ({ signal }) => (await apiGet<{ vacancy: Detail }>(`/api/vacancies/${encodeURIComponent(id)}`, { signal })).vacancy,
    enabled: enabled && user !== undefined,
    retry: false,
  })
}

/** Опубликованные вакансии организации или одного её подразделения. */
export function useOrgVacancies(slug: string, unitId: string | null) {
  return useQuery({
    queryKey: keys.ofOrg(slug, unitId),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ org: slug })
      if (unitId) qs.set('unit', unitId)
      return apiGet<VacancyList>(`/api/vacancies?${qs}`, { signal })
    },
    retry: false,
  })
}

export function useMyVacancies(status: string, page: number) {
  return useQuery({
    queryKey: keys.mine(status, page),
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams({ limit: String(PAGE_SIZE), offset: String((page - 1) * PAGE_SIZE) })
      if (status) qs.set('status', status)
      return apiGet<MineList>(`/api/my/vacancies?${qs}`, { signal })
    },
    placeholderData: (previous) => previous,
    retry: false,
  })
}

export function useTargets() {
  return useQuery({
    queryKey: keys.targets,
    queryFn: async ({ signal }) => (await apiGet<{ targets: Target[] }>('/api/my/vacancy-targets', { signal })).targets,
    retry: false,
  })
}

// ---- действия ----

export async function createVacancy(organization: string, fields: VacancyFields): Promise<Detail> {
  const res = await apiSend<{ vacancy: Detail }>('POST', '/api/vacancies', { organization, ...fields })
  return res.vacancy
}

export async function updateVacancy(id: string, fields: VacancyFields): Promise<Detail> {
  const res = await apiSend<{ vacancy: Detail }>('PATCH', `/api/vacancies/${id}`, fields)
  return res.vacancy
}

export async function setVacancyStatus(id: string, status: Status): Promise<Detail> {
  const res = await apiSend<{ vacancy: Detail }>('POST', `/api/vacancies/${id}/status`, { status })
  return res.vacancy
}

export function deleteVacancy(id: string) {
  return apiSend('DELETE', `/api/vacancies/${id}`)
}

/** Вакансия изменилась: страница, списки организаций и «Мои вакансии» перечитаются. */
export function refreshVacancies(client: QueryClient, id?: string) {
  return Promise.all([
    client.invalidateQueries({ queryKey: ['vacancies'] }),
    id ? client.invalidateQueries({ queryKey: keys.oneAll(id) }) : undefined,
  ])
}

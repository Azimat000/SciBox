import type { Card, Detail, MineList, Reference, Target } from '../features/vacancies/api'
import { reply, type Route } from './api'
import { SLUG, lab } from './orgs'

export const reference: Reference = {
  science: [
    {
      code: '1',
      name: 'Естественные науки',
      groups: [
        {
          code: '1.4',
          name: 'Химические науки',
          specialties: [
            { code: '1.4.3', name: 'Органическая химия' },
            { code: '1.4.4', name: 'Физическая химия' },
          ],
        },
        {
          code: '1.3',
          name: 'Физические науки',
          specialties: [
            { code: '1.3.1', name: 'Физика космоса, астрономия' },
            { code: '1.3.2', name: 'Приборы и методы экспериментальной физики' },
            { code: '1.3.3', name: 'Теоретическая физика' },
            { code: '1.3.8', name: 'Физика конденсированного состояния' },
          ],
        },
      ],
    },
  ],
  regions: [
    { code: '54', name: 'Новосибирская область' },
    { code: '77', name: 'Москва' },
  ],
  positions: [
    { code: 'senior_researcher', type: 'research', name: 'Старший научный сотрудник' },
    { code: 'researcher', type: 'research', name: 'Научный сотрудник' },
    { code: 'docent', type: 'teaching', name: 'Доцент' },
    { code: 'postdoc', type: 'early_career', name: 'Постдок' },
    { code: 'grant_manager', type: 'management', name: 'Грант-менеджер' },
  ],
  sources: [],
}

export const VACANCY_ID = '99999999-9999-4999-8999-999999999999'

export const card: Card = {
  id: VACANCY_ID,
  status: 'published',
  title: 'Старший научный сотрудник: сверхпроводящие плёнки',
  summary: 'Рост и измерение эпитаксиальных плёнок.',
  position: { code: 'senior_researcher', name: 'Старший научный сотрудник', type: 'research' },
  organization: { slug: SLUG, name: 'Сибирский институт', kind: 'institute', city: 'Новосибирск' },
  unit: { id: lab.id, name: lab.name },
  city: 'Новосибирск',
  region: { code: '54', name: 'Новосибирская область' },
  work_format: 'onsite',
  career_level: 3,
  rate_percent: 100,
  salary_from: 95000,
  salary_to: 130000,
  contract_type: 'fixed',
  contract_months: 36,
  is_competition: true,
  deadline: '2099-12-31',
  specialties: [
    { code: '1.4.4', name: 'Физическая химия' },
    { code: '1.3.8', name: 'Физика конденсированного состояния' },
  ],
  published_at: '2026-10-01T10:00:00Z',
  updated_at: '2026-10-02T10:00:00Z',
}

export const detail: Detail = {
  ...card,
  description: 'Лаборатория ведёт четыре проекта.\nНужен руководитель роста образцов.',
  requirements: 'Кандидат наук, опыт эпитаксии.',
  focus: 'Рост и характеризация плёнок',
  housing: 'service',
  funding_source: 'grant',
  funding_note: 'Грант РНФ 24-12-00184',
  degree_required: 'candidate',
  title_required: 'none',
  created_at: '2026-10-01T09:00:00Z',
  viewer: { can_manage: false, transitions: [] },
}

export const withViewer = (over: Partial<Detail>, canManage: boolean, transitions: Detail['viewer']['transitions'] = []): Detail => ({
  ...detail,
  ...over,
  viewer: { can_manage: canManage, transitions },
})

export const vacancyRoute = (d: Detail): Record<string, Route> => ({ [`GET /api/vacancies/${d.id}`]: reply(200, { vacancy: d }) })

/** Список вакансий организации (и, при unit, подразделения) для страниц организации. */
export const orgVacancies = (items: Card[] = [], unit?: string): Record<string, Route> => ({
  [`GET /api/vacancies?org=${SLUG}${unit ? `&unit=${unit}` : ''}`]: reply(200, { items, total: items.length }),
})

export const emptyCounts = { draft: 0, published: 0, closed: 0, archived: 0 }

export const mineList = (items: Card[], counts: Partial<MineList['counts']> = {}, total = items.length): MineList => ({
  items,
  total,
  counts: { ...emptyCounts, ...counts },
})

export const target: Target = {
  organization: { slug: SLUG, name: 'Сибирский институт', kind: 'institute', city: 'Новосибирск' },
  whole_org: true,
  units: [{ id: lab.id, name: lab.name }],
}

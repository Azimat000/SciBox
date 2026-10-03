import type { LandingStats } from '../features/landing/api'

export const stats: LandingStats = {
  open_vacancies: 188,
  hiring_organizations: 30,
  scientists: 7,
  fields: [
    { code: '1', vacancies: 138 },
    { code: '2', vacancies: 52 },
  ],
  types: [
    { type: 'early_career', vacancies: 55 },
    { type: 'research', vacancies: 94 },
  ],
}

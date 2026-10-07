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
    { type: 'research', vacancies: 94 },
    { type: 'phd', vacancies: 55 },
  ],
}

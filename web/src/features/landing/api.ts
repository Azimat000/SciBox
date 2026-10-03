import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'
import type { VacancyList } from '../vacancies/api'

/** Числа главной страницы: всё это счётчики, без записей и имён. */
export type LandingStats = {
  open_vacancies: number
  hiring_organizations: number
  scientists: number
  fields: { code: string; vacancies: number }[]
  types: { type: string; vacancies: number }[]
}

/** Сколько свежих вакансий показывает главная. */
export const FRESH_COUNT = 6

/** Числа для главной. Страница работает и без них: при ошибке блоки с числами просто не показываются. */
export function useLandingStats() {
  return useQuery({
    queryKey: ['landing', 'stats'],
    queryFn: ({ signal }) => apiGet<LandingStats>('/api/landing', { signal }),
    retry: false,
    staleTime: 60_000,
  })
}

/** Самые свежие опубликованные вакансии. Ключ начинается с «vacancies»: правка вакансии обновляет и главную. */
export function useFreshVacancies() {
  return useQuery({
    queryKey: ['vacancies', 'fresh'],
    queryFn: ({ signal }) => apiGet<VacancyList>(`/api/vacancies?limit=${FRESH_COUNT}`, { signal }),
    retry: false,
  })
}

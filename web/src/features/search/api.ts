import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'
import type { VacancyList } from '../vacancies/api'
import { toApiParams, type Search } from './params'

/** Страница выдачи; fuzzy — точных совпадений со словами нет, показаны похожие (с опечатками). */
export type SearchResult = VacancyList & { fuzzy: boolean }

/** Поиск среди опубликованных вакансий. Ключ начинается с «vacancies»: любое изменение вакансии обновляет и выдачу. */
export function useSearch(search: Search) {
  const qs = toApiParams(search).toString()
  return useQuery({
    queryKey: ['vacancies', 'search', qs],
    queryFn: ({ signal }) => apiGet<SearchResult>(`/api/vacancies?${qs}`, { signal }),
    // Пока грузится новая выдача, остаётся прежняя: список не мигает при каждом щелчке по фильтру.
    placeholderData: (previous) => previous,
    retry: false,
  })
}

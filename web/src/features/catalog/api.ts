import { useQuery } from '@tanstack/react-query'
import { apiGet } from '../../api/client'
import { useMe } from '../auth/api'
import { toApiParams, type CatalogSearch } from './params'

export type Code = { code: string; name: string }

/** Учёный в каталоге: только то, что видно из любого режима приватности, без контактов. */
export type CatalogCard = {
  id: string
  name: string
  headline: string
  city: string
  region: string
  degree: 'none' | 'candidate' | 'doctor'
  academic_title: 'none' | 'docent' | 'professor'
  open_to_offers: boolean
  h_index: number | null
  publications: number
  specialties: Code[]
  updated_at: string
}

/** Страница выдачи; fuzzy — точных совпадений со словами нет, показаны похожие (с опечатками). */
export type CatalogResult = { items: CatalogCard[]; total: number; fuzzy: boolean }

/**
 * Поиск по каталогу. Что видно, зависит от того, кто смотрит (организациям открыто больше), поэтому кеш лежит под ключом
 * с номером человека.
 */
export function useCatalog(search: CatalogSearch) {
  const { user } = useMe()
  const qs = toApiParams(search).toString()
  return useQuery({
    queryKey: ['scientists', 'catalog', user?.id ?? 'anonymous', qs],
    queryFn: ({ signal }) => apiGet<CatalogResult>(`/api/scientists?${qs}`, { signal }),
    enabled: user !== undefined,
    // Пока грузится новая выдача, остаётся прежняя: список не мигает при каждом щелчке по фильтру.
    placeholderData: (previous) => previous,
    retry: false,
  })
}

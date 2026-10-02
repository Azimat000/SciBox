import { useId, type FormEvent } from 'react'
import { t } from '../i18n'
import { Button } from './Button'
import { ChevronDownIcon, SearchIcon } from './icons'
import type { Option } from './Select'
import './SearchBar.css'

type SearchBarProps = {
  query: string
  onQueryChange: (query: string) => void
  /** Список регионов; без него остаётся одно поле поиска. */
  regions?: readonly Option[]
  region?: string
  onRegionChange?: (region: string) => void
  onSubmit: () => void
  /** Тексты для других списков (организации): по умолчанию это поиск вакансий. */
  labels?: { query?: string; placeholder?: string; select?: string; allOptions?: string }
}

/** Главная строка поиска: слова, регион и кнопка «Найти». Отправляется и по Enter. */
export function SearchBar({ query, onQueryChange, regions, region = '', onRegionChange, onSubmit, labels }: SearchBarProps) {
  const queryId = useId()
  const regionId = useId()

  function submit(e: FormEvent) {
    e.preventDefault()
    onSubmit()
  }

  return (
    <form className="searchbar" role="search" onSubmit={submit}>
      <div className="searchbar-query">
        <label htmlFor={queryId} className="visually-hidden">
          {labels?.query ?? t.search.queryLabel}
        </label>
        <SearchIcon className="searchbar-icon" size={20} />
        <input
          id={queryId}
          className="searchbar-input"
          type="search"
          value={query}
          placeholder={labels?.placeholder ?? t.search.queryPlaceholder}
          onChange={(e) => onQueryChange(e.target.value)}
        />
      </div>
      {regions && (
        <div className="searchbar-region">
          <label htmlFor={regionId} className="visually-hidden">
            {labels?.select ?? t.search.regionLabel}
          </label>
          <select id={regionId} className="searchbar-select" value={region} onChange={(e) => onRegionChange?.(e.target.value)}>
            <option value="">{labels?.allOptions ?? t.search.allRegions}</option>
            {regions.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
          <ChevronDownIcon className="searchbar-chevron" size={18} />
        </div>
      )}
      <Button type="submit" className="searchbar-go">
        {t.search.submit}
      </Button>
    </form>
  )
}

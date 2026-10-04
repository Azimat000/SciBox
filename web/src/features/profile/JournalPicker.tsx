import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { TextField } from '../../ui/TextField'
import { searchJournals, type FoundJournal, type JournalRef } from './api'
import { QuartileMark } from './ProfileView'

/** Журнал публикации: ISSN и, если он есть в справочнике, сам журнал. */
export type JournalChoice = { issn: string; journal: JournalRef | null }

type Props = {
  value: JournalChoice
  onChange: (next: JournalChoice) => void
  /** Человек выбрал журнал из поиска (окно подставит название в «Журнал, сборник…», если оно пустое). */
  onPick?: (found: FoundJournal) => void
  error?: string
}

const jp = t.profile.journals.picker
const MIN_QUERY = 2
const DEBOUNCE_MS = 300

/**
 * Журнал из справочника SCImago. Выбранный показывается строкой с квартилем и кнопками «Другой журнал» и «Убрать»;
 * без журнала — поле поиска по названию или ISSN с выдачей под ним. Поиск идёт на сервере (справочник большой).
 */
export function JournalPicker({ value, onChange, onPick, error }: Props) {
  const [searching, setSearching] = useState(value.issn === '')
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query.trim()), DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [query])

  const ready = debounced.length >= MIN_QUERY
  const found = useQuery({
    queryKey: ['journals', debounced],
    queryFn: ({ signal }) => searchJournals(debounced, signal),
    enabled: searching && ready,
    staleTime: 5 * 60_000,
    retry: false,
  })

  function choose(j: FoundJournal) {
    onChange({ issn: j.issn, journal: { title: j.title, issn: j.issn, quartile: j.quartile, year: j.year } })
    onPick?.(j)
    setSearching(false)
    setQuery('')
  }

  if (!searching && value.issn !== '') {
    const j = value.journal
    return (
      <div className="journal-picker profile-field-wide">
        <p className="journal-picker-label">{jp.title}</p>
        <div className="journal-chosen">
          <div className="journal-chosen-body">
            {j ? (
              <>
                <p className="journal-title">{j.title}</p>
                <p className="journal-meta">
                  <span className="num">{jp.issn(value.issn)}</span>
                  {j.quartile != null ? <QuartileMark quartile={j.quartile} year={j.year} /> : <span>{jp.noQuartile}</span>}
                </p>
              </>
            ) : (
              <p className="journal-meta">{jp.unknownIssn(value.issn)}</p>
            )}
          </div>
          <div className="journal-chosen-actions">
            <Button variant="quiet" size="sm" onClick={() => setSearching(true)}>
              {jp.change}
            </Button>
            <Button variant="quiet" size="sm" onClick={() => onChange({ issn: '', journal: null })}>
              {jp.remove}
            </Button>
          </div>
        </div>
        {error && (
          <div className="field-error">
            <p className="field-message">{error}</p>
          </div>
        )}
      </div>
    )
  }

  const items = found.data ?? []
  let status = ''
  if (query.trim() !== '' && query.trim().length < MIN_QUERY) status = jp.tooShort
  else if (ready && found.isFetching) status = jp.searching
  else if (ready && found.isError) status = jp.error
  else if (ready && found.isSuccess && items.length === 0) status = jp.nothing

  return (
    <div className="journal-picker profile-field-wide">
      <TextField
        label={jp.title}
        hint={jp.hint}
        optional
        type="search"
        name="journal-search"
        autoComplete="off"
        placeholder={jp.search}
        value={query}
        error={error}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          // Enter в поиске не отправляет всю форму публикации
          if (e.key === 'Enter') e.preventDefault()
        }}
      />
      <p className="journal-status" role="status">
        {status}
      </p>
      {ready && items.length > 0 && (
        <ul className="journal-results" aria-label={jp.results} aria-busy={found.isFetching}>
          {items.map((j) => (
            <li key={j.id} className="journal-result">
              <div className="journal-result-body">
                <p className="journal-title">{j.title}</p>
                <p className="journal-meta">
                  <span className="num">{jp.issn(j.issns.join(', '))}</span>
                  {j.publisher && <span>{j.publisher}</span>}
                  {j.quartile != null ? <QuartileMark quartile={j.quartile} year={j.year} /> : <span>{jp.noQuartile}</span>}
                </p>
              </div>
              <Button variant="secondary" size="sm" aria-label={jp.chooseAria(j.title)} onClick={() => choose(j)}>
                {jp.choose}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {value.issn !== '' && (
        <Button variant="quiet" size="sm" onClick={() => setSearching(false)}>
          {jp.keep}
        </Button>
      )}
    </div>
  )
}

import { plural } from '../lib/plural'
import { t } from '../i18n'
import './ResultsHeader.css'

type ResultsHeaderProps = {
  count: number
  /** Как упорядочен список: «сначала ближайший срок». */
  sortedBy?: string
}

/** Строка над списком: сколько нашлось и как упорядочено. Под ней чёрная линия, как под колонтитулом журнала. */
export function ResultsHeader({ count, sortedBy }: ResultsHeaderProps) {
  const found = t.search.found(count, plural(count, t.search.foundForms), plural(count, t.search.vacancyForms))
  return (
    <p className="results-header" aria-live="polite">
      <span className="num">{found}</span>
      {sortedBy && <span> · {sortedBy}</span>}
    </p>
  )
}

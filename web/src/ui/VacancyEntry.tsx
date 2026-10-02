import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { t } from '../i18n'
import { Deadline } from './Deadline'
import { Tag } from './Tag'
import './VacancyEntry.css'

export type VacancyEntryProps = {
  to: string
  title: string
  organization: string
  city: string
  /** Две-три строки о позиции. */
  abstract?: string
  /** Уровень R1–R4 или «ППС» и его расшифровка: [«R3», «состоявшийся исследователь»]. */
  level?: readonly [string, string]
  /** Остальные факты: область науки, ставка, срок договора. */
  facts?: readonly string[]
  competition?: boolean
  /** Срок подачи, `2026-11-14`. */
  deadline?: string
  now?: Date
  /** Уровень заголовка в документе; по умолчанию h3. */
  headingLevel?: 2 | 3 | 4
  footer?: ReactNode
}

/** Вакансия в списке, набранная как запись в содержании журнала. */
export function VacancyEntry({
  to,
  title,
  organization,
  city,
  abstract,
  level,
  facts = [],
  competition = false,
  deadline,
  now,
  headingLevel = 3,
  footer,
}: VacancyEntryProps) {
  const Heading = `h${headingLevel}` as const
  return (
    <article className="entry">
      <Heading className="entry-title">
        <Link to={to}>{title}</Link>
      </Heading>
      <p className="entry-org">
        {organization}, {city}
      </p>
      {deadline && (
        <div className="entry-side">
          <Deadline date={deadline} now={now} align="end" />
        </div>
      )}
      {abstract && <p className="entry-abstract">{abstract}</p>}
      <ul className="entry-facts" aria-label={t.vacancy.facts}>
        {level && (
          <li>
            <strong>{level[0]}</strong> · {level[1]}
          </li>
        )}
        {facts.map((fact) => (
          <li key={fact}>{fact}</li>
        ))}
        {competition && (
          <li>
            <Tag>{t.vacancy.competition}</Tag>
          </li>
        )}
      </ul>
      {footer && <div className="entry-footer">{footer}</div>}
    </article>
  )
}

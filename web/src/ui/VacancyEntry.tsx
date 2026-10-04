import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { t } from '../i18n'
import { typo } from '../lib/typo'
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
  /** Остальные факты: должность, ставка, срок договора, деньги. */
  facts?: readonly string[]
  /** Научные специальности одной строкой: «Физическая химия и ещё 1». */
  topics?: string
  competition?: boolean
  /** Срок подачи, `2026-11-14`; null — срока нет, приём до закрытия; не задан — срок не показываем. */
  deadline?: string | null
  now?: Date
  /** Уровень заголовка в документе; по умолчанию h3. */
  headingLevel?: 2 | 3 | 4
  footer?: ReactNode
  /** Действие над записью (например, закладка): стоит в правой колонке под сроком. */
  action?: ReactNode
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
  topics,
  competition = false,
  deadline,
  now,
  headingLevel = 3,
  footer,
  action,
}: VacancyEntryProps) {
  const Heading = `h${headingLevel}` as const
  const hasDeadline = deadline !== undefined
  return (
    <article className="entry">
      <Heading className="entry-title">
        <Link to={to}>{typo(title)}</Link>
      </Heading>
      <p className="entry-org">
        {organization}, {city}
      </p>
      {(hasDeadline || action) && (
        <div className="entry-side">
          {deadline ? (
            <Deadline date={deadline} now={now} align="end" />
          ) : (
            deadline === null && (
              <p className="deadline deadline-open" data-align="end">
                <span className="deadline-date">{t.deadline.open}</span>
                <span className="deadline-rest">{t.deadline.openHint}</span>
              </p>
            )
          )}
          {action && <div className="entry-action">{action}</div>}
        </div>
      )}
      {abstract && <p className="entry-abstract">{typo(abstract)}</p>}
      <ul className="entry-facts" aria-label={t.vacancy.facts}>
        {competition && (
          <li className="entry-facts-tag">
            <Tag tone="accent">{t.vacancy.competition}</Tag>
          </li>
        )}
        {level && (
          <li>
            <strong>{level[0]}</strong> {level[1]}
          </li>
        )}
        {facts.map((fact) => (
          <li key={fact}>{fact}</li>
        ))}
      </ul>
      {topics && <p className="entry-topics">{topics}</p>}
      {footer && <div className="entry-footer">{footer}</div>}
    </article>
  )
}

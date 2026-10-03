import { Link } from 'react-router'
import { t } from '../../i18n'
import { remainingText } from '../../lib/deadline'
import { plural } from '../../lib/plural'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { RequireUser } from '../orgs/RequireUser'
import { LoadFailed, PageSkeleton } from '../orgs/states'
import { placeText } from '../vacancies/labels'
import { useCalendar, type DeadlineItem } from './api'
import { ShelfTabs } from './ShelfTabs'
import '../../ui/Deadline.css'
import './matching.css'

/** «Сроки»: когда заканчивается приём заявок на вакансии из избранного, по порядку и по месяцам. */
export function DeadlinesPage() {
  return <RequireUser>{() => <Deadlines />}</RequireUser>
}

/** За сколько дней начинаются напоминания о сроке (D-105). */
const REMINDER_DAYS = 7

const monthFormat = new Intl.DateTimeFormat('ru-RU', { month: 'long', year: 'numeric' })
const weekdayFormat = new Intl.DateTimeFormat('ru-RU', { weekday: 'short' })

/** Дата вида `2026-11-14` без пересчёта часовых поясов. */
function parseDay(date: string): Date {
  const [y, m, d] = date.split('-').map(Number)
  return new Date(y, m - 1, d)
}

type Month = { key: string; label: string; items: DeadlineItem[] }

function groupByMonth(items: DeadlineItem[]): Month[] {
  const months: Month[] = []
  for (const item of items) {
    const day = parseDay(item.vacancy.deadline)
    const key = `${day.getFullYear()}-${day.getMonth()}`
    const last = months.at(-1)
    if (last && last.key === key) last.items.push(item)
    else months.push({ key, label: monthFormat.format(day), items: [item] })
  }
  return months
}

function Deadlines() {
  const m = t.matching.deadlines
  const query = useCalendar()

  if (query.isPending) return <PageSkeleton />
  if (query.isError) return <LoadFailed title={m.loadError} error={query.error} onRetry={() => void query.refetch()} />

  const { items, without_deadline: undated } = query.data
  return (
    <div className="page shelf-page">
      <h1>{m.title}</h1>
      <p className="lead">{m.lead}</p>
      <ShelfTabs current="deadlines" />
      {items.length === 0 ? (
        <EmptyState
          headingLevel={2}
          title={m.emptyTitle}
          text={m.emptyText}
          action={<ButtonLink to="/favorites">{m.emptyAction}</ButtonLink>}
        />
      ) : (
        <div className="cal">
          {groupByMonth(items).map((month) => (
            <section className="cal-month" key={month.key} aria-labelledby={`cal-${month.key}`}>
              <h2 id={`cal-${month.key}`} className="cal-month-title">
                {month.label}
              </h2>
              <ol className="cal-list" aria-labelledby={`cal-${month.key}`}>
                {month.items.map((item) => (
                  <Entry key={item.vacancy.id} item={item} />
                ))}
              </ol>
            </section>
          ))}
        </div>
      )}
      {undated > 0 && (
        <p className="cal-note">
          {m.undated(undated, plural(undated, m.undatedForms))} <Link to="/favorites">{m.toFavorites}</Link>
        </p>
      )}
      <p className="cal-note">
        {m.mailNote} <Link to="/account">{m.mailLink}</Link>
      </p>
    </div>
  )
}

function Entry({ item }: { item: DeadlineItem }) {
  const card = item.vacancy
  const day = parseDay(card.deadline)
  // Маркер у срока, о котором мы уже напоминаем (в пределах недели), и только если человек ещё не откликнулся.
  const urgent = item.days_left <= REMINDER_DAYS && !item.application_id
  const place = placeText(card)
  return (
    <li className="cal-entry">
      <time className="cal-date" dateTime={card.deadline}>
        <span className={urgent ? 'cal-day mark' : 'cal-day'}>{day.getDate()}</span>
        <span className="cal-weekday">{weekdayFormat.format(day)}</span>
      </time>
      <div className="cal-body">
        <h3 className="cal-title">
          <Link to={`/vacancies/${card.id}`}>{card.title}</Link>
        </h3>
        <p className="cal-org">{place ? `${card.organization.name}, ${place}` : card.organization.name}</p>
        <p className="cal-left">
          <span className="num">{remainingText(item.days_left)}</span>
          {item.application_id && <Tag tone="accent">{t.matching.deadlines.applied}</Tag>}
        </p>
      </div>
    </li>
  )
}

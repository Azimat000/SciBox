import { useState } from 'react'
import { describeDeadline } from '../lib/deadline'
import { t } from '../i18n'
import './Deadline.css'

type DeadlineProps = {
  /** Дата вида `2026-11-14`. */
  date: string
  /** «Сейчас», подменяется в тестах. */
  now?: Date
  align?: 'start' | 'end'
}

/** Срок подачи заявок. Близкий срок (до двух недель) отмечен жёлтым маркером. */
export function Deadline({ date, now, align = 'start' }: DeadlineProps) {
  const [mountedAt] = useState(() => new Date())
  const view = describeDeadline(date, now ?? mountedAt)
  if (!view) return null
  const label = view.expired ? t.deadline.closed : t.deadline.apply(view.date)

  return (
    <p className="deadline" data-align={align} data-state={view.expired ? 'expired' : view.urgent ? 'urgent' : 'normal'}>
      <time dateTime={date} className={view.urgent ? 'deadline-date mark' : 'deadline-date'}>
        {label}
      </time>
      <span className="deadline-rest">{view.relative}</span>
    </p>
  )
}

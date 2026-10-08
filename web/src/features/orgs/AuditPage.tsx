import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Tag } from '../../ui/Tag'
import { useMe } from '../auth/api'
import { Link } from 'react-router'
import { useAudit, type AuditEntry } from './api'
import { auditDay, auditTime, describeAudit, quoted } from './auditText'
import { useManageContext } from './manageContext'
import { LoadFailed, PageSkeleton } from './states'

/** Журнал действий организации. Только для владельца: в нём действия и имена всех сотрудников. */
export function AuditPage() {
  const { slug, viewer } = useManageContext()
  const audit = useAudit(slug, viewer.can_view_audit)
  const { user } = useMe()

  if (!viewer.can_view_audit) {
    return (
      <EmptyState
        headingLevel={2}
        title={t.orgs.manage.noAccessTitle}
        text={t.orgs.manage.noAccessText}
        action={<ButtonLink to={`/my-organization/${slug}`}>{t.orgs.manage.tabData}</ButtonLink>}
      />
    )
  }
  if (audit.isPending) return <PageSkeleton />
  // Ошибка следующей страницы не прячет уже загруженное: у неё своё сообщение под списком.
  if (audit.isError && !audit.data) return <LoadFailed title={t.orgs.audit.loadError} error={audit.error} onRetry={() => void audit.refetch()} />

  const failedMore = audit.isFetchNextPageError
  const more = () => void audit.fetchNextPage()
  const entries = (audit.data?.pages ?? []).flatMap((p) => p.items)
  // Записи идут от новых к старым; новый заголовок дня ставится, когда день меняется.
  const days: { day: string; items: AuditEntry[] }[] = []
  for (const e of entries) {
    const day = auditDay(e.at)
    const last = days.at(-1)
    if (last && last.day === day) last.items.push(e)
    else days.push({ day, items: [e] })
  }

  return (
    <>
      <p className="manage-intro">{t.orgs.audit.lead}</p>
      <section className="manage-section" aria-labelledby="audit-list">
        <h2 id="audit-list">{t.orgs.audit.listTitle}</h2>
        {entries.length === 0 ? (
          <EmptyState title={t.orgs.audit.emptyTitle} text={t.orgs.audit.emptyText} />
        ) : (
          days.map(({ day, items }) => (
            <section key={day} className="audit-day" aria-label={day}>
              <h3 className="audit-day-title">{day}</h3>
              <ol className="audit-entries">
                {items.map((e) => (
                  <AuditRow key={e.id} entry={e} slug={slug} youId={user?.id} />
                ))}
              </ol>
            </section>
          ))
        )}
        {failedMore && (
          <Alert
            kind="error"
            title={t.orgs.audit.moreFailed}
            action={
              <Button variant="secondary" size="sm" onClick={more}>
                {t.common.retry}
              </Button>
            }
          />
        )}
        {audit.hasNextPage && !failedMore && (
          <div className="audit-more">
            <Button variant="secondary" loading={audit.isFetchingNextPage} onClick={more}>
              {t.orgs.audit.more}
            </Button>
          </div>
        )}
      </section>
    </>
  )
}

function AuditRow({ entry, slug, youId }: { entry: AuditEntry; slug: string; youId: string | undefined }) {
  const line = describeAudit(entry, slug)
  const facts = [line.subject, line.detail].filter((x): x is string => Boolean(x))
  return (
    <li className="audit-row">
      <time className="audit-time num" dateTime={entry.at}>
        {auditTime(entry.at)}
      </time>
      <div className="audit-main">
        <p className="audit-what">{line.what}</p>
        {line.target && (
          <p className="audit-target">
            {line.target.to ? <Link to={line.target.to}>{quoted(line.target.label)}</Link> : <>{quoted(line.target.label)}</>}
          </p>
        )}
        {facts.length > 0 && <p className="audit-facts">{facts.join(' · ')}</p>}
        <p className="audit-who">
          <span className="visually-hidden">{t.orgs.audit.actors}: </span>
          {entry.actor?.name ?? t.orgs.audit.system}
          {entry.actor && youId === entry.actor.id && <> <Tag>{t.orgs.audit.you}</Tag></>}
        </p>
      </div>
    </li>
  )
}

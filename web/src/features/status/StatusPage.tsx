import { useHealth } from '../../api/health'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { StatusIcon } from '../../ui/StatusIcon'
import { describeHealth } from './describe'
import './status.css'

export function StatusPage() {
  const health = useHealth()
  const view = describeHealth(health)
  const s = t.status

  return (
    <div className="page status-page">
      <h1>{s.title}</h1>
      <p className="lead">{s.lead}</p>

      <section className="check" aria-labelledby="check-title" aria-busy={health.isFetching}>
        <h2 id="check-title">{s.checkTitle}</h2>
        <dl className="check-list" aria-live="polite">
          {view.rows.map((row) => (
            <div key={row.label} className="check-row" data-kind={row.kind}>
              <dt>{row.label}</dt>
              <dd>
                <StatusIcon kind={row.kind} />
                <span>{row.value}</span>
              </dd>
            </div>
          ))}
        </dl>
        {view.hint && <p className="check-hint">{view.hint}</p>}
        {view.version && <p className="check-meta">{view.version}</p>}
        {health.isError && (
          <Button variant="secondary" onClick={() => void health.refetch()} loading={health.isFetching}>
            {t.common.retry}
          </Button>
        )}
      </section>

      <p className="note">{s.note}</p>
    </div>
  )
}

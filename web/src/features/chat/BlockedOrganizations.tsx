import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button } from '../../ui/Button'
import { Skeleton } from '../../ui/Skeleton'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { keys, unblockOrganization, useChatBlocks, type ChatBlock } from './api'
import './chat-tools.css'

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })

/**
 * Раздел «Заблокированные организации» в настройках аккаунта (срез 36): кого человек закрыл от себя и как снять блокировку.
 * Пока на сервере нет чата (404), раздела нет вовсе: пустой список там означал бы неправду.
 */
export function BlockedOrganizations() {
  const b = t.chat.blocked
  const query = useChatBlocks()

  if (query.isError && query.error instanceof ApiError && query.error.status === 404) return null

  return (
    <section className="account-section" aria-labelledby="account-blocked">
      <h2 id="account-blocked" className="auth-section-title">
        {b.title}
      </h2>
      <p className="auth-note">{b.lead}</p>
      {query.isPending ? (
        <div role="status" aria-busy="true" aria-label={t.common.loading}>
          <Skeleton width="60%" height="1.25rem" />
        </div>
      ) : query.isError ? (
        <Alert
          kind="error"
          title={b.loadError}
          action={
            <Button variant="secondary" size="sm" onClick={() => void query.refetch()}>
              {t.common.retry}
            </Button>
          }
        >
          {describeError(query.error)}
        </Alert>
      ) : query.data.length === 0 ? (
        <p className="blocked-empty">{b.empty}</p>
      ) : (
        <ul className="blocked-list">
          {query.data.map((block) => (
            <BlockedRow key={block.org_id} block={block} />
          ))}
        </ul>
      )}
    </section>
  )
}

function BlockedRow({ block }: { block: ChatBlock }) {
  const b = t.chat.blocked
  const client = useQueryClient()
  const toast = useToast()
  const unblock = useMutation({
    mutationFn: () => unblockOrganization(block.org_id),
    onSuccess: () => {
      // Переписки с организацией открылись: списки, шапки и значок перечитываются
      void client.invalidateQueries({ queryKey: keys.all })
      toast.show({ kind: 'success', title: b.unblocked, text: b.unblockedText })
    },
    onError: (err) => {
      // Блокировки уже нет (сняли в другой вкладке): список просто обновляется
      if (err instanceof ApiError && err.status === 404) void client.invalidateQueries({ queryKey: keys.all })
      else toast.show({ kind: 'error', title: b.unblockFailed, text: describeError(err) })
    },
  })
  return (
    <li className="blocked-row">
      <div className="blocked-info">
        <Link className="blocked-name" to={`/organizations/${encodeURIComponent(block.org_slug)}`}>
          {block.org_name}
        </Link>
        <span className="blocked-since num">{b.since(dateFormat.format(new Date(block.created_at)))}</span>
      </div>
      <Button size="sm" variant="secondary" loading={unblock.isPending} aria-label={b.unblockLabel(block.org_name)} onClick={() => unblock.mutate()}>
        {b.unblock}
      </Button>
    </li>
  )
}

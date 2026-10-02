import { ApiError } from '../../api/client'
import type { useHealth } from '../../api/health'
import { t } from '../../i18n'
import type { StatusKind } from '../../ui/StatusIcon'

export type Row = { label: string; kind: StatusKind; value: string }

export type View = { rows: Row[]; hint?: string; version?: string }

// Переводит состояние запроса /api/health в строки таблицы и подсказку.
export function describeHealth(health: ReturnType<typeof useHealth>): View {
  const s = t.status
  if (health.isPending) {
    return {
      rows: [
        { label: s.server, kind: 'pending', value: s.checking },
        { label: s.database, kind: 'pending', value: s.checking },
      ],
    }
  }
  if (health.isSuccess) {
    return {
      rows: [
        { label: s.server, kind: 'ok', value: s.serverOk },
        { label: s.database, kind: 'ok', value: s.databaseOk(health.data.database.schema_version) },
      ],
      version: s.version(health.data.version),
    }
  }
  const code = health.error instanceof ApiError ? health.error.code : undefined
  if (code === 'database_unavailable') {
    return {
      rows: [
        { label: s.server, kind: 'ok', value: s.serverOk },
        { label: s.database, kind: 'fail', value: s.databaseDown },
      ],
      hint: s.databaseDownHint,
    }
  }
  if (code === 'unreachable') {
    return {
      rows: [
        { label: s.server, kind: 'fail', value: s.serverDown },
        { label: s.database, kind: 'pending', value: s.databaseUnknown },
      ],
      hint: s.serverDownHint,
    }
  }
  return {
    rows: [
      { label: s.server, kind: 'fail', value: health.error?.message ?? s.serverDown },
      { label: s.database, kind: 'pending', value: s.databaseUnknown },
    ],
    hint: s.unexpectedHint,
  }
}

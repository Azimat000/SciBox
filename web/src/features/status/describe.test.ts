import { describe, expect, it } from 'vitest'
import type { useHealth } from '../../api/health'
import { describeHealth } from './describe'

type Health = ReturnType<typeof useHealth>

describe('describeHealth', () => {
  it('falls back to a generic message for a non-API error', () => {
    const view = describeHealth({ isPending: false, isSuccess: false, error: new Error('boom') } as unknown as Health)
    expect(view.rows[0]).toEqual({ label: 'Сервер', kind: 'fail', value: 'boom' })
    expect(view.hint).toMatch(/журнале сервера/)
  })

  it('uses "server down" text when there is no error object', () => {
    const view = describeHealth({ isPending: false, isSuccess: false, error: null } as unknown as Health)
    expect(view.rows[0].value).toBe('Не отвечает')
  })
})

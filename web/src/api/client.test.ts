import { describe, expect, it, vi } from 'vitest'
import { jsonResponse } from '../test/render'
import { ApiError, BAD_RESPONSE, UNREACHABLE, apiGet } from './client'

function stubFetch(impl: typeof fetch) {
  const fn = vi.fn<typeof fetch>(impl)
  vi.stubGlobal('fetch', fn)
  return fn
}

async function caught(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError)
    return err as ApiError
  }
  throw new Error('expected rejection')
}

describe('apiGet', () => {
  it('returns parsed JSON and asks for JSON', async () => {
    const fetch = stubFetch(async () => jsonResponse(200, { status: 'ok' }))
    await expect(apiGet('/api/health')).resolves.toEqual({ status: 'ok' })
    expect(fetch).toHaveBeenCalledWith('/api/health', expect.objectContaining({ headers: { Accept: 'application/json' } }))
  })

  it('passes the abort signal through', async () => {
    const fetch = stubFetch(async () => jsonResponse(200, {}))
    const ctrl = new AbortController()
    await apiGet('/x', { signal: ctrl.signal })
    expect(fetch.mock.calls[0][1]).toMatchObject({ signal: ctrl.signal })
  })

  it('turns the unified error format into ApiError', async () => {
    stubFetch(async () => jsonResponse(503, { error: { code: 'database_unavailable', message: 'База данных недоступна' } }))
    const err = await caught(apiGet('/api/health'))
    expect(err).toMatchObject({ status: 503, code: 'database_unavailable', message: 'База данных недоступна' })
  })

  it.each([
    ['plain text from the dev proxy', new Response('', { status: 500 })],
    ['JSON without error', jsonResponse(502, { oops: true })],
    ['error is not an object', jsonResponse(502, { error: 'bad' })],
    ['error is null', jsonResponse(502, { error: null })],
    ['code is not a string', jsonResponse(502, { error: { code: 1, message: 'x' } })],
    ['message is missing', jsonResponse(502, { error: { code: 'x' } })],
    ['body is null', jsonResponse(502, null)],
  ])('treats a foreign error response as unreachable: %s', async (_name, res) => {
    stubFetch(async () => res)
    const err = await caught(apiGet('/x'))
    expect(err.code).toBe(UNREACHABLE)
    expect(err.status).toBe(res.status)
  })

  it('reports a network failure as unreachable', async () => {
    stubFetch(async () => {
      throw new TypeError('Failed to fetch')
    })
    const err = await caught(apiGet('/x'))
    expect(err).toMatchObject({ status: 0, code: UNREACHABLE, message: 'Сервер не отвечает' })
  })

  it('rethrows aborts untouched', async () => {
    const abort = new DOMException('aborted', 'AbortError')
    stubFetch(async () => {
      throw abort
    })
    await expect(apiGet('/x')).rejects.toBe(abort)
  })

  it('rejects a successful response that is not JSON', async () => {
    stubFetch(async () => new Response('<html>', { status: 200 }))
    const err = await caught(apiGet('/x'))
    expect(err).toMatchObject({ status: 200, code: BAD_RESPONSE })
  })
})

import { describe, expect, it, vi } from 'vitest'
import { jsonResponse } from '../test/render'
import { ApiError, BAD_RESPONSE, UNREACHABLE, apiGet, apiSend, apiSendForm } from './client'

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

describe('apiSend', () => {
  it('sends JSON with the method, content type and body', async () => {
    const fetch = stubFetch(async () => jsonResponse(200, { user: null }))
    await expect(apiSend('POST', '/api/auth/login', { email: 'a@b.ru' })).resolves.toEqual({ user: null })
    const [path, init] = fetch.mock.calls[0]
    expect(path).toBe('/api/auth/login')
    expect(init).toMatchObject({
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: '{"email":"a@b.ru"}',
    })
  })

  it('sends an empty object when there is no body, so the server still gets JSON', async () => {
    const fetch = stubFetch(async () => new Response(null, { status: 204 }))
    await apiSend('POST', '/api/auth/logout')
    expect(fetch.mock.calls[0][1]).toMatchObject({ body: '{}' })
  })

  it('treats 204 as success without a body', async () => {
    stubFetch(async () => new Response(null, { status: 204 }))
    await expect(apiSend('PATCH', '/x', { a: 1 })).resolves.toBeUndefined()
  })

  it('does not accept 204 for GET: a read without data is a broken answer', async () => {
    stubFetch(async () => new Response(null, { status: 204 }))
    const err = await caught(apiGet('/x'))
    expect(err.code).toBe(BAD_RESPONSE)
  })

  it('carries field messages and retry delay from the error body', async () => {
    stubFetch(async () =>
      jsonResponse(422, { error: { code: 'validation_failed', message: 'Проверьте поля', fields: { email: 'Занято', age: 5, nothing: null } } }),
    )
    const err = await caught(apiSend('POST', '/x', {}))
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', fields: { email: 'Занято' }, retryAfter: undefined })
    expect(err.fields).not.toHaveProperty('age')

    stubFetch(async () => jsonResponse(429, { error: { code: 'rate_limited', message: 'Много', retry_after: 120 } }))
    expect(await caught(apiSend('POST', '/x', {}))).toMatchObject({ code: 'rate_limited', retryAfter: 120, fields: {} })
  })

  it('ignores junk in fields and retry_after', async () => {
    stubFetch(async () => jsonResponse(400, { error: { code: 'x', message: 'y', fields: 'nope', retry_after: 'soon' } }))
    const err = await caught(apiSend('POST', '/x', {}))
    expect(err.fields).toEqual({})
    expect(err.retryAfter).toBeUndefined()
    stubFetch(async () => jsonResponse(400, { error: { code: 'x', message: 'y', fields: null } }))
    expect((await caught(apiSend('POST', '/x', {}))).fields).toEqual({})
  })

  it('reports an unreachable server and a broken success body like apiGet does', async () => {
    stubFetch(async () => {
      throw new TypeError('Failed to fetch')
    })
    expect(await caught(apiSend('POST', '/x', {}))).toMatchObject({ code: UNREACHABLE, status: 0 })
    stubFetch(async () => new Response('<html>', { status: 200 }))
    expect(await caught(apiSend('POST', '/x', {}))).toMatchObject({ code: BAD_RESPONSE })
    stubFetch(async () => new Response('', { status: 502 }))
    expect(await caught(apiSend('POST', '/x', {}))).toMatchObject({ code: UNREACHABLE, status: 502 })
  })
})

describe('apiSendForm', () => {
  it('sends the fields as JSON in the data part and the files as file parts, leaving the content type to the browser', async () => {
    const fetch = stubFetch(async () => jsonResponse(201, { ok: true }))
    const a = new File(['%PDF-a'], 'a.pdf', { type: 'application/pdf' })
    const b = new File(['%PDF-b'], 'б.pdf', { type: 'application/pdf' })
    await expect(apiSendForm('POST', '/api/applications', { vacancy_id: 'v1', text: 'привет' }, [a, b])).resolves.toEqual({ ok: true })
    const [path, init] = fetch.mock.calls[0]
    expect(path).toBe('/api/applications')
    expect(init).toMatchObject({ method: 'POST', headers: { Accept: 'application/json' } })
    expect((init!.headers as Record<string, string>)['Content-Type']).toBeUndefined()
    const form = init!.body as FormData
    expect(JSON.parse(String(form.get('data')))).toEqual({ vacancy_id: 'v1', text: 'привет' })
    expect(form.getAll('file').map((f) => (f as File).name)).toEqual(['a.pdf', 'б.pdf'])
  })

  it('works without files and accepts an empty answer', async () => {
    const fetch = stubFetch(async () => new Response(null, { status: 204 }))
    await expect(apiSendForm('POST', '/x', { a: 1 })).resolves.toBeUndefined()
    expect((fetch.mock.calls[0][1]!.body as FormData).getAll('file')).toEqual([])
  })

  it('turns an error answer into ApiError with the field messages', async () => {
    stubFetch(async () => jsonResponse(422, { error: { code: 'validation_failed', message: 'Проверьте файлы', fields: { files: 'Файл не PDF' } } }))
    const err = await caught(apiSendForm('POST', '/x', {}, []))
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', fields: { files: 'Файл не PDF' } })
  })
})

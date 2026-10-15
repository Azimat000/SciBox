import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { textMessage } from '../../test/chat'
import { chatHub } from './hub'
import { useConversation } from './useConversation'

// Лента одной переписки без экрана: охрана подгрузки, повтор отправки, дочитывание (срез 35).

afterEach(() => vi.unstubAllGlobals())

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('useConversation', () => {
  it('asks for older messages once while the page loads and not at all when nothing is older', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    const fetchMock = vi.fn(async (url: string) => {
      if (url.includes('before=')) {
        await gate
        return json({ items: [textMessage(1)], has_more: false })
      }
      return json({ items: [textMessage(2)], has_more: true })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useConversation('c1', 0))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => {
      void result.current.loadOlder()
      void result.current.loadOlder()
    })
    release()
    await waitFor(() => expect(result.current.state.items.map((m) => m.seq)).toEqual([1, 2]))
    expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('before='))).toHaveLength(1)
    await act(async () => {
      await result.current.loadOlder()
    })
    expect(fetchMock.mock.calls.filter((c) => String(c[0]).includes('before='))).toHaveLength(1)
  })

  it('does not ask for older messages before the first page arrived', async () => {
    const fetchMock = vi.fn(async (_url: string) => json({ items: [], has_more: false }))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useConversation('c1', 0))
    await act(async () => {
      await result.current.loadOlder()
    })
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    expect(fetchMock.mock.calls.every((c) => !String(c[0]).includes('before='))).toBe(true)
  })

  it('retries a failed send with the same text and client id', async () => {
    let n = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          n += 1
          const body = JSON.parse(String(init.body)) as { text: string; client_id: string }
          return n === 1 ? json({ error: { code: 'internal', message: 'сбой' } }, 500) : json({ message: textMessage(1, { text: body.text, client_id: body.client_id }) }, 201)
        }
        return json({ items: [], has_more: false })
      }),
    )
    const failed = vi.fn()
    const { result } = renderHook(() => useConversation('c1', 0, { onSendFailed: failed }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.send('Привет'))
    await waitFor(() => expect(result.current.state.pending[0]?.status).toBe('failed'))
    expect(failed).toHaveBeenCalledTimes(1)
    act(() => result.current.retry(result.current.state.pending[0]))
    await waitFor(() => expect(result.current.state.pending).toHaveLength(0))
    expect(result.current.state.items[0].text).toBe('Привет')
  })

  it('refreshes by reading what is newer than the last message', async () => {
    const fetchMock = vi.fn(async (url: string) => (url.includes('after=1') ? json({ items: [textMessage(2)], has_more: false }) : json({ items: [textMessage(1)], has_more: false })))
    vi.stubGlobal('fetch', fetchMock)
    const { result } = renderHook(() => useConversation('c1', 0))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      await result.current.refresh()
    })
    expect(result.current.state.items.map((m) => m.seq)).toEqual([1, 2])
  })

  it('survives a failing refresh', async () => {
    let fail = false
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => (fail ? json({ error: { code: 'internal', message: 'сбой' } }, 500) : json({ items: [textMessage(1)], has_more: false }))),
    )
    const { result } = renderHook(() => useConversation('c1', 0))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    fail = true
    await act(async () => {
      await result.current.refresh()
    })
    act(() => chatHub.emit({ type: 'reconnected' }))
    expect(result.current.state.items).toHaveLength(1)
  })

  it('removes a message after the server confirmed and keeps it on error', async () => {
    let fail = true
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'DELETE') return fail ? json({ error: { code: 'edit_window_closed', message: 'поздно' } }, 409) : new Response(null, { status: 204 })
        return json({ items: [textMessage(1)], has_more: false })
      }),
    )
    const { result } = renderHook(() => useConversation('c1', 0))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await expect(result.current.remove(1)).rejects.toThrow()
    expect(result.current.state.items[0].deleted).toBe(false)
    fail = false
    await act(async () => {
      await result.current.remove(1)
    })
    expect(result.current.state.items[0].deleted).toBe(true)
  })

  it('takes an edited message from the server answer', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) =>
        init?.method === 'PATCH' ? json({ message: textMessage(1, { text: 'Новый', edited_at: '2026-10-14T11:00:00Z' }) }) : json({ items: [textMessage(1)], has_more: false }),
      ),
    )
    const { result } = renderHook(() => useConversation('c1', 0))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      await result.current.edit(1, 'Новый')
    })
    expect(result.current.state.items[0].text).toBe('Новый')
  })
})

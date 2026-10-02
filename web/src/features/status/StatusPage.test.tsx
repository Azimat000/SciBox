import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { jsonResponse, renderApp } from '../../test/render'

const healthy = {
  status: 'ok',
  product: 'SciBox',
  version: 'dev',
  database: { schema_version: 1, server_version: '16.15' },
}

function row(name: string) {
  const term = screen.getByText(name, { selector: 'dt' })
  return term.parentElement as HTMLElement
}

describe('StatusPage', () => {
  it('shows a pending state while the check runs', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    renderApp('/')
    expect(screen.getByRole('heading', { level: 1, name: 'Вакансии в науке' })).toBeInTheDocument()
    expect(row('Сервер')).toHaveAttribute('data-kind', 'pending')
    expect(within(row('База данных')).getByText('Проверяем…')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Проверить ещё раз' })).not.toBeInTheDocument()
  })

  it('reports a healthy server and database', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse(200, healthy)))
    renderApp('/')
    expect(await screen.findByText('Работает, схема версии 1')).toBeInTheDocument()
    expect(row('Сервер')).toHaveAttribute('data-kind', 'ok')
    expect(screen.getByText('Версия сервера: dev')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Проверить ещё раз' })).not.toBeInTheDocument()
  })

  it('separates "database down" from "server down"', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(503, { error: { code: 'database_unavailable', message: 'База данных недоступна' } })),
    )
    renderApp('/')
    expect(await screen.findByText('Недоступна')).toBeInTheDocument()
    expect(row('Сервер')).toHaveAttribute('data-kind', 'ok')
    expect(row('База данных')).toHaveAttribute('data-kind', 'fail')
    expect(screen.getByText(/make db-up/)).toBeInTheDocument()
  })

  it('explains how to start a server that does not answer, and recovers on retry', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(new Response('', { status: 500 }))
      .mockResolvedValueOnce(jsonResponse(200, healthy))
    vi.stubGlobal('fetch', fetch)
    renderApp('/')

    expect(await screen.findByText('Не отвечает')).toBeInTheDocument()
    expect(screen.getByText(/make dev/)).toBeInTheDocument()
    expect(within(row('База данных')).getByText('Неизвестно, пока не отвечает сервер')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByText('Работает, схема версии 1')).toBeInTheDocument()
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('shows the server message for an unexpected API error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse(500, { error: { code: 'internal', message: 'Что-то сломалось на сервере' } })),
    )
    renderApp('/')
    expect(await screen.findByText('Что-то сломалось на сервере')).toBeInTheDocument()
    expect(row('Сервер')).toHaveAttribute('data-kind', 'fail')
    expect(screen.getByText(/журнале сервера/)).toBeInTheDocument()
  })
})

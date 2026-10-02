import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { productName } from '../config/product'
import { renderApp } from '../test/render'
import { App } from './App'
import { Layout } from './Layout'
import { CrashPage } from './CrashPage'

describe('App shell', () => {
  it('renders on the real browser router by default', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    window.history.pushState({}, '', '/')
    render(<App />)
    expect(await screen.findByRole('link', { name: productName })).toHaveAttribute('href', '/')
    expect(screen.getByRole('main')).toBeInTheDocument()
  })

  it('shows a helpful 404 with a way home', async () => {
    const { router } = renderApp('/no/such/page')
    expect(screen.getByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    await userEvent.click(screen.getByRole('link', { name: 'На главную' }))
    expect(router.state.location.pathname).toBe('/')
  })

  it('shows the crash page when a page throws, and reloads on demand', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    function Boom(): never {
      throw new Error('render failed')
    }
    renderApp('/', [{ element: <Layout />, errorElement: <CrashPage />, children: [{ index: true, element: <Boom /> }] }])

    expect(screen.getByRole('heading', { level: 1, name: 'Что-то пошло не так' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Обновить страницу' }))
    expect(reload).toHaveBeenCalledOnce()
  })
})

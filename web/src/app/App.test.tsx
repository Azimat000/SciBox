import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { productName } from '../config/product'
import { stubApi } from '../test/api'
import { renderApp } from '../test/render'
import { detail, vacancyRoute } from '../test/vacancies'
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

describe('browser tab titles', () => {
  it('names the page: «Вакансии — SciBox», only the product on the home page', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(() => {})))
    const { router } = renderApp('/vacancies')
    await waitFor(() => expect(document.title).toBe(`Вакансии — ${productName}`))
    await router.navigate('/')
    await waitFor(() => expect(document.title).toBe(productName))
    await router.navigate('/my-organization/lab/members')
    await waitFor(() => expect(document.title).toBe(`Управление организацией — ${productName}`))
    await router.navigate('/no/such/page')
    await waitFor(() => expect(document.title).toBe(`Страница не найдена — ${productName}`))
  })

  it('a page with data puts its own name, once the data is there', async () => {
    stubApi(vacancyRoute(detail))
    renderApp(`/vacancies/${detail.id}`)
    expect(document.title).toBe(productName)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    await waitFor(() => expect(document.title).toBe(`${detail.title} — ${productName}`))
  })
})

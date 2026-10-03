import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { APP_ID, notice, noticeList } from '../../test/applications'
import { renderApp } from '../../test/render'
import { POLL_MS } from './api'

afterEach(() => {
  vi.useRealTimers()
})

const bell = (name: RegExp | string = /^Уведомления/) => screen.findByRole('button', { name })
const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/notifications/unread-count': reply(200, { unread: 2 }),
  ...extra,
})
const unread = notice()
const read = notice({ id: 'n0000000-0000-4000-8000-000000000002', read: true, title: 'Отклик отправлен', kind: 'application_sent', body: '', link: `/applications/${APP_ID}`, created_at: '2026-10-02T09:00:00Z' })
const noLink = notice({ id: 'n0000000-0000-4000-8000-000000000003', title: 'Без ссылки', link: '' })

describe('bell in the header', () => {
  it('is not shown to a visitor and asks nothing', async () => {
    const { calls } = stubApi()
    renderApp('/')
    await screen.findByRole('link', { name: 'Войти' })
    expect(screen.queryByRole('button', { name: /^Уведомления/ })).not.toBeInTheDocument()
    expect(calls.filter((c) => c.path.startsWith('/api/notifications'))).toHaveLength(0)
  })

  it('shows how many are unread in its name and as a badge', async () => {
    stubApi(signedIn())
    renderApp('/')
    const button = await bell('Уведомления, непрочитанных: 2')
    expect(within(button).getByText('2')).toBeInTheDocument()
  })

  it('has a plain name and no badge when there is nothing new', async () => {
    stubApi(signedIn({ 'GET /api/notifications/unread-count': reply(200, { unread: 0 }) }))
    renderApp('/')
    const button = await bell('Уведомления')
    expect(within(button).queryByText('0')).not.toBeInTheDocument()
  })

  it('caps a huge number', async () => {
    stubApi(signedIn({ 'GET /api/notifications/unread-count': reply(200, { unread: 250 }) }))
    renderApp('/')
    expect(within(await bell(/непрочитанных: 250/)).getByText('99+')).toBeInTheDocument()
  })

  it('stays quiet when the count cannot be loaded', async () => {
    stubApi(signedIn({ 'GET /api/notifications/unread-count': apiError(500, 'internal', 'Сбой') }))
    renderApp('/')
    expect(within(await bell('Уведомления')).queryByText(/\d/)).not.toBeInTheDocument()
  })

  it('asks for the count again every minute', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { called } = stubApi(signedIn())
    renderApp('/')
    await bell(/непрочитанных: 2/)
    const before = called('GET', '/api/notifications/unread-count').length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS + 100)
    })
    await waitFor(() => expect(called('GET', '/api/notifications/unread-count').length).toBeGreaterThan(before))
  })

  it('opens a panel with the latest notices, unread first marked, and goes to the page of one', async () => {
    const { calls } = stubApi(
      signedIn({
        'GET /api/notifications?limit=6': reply(200, noticeList([unread, read, noLink])),
        [`POST /api/notifications/${unread.id}/read`]: reply(204),
        [`GET /api/candidates/${APP_ID}`]: reply(200, {}),
      }),
    )
    const { router } = renderApp('/')
    await userEvent.click(await bell(/непрочитанных/))
    const panel = document.getElementById('bell-panel')!
    expect(panel).toBeVisible()
    expect(await within(panel).findByText('Новый отклик')).toBeInTheDocument()
    expect(within(panel).getAllByText('Отклик от Анны Смирновой на вакансию «Старший научный сотрудник».')).toHaveLength(2)
    expect(within(panel).getAllByText('(не прочитано)')).toHaveLength(2)
    expect(within(panel).getByRole('link', { name: 'Все уведомления' })).toHaveAttribute('href', '/notifications')
    await userEvent.click(within(panel).getByRole('button', { name: /Новый отклик/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe(`/candidates/${APP_ID}`))
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/notifications/${unread.id}/read`)).toBe(true)
    expect(panel).not.toBeVisible()
  })

  it('does not mark an already read notice again and does not navigate without a link', async () => {
    const { calls } = stubApi(signedIn({ 'GET /api/notifications?limit=6': reply(200, noticeList([read, noLink])), [`POST /api/notifications/${noLink.id}/read`]: reply(204) }))
    const { router } = renderApp('/')
    await userEvent.click(await bell(/непрочитанных/))
    const panel = document.getElementById('bell-panel')!
    await userEvent.click(await within(panel).findByRole('button', { name: /Отклик отправлен/ }))
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
    expect(router.state.location.pathname).toBe(`/applications/${APP_ID}`)
    await userEvent.click(await bell(/непрочитанных/))
    await userEvent.click(await within(panel).findByRole('button', { name: /Без ссылки/ }))
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1))
    expect(router.state.location.pathname).toBe(`/applications/${APP_ID}`)
  })

  it('marks everything read from the panel', async () => {
    const { calls } = stubApi(signedIn({ 'GET /api/notifications?limit=6': reply(200, noticeList([unread, noLink])), 'POST /api/notifications/read-all': reply(204) }))
    renderApp('/')
    await userEvent.click(await bell(/непрочитанных/))
    await userEvent.click(await within(document.getElementById('bell-panel')!).findByRole('button', { name: 'Прочитать все' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === '/api/notifications/read-all')).toBe(true))
  })

  it('says there is nothing, or that the list could not be loaded', async () => {
    stubApi(signedIn({ 'GET /api/notifications?limit=6': reply(200, noticeList([])) }))
    renderApp('/')
    await userEvent.click(await bell(/непрочитанных/))
    expect(await within(document.getElementById('bell-panel')!).findByText('Новых уведомлений нет.')).toBeInTheDocument()
  })

  it('reports a failed load inside the panel', async () => {
    stubApi(signedIn({ 'GET /api/notifications?limit=6': apiError(500, 'internal', 'Сбой') }))
    renderApp('/')
    await userEvent.click(await bell(/непрочитанных/))
    expect(await within(document.getElementById('bell-panel')!).findByText('Не удалось загрузить уведомления')).toBeInTheDocument()
  })

  it('closes with Escape (focus returns to the bell) and with a click elsewhere', async () => {
    stubApi(signedIn({ 'GET /api/notifications?limit=6': reply(200, noticeList([unread])) }))
    renderApp('/')
    const button = await bell(/непрочитанных/)
    const panel = () => document.getElementById('bell-panel')!
    await userEvent.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'true')
    await userEvent.keyboard('{Escape}')
    expect(panel()).not.toBeVisible()
    expect(button).toHaveFocus()
    await userEvent.click(button)
    expect(panel()).toBeVisible()
    await userEvent.click(screen.getByRole('link', { name: 'SciBox' }))
    expect(panel()).not.toBeVisible()
    await userEvent.click(button)
    await userEvent.click(document.body)
    expect(panel()).not.toBeVisible()
    // Другие клавиши и клики внутри окна его не закрывают.
    await userEvent.click(button)
    await userEvent.keyboard('{Tab}')
    await userEvent.click(within(panel()).getByText('Уведомления', { selector: '.bell-panel-title' }))
    expect(panel()).toBeVisible()
    await userEvent.click(button)
    expect(panel()).not.toBeVisible()
  })
})

describe('notifications page', () => {
  const page = (items = [unread, read, noLink], unreadCount?: number, total?: number) => ({
    'GET /api/notifications?limit=20&offset=0': reply(200, noticeList(items, unreadCount, total)),
  })

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/notifications')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists notices with time and an unread mark and opens the one that is clicked', async () => {
    const { calls } = stubApi(signedIn({ ...page(), [`POST /api/notifications/${unread.id}/read`]: reply(204) }))
    const { router } = renderApp('/notifications')
    expect(await screen.findByRole('heading', { level: 1, name: 'Уведомления' })).toBeInTheDocument()
    const rows = (await screen.findAllByRole('listitem')).filter((li) => li.classList.contains('notice-row'))
    expect(rows).toHaveLength(3)
    expect(within(rows[0]).getByText('(не прочитано)')).toBeInTheDocument()
    expect(within(rows[1]).queryByText('(не прочитано)')).not.toBeInTheDocument()
    expect(within(rows[0]).getByText(/^3 октября в \d\d:\d\d$/)).toBeInTheDocument()
    await userEvent.click(within(rows[0]).getByRole('button'))
    await waitFor(() => expect(router.state.location.pathname).toBe(`/candidates/${APP_ID}`))
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/notifications/${unread.id}/read`)).toBe(true)
  })

  it('marks everything read, and only shows the button when something is unread', async () => {
    const { calls } = stubApi(signedIn({ ...page(), 'POST /api/notifications/read-all': reply(204) }))
    renderApp('/notifications')
    await userEvent.click(await screen.findByRole('button', { name: 'Прочитать все' }))
    await waitFor(() => expect(calls.some((c) => c.path === '/api/notifications/read-all')).toBe(true))
  })

  it('hides the button when everything is read, and does not mark read ones again', async () => {
    const { calls } = stubApi(signedIn(page([read], 0)))
    const { router } = renderApp('/notifications')
    await userEvent.click(await screen.findByRole('button', { name: /Отклик отправлен/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe(`/applications/${APP_ID}`))
    expect(screen.queryByRole('button', { name: 'Прочитать все' })).not.toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
  })

  it('stays on the page when a notice has no link', async () => {
    stubApi(signedIn({ ...page([noLink]), [`POST /api/notifications/${noLink.id}/read`]: reply(204) }))
    const { router } = renderApp('/notifications')
    await userEvent.click(await screen.findByRole('button', { name: /Без ссылки/ }))
    expect(router.state.location.pathname).toBe('/notifications')
  })

  it('says there is nothing yet', async () => {
    stubApi(signedIn(page([], 0, 0)))
    renderApp('/notifications')
    expect(await screen.findByRole('heading', { name: 'Пока ничего нет' })).toBeInTheDocument()
  })

  it('offers to retry when the list does not load', async () => {
    let fail = true
    stubApi(signedIn({ 'GET /api/notifications?limit=20&offset=0': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, noticeList([unread]))) }))
    renderApp('/notifications')
    expect(await screen.findByText('Не удалось загрузить уведомления')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByText('Новый отклик')).toBeInTheDocument()
  })

  it('pages through a long list', async () => {
    const items = Array.from({ length: 20 }, (_, i) => notice({ id: `n0000000-0000-4000-8000-${String(100 + i).padStart(12, '0')}`, title: `Событие ${i}`, read: true }))
    stubApi(
      signedIn({
        'GET /api/notifications?limit=20&offset=0': reply(200, noticeList(items, 0, 45)),
        'GET /api/notifications?limit=20&offset=20': reply(200, noticeList([notice({ title: 'Событие второй страницы', read: true })], 0, 45)),
      }),
    )
    const { router } = renderApp('/notifications')
    await screen.findByText('Страница 1 из 3')
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await userEvent.click(screen.getByRole('button', { name: 'Дальше' }))
    expect(await screen.findByText('Событие второй страницы')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    await userEvent.click(screen.getByRole('button', { name: 'Назад' }))
    await screen.findByText('Страница 1 из 3')
    expect(router.state.location.search).toBe('')
  })
})

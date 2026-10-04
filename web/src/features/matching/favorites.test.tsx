import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { cardTwo, cardThree, favoriteItem, favoriteList } from '../../test/matching'
import { renderApp } from '../../test/render'
import { card, detail, reference } from '../../test/vacancies'
import { ROLE_STORAGE_KEY } from '../shell/role-context'

beforeEach(() => window.localStorage.clear())

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/reference': reply(200, reference),
  'GET /api/favorites/ids': reply(200, { ids: [] }),
  ...extra,
})
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })

// ---------------------------------------------------------------- закладка на вакансии

describe('favorite button', () => {
  const search = (extra: Record<string, Route> = {}) =>
    signedIn({ 'GET /api/vacancies?*': reply(200, { items: [card], total: 1, fuzzy: false }), ...extra })

  it('leads a visitor to sign in and back to the same search', async () => {
    stubApi({ 'GET /api/reference': reply(200, reference), 'GET /api/vacancies?*': reply(200, { items: [card], total: 1, fuzzy: false }) })
    renderApp('/vacancies?q=%D1%84%D0%B8%D0%B7%D0%B8%D0%BA%D0%B0')
    const link = await screen.findByRole('link', { name: `Войти, чтобы добавить в избранное: ${card.title}` })
    expect(link).toHaveAttribute('href', `/login?next=${encodeURIComponent('/vacancies?q=%D1%84%D0%B8%D0%B7%D0%B8%D0%BA%D0%B0')}`)
    expect(link).toHaveTextContent('В избранное')
  })

  it('does not ask a visitor for the list of favorites', async () => {
    const { called } = stubApi({ 'GET /api/reference': reply(200, reference), 'GET /api/vacancies?*': reply(200, { items: [card], total: 1, fuzzy: false }) })
    renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    expect(called('GET', '/api/favorites/ids')).toHaveLength(0)
  })

  it('is not shown in the hiring mode', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    stubApi(search())
    renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    expect(screen.queryByRole('button', { name: /в избранное/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /в избранное/i })).not.toBeInTheDocument()
  })

  it('shows what is already in favorites as pressed', async () => {
    stubApi(search({ 'GET /api/favorites/ids': reply(200, { ids: [card.id] }) }))
    renderApp('/vacancies')
    const button = await screen.findByRole('button', { name: `В избранном: ${card.title}` })
    expect(button).toHaveAttribute('aria-pressed', 'true')
    expect(button).toHaveTextContent('В избранном')
  })

  it('adds at once and removes at once, telling the server each time', async () => {
    const user = userEvent.setup()
    const ids = new Set<string>()
    const { called } = stubApi(
      search({
        'GET /api/favorites/ids': () => reply(200, { ids: [...ids] }),
        [`PUT /api/favorites/${card.id}`]: () => (ids.add(card.id), reply(204)),
        [`DELETE /api/favorites/${card.id}`]: () => (ids.delete(card.id), reply(204)),
      }),
    )
    renderApp('/vacancies')
    await user.click(await screen.findByRole('button', { name: `В избранное: ${card.title}` }))
    expect(await screen.findByRole('button', { name: `В избранном: ${card.title}` })).toHaveAttribute('aria-pressed', 'true')
    expect(called('PUT', `/api/favorites/${card.id}`)).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: `В избранном: ${card.title}` }))
    expect(await screen.findByRole('button', { name: `В избранное: ${card.title}` })).toHaveAttribute('aria-pressed', 'false')
    expect(called('DELETE', `/api/favorites/${card.id}`)).toHaveLength(1)
  })

  it('goes back and says why when the server refuses', async () => {
    const user = userEvent.setup()
    stubApi(search({ [`PUT /api/favorites/${card.id}`]: apiError(409, 'too_many_favorites', 'В избранном уже слишком много вакансий') }))
    renderApp('/vacancies')
    await user.click(await screen.findByRole('button', { name: `В избранное: ${card.title}` }))
    expect(await toast('Не удалось изменить избранное')).toBeInTheDocument()
    expect(screen.getByText('В избранном уже слишком много вакансий', { selector: '.toast *' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: `В избранное: ${card.title}` })).toHaveAttribute('aria-pressed', 'false')
  })

  it('is on the vacancy page next to the apply button, for published and closed vacancies', async () => {
    const route = (status: string): Record<string, Route> => ({
      [`GET /api/vacancies/${detail.id}`]: reply(200, { vacancy: { ...detail, status } }),
      [`GET /api/applications/for-vacancy/${detail.id}`]: reply(200, { can_apply: true }),
    })
    stubApi(signedIn(route('published')))
    const published = renderApp(`/vacancies/${detail.id}`)
    expect(await screen.findByRole('button', { name: `В избранное: ${detail.title}` })).toBeInTheDocument()
    expect(await screen.findAllByRole('link', { name: 'Откликнуться' })).toHaveLength(2)
    published.unmount()

    stubApi(signedIn(route('closed')))
    renderApp(`/vacancies/${detail.id}`)
    expect(await screen.findByRole('button', { name: `В избранное: ${detail.title}` })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
  })

  it('is not on a vacancy that the person manages, nor on a draft or an archived one', async () => {
    for (const status of ['published', 'draft', 'archived']) {
      stubApi(signedIn({ [`GET /api/vacancies/${detail.id}`]: reply(200, { vacancy: { ...detail, status, viewer: { can_manage: true, transitions: [] } } }) }))
      const view = renderApp(`/vacancies/${detail.id}`)
      await screen.findByRole('heading', { level: 1, name: detail.title })
      expect(screen.queryByRole('button', { name: /в избранное/i })).not.toBeInTheDocument()
      view.unmount()
    }
    // Черновик без прав управления сервер не отдаёт; но и на архивной вакансии закладки нет, даже если её увидели.
    stubApi(signedIn({ [`GET /api/vacancies/${detail.id}`]: reply(200, { vacancy: { ...detail, status: 'archived' } }) }))
    renderApp(`/vacancies/${detail.id}`)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    expect(screen.queryByRole('button', { name: /в избранное/i })).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- «Избранное»

describe('favorites page', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/favorites')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=%2Ffavorites')
  })

  it('holds the place while loading', () => {
    stubApi(signedIn({ 'GET /api/favorites?*': () => new Promise(() => {}) as never }))
    renderApp('/favorites')
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
  })

  it('lists the vacancies with their state, the application and the date they were added', async () => {
    const items = [
      favoriteItem(),
      favoriteItem({ vacancy: cardTwo, state: 'closed', added_at: '2026-10-02T09:00:00Z' }),
      favoriteItem({ vacancy: cardThree, state: 'expired', application_id: 'app-1' }),
    ]
    stubApi(signedIn({ 'GET /api/favorites?*': reply(200, favoriteList(items)), 'GET /api/favorites/ids': reply(200, { ids: items.map((i) => i.vacancy.id) }) }))
    renderApp('/favorites')
    expect(await screen.findByRole('heading', { level: 1, name: 'Избранное' })).toBeInTheDocument()
    expect(screen.getByText('3 вакансии')).toBeInTheDocument()
    const rows = screen.getAllByRole('article')
    expect(rows).toHaveLength(3)
    expect(within(rows[0]).getByRole('link', { name: card.title })).toHaveAttribute('href', `/vacancies/${card.id}`)
    expect(within(rows[0]).getByText('Добавлено 3 октября 2026 г.')).toBeInTheDocument()
    expect(within(rows[0]).getByText(/Заявки до/)).toBeInTheDocument()
    // Закрытая: без срока подачи и с отметкой.
    expect(within(rows[1]).getByText('Набор закончен')).toBeInTheDocument()
    expect(within(rows[1]).queryByText(/Заявки до|Приём заявок закончен/)).not.toBeInTheDocument()
    // Срок прошёл, но человек уже откликнулся.
    expect(within(rows[2]).getByText('Срок подачи прошёл')).toBeInTheDocument()
    expect(within(rows[2]).getByRole('link', { name: 'Смотреть отклик' })).toHaveAttribute('href', '/applications/app-1')
  })

  it('has the four tabs and marks the section in the menu', async () => {
    stubApi(signedIn({ 'GET /api/favorites?*': reply(200, favoriteList()) }))
    renderApp('/favorites')
    const tabs = await screen.findByRole('navigation', { name: 'Разделы избранного' })
    expect(within(tabs).getByRole('link', { name: 'Избранное' })).toHaveAttribute('aria-current', 'page')
    expect(within(tabs).getByRole('link', { name: 'Подходящие' })).toHaveAttribute('href', '/matches')
    expect(within(tabs).getByRole('link', { name: 'Поиски' })).toHaveAttribute('href', '/saved-searches')
    expect(within(tabs).getByRole('link', { name: 'Сроки' })).toHaveAttribute('href', '/deadlines')
    const menu = screen.getAllByRole('navigation', { name: 'Основное меню' })[0]
    expect(within(menu).getByRole('link', { name: 'Избранное' })).toHaveAttribute('aria-current', 'page')
  })

  it.each(['/matches', '/saved-searches', '/deadlines'])('keeps «Избранное» current in the menu on %s', async (path) => {
    stubApi(
      signedIn({
        'GET /api/matches?*': reply(200, { items: [], total: 0, ready: false, basis: { specialties: 0, level: 1, has_region: false, has_degree: false } }),
        'GET /api/saved-searches': reply(200, { items: [] }),
        'GET /api/deadlines': reply(200, { items: [], without_deadline: 0 }),
      }),
    )
    renderApp(path)
    const menu = (await screen.findAllByRole('navigation', { name: 'Основное меню' }))[0]
    expect(within(menu).getByRole('link', { name: 'Избранное' })).toHaveAttribute('aria-current', 'page')
  })

  it('explains an empty list and leads to the search', async () => {
    stubApi(signedIn({ 'GET /api/favorites?*': reply(200, favoriteList([])) }))
    renderApp('/favorites')
    expect(await screen.findByRole('heading', { level: 2, name: 'В избранном пока пусто' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Найти вакансии' })).toHaveAttribute('href', '/vacancies')
  })

  it('shows an error with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi(signedIn({ 'GET /api/favorites?*': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, favoriteList())) }))
    renderApp('/favorites')
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить избранное' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('turns pages and offers the start when the page is gone', async () => {
    const user = userEvent.setup()
    const calls: string[] = []
    const many = Array.from({ length: 20 }, (_, i) => favoriteItem({ vacancy: { ...card, id: `v${i}`, title: `Вакансия ${i}` } }))
    stubApi(
      signedIn({
        'GET /api/favorites?*': (call) => {
          calls.push(call.path)
          return reply(200, call.path.includes('offset=40') ? favoriteList([], { total: 25 }) : favoriteList(many, { total: 25 }))
        },
      }),
    )
    const { router } = renderApp('/favorites')
    expect(await screen.findByText('Страница 1 из 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Дальше' }))
    await waitFor(() => expect(calls.at(-1)).toContain('offset=20'))
    expect(router.state.location.search).toBe('?page=2')
    await user.click(await screen.findByRole('button', { name: 'Назад' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    await router.navigate('/favorites?page=3')
    expect(await screen.findByRole('heading', { level: 2, name: 'Страницы 3 нет' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'К началу списка' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  describe('removing', () => {
    const setup = (extra: Record<string, Route> = {}) =>
      stubApi(
        signedIn({
          'GET /api/favorites?*': reply(200, favoriteList()),
          'GET /api/favorites/ids': reply(200, { ids: [card.id] }),
          [`DELETE /api/favorites/${card.id}`]: reply(204),
          [`PUT /api/favorites/${card.id}`]: reply(204),
          ...extra,
        }),
      )

    it('keeps a row with «Вернуть» after removing, and puts the vacancy back', async () => {
      const user = userEvent.setup()
      const { called, calls } = setup()
      renderApp('/favorites')
      await user.click(await screen.findByRole('button', { name: `В избранном: ${card.title}` }))
      const gone = await screen.findByRole('status')
      expect(gone).toHaveTextContent(`«${card.title}» убрана из избранного`)
      expect(called('DELETE', `/api/favorites/${card.id}`)).toHaveLength(1)
      // Список не перечитывается, пока человек на странице: строка не исчезает.
      expect(calls.filter((c) => c.method === 'GET' && c.path.startsWith('/api/favorites?'))).toHaveLength(1)
      await user.click(within(gone).getByRole('button', { name: 'Вернуть' }))
      expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
      expect(called('PUT', `/api/favorites/${card.id}`)).toHaveLength(1)
      expect(screen.getByRole('button', { name: `В избранном: ${card.title}` })).toBeInTheDocument()
    })

    it('says so when removing fails, and keeps the vacancy', async () => {
      const user = userEvent.setup()
      setup({ [`DELETE /api/favorites/${card.id}`]: apiError(500, 'internal', 'Сбой на сервере') })
      renderApp('/favorites')
      await user.click(await screen.findByRole('button', { name: `В избранном: ${card.title}` }))
      expect(await toast('Не удалось изменить избранное')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: card.title })).toBeInTheDocument()
    })

    it('says so when putting back fails', async () => {
      const user = userEvent.setup()
      setup({ [`PUT /api/favorites/${card.id}`]: apiError(409, 'too_many_favorites', 'Слишком много') })
      renderApp('/favorites')
      await user.click(await screen.findByRole('button', { name: `В избранном: ${card.title}` }))
      await user.click(await screen.findByRole('button', { name: 'Вернуть' }))
      expect(await toast('Не удалось изменить избранное')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Вернуть' })).toBeInTheDocument()
    })
  })
})

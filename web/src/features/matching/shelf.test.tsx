import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { SEARCH_ID, cardThree, cardTwo, matchItem, matchList, savedSearch } from '../../test/matching'
import { renderApp } from '../../test/render'
import { card, reference } from '../../test/vacancies'

beforeEach(() => window.localStorage.clear())

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/reference': reply(200, reference),
  'GET /api/favorites/ids': reply(200, { ids: [] }),
  ...extra,
})
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })

// ---------------------------------------------------------------- «Подходящие вам»

describe('matches page', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/matches')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists vacancies with the reasons why they are there and what the matching rests on', async () => {
    const items = [
      matchItem(),
      matchItem({ vacancy: cardTwo, reasons: ['group', 'level_near', 'remote'] }),
      matchItem({ vacancy: cardThree, reasons: ['specialty', 'strange_code'] }),
    ]
    stubApi(signedIn({ 'GET /api/matches?*': reply(200, matchList(items)) }))
    renderApp('/matches')
    expect(await screen.findByRole('heading', { level: 1, name: 'Подходящие вам' })).toBeInTheDocument()
    expect(screen.getByText('3 подходящие вакансии')).toBeInTheDocument()
    const basis = screen.getByRole('region', { name: 'Подбор опирается на профиль' })
    expect(basis).toHaveTextContent('2 области науки · уровень R2 · учёная степень · регион.')
    expect(within(basis).getByRole('link', { name: 'Изменить профиль' })).toHaveAttribute('href', '/profile/edit')
    const rows = screen.getAllByRole('article')
    expect(within(rows[0]).getByText('Почему в подборке:')).toBeInTheDocument()
    expect(within(rows[0]).getAllByRole('listitem').map((li) => li.textContent)).toEqual(
      expect.arrayContaining(['Совпала специальность', 'Уровень совпал с вашим', 'Степень отвечает требованию', 'Ваш регион']),
    )
    expect(within(rows[1]).getByText('Та же группа специальностей')).toBeInTheDocument()
    expect(within(rows[1]).getByText('Соседний уровень')).toBeInTheDocument()
    expect(within(rows[1]).getByText('Можно работать удалённо')).toBeInTheDocument()
    // Неизвестный код показывается как есть: подбор не ломается от новой причины.
    expect(within(rows[2]).getByText('strange_code')).toBeInTheDocument()
  })

  it('leaves out what the person does not have in the profile', async () => {
    stubApi(signedIn({ 'GET /api/matches?*': reply(200, matchList([matchItem()], { basis: { specialties: 1, level: 1, has_region: false, has_degree: false } })) }))
    renderApp('/matches')
    const basis = await screen.findByRole('region', { name: 'Подбор опирается на профиль' })
    expect(basis).toHaveTextContent('1 область науки · уровень R1.')
    expect(basis).not.toHaveTextContent('степень')
    expect(basis).not.toHaveTextContent('регион')
  })

  it('asks to fill in the fields of science when the profile has none', async () => {
    stubApi(signedIn({ 'GET /api/matches?*': reply(200, matchList([], { ready: false, basis: { specialties: 0, level: 1, has_region: false, has_degree: false } })) }))
    renderApp('/matches')
    expect(await screen.findByRole('heading', { level: 2, name: 'Подбирать пока не по чему' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Заполнить профиль' })).toHaveAttribute('href', '/profile/edit')
    expect(screen.queryByRole('region', { name: 'Подбор опирается на профиль' })).not.toBeInTheDocument()
  })

  it('says so when nothing fits and leads to the search', async () => {
    stubApi(signedIn({ 'GET /api/matches?*': reply(200, matchList([])) }))
    renderApp('/matches')
    expect(await screen.findByRole('heading', { level: 2, name: 'Подходящих вакансий пока нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Найти вакансии' })).toHaveAttribute('href', '/vacancies')
  })

  it('shows an error with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi(signedIn({ 'GET /api/matches?*': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, matchList())) }))
    renderApp('/matches')
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить подборку' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('turns pages and offers the start when the page is gone', async () => {
    const user = userEvent.setup()
    const many = Array.from({ length: 20 }, (_, i) => matchItem({ vacancy: { ...card, id: `v${i}`, title: `Вакансия ${i}` } }))
    stubApi(
      signedIn({
        'GET /api/matches?*': (call) => reply(200, call.path.includes('offset=40') ? matchList([], { total: 25 }) : matchList(many, { total: 25 })),
      }),
    )
    const { router } = renderApp('/matches')
    expect(await screen.findByText('Страница 1 из 2')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Дальше' }))
    expect(router.state.location.search).toBe('?page=2')
    await user.click(await screen.findByRole('button', { name: 'Назад' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    await router.navigate('/matches?page=3')
    expect(await screen.findByRole('heading', { level: 2, name: 'Страницы 3 нет' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'К началу списка' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })
})

// ---------------------------------------------------------------- «Поиски»

describe('saved searches page', () => {
  const list = (items = [savedSearch()]) => reply(200, { items })

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/saved-searches')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists the searches with their conditions, frequency and dates', async () => {
    const items = [
      savedSearch({ last_sent_at: '2026-10-02T06:00:00Z' }),
      savedSearch({ id: 'second', name: 'Слова', query: 'q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F&format=remote', frequency: 'off' }),
    ]
    stubApi(signedIn({ 'GET /api/saved-searches': list(items) }))
    renderApp('/saved-searches')
    expect(await screen.findByRole('heading', { level: 1, name: 'Сохранённые поиски' })).toBeInTheDocument()
    expect(screen.getByText('2 поиска')).toBeInTheDocument()
    const rows = screen.getAllByRole('listitem').filter((li) => li.classList.contains('saved-entry'))
    const first = within(rows[0])
    expect(first.getByRole('link', { name: 'Химия в Новосибирске' })).toHaveAttribute('href', `/saved-searches/${SEARCH_ID}`)
    expect(first.getByRole('list', { name: 'Условия поиска' })).toHaveTextContent('1.4 Химические наукиНовосибирская область')
    expect(first.getByRole('combobox', { name: 'Как сообщать' })).toHaveValue('daily')
    expect(first.getByText('Одно сообщение утром (9:00 по Москве), если есть новые вакансии')).toBeInTheDocument()
    expect(first.getByText('Сохранён 1 октября 2026 г.')).toBeInTheDocument()
    expect(first.getByText('Последнее сообщение 2 октября 2026 г.')).toBeInTheDocument()
    const second = within(rows[1])
    expect(second.getByRole('list', { name: 'Условия поиска' })).toHaveTextContent('Слова: химияУдалённо')
    expect(second.getByRole('combobox', { name: 'Как сообщать' })).toHaveValue('off')
    expect(second.getByText('Сообщений пока не было')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Настройки писем' })).toHaveAttribute('href', '/account')
  })

  it('shows a very long name without breaking the page', async () => {
    const name = 'Очень длинное название сохранённого поиска, которое занимает несколько строк и не помещается в одну '.repeat(2).trim()
    stubApi(signedIn({ 'GET /api/saved-searches': list([savedSearch({ name })]) }))
    renderApp('/saved-searches')
    const title = await screen.findByRole('heading', { level: 2, name })
    expect(title).toHaveAttribute('data-long', 'true')
  })

  it('explains an empty list', async () => {
    stubApi(signedIn({ 'GET /api/saved-searches': list([]) }))
    renderApp('/saved-searches')
    expect(await screen.findByRole('heading', { level: 2, name: 'Сохранённых поисков нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Перейти к поиску' })).toHaveAttribute('href', '/vacancies')
  })

  it('shows an error with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi(signedIn({ 'GET /api/saved-searches': () => (fail ? apiError(500, 'internal', 'Сбой') : list()) }))
    renderApp('/saved-searches')
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить поиски' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: 'Химия в Новосибирске' })).toBeInTheDocument()
  })

  describe('changing a search', () => {
    const setup = (extra: Record<string, Route> = {}) => {
      let current = savedSearch()
      return stubApi(
        signedIn({
          'GET /api/saved-searches': () => reply(200, { items: [current] }),
          [`PATCH /api/saved-searches/${SEARCH_ID}`]: (call) => {
            current = { ...current, ...(call.body as object) }
            return reply(200, { search: current })
          },
          [`DELETE /api/saved-searches/${SEARCH_ID}`]: reply(204),
          ...extra,
        }),
      )
    }

    it('saves another frequency at once', async () => {
      const user = userEvent.setup()
      const { calls } = setup()
      renderApp('/saved-searches')
      await user.selectOptions(await screen.findByRole('combobox', { name: 'Как сообщать' }), 'instant')
      expect(await toast('Частота сохранена')).toBeInTheDocument()
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ name: 'Химия в Новосибирске', frequency: 'instant' })
      await waitFor(() => expect(screen.getByRole('combobox', { name: 'Как сообщать' })).toHaveValue('instant'))
      expect(screen.getByText('Сообщение в течение нескольких минут после публикации вакансии')).toBeInTheDocument()
    })

    it('says so when the frequency cannot be saved', async () => {
      const user = userEvent.setup()
      setup({ [`PATCH /api/saved-searches/${SEARCH_ID}`]: apiError(500, 'internal', 'Сбой') })
      renderApp('/saved-searches')
      await user.selectOptions(await screen.findByRole('combobox', { name: 'Как сообщать' }), 'weekly')
      expect(await toast('Не удалось сохранить изменения')).toBeInTheDocument()
      expect(screen.getByRole('combobox', { name: 'Как сообщать' })).toHaveValue('daily')
    })

    it('renames through a window, checking the name first', async () => {
      const user = userEvent.setup()
      const { calls } = setup()
      renderApp('/saved-searches')
      await user.click(await screen.findByRole('button', { name: 'Переименовать: Химия в Новосибирске' }))
      const dialog = await screen.findByRole('dialog', { name: 'Переименовать поиск' })
      const field = within(dialog).getByRole('textbox', { name: /Название/ })
      expect(field).toHaveValue('Химия в Новосибирске')
      await user.clear(field)
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить название' }))
      expect(await within(dialog).findByText('Назовите поиск')).toBeInTheDocument()
      expect(calls.filter((c) => c.method === 'PATCH')).toHaveLength(0)
      await user.type(field, 'x'.repeat(121))
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить название' }))
      expect(await within(dialog).findByText('Название длиннее 120 знаков')).toBeInTheDocument()
      await user.clear(field)
      await user.type(field, '  Новое название ')
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить название' }))
      expect(await toast('Название сохранено')).toBeInTheDocument()
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ name: 'Новое название', frequency: 'daily' })
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
      expect(await screen.findByRole('link', { name: 'Новое название' })).toBeInTheDocument()
    })

    it('shows the answer of the server in the rename window and lets cancel', async () => {
      const user = userEvent.setup()
      setup({ [`PATCH /api/saved-searches/${SEARCH_ID}`]: apiError(500, 'internal', 'Сбой на сервере') })
      renderApp('/saved-searches')
      await user.click(await screen.findByRole('button', { name: 'Переименовать: Химия в Новосибирске' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить название' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('Сбой на сервере')
      await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    })

    it('shows a field error of the server under the name', async () => {
      const user = userEvent.setup()
      setup({ [`PATCH /api/saved-searches/${SEARCH_ID}`]: apiError(422, 'validation_failed', 'Проверьте поиск', { fields: { name: 'Название уже занято' } }) })
      renderApp('/saved-searches')
      await user.click(await screen.findByRole('button', { name: 'Переименовать: Химия в Новосибирске' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить название' }))
      expect(await within(dialog).findByText('Название уже занято')).toBeInTheDocument()
    })

    it('deletes after a confirmation', async () => {
      const user = userEvent.setup()
      const { called } = setup()
      renderApp('/saved-searches')
      await user.click(await screen.findByRole('button', { name: 'Удалить: Химия в Новосибирске' }))
      const dialog = await screen.findByRole('dialog', { name: 'Удалить поиск?' })
      expect(dialog).toHaveTextContent('Поиск «Химия в Новосибирске» исчезнет из списка, сообщения по нему прекратятся.')
      await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
      expect(called('DELETE', `/api/saved-searches/${SEARCH_ID}`)).toHaveLength(0)
      await user.click(screen.getByRole('button', { name: 'Удалить: Химия в Новосибирске' }))
      await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить поиск' }))
      expect(await toast('Поиск удалён')).toBeInTheDocument()
      expect(called('DELETE', `/api/saved-searches/${SEARCH_ID}`)).toHaveLength(1)
    })

    it('keeps the search and says so when it cannot be deleted', async () => {
      const user = userEvent.setup()
      setup({ [`DELETE /api/saved-searches/${SEARCH_ID}`]: apiError(500, 'internal', 'Сбой') })
      renderApp('/saved-searches')
      await user.click(await screen.findByRole('button', { name: 'Удалить: Химия в Новосибирске' }))
      await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить поиск' }))
      expect(await toast('Не удалось удалить поиск')).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Химия в Новосибирске' })).toBeInTheDocument()
    })
  })
})

// ---------------------------------------------------------------- адрес из уведомления

describe('opening a saved search', () => {
  const route = (search = savedSearch()): Record<string, Route> => signedIn({ [`GET /api/saved-searches/${SEARCH_ID}`]: reply(200, { search }) })

  it('sends a visitor to sign in and back', async () => {
    stubApi()
    const { router } = renderApp(`/saved-searches/${SEARCH_ID}`)
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toContain(`next=%2Fsaved-searches%2F${SEARCH_ID}`)
  })

  it('opens the search with the newest vacancies first', async () => {
    stubApi({ ...route(), 'GET /api/vacancies?*': reply(200, { items: [card], total: 1, fuzzy: false }) })
    const { router } = renderApp(`/saved-searches/${SEARCH_ID}`)
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies'))
    expect(router.state.location.search).toBe('?field=1.4&region=54&sort=new')
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('keeps the order the person had chosen', async () => {
    stubApi({ ...route(savedSearch({ query: 'field=1.4&sort=deadline' })), 'GET /api/vacancies?*': reply(200, { items: [], total: 0, fuzzy: false }) })
    const { router } = renderApp(`/saved-searches/${SEARCH_ID}`)
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies'))
    expect(router.state.location.search).toBe('?field=1.4&sort=deadline')
  })

  it('says there is no such search', async () => {
    stubApi(signedIn({ [`GET /api/saved-searches/${SEARCH_ID}`]: apiError(404, 'not_found', 'Нет') }))
    renderApp(`/saved-searches/${SEARCH_ID}`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Такого поиска нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К сохранённым поискам' })).toHaveAttribute('href', '/saved-searches')
  })

  it('shows another failure with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi({
      ...signedIn({ [`GET /api/saved-searches/${SEARCH_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { search: savedSearch() })) }),
      'GET /api/vacancies?*': reply(200, { items: [], total: 0, fuzzy: false }),
    })
    const { router } = renderApp(`/saved-searches/${SEARCH_ID}`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось открыть поиск' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies'))
  })
})

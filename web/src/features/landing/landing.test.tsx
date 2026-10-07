import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { stats } from '../../test/landing'
import { renderApp } from '../../test/render'
import { card, reference } from '../../test/vacancies'

const second = { ...card, id: 'b', title: 'Постдок: геномика' }

function setup(over: Record<string, Route> = {}) {
  return stubApi({
    'GET /api/reference': reply(200, reference),
    'GET /api/landing': reply(200, stats),
    'GET /api/vacancies?*': reply(200, { items: [card, second], total: 188, fuzzy: false }),
    ...over,
  })
}

const link = (name: string | RegExp) => screen.findByRole('link', { name })

describe('landing page', () => {
  it('tells both sides what the site is, with the numbers and the fresh vacancies from the server', async () => {
    const { called } = setup()
    renderApp('/')
    expect(await screen.findByRole('heading', { level: 1, name: /Найдите место в\sнаучно-образовательной сфере/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Или человека в ваш коллектив.')

    const facts = await screen.findByRole('list', { name: 'Сейчас на сайте' })
    expect(within(facts).getByRole('link', { name: '188 открытых вакансий' })).toHaveAttribute('href', '/vacancies')
    expect(within(facts).getByRole('link', { name: '30 организаций ищут людей' })).toHaveAttribute('href', '/organizations')
    expect(within(facts).getByRole('link', { name: '7 соискателей открыли профиль' })).toHaveAttribute('href', '/scientists')

    expect(await link(card.title)).toHaveAttribute('href', `/vacancies/${card.id}`)
    expect(await link('Все вакансии: 188')).toHaveAttribute('href', '/vacancies')
    expect(called('GET', '/api/vacancies?limit=6')).toHaveLength(1)

    expect(screen.getByRole('heading', { level: 2, name: 'Если вы ищете место' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Если вы набираете людей' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Устроено под науку и образование' })).toBeInTheDocument()
    expect(screen.getAllByRole('term')).toHaveLength(6)
  })

  it('leads from the table of contents into the search with that one condition', async () => {
    setup()
    renderApp('/')
    const nav = await screen.findByRole('navigation', { name: 'Обзор вакансий' })
    expect(within(nav).getByRole('link', { name: 'Естественные науки: 138 вакансий' })).toHaveAttribute('href', '/vacancies?field=1')
    expect(within(nav).getByRole('link', { name: 'Научный работник: 94 вакансии' })).toHaveAttribute('href', '/vacancies?type=research')
    expect(within(nav).getByRole('link', { name: 'Аспирантура: 55 вакансий' })).toHaveAttribute(
      'href',
      '/vacancies?type=phd',
    )
    // Название области, которого нет в справочнике, не пропадает.
    expect(within(nav).getByRole('link', { name: 'Область 2: 52 вакансии' })).toBeInTheDocument()
  })

  it('hands the words and the region over to /vacancies', async () => {
    const user = userEvent.setup()
    setup()
    const { router } = renderApp('/')
    await screen.findByRole('list', { name: 'Сейчас на сайте' })
    await user.type(screen.getByRole('searchbox', { name: 'Что ищете' }), '  органическая химия ')
    await user.selectOptions(await screen.findByRole('combobox', { name: 'Регион' }), '54')
    await user.click(screen.getByRole('button', { name: 'Найти' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies'))
    const params = new URLSearchParams(router.state.location.search)
    expect(params.get('q')).toBe('органическая химия')
    expect(params.get('region')).toBe('54')
  })

  it('opens the plain search when nothing was typed', async () => {
    const user = userEvent.setup()
    setup()
    const { router } = renderApp('/')
    await screen.findByRole('list', { name: 'Сейчас на сайте' })
    await user.click(screen.getByRole('button', { name: 'Найти' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies'))
    expect(router.state.location.search).toBe('')
  })

  it('offers registration to a guest and the own sections to a signed-in person', async () => {
    setup()
    const guest = renderApp('/')
    expect(await link('Создать профиль')).toHaveAttribute('href', '/register')
    const tracks = within(screen.getByRole('region', { name: 'Для соискателей и для организаций' }))
    expect(tracks.getByRole('link', { name: 'Создать организацию' })).toHaveAttribute('href', '/organizations/new')
    expect(tracks.getByRole('link', { name: 'Организации' })).toHaveAttribute('href', '/organizations')
    expect(tracks.getByRole('link', { name: 'Соискатели' })).toHaveAttribute('href', '/scientists')
    guest.unmount()

    setup(signedInAs())
    renderApp('/')
    expect(await link('Мой профиль')).toHaveAttribute('href', '/profile')
    expect(screen.getByRole('link', { name: 'Моя организация' })).toHaveAttribute('href', '/my-organization')
    expect(screen.queryByRole('link', { name: 'Создать профиль' })).not.toBeInTheDocument()
  })

  it('does not guess the buttons while it is not known who is signed in', async () => {
    setup({ 'GET /api/auth/me': () => new Promise<never>(() => {}) })
    renderApp('/')
    await link(card.title)
    expect(screen.queryByRole('link', { name: 'Создать профиль' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Мой профиль' })).not.toBeInTheDocument()
  })

  it('works without the numbers: no facts and no table of contents, the rest stays', async () => {
    setup({ 'GET /api/landing': apiError(500, 'internal', 'Что-то сломалось') })
    renderApp('/')
    expect(await link(card.title)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Сейчас на сайте' })).not.toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Обзор вакансий' })).not.toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Что ищете' })).toBeInTheDocument()
    // Без числа вакансий ссылка в конец списка без числа.
    expect(screen.getByRole('link', { name: 'Все вакансии' })).toBeInTheDocument()
  })

  it('keeps a place for the table of contents while the numbers load', async () => {
    setup({ 'GET /api/landing': () => new Promise<never>(() => {}) })
    renderApp('/')
    await link(card.title)
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Сейчас на сайте' })).not.toBeInTheDocument()
  })

  it('shows only what is not zero, and nothing at all when the base is empty', async () => {
    setup({ 'GET /api/landing': reply(200, { open_vacancies: 3, hiring_organizations: 0, scientists: 0, fields: [], types: [{ type: 'weird', vacancies: 3 }] }) })
    const first = renderApp('/')
    const facts = await screen.findByRole('list', { name: 'Сейчас на сайте' })
    expect(within(facts).getAllByRole('listitem')).toHaveLength(1)
    // Неизвестный вид позиции показан как есть.
    expect(screen.getByRole('link', { name: 'weird: 3 вакансии' })).toBeInTheDocument()
    expect(screen.queryByText('По областям науки')).not.toBeInTheDocument()
    first.unmount()

    setup({ 'GET /api/landing': reply(200, { open_vacancies: 0, hiring_organizations: 0, scientists: 0, fields: [], types: [] }) })
    renderApp('/')
    await link(card.title)
    await waitFor(() => expect(screen.queryByRole('status', { name: 'Загрузка' })).not.toBeInTheDocument())
    expect(screen.queryByRole('list', { name: 'Сейчас на сайте' })).not.toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Обзор вакансий' })).not.toBeInTheDocument()
  })

  it('shows only the types when there are no fields', async () => {
    setup({ 'GET /api/landing': reply(200, { ...stats, fields: [] }) })
    renderApp('/')
    await screen.findByText('По видам вакансий')
    expect(screen.queryByText('По областям науки')).not.toBeInTheDocument()
  })

  it('links to the whole list without a number when everything fits on the page', async () => {
    setup({ 'GET /api/vacancies?*': reply(200, { items: [card, second], total: 2, fuzzy: false }), 'GET /api/landing': reply(200, { ...stats, open_vacancies: 2 }) })
    renderApp('/')
    expect(await link('Все вакансии')).toHaveAttribute('href', '/vacancies')
  })

  it('shows loading, then an error with a retry that works', async () => {
    const user = userEvent.setup()
    let fail = true
    setup({
      'GET /api/vacancies?*': () => (fail ? apiError(500, 'internal', 'Что-то сломалось') : reply(200, { items: [card], total: 1, fuzzy: false })),
    })
    renderApp('/')
    expect(screen.getAllByRole('status', { name: 'Загрузка' }).length).toBeGreaterThan(0)
    expect(await screen.findByText('Не удалось загрузить свежие вакансии')).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await link(card.title)).toBeInTheDocument()
  })

  it('invites to publish the first vacancy when there are none, by the way in that fits the person', async () => {
    setup({ 'GET /api/vacancies?*': reply(200, { items: [], total: 0, fuzzy: false }) })
    const guest = renderApp('/')
    expect(await screen.findByText('Открытых вакансий пока нет')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Опубликовать вакансию' })).toHaveAttribute('href', '/login?next=%2Fmy-vacancies%2Fnew')
    expect(screen.queryByRole('link', { name: /^Все вакансии/ })).not.toBeInTheDocument()
    guest.unmount()

    setup({ ...signedInAs(), 'GET /api/vacancies?*': reply(200, { items: [], total: 0, fuzzy: false }) })
    renderApp('/')
    expect(await screen.findByRole('link', { name: 'Опубликовать вакансию' })).toHaveAttribute('href', '/my-vacancies/new')
  })

  it('shows no more than six fresh vacancies even if the server sends more', async () => {
    const many = Array.from({ length: 8 }, (_, i) => ({ ...card, id: `v${i}`, title: `Вакансия ${i}` }))
    setup({ 'GET /api/vacancies?*': reply(200, { items: many, total: 8, fuzzy: false }) })
    renderApp('/')
    await link('Вакансия 0')
    expect(screen.queryByRole('link', { name: 'Вакансия 6' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Вакансия 5' })).toBeInTheDocument()
  })
})

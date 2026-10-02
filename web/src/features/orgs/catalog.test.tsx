import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { summary } from '../../test/orgs'
import { renderApp } from '../../test/render'

const list = (items = [summary(1), summary(2)], total = items.length) => reply(200, { items, total })

describe('catalog of organizations', () => {
  it('lists organizations with type, city, summary and number of units', async () => {
    stubApi({
      'GET /api/organizations?limit=20&offset=0': list([
        summary(1, { name: 'Приволжский университет', kind: 'university', city: 'Казань', summary: 'Химия и математика.', unit_count: 3 }),
        summary(2, { name: 'Нейрофотоника', kind: 'rd_company', city: 'Санкт-Петербург', summary: '', unit_count: 0 }),
        summary(3, { name: 'Центр', kind: 'other', unit_count: 1 }),
      ]),
    })
    renderApp('/organizations')
    expect(await screen.findByRole('heading', { level: 1, name: 'Организации' })).toBeInTheDocument()
    expect(await screen.findByText('Найдено 3 организации')).toBeInTheDocument()

    const first = screen.getByRole('link', { name: 'Приволжский университет' })
    expect(first).toHaveAttribute('href', '/organizations/org-1')
    const entry = first.closest('li')!
    expect(within(entry).getByText('Вуз · Казань')).toBeInTheDocument()
    expect(within(entry).getByText('Химия и математика.')).toBeInTheDocument()
    expect(within(entry).getByText('3 подразделения')).toBeInTheDocument()

    const second = screen.getByRole('link', { name: 'Нейрофотоника' }).closest('li')!
    expect(within(second).getByText('Компания с R&D · Санкт-Петербург')).toBeInTheDocument()
    expect(within(second).getByText('Описание не заполнено')).toBeInTheDocument()
    expect(within(second).getByText('Подразделения не добавлены')).toBeInTheDocument()
    expect(screen.getByText('1 подразделение')).toBeInTheDocument()
  })

  it('holds the place with grey placeholders while loading', () => {
    stubApi({ 'GET /api/organizations?limit=20&offset=0': () => new Promise(() => {}) as never })
    renderApp('/organizations')
    expect(screen.getByRole('status', { name: 'Загрузка' })).toHaveAttribute('aria-busy', 'true')
  })

  it('searches by words and type, keeps them in the address and asks the server', async () => {
    const api = stubApi({
      'GET /api/organizations?limit=20&offset=0': list(),
      'GET /api/organizations?limit=20&offset=0&q=%D0%BA%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C': list([summary(5, { name: 'Казанский институт' })]),
      'GET /api/organizations?limit=20&offset=0&q=%D0%BA%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C&kind=institute': list([summary(6, { name: 'Институт в Казани', kind: 'institute' })]),
    })
    const { router } = renderApp('/organizations')
    await screen.findByText('Найдено 2 организации')

    await userEvent.type(screen.getByRole('searchbox', { name: 'Название или город' }), '  казань ')
    await press('Найти')
    expect(await screen.findByRole('link', { name: 'Казанский институт' })).toBeInTheDocument()
    expect(router.state.location.search).toBe('?q=%D0%BA%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Тип организации' }), 'institute')
    expect(await screen.findByRole('link', { name: 'Институт в Казани' })).toBeInTheDocument()
    expect(router.state.location.search).toBe('?q=%D0%BA%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C&kind=institute')
    expect(api.called('GET', '/api/organizations?limit=20&offset=0&q=%D0%BA%D0%B0%D0%B7%D0%B0%D0%BD%D1%8C&kind=institute')).toHaveLength(1)
  })

  it('opens already filtered from a shared link and ignores an unknown type', async () => {
    stubApi({
      'GET /api/organizations?limit=20&offset=0&kind=technopark': list([summary(1, { name: 'Технопарк' })]),
      'GET /api/organizations?limit=20&offset=0': list(),
    })
    renderApp('/organizations?kind=technopark')
    expect(await screen.findByRole('link', { name: 'Технопарк' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Тип организации' })).toHaveValue('technopark')
  })

  it('treats a type it does not know as "any"', async () => {
    const api = stubApi({ 'GET /api/organizations?limit=20&offset=0': list() })
    renderApp('/organizations?kind=spaceship&page=abc')
    await screen.findByText('Найдено 2 организации')
    expect(screen.getByRole('combobox', { name: 'Тип организации' })).toHaveValue('')
    expect(api.called('GET', '/api/organizations?limit=20&offset=0')).toHaveLength(1)
  })

  it('says nothing was found and offers to reset the search', async () => {
    stubApi({
      'GET /api/organizations?limit=20&offset=0&q=zzz': list([], 0),
      'GET /api/organizations?limit=20&offset=0': list([summary(1)]),
    })
    const { router } = renderApp('/organizations?q=zzz')
    expect(await screen.findByRole('heading', { name: 'Ничего не нашлось' })).toBeInTheDocument()
    expect(screen.getByText('Найдено 0 организаций')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Название или город' })).toHaveValue('zzz')
    await press('Сбросить поиск')
    expect(await screen.findByRole('link', { name: 'Организация 1' })).toBeInTheDocument()
    expect(router.state.location.search).toBe('')
    // Строка поиска следует за адресом.
    expect(screen.getByRole('searchbox', { name: 'Название или город' })).toHaveValue('')
  })

  it('invites to create the first organization when there are none at all', async () => {
    stubApi({ 'GET /api/organizations?limit=20&offset=0': list([], 0) })
    renderApp('/organizations')
    expect(await screen.findByRole('heading', { name: 'Организаций пока нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Создать организацию' })).toHaveAttribute('href', '/organizations/new')
  })

  it('says so when the server fails, and recovers on retry', async () => {
    let n = 0
    stubApi({ 'GET /api/organizations?limit=20&offset=0': () => (n++ === 0 ? apiError(500, 'internal', 'Что-то сломалось на сервере') : list()) })
    renderApp('/organizations')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить организации')
    expect(screen.getByRole('alert')).toHaveTextContent('Что-то сломалось на сервере')
    await press('Проверить ещё раз')
    expect(await screen.findByText('Найдено 2 организации')).toBeInTheDocument()
  })

  it('pages through long lists', async () => {
    const api = stubApi({
      'GET /api/organizations?limit=20&offset=0': list([summary(1)], 45),
      'GET /api/organizations?limit=20&offset=20&q=a': list([summary(2)], 45),
      'GET /api/organizations?limit=20&offset=40&q=a': list([summary(3)], 45),
      'GET /api/organizations?limit=20&offset=20': list([summary(2)], 45),
      'GET /api/organizations?limit=20&offset=40': list([summary(3)], 45),
    })
    const { router } = renderApp('/organizations')
    expect(await screen.findByText('Страница 1 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()

    await press('Дальше')
    expect(await screen.findByText('Страница 2 из 3')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    await press('Дальше')
    expect(await screen.findByText('Страница 3 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Дальше' })).toBeDisabled()
    await press('Назад')
    await waitFor(() => expect(router.state.location.search).toBe('?page=2'))
    expect(api.called('GET', '/api/organizations?limit=20&offset=40')).toHaveLength(1)
  })

  it('does not show a pager for a single page', async () => {
    stubApi({ 'GET /api/organizations?limit=20&offset=0': list([summary(1)], 20) })
    renderApp('/organizations')
    await screen.findByText('Найдено 20 организаций')
    expect(screen.queryByRole('navigation', { name: 'Страницы списка' })).not.toBeInTheDocument()
  })

  it('handles very long names and summaries without breaking the line', async () => {
    const longName = 'Федеральное государственное автономное образовательное учреждение высшего образования ' + 'Сверхдлинное'.repeat(12)
    stubApi({ 'GET /api/organizations?limit=20&offset=0': list([summary(1, { name: longName, city: 'Ростов-на-Дону', summary: 'слово '.repeat(40) })]) })
    renderApp('/organizations')
    expect(await screen.findByRole('link', { name: longName })).toBeInTheDocument()
  })
})

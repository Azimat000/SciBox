import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { renderApp } from '../../test/render'
import { card, mineList, target } from '../../test/vacancies'

const all = 'GET /api/my/vacancies?limit=50&offset=0'
const targetsRoute = (targets = [target]) => ({ 'GET /api/my/vacancy-targets': reply(200, { targets }) })
const draft = { ...card, id: 'd1', status: 'draft' as const, title: 'Черновик вакансии', unit: null }

describe('my vacancies', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/my-vacancies')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe(`?next=${encodeURIComponent('/my-vacancies')}`)
  })

  it('lists vacancies with status, place and actions, and shows counts on the tabs', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute(), [all]: reply(200, mineList([card, draft], { draft: 1, published: 1 })) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('heading', { level: 1, name: 'Мои вакансии' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Создать вакансию' })).toHaveAttribute('href', '/my-vacancies/new')
    const tabs = within(screen.getByRole('navigation', { name: 'Статус вакансий' }))
    expect(tabs.getAllByRole('link').map((a) => a.textContent)).toEqual(['Все2', 'Черновики1', 'Опубликованные1', 'Закрытые0', 'Архив0'])
    expect(tabs.getByRole('link', { name: /^Все/ })).toHaveAttribute('aria-current', 'page')
    const row = screen.getByRole('link', { name: card.title }).closest('.mine-row')! as HTMLElement
    expect(within(row).getByText('Опубликована')).toBeInTheDocument()
    expect(within(row).getByText(/Сибирский институт · Лаборатория сверхпроводников/)).toBeInTheDocument()
    expect(within(row).getByRole('link', { name: `Править: ${card.title}` })).toHaveAttribute('href', `/my-vacancies/${card.id}/edit`)
    // Открывает вакансию её название; срок подачи виден у опубликованной.
    expect(within(row).getByRole('link', { name: card.title })).toHaveAttribute('href', `/vacancies/${card.id}`)
    expect(within(row).getByText(/Заявки до 31 декабря 2099/)).toBeInTheDocument()
    // Вакансия без подразделения подписана «Вся организация».
    const draftRow = screen.getByRole('link', { name: draft.title }).closest('.mine-row')! as HTMLElement
    expect(within(draftRow).getByText(/Вся организация/)).toBeInTheDocument()
    expect(within(draftRow).getByText('Черновик')).toBeInTheDocument()
  })

  it('opens a tab by address and asks the server for that status only', async () => {
    const api = stubApi({
      ...signedInAs(ann),
      ...targetsRoute(),
      'GET /api/my/vacancies?limit=50&offset=0&status=draft': reply(200, mineList([draft], { draft: 1, published: 4 })),
    })
    renderApp('/my-vacancies?status=draft')
    expect(await screen.findByRole('link', { name: draft.title })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^Черновики/ })).toHaveAttribute('aria-current', 'page')
    expect(api.calls.some((c) => c.path.includes('status=draft'))).toBe(true)
  })

  it('treats an unknown status in the address as «all»', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute(), [all]: reply(200, mineList([card], { published: 1 })) })
    renderApp('/my-vacancies?status=weird')
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('says the whole list is empty and offers to create the first vacancy', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute(), [all]: reply(200, mineList([])) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('heading', { level: 2, name: 'Вакансий пока нет' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Создать вакансию' })).toHaveLength(2)
  })

  it('says a tab is empty and does not push to create from there', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute(), 'GET /api/my/vacancies?limit=50&offset=0&status=closed': reply(200, mineList([], { published: 3 })) })
    renderApp('/my-vacancies?status=closed')
    expect(await screen.findByRole('heading', { level: 2, name: 'В этой вкладке пусто' })).toBeInTheDocument()
  })

  it('explains that vacancies belong to organizations when the person has none', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute([]), [all]: reply(200, mineList([])) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('heading', { level: 2, name: 'Вакансии ведут сотрудники организаций' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К моим организациям' })).toHaveAttribute('href', '/my-organization')
    expect(screen.queryByRole('link', { name: 'Создать вакансию' })).not.toBeInTheDocument()
  })

  it('still shows existing vacancies to a person who can no longer create new ones', async () => {
    stubApi({ ...signedInAs(ann), ...targetsRoute([]), [all]: reply(200, mineList([card], { published: 1 })) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Создать вакансию' })).not.toBeInTheDocument()
  })

  it('pages through a long list', async () => {
    const api = stubApi({
      ...signedInAs(ann),
      ...targetsRoute(),
      [all]: reply(200, mineList([card], { published: 120 }, 120)),
      'GET /api/my/vacancies?limit=50&offset=50': reply(200, mineList([draft], { published: 120 }, 120)),
    })
    const { router } = renderApp('/my-vacancies')
    expect(await screen.findByText('Страница 1 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await press('Дальше')
    expect(await screen.findByRole('link', { name: draft.title })).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    await press('Назад')
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(api.calls.map((c) => c.path)).toContain('/api/my/vacancies?limit=50&offset=50')
  })

  it('shows loading, then an error with retry', async () => {
    let n = 0
    stubApi({ ...signedInAs(ann), ...targetsRoute(), [all]: () => (n++ === 0 ? reply(500) : reply(200, mineList([card], { published: 1 }))) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить ваши вакансии')
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('shows an error when the places to create vacancies cannot be loaded', async () => {
    stubApi({ ...signedInAs(ann), 'GET /api/my/vacancy-targets': apiError(500, 'internal', 'Что-то сломалось'), [all]: reply(200, mineList([card], { published: 1 })) })
    renderApp('/my-vacancies')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить ваши вакансии')
  })
})

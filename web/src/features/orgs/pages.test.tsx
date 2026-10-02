import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { apiError, reply, signedInAs, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { dept, lab, orgRoutes, org, SLUG, viewers } from '../../test/orgs'
import { renderApp } from '../../test/render'

describe('public page of an organization', () => {
  it('shows the description, units with heads and topics, and an empty vacancies section', async () => {
    stubApi(orgRoutes(null))
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Сибирский институт' })).toBeInTheDocument()
    expect(screen.getByText('НИИ / институт РАН')).toBeInTheDocument()
    expect(screen.getByText('Новосибирск')).toBeInTheDocument()
    const site = screen.getByRole('link', { name: 'sikm.example.ru' })
    expect(site).toHaveAttribute('href', 'https://sikm.example.ru')
    expect(site).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText(/Изучаем квантовые материалы/)).toBeInTheDocument()

    const units = within(screen.getByRole('heading', { level: 2, name: 'Подразделения' }).closest('section')!)
    const labEntry = units.getByRole('link', { name: lab.name }).closest('li')!
    expect(labEntry.querySelector('a')).toHaveAttribute('href', `/organizations/${SLUG}/units/${lab.id}`)
    expect(within(labEntry).getByText('Лаборатория · Руководитель: Анна Смирнова')).toBeInTheDocument()
    expect(within(labEntry).getByText('Сверхпроводимость')).toBeInTheDocument()
    const deptEntry = units.getByRole('link', { name: dept.name }).closest('li')!
    expect(within(deptEntry).getByText('Кафедра')).toBeInTheDocument()
    expect(within(deptEntry).queryByRole('list')).not.toBeInTheDocument()

    expect(screen.getByRole('heading', { level: 3, name: /Открытых вакансий пока нет/ })).toBeInTheDocument()
    // Посетителю кнопки управления не показываются.
    expect(screen.queryByRole('link', { name: 'Управление организацией' })).not.toBeInTheDocument()
  })

  it('has no site line and no description when they are empty, and says when there are no units', async () => {
    stubApi({ [`GET /api/organizations/${SLUG}`]: reply(200, { organization: { ...org, website: '', description: '' }, units: [], viewer: null }) })
    renderApp(`/organizations/${SLUG}`)
    await screen.findByRole('heading', { level: 1, name: 'Сибирский институт' })
    expect(screen.queryByRole('link', { name: /example/ })).not.toBeInTheDocument()
    expect(screen.getByText('Подразделения пока не добавлены.')).toBeInTheDocument()
  })

  it('shows a way to manage the organization only to its staff', async () => {
    stubApi({ ...signedInAs(), ...orgRoutes(viewers.hr) })
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByRole('link', { name: 'Управление организацией' })).toHaveAttribute('href', `/my-organization/${SLUG}`)
  })

  it('does not offer management to a signed-in person who does not work there', async () => {
    stubApi({ ...signedInAs(), ...orgRoutes(viewers.stranger) })
    renderApp(`/organizations/${SLUG}`)
    await screen.findByRole('heading', { level: 1, name: 'Сибирский институт' })
    expect(screen.queryByRole('link', { name: 'Управление организацией' })).not.toBeInTheDocument()
  })

  it('says the page does not exist', async () => {
    stubApi({ [`GET /api/organizations/${SLUG}`]: apiError(404, 'not_found', 'Такой страницы нет') })
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('holds the place while loading and recovers from a server failure', async () => {
    let n = 0
    stubApi({ [`GET /api/organizations/${SLUG}`]: () => (n++ === 0 ? reply(500) : reply(200, { organization: org, units: [], viewer: null })) })
    renderApp(`/organizations/${SLUG}`)
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить страницу организации')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Сибирский институт' })).toBeInTheDocument()
  })
})

describe('public page of a unit', () => {
  const unitRoute = (viewer: (typeof viewers)[keyof typeof viewers] | null, unit = lab) => ({
    [`GET /api/organizations/${SLUG}/units/${unit.id}`]: reply(200, {
      organization: { slug: SLUG, name: org.name, kind: org.kind, city: org.city },
      unit,
      viewer,
    }),
  })

  it('shows the unit, its head, topics and the organization it belongs to', async () => {
    stubApi(unitRoute(null))
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(await screen.findByRole('heading', { level: 1, name: lab.name })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', `/organizations/${SLUG}`)
    expect(screen.getByText('Руководитель:', { exact: false })).toHaveTextContent('Руководитель: Анна Смирнова')
    expect(screen.getByText('Синтез и измерение плёнок.')).toBeInTheDocument()
    const topics = within(screen.getByRole('heading', { level: 2, name: 'Научные темы' }).closest('section')!)
    expect(topics.getByText('Сверхпроводимость')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: /Открытых вакансий пока нет/ })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Править подразделение' })).not.toBeInTheDocument()
  })

  it('says when there is no head, no description and no topics', async () => {
    stubApi(unitRoute(null, dept))
    renderApp(`/organizations/${SLUG}/units/${dept.id}`)
    expect(await screen.findByText('Руководитель не назначен')).toBeInTheDocument()
    expect(screen.getByText('Описание не заполнено.')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: 'Научные темы' })).not.toBeInTheDocument()
  })

  it('lets the head of this unit edit it, but not the head of another', async () => {
    stubApi({ ...signedInAs(), ...unitRoute(viewers.head) })
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(await screen.findByRole('link', { name: 'Править подразделение' })).toHaveAttribute('href', `/my-organization/${SLUG}/units/${lab.id}`)
  })

  it('does not offer editing to someone who may not edit this unit', async () => {
    stubApi({ ...signedInAs(), ...unitRoute(viewers.head, dept) })
    renderApp(`/organizations/${SLUG}/units/${dept.id}`)
    await screen.findByRole('heading', { level: 1, name: dept.name })
    expect(screen.queryByRole('link', { name: 'Править подразделение' })).not.toBeInTheDocument()
  })

  it('lets the owner edit any unit', async () => {
    stubApi({ ...signedInAs(), ...unitRoute(viewers.owner, dept) })
    renderApp(`/organizations/${SLUG}/units/${dept.id}`)
    expect(await screen.findByRole('link', { name: 'Править подразделение' })).toBeInTheDocument()
  })

  it('says the page does not exist, and recovers from a server failure', async () => {
    stubApi({ [`GET /api/organizations/${SLUG}/units/${lab.id}`]: apiError(404, 'not_found', 'Такой страницы нет') })
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('shows loading and then an error with retry', async () => {
    let n = 0
    stubApi({ [`GET /api/organizations/${SLUG}/units/${lab.id}`]: () => (n++ === 0 ? reply(500) : unitRoute(null)[`GET /api/organizations/${SLUG}/units/${lab.id}`]) })
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить страницу организации')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: lab.name })).toBeInTheDocument()
  })
})

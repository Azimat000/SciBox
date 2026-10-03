import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { apiError, reply, stubApi } from '../../test/api'
import { lab, org, orgRoutes, SLUG } from '../../test/orgs'
import { renderApp } from '../../test/render'
import { card, orgVacancies } from '../../test/vacancies'

describe('vacancies on the organization page', () => {
  it('lists published vacancies as journal entries with the facts that matter', async () => {
    stubApi({ ...orgRoutes(null), ...orgVacancies([card, { ...card, id: 'b', title: 'Постдок', is_competition: false, deadline: '', career_level: null, region: null, city: '', specialties: [] }]) })
    renderApp(`/organizations/${SLUG}`)
    const section = within((await screen.findByRole('heading', { level: 2, name: 'Вакансии' })).closest('section')!)
    const entry = (await section.findByRole('link', { name: card.title })).closest('article')!
    expect(entry.querySelector('a')).toHaveAttribute('href', `/vacancies/${card.id}`)
    expect(within(entry).getByText('Рост и измерение эпитаксиальных плёнок.')).toBeInTheDocument()
    expect(within(entry).getByText('Конкурс')).toBeInTheDocument()
    expect(within(entry).getByText('Срочный договор, 3 года')).toBeInTheDocument()
    expect(within(entry).getByText('R3')).toBeInTheDocument()
    expect(within(entry).getByText(/Заявки до/)).toBeInTheDocument()
    // Без города у вакансии показываем город организации.
    const second = section.getByRole('link', { name: 'Постдок' }).closest('article')!
    expect(within(second).getByText(`${org.name}, ${org.city}`)).toBeInTheDocument()
    expect(within(second).queryByText('Конкурс')).not.toBeInTheDocument()
  })

  it('shows loading, then an error that does not break the rest of the page', async () => {
    stubApi({ ...orgRoutes(null), [`GET /api/vacancies?org=${SLUG}`]: apiError(500, 'internal', 'Что-то сломалось') })
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByRole('heading', { level: 1, name: org.name })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить вакансии')
  })

  it('shows a loading placeholder while the list loads', async () => {
    stubApi({ ...orgRoutes(null), [`GET /api/vacancies?org=${SLUG}`]: () => new Promise(() => undefined) as never })
    renderApp(`/organizations/${SLUG}`)
    await screen.findByRole('heading', { level: 1, name: org.name })
    expect(await screen.findAllByRole('status', { name: 'Загрузка' })).not.toHaveLength(0)
  })

  it('says how many are not shown when there are more than the page holds', async () => {
    stubApi({
      ...orgRoutes(null),
      [`GET /api/vacancies?org=${SLUG}`]: reply(200, { items: [card], total: 120 }),
    })
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByText('Показаны 1 из 120. Остальные будут доступны в поиске вакансий.')).toBeInTheDocument()
  })

  it('invites to come back later when there are none', async () => {
    stubApi(orgRoutes(null))
    renderApp(`/organizations/${SLUG}`)
    expect(await screen.findByRole('heading', { level: 3, name: 'Открытых вакансий пока нет' })).toBeInTheDocument()
  })
})

describe('vacancies on the unit page', () => {
  const unitRoutes = (items: (typeof card)[]) => ({
    [`GET /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, {
      organization: { slug: SLUG, name: org.name, kind: org.kind, city: org.city },
      unit: lab,
      viewer: null,
    }),
    [`GET /api/vacancies?org=${SLUG}&unit=${lab.id}`]: reply(200, { items, total: items.length }),
  })

  it('lists the vacancies of this unit only', async () => {
    const { calls } = stubApi(unitRoutes([card]))
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
    expect(calls.some((c) => c.path === `/api/vacancies?org=${SLUG}&unit=${lab.id}`)).toBe(true)
  })

  it('says the unit has no open vacancies', async () => {
    stubApi(unitRoutes([]))
    renderApp(`/organizations/${SLUG}/units/${lab.id}`)
    expect(await screen.findByRole('heading', { level: 3, name: 'В подразделении открытых вакансий нет' })).toBeInTheDocument()
  })
})

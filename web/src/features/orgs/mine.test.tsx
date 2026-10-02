import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { lab, mine, org, SLUG } from '../../test/orgs'
import { renderApp } from '../../test/render'
import { setMe } from '../auth/api'

const myOrg = { id: org.id, slug: SLUG, name: org.name, kind: org.kind, city: org.city, role: 'owner' as const }
const invitation = {
  id: '88888888-8888-4888-8888-888888888888',
  organization: { slug: SLUG, name: org.name },
  role: 'unit_head',
  unit_name: lab.name,
  expires_at: '2026-11-02T10:00:00Z',
}

describe('RequireUser (shared by pages for signed-in people)', () => {
  it('sends a visitor to sign in and brings them back to the same address', async () => {
    stubApi()
    const { router } = renderApp('/my-organization')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=%2Fmy-organization')
    expect(await screen.findByText('Войдите, чтобы открыть эту страницу.')).toBeInTheDocument()
  })

  it('holds the place while it finds out who is signed in', () => {
    stubApi({ 'GET /api/auth/me': () => new Promise(() => {}) as never })
    renderApp('/my-organization')
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
  })

  it('says so when the server does not answer, and recovers on retry', async () => {
    let n = 0
    stubApi({ 'GET /api/auth/me': () => (n++ === 0 ? reply(500) : reply(200, { user: ann })), 'GET /api/my/organizations': mine() })
    renderApp('/my-organization')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить аккаунт')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Организация' })).toBeInTheDocument()
  })

  it('sends someone who signed out on purpose to the home page, not to the sign-in form', async () => {
    stubApi({ ...signedInAs(), 'GET /api/my/organizations': mine() })
    const { router, queryClient } = renderApp('/my-organization')
    await screen.findByRole('heading', { level: 1, name: 'Организация' })
    await setMe(queryClient, null)
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
  })
})

describe('my organizations', () => {
  it('lists organizations with the role and links to the page and to management', async () => {
    stubApi({ ...signedInAs(), 'GET /api/my/organizations': mine([myOrg, { ...myOrg, id: 'x', slug: 'other', name: 'Вторая', role: 'hr' }]) })
    renderApp('/my-organization')
    const section = (await screen.findByRole('heading', { level: 2, name: 'Ваши организации' })).closest('section')!
    const rows = within(section)
    expect(rows.getByText(org.name)).toBeInTheDocument()
    expect(rows.getByText('Владелец')).toBeInTheDocument()
    expect(rows.getByText('Кадровик')).toBeInTheDocument()
    expect(rows.getAllByRole('link', { name: 'Управление' })[0]).toHaveAttribute('href', `/my-organization/${SLUG}`)
    expect(rows.getAllByRole('link', { name: 'Страница' })[0]).toHaveAttribute('href', `/organizations/${SLUG}`)
    expect(rows.getByRole('link', { name: 'Создать организацию' })).toHaveAttribute('href', '/organizations/new')
  })

  it('invites to create an organization when there is none and no invitation', async () => {
    stubApi({ ...signedInAs(), 'GET /api/my/organizations': mine() })
    renderApp('/my-organization')
    expect(await screen.findByRole('heading', { level: 2, name: 'У вас пока нет организации' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Создать организацию' })).toHaveAttribute('href', '/organizations/new')
  })

  it('shows an invitation waiting for the person and accepts it', async () => {
    let accepted = false
    const api = stubApi({
      ...signedInAs(),
      'GET /api/my/organizations': () => (accepted ? mine([myOrg]) : mine([], [invitation])),
      [`POST /api/invitations/${invitation.id}/accept`]: () => {
        accepted = true
        return reply(200, { slug: SLUG, name: org.name, role: 'unit_head' })
      },
    })
    renderApp('/my-organization')
    const section = (await screen.findByRole('heading', { level: 2, name: 'Вас приглашают' })).closest('section')!
    expect(within(section).getByRole('link', { name: org.name })).toBeInTheDocument()
    expect(within(section).getByText('Руководитель подразделения')).toBeInTheDocument()
    expect(within(section).getByText(`Подразделение: ${lab.name}`)).toBeInTheDocument()
    expect(within(section).getByText(/Приглашение действует до 2 ноября 2026/)).toBeInTheDocument()
    // Пока приглашение не принято, второго «создать» нет: остаётся одна кнопка под списком.
    expect(screen.getAllByRole('link', { name: 'Создать организацию' })).toHaveLength(1)

    await press('Принять')
    expect(await screen.findByText(`Вы теперь в организации «${org.name}»`)).toBeInTheDocument()
    expect(await screen.findByRole('heading', { level: 2, name: 'Ваши организации' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: 'Вас приглашают' })).not.toBeInTheDocument()
    expect(api.called('POST', `/api/invitations/${invitation.id}/accept`)).toHaveLength(1)
  })

  it('shows an invitation without a unit, and tells when accepting fails', async () => {
    stubApi({
      ...signedInAs(),
      'GET /api/my/organizations': mine([myOrg], [{ ...invitation, unit_name: null }]),
      [`POST /api/invitations/${invitation.id}/accept`]: apiError(400, 'invalid_invitation', 'Приглашение устарело, отозвано или уже принято. Попросите отправить новое'),
    })
    renderApp('/my-organization')
    await screen.findByRole('heading', { level: 2, name: 'Вас приглашают' })
    expect(screen.queryByText(/Подразделение:/)).not.toBeInTheDocument()
    await press('Принять')
    expect(await screen.findByText('Не удалось принять приглашение')).toBeInTheDocument()
    expect(screen.getByText(/Приглашение устарело/)).toBeInTheDocument()
  })

  it('offers to create an organization next to the invitations when the person has none yet', async () => {
    stubApi({ ...signedInAs(), 'GET /api/my/organizations': mine([], [invitation]) })
    renderApp('/my-organization')
    await screen.findByRole('heading', { level: 2, name: 'Вас приглашают' })
    expect(screen.queryByRole('heading', { name: 'У вас пока нет организации' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Создать организацию' })).toBeInTheDocument()
  })

  it('shows loading and an error with retry', async () => {
    let n = 0
    stubApi({ ...signedInAs(), 'GET /api/my/organizations': () => (n++ === 0 ? reply(500) : mine()) })
    renderApp('/my-organization')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить ваши организации')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 2, name: 'У вас пока нет организации' })).toBeInTheDocument()
  })
})

describe('create an organization', () => {
  it('requires a name, a type and a city before asking the server', async () => {
    const api = stubApi(signedInAs())
    renderApp('/organizations/new')
    expect(await screen.findByRole('heading', { level: 1, name: 'Новая организация' })).toBeInTheDocument()
    await press('Создать организацию')
    expect(screen.getByText('Укажите название')).toBeInTheDocument()
    expect(screen.getByText('Выберите тип организации из списка')).toBeInTheDocument()
    expect(screen.getByText('Укажите город')).toBeInTheDocument()
    expect(field(/^Название/)).toHaveFocus()
    expect(api.called('POST', '/api/organizations')).toHaveLength(0)
  })

  it('creates the organization and goes on to add units', async () => {
    const api = stubApi({
      ...signedInAs(),
      'POST /api/organizations': reply(201, { organization: org }),
      [`GET /api/organizations/${SLUG}`]: reply(200, { organization: org, units: [], viewer: { role: 'owner', can_edit_organization: true, can_manage_members: true, can_manage_units: true, editable_units: [] } }),
    })
    const { router } = renderApp('/organizations/new')
    await screen.findByRole('heading', { level: 1, name: 'Новая организация' })
    await fill(/^Название/, 'Сибирский институт')
    await userEvent.selectOptions(field(/^Тип организации/), 'institute')
    await fill(/^Город/, 'Новосибирск')
    await fill(/^Сайт/, 'sikm.example.ru')
    await fill(/^Описание/, 'Изучаем материалы.')
    await press('Создать организацию')

    expect(await screen.findByText('Организация создана')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe(`/my-organization/${SLUG}/units`))
    expect(api.called('POST', '/api/organizations')[0].body).toEqual({
      name: 'Сибирский институт', kind: 'institute', city: 'Новосибирск', website: 'sikm.example.ru', description: 'Изучаем материалы.',
    })
  })

  it('shows the server\'s objections under the fields', async () => {
    stubApi({
      ...signedInAs(),
      'POST /api/organizations': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { website: 'Адрес сайта не похож на настоящий' } }),
    })
    renderApp('/organizations/new')
    await screen.findByRole('heading', { level: 1, name: 'Новая организация' })
    await fill(/^Название/, 'Институт')
    await userEvent.selectOptions(field(/^Тип организации/), 'other')
    await fill(/^Город/, 'Москва')
    await fill(/^Сайт/, 'нет')
    await press('Создать организацию')
    expect(await screen.findByText('Адрес сайта не похож на настоящий')).toBeInTheDocument()
    expect(field(/^Сайт/)).toHaveFocus()
  })

  it('says so when the daily limit is reached', async () => {
    stubApi({
      ...signedInAs(),
      'POST /api/organizations': apiError(429, 'rate_limited', 'Слишком много попыток', { retry_after: 7200 }),
    })
    renderApp('/organizations/new')
    await screen.findByRole('heading', { level: 1, name: 'Новая организация' })
    await fill(/^Название/, 'Институт')
    await userEvent.selectOptions(field(/^Тип организации/), 'other')
    await fill(/^Город/, 'Москва')
    await press('Создать организацию')
    expect(await screen.findByRole('alert')).toHaveTextContent('Слишком много попыток')
  })

  it('asks a visitor to sign in first', async () => {
    stubApi()
    const { router } = renderApp('/organizations/new')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=%2Forganizations%2Fnew')
  })
})

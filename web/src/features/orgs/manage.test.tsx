import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { dept, lab, members, membersRoute, mine, org, orgRoutes, orgView, pending, SLUG, viewers } from '../../test/orgs'
import { renderApp } from '../../test/render'

const base = `/my-organization/${SLUG}`
const me = { ...ann, id: members[0].user_id, name: 'Анна Смирнова' }
const asOwner = (extra: Record<string, Route> = {}) => ({ ...signedInAs(me), ...orgRoutes(viewers.owner), ...membersRoute(), ...extra })
const as = (viewer: keyof typeof viewers, extra: Record<string, Route> = {}) => ({ ...signedInAs(me), ...orgRoutes(viewers[viewer]), ...extra })

describe('management frame', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp(base)
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe(`?next=${encodeURIComponent(base)}`)
  })

  it('shows the organization, the role and all three sections to the owner', async () => {
    stubApi(asOwner())
    renderApp(base)
    expect(await screen.findByRole('heading', { level: 1, name: org.name })).toBeInTheDocument()
    const header = within(screen.getByRole('main'))
    expect(header.getByText('Владелец')).toBeInTheDocument()
    expect(header.getByRole('link', { name: 'Открыть публичную страницу' })).toHaveAttribute('href', `/organizations/${SLUG}`)
    const tabs = within(screen.getByRole('navigation', { name: 'Разделы управления' }))
    expect(tabs.getAllByRole('link').map((a) => a.textContent)).toEqual(['Данные', 'Подразделения', 'Сотрудники'])
    expect(tabs.getByRole('link', { name: 'Данные' })).toHaveAttribute('aria-current', 'page')
  })

  it('hides the people section from everyone but the owner', async () => {
    stubApi(as('hr'))
    renderApp(base)
    await screen.findByRole('heading', { level: 1, name: org.name })
    const tabs = within(screen.getByRole('navigation', { name: 'Разделы управления' }))
    expect(tabs.queryByRole('link', { name: 'Сотрудники' })).not.toBeInTheDocument()
    expect(tabs.getByRole('link', { name: 'Подразделения' })).toBeInTheDocument()
  })

  it('tells a person who does not work there that they cannot manage', async () => {
    stubApi(as('stranger'))
    renderApp(base)
    expect(await screen.findByRole('heading', { level: 2, name: 'Вы не работаете в этой организации' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: org.name })).toHaveAttribute('href', `/organizations/${SLUG}`)
    expect(screen.queryByRole('navigation', { name: 'Разделы управления' })).not.toBeInTheDocument()
  })

  it('treats an answer without a viewer the same way', async () => {
    stubApi({ ...signedInAs(me), [`GET /api/organizations/${SLUG}`]: reply(200, orgView(null)) })
    renderApp(base)
    expect(await screen.findByRole('heading', { level: 2, name: 'Вы не работаете в этой организации' })).toBeInTheDocument()
  })

  it('says the organization does not exist', async () => {
    stubApi({ ...signedInAs(me), [`GET /api/organizations/${SLUG}`]: apiError(404, 'not_found', 'Такой страницы нет') })
    renderApp(base)
    expect(await screen.findByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('shows loading, then an error with retry', async () => {
    let n = 0
    stubApi({ ...as('owner'), [`GET /api/organizations/${SLUG}`]: () => (n++ === 0 ? reply(500) : reply(200, orgView(viewers.owner))) })
    renderApp(base)
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить организацию')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: org.name })).toBeInTheDocument()
  })
})

describe('organization data', () => {
  it('lets the owner change the data', async () => {
    const api = stubApi(asOwner({ [`PATCH /api/organizations/${SLUG}`]: reply(200, { organization: org }) }))
    renderApp(base)
    await screen.findByRole('heading', { level: 1, name: org.name })
    expect(field(/^Название/)).toHaveValue(org.name)
    expect(field(/^Тип организации/)).toHaveValue('institute')
    expect(field(/^Город/)).toHaveValue('Новосибирск')
    await userEvent.clear(field(/^Город/))
    await fill(/^Город/, 'Томск')
    await press('Сохранить')
    expect(await screen.findByText('Данные сохранены')).toBeInTheDocument()
    expect(api.called('PATCH', `/api/organizations/${SLUG}`)[0].body).toMatchObject({ city: 'Томск', name: org.name })
  })

  it('shows the server\'s objection to the owner', async () => {
    stubApi(
      asOwner({
        [`PATCH /api/organizations/${SLUG}`]: apiError(422, 'validation_failed', 'x', { fields: { description: 'Описание слишком длинное' } }),
      }),
    )
    renderApp(base)
    await screen.findByRole('heading', { level: 1, name: org.name })
    await press('Сохранить')
    expect(await screen.findByText('Описание слишком длинное')).toBeInTheDocument()
  })

  it('shows only a note and the role to the others, with no form', async () => {
    for (const [who, label] of [
      ['hr', 'Кадровик'],
      ['head', 'Руководитель подразделения'],
    ] as const) {
      stubApi(as(who))
      const { unmount } = renderApp(base)
      await screen.findByText('Менять данные организации может только владелец.')
      expect(screen.queryByRole('textbox', { name: /Название/ })).not.toBeInTheDocument()
      expect(within(screen.getByRole('definition').parentElement!).getByText(label, { selector: 'dt' })).toBeInTheDocument()
      unmount()
    }
  })

  it('lets a member leave the organization after confirming', async () => {
    const api = stubApi(as('hr', { [`DELETE /api/organizations/${SLUG}/members/${me.id}`]: reply(204), 'GET /api/my/organizations': mine() }))
    const { router } = renderApp(base)
    await screen.findByText('Менять данные организации может только владелец.')
    await press('Выйти из организации')
    const dialog = screen.getByRole('dialog', { name: 'Выйти из организации?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Выйти' }))
    expect(await screen.findByText('Вы вышли из организации')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe('/my-organization'))
    expect(api.called('DELETE', `/api/organizations/${SLUG}/members/${me.id}`)).toHaveLength(1)
  })

  it('does nothing when the member changes their mind', async () => {
    const api = stubApi(as('hr'))
    renderApp(base)
    await screen.findByText('Менять данные организации может только владелец.')
    await press('Выйти из организации')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
  })

  it('explains why the last owner cannot leave', async () => {
    stubApi(
      asOwner({
        [`DELETE /api/organizations/${SLUG}/members/${me.id}`]: apiError(409, 'last_owner', 'В организации должен остаться хотя бы один владелец'),
      }),
    )
    renderApp(base)
    await screen.findByRole('heading', { level: 1, name: org.name })
    await press('Выйти из организации')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Выйти' }))
    expect(await screen.findByText('Не удалось убрать сотрудника')).toBeInTheDocument()
    expect(screen.getByText('В организации должен остаться хотя бы один владелец')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('units list', () => {
  it('lets the owner add, edit and delete', async () => {
    const api = stubApi(asOwner({ [`DELETE /api/organizations/${SLUG}/units/${dept.id}`]: reply(204) }))
    renderApp(`${base}/units`)
    expect(await screen.findByRole('link', { name: lab.name })).toHaveAttribute('href', `/organizations/${SLUG}/units/${lab.id}`)
    expect(screen.getAllByRole('link', { name: 'Добавить подразделение' })[0]).toHaveAttribute('href', `${base}/units/new`)
    expect(screen.getByText('Руководитель: Анна Смирнова')).toBeInTheDocument()
    expect(screen.getByText('Руководитель не назначен')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Изменить' })).toHaveLength(2)

    const deptRow = screen.getByRole('link', { name: dept.name }).closest('li')!
    await userEvent.click(within(deptRow).getByRole('button', { name: 'Удалить' }))
    const dialog = screen.getByRole('dialog', { name: `Удалить «${dept.name}»?` })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Удалить' }))
    expect(await screen.findByText('Подразделение удалено')).toBeInTheDocument()
    expect(api.called('DELETE', `/api/organizations/${SLUG}/units/${dept.id}`)).toHaveLength(1)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('does not delete when cancelled, and reports a failure', async () => {
    const api = stubApi(
      asOwner({ [`DELETE /api/organizations/${SLUG}/units/${lab.id}`]: apiError(500, 'internal', 'Что-то сломалось на сервере') }),
    )
    renderApp(`${base}/units`)
    const labRow = (await screen.findByRole('link', { name: lab.name })).closest('li')!
    await userEvent.click(within(labRow).getByRole('button', { name: 'Удалить' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    expect(api.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)

    await userEvent.click(within(labRow).getByRole('button', { name: 'Удалить' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Удалить' }))
    expect(await screen.findByText('Не удалось удалить подразделение')).toBeInTheDocument()
  })

  it('invites the owner to add the first unit', async () => {
    stubApi({ ...signedInAs(me), ...orgRoutes(viewers.owner, []), ...membersRoute() })
    renderApp(`${base}/units`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Подразделений пока нет' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Добавить подразделение' })).toHaveLength(2)
  })

  it('shows an empty list without an add button to someone who cannot add', async () => {
    stubApi({ ...signedInAs(me), ...orgRoutes(viewers.hr, []) })
    renderApp(`${base}/units`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Подразделений пока нет' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Добавить подразделение' })).not.toBeInTheDocument()
  })

  it('lets a head edit only their own unit, and never add or delete', async () => {
    stubApi(as('head'))
    renderApp(`${base}/units`)
    const labRow = (await screen.findByRole('link', { name: lab.name })).closest('li')!
    expect(within(labRow).getByRole('link', { name: 'Изменить' })).toHaveAttribute('href', `${base}/units/${lab.id}`)
    expect(within(labRow).queryByRole('button', { name: 'Удалить' })).not.toBeInTheDocument()
    const deptRow = screen.getByRole('link', { name: dept.name }).closest('li')!
    expect(within(deptRow).queryByRole('link', { name: 'Изменить' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Добавить подразделение' })).not.toBeInTheDocument()
  })

  it('shows an hr person the list without any actions', async () => {
    stubApi(as('hr'))
    renderApp(`${base}/units`)
    await screen.findByRole('link', { name: lab.name })
    expect(screen.queryByRole('link', { name: 'Изменить' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Удалить' })).not.toBeInTheDocument()
  })
})

describe('add and edit a unit', () => {
  it('adds a unit with topics one per line and a head', async () => {
    const api = stubApi(
      asOwner({
        [`POST /api/organizations/${SLUG}/units`]: reply(201, { unit: { ...lab, id: 'new-unit' } }),
        [`PUT /api/organizations/${SLUG}/units/new-unit/head`]: reply(200, { unit: lab }),
      }),
    )
    const { router } = renderApp(`${base}/units/new`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })).toBeInTheDocument()
    await fill(/^Название/, 'Лаборатория оптики')
    await fill(/^Описание/, 'Свет и вещество.')
    await userEvent.selectOptions(field(/^Вид подразделения/), 'laboratory')
    await fill(/^Научные темы/, 'Фотоны{Enter}Кубиты')
    await userEvent.selectOptions(field(/^Руководитель/), members[1].user_id)
    await press('Добавить подразделение')
    expect(await screen.findByText('Подразделение добавлено')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe(`${base}/units`))
    expect(api.called('POST', `/api/organizations/${SLUG}/units`)[0].body).toMatchObject({
      name: 'Лаборатория оптики', kind: 'laboratory', description: 'Свет и вещество.', topics: ['Фотоны', 'Кубиты'],
    })
    expect(api.called('PUT', `/api/organizations/${SLUG}/units/new-unit/head`)[0].body).toEqual({ user_id: members[1].user_id })
  })

  it('does not set a head when none is chosen', async () => {
    const api = stubApi(asOwner({ [`POST /api/organizations/${SLUG}/units`]: reply(201, { unit: { ...lab, id: 'new-unit' } }) }))
    renderApp(`${base}/units/new`)
    await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })
    await fill(/^Название/, 'Отдел')
    await userEvent.selectOptions(field(/^Вид подразделения/), 'division')
    await press('Добавить подразделение')
    expect(await screen.findByText('Подразделение добавлено')).toBeInTheDocument()
    expect(api.calls.filter((c) => c.method === 'PUT')).toHaveLength(0)
  })

  it('requires a name and a kind', async () => {
    const api = stubApi(asOwner())
    renderApp(`${base}/units/new`)
    await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })
    await press('Добавить подразделение')
    expect(screen.getByText('Укажите название')).toBeInTheDocument()
    expect(screen.getByText('Выберите вид подразделения из списка')).toBeInTheDocument()
    expect(api.called('POST', `/api/organizations/${SLUG}/units`)).toHaveLength(0)
  })

  it('shows the server\'s objection to topics', async () => {
    stubApi(
      asOwner({
        [`POST /api/organizations/${SLUG}/units`]: apiError(422, 'validation_failed', 'x', { fields: { topics: 'Тем не больше десяти' } }),
      }),
    )
    renderApp(`${base}/units/new`)
    await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })
    await fill(/^Название/, 'Отдел')
    await userEvent.selectOptions(field(/^Вид подразделения/), 'division')
    await press('Добавить подразделение')
    expect(await screen.findByText('Тем не больше десяти')).toBeInTheDocument()
  })

  it('shows a general failure above the form', async () => {
    stubApi(asOwner({ [`POST /api/organizations/${SLUG}/units`]: apiError(500, 'internal', 'Что-то сломалось на сервере') }))
    renderApp(`${base}/units/new`)
    await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })
    await fill(/^Название/, 'Отдел')
    await userEvent.selectOptions(field(/^Вид подразделения/), 'division')
    await press('Добавить подразделение')
    expect(await screen.findByRole('alert')).toHaveTextContent('Что-то сломалось на сервере')
  })

  it('edits a unit: new name, a different head', async () => {
    const api = stubApi(
      asOwner({
        [`PATCH /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, { unit: lab }),
        [`PUT /api/organizations/${SLUG}/units/${lab.id}/head`]: reply(200, { unit: lab }),
      }),
    )
    renderApp(`${base}/units/${lab.id}`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Подразделение' })).toBeInTheDocument()
    expect(field(/^Название/)).toHaveValue(lab.name)
    expect(field(/^Научные темы/)).toHaveValue('Сверхпроводимость\nТонкие плёнки')
    expect(field(/^Руководитель/)).toHaveValue(lab.head_user_id ?? '')
    await userEvent.clear(field(/^Название/))
    await fill(/^Название/, 'Лаборатория сверхпроводимости')
    await userEvent.selectOptions(field(/^Руководитель/), members[1].user_id)
    await press('Сохранить')
    expect(await screen.findByText('Подразделение сохранено')).toBeInTheDocument()
    expect(api.called('PATCH', `/api/organizations/${SLUG}/units/${lab.id}`)[0].body).toMatchObject({ name: 'Лаборатория сверхпроводимости' })
    expect(api.called('PUT', `/api/organizations/${SLUG}/units/${lab.id}/head`)[0].body).toEqual({ user_id: members[1].user_id })
  })

  it('leaves the head alone when it did not change', async () => {
    const api = stubApi(
      asOwner({
        [`PATCH /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, { unit: lab }),
        [`PUT /api/organizations/${SLUG}/units/${lab.id}/head`]: reply(200, { unit: lab }),
      }),
    )
    renderApp(`${base}/units/${lab.id}`)
    await screen.findByRole('heading', { level: 2, name: 'Подразделение' })
    await press('Сохранить')
    await screen.findByText('Подразделение сохранено')
    expect(api.calls.filter((c) => c.method === 'PUT')).toHaveLength(0)

  })

  it('shows the server\'s objection to the head under the field', async () => {
    stubApi(
      asOwner({
        [`PATCH /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, { unit: lab }),
        [`PUT /api/organizations/${SLUG}/units/${lab.id}/head`]: apiError(422, 'validation_failed', 'x', { fields: { user_id: 'Руководителем может быть только сотрудник организации' } }),
      }),
    )
    renderApp(`${base}/units/${lab.id}`)
    await screen.findByRole('heading', { level: 2, name: 'Подразделение' })
    await userEvent.selectOptions(field(/^Руководитель/), members[1].user_id)
    await press('Сохранить')
    expect(await screen.findByText('Руководителем может быть только сотрудник организации')).toBeInTheDocument()
  })

  it('removes the head when "not assigned" is chosen', async () => {
    const api = stubApi(
      asOwner({
        [`PATCH /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, { unit: lab }),
        [`PUT /api/organizations/${SLUG}/units/${lab.id}/head`]: reply(200, { unit: lab }),
      }),
    )
    renderApp(`${base}/units/${lab.id}`)
    await screen.findByRole('heading', { level: 2, name: 'Подразделение' })
    await userEvent.selectOptions(field(/^Руководитель/), '')
    await press('Сохранить')
    await screen.findByText('Подразделение сохранено')
    expect(api.called('PUT', `/api/organizations/${SLUG}/units/${lab.id}/head`)[0].body).toEqual({ user_id: null })
  })

  it('shows the head of a unit without head options to someone who cannot assign', async () => {
    const api = stubApi(as('head', { [`PATCH /api/organizations/${SLUG}/units/${lab.id}`]: reply(200, { unit: lab }) }))
    renderApp(`${base}/units/${lab.id}`)
    await screen.findByRole('heading', { level: 2, name: 'Подразделение' })
    expect(screen.queryByRole('combobox', { name: /Руководитель/ })).not.toBeInTheDocument()
    expect(screen.getByText(/Руководитель: Анна Смирнова/)).toBeInTheDocument()
    expect(screen.getByText(/Руководителя назначает владелец/)).toBeInTheDocument()
    await press('Сохранить')
    expect(await screen.findByText('Подразделение сохранено')).toBeInTheDocument()
    expect(api.calls.filter((c) => c.method === 'PUT')).toHaveLength(0)
    expect(api.calls.filter((c) => c.path.endsWith('/members'))).toHaveLength(0)
  })

  it('shows the note without a name when the unit has no head', async () => {
    stubApi({ ...signedInAs(me), ...orgRoutes({ ...viewers.head, editable_units: [dept.id] }) })
    renderApp(`${base}/units/${dept.id}`)
    await screen.findByRole('heading', { level: 2, name: 'Подразделение' })
    expect(screen.getByText('Руководителя назначает владелец организации.')).toBeInTheDocument()
  })

  it('refuses someone who may not edit this unit, and someone who may not add', async () => {
    stubApi(as('head'))
    const first = renderApp(`${base}/units/${dept.id}`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Здесь нет прав' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: /Название/ })).not.toBeInTheDocument()
    first.unmount()

    stubApi(as('hr'))
    renderApp(`${base}/units/new`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Здесь нет прав' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К списку подразделений' })).toHaveAttribute('href', `${base}/units`)
  })

  it('says when the unit does not exist', async () => {
    stubApi(asOwner())
    renderApp(`${base}/units/99999999-9999-4999-8999-999999999999`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Такого подразделения нет' })).toBeInTheDocument()
  })

  it('shows loading and an error while the owner\'s people list loads', async () => {
    let n = 0
    stubApi(asOwner({ [`GET /api/organizations/${SLUG}/members`]: () => (n++ === 0 ? reply(500) : reply(200, { members, invitations: [] })) }))
    renderApp(`${base}/units/new`)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить сотрудников')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 2, name: 'Новое подразделение' })).toBeInTheDocument()
  })
})

describe('people and invitations', () => {
  it('lists people with roles and invitations that wait for an answer', async () => {
    stubApi(asOwner())
    renderApp(`${base}/members`)
    expect(await screen.findByRole('heading', { level: 2, name: 'В организации' })).toBeInTheDocument()
    const main = within(screen.getByRole('main'))
    const first = main.getByText('Анна Смирнова').closest('.member-row') as HTMLElement
    expect(within(first).getByText('это вы')).toBeInTheDocument()
    expect(within(first).getByText('anna@example.ru')).toBeInTheDocument()
    expect(within(first).getByRole('combobox', { name: 'Роль: Анна Смирнова' })).toHaveValue('owner')
    expect(within(first).queryByRole('button', { name: 'Убрать' })).not.toBeInTheDocument()
    const second = main.getByText('Борис Кадров').closest('.member-row') as HTMLElement
    expect(within(second).getByRole('combobox')).toHaveValue('hr')
    expect(within(second).getByRole('button', { name: 'Убрать' })).toBeInTheDocument()

    const waiting = within(screen.getByRole('heading', { level: 2, name: 'Ждут ответа' }).closest('section')!)
    expect(waiting.getByText('new@example.ru')).toBeInTheDocument()
    expect(waiting.getByText(`подразделение «${lab.name}»`)).toBeInTheDocument()
    expect(waiting.getByText(/действует до 9 октября 2026/)).toBeInTheDocument()
    // Справка о ролях.
    expect(screen.getAllByText('Руководитель подразделения').length).toBeGreaterThan(0)
  })

  it('says nothing waits when there are no pending invitations', async () => {
    stubApi(asOwner({ ...membersRoute(members, []) }))
    renderApp(`${base}/members`)
    expect(await screen.findByText('Неотвеченных приглашений нет.')).toBeInTheDocument()
  })

  it('is closed to everyone but the owner, even by direct address', async () => {
    stubApi(as('hr'))
    renderApp(`${base}/members`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Здесь нет прав' })).toBeInTheDocument()
    expect(within(screen.getByRole('main')).queryByText('anna@example.ru')).not.toBeInTheDocument()
  })

  it('shows an error with retry when the list does not load', async () => {
    let n = 0
    stubApi(asOwner({ [`GET /api/organizations/${SLUG}/members`]: () => (n++ === 0 ? reply(500) : reply(200, { members, invitations: [] })) }))
    renderApp(`${base}/members`)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить сотрудников')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 2, name: 'В организации' })).toBeInTheDocument()
  })

  describe('invite', () => {
    it('sends an invitation and clears the form', async () => {
      const api = stubApi(asOwner({ [`POST /api/organizations/${SLUG}/invitations`]: reply(201, { invitation: { ...pending, email: 'kolya@example.ru' } }) }))
      renderApp(`${base}/members`)
      await screen.findByRole('heading', { level: 2, name: 'Пригласить сотрудника' })
      await fill(/^Почта сотрудника/, '  kolya@example.ru ')
      await userEvent.selectOptions(field(/^Роль ·/), 'hr')
      expect(screen.queryByRole('combobox', { name: /^Подразделение/ })).not.toBeInTheDocument()
      await press('Отправить приглашение')
      expect(await screen.findByText('Приглашение отправлено на kolya@example.ru')).toBeInTheDocument()
      expect(api.called('POST', `/api/organizations/${SLUG}/invitations`)[0].body).toEqual({ email: 'kolya@example.ru', role: 'hr', unit_id: null })
      await waitFor(() => expect(field(/^Почта сотрудника/)).toHaveValue(''))
    })

    it('offers only units without a head to a new head, and sends the chosen one', async () => {
      const api = stubApi(asOwner({ [`POST /api/organizations/${SLUG}/invitations`]: reply(201, { invitation: pending }) }))
      renderApp(`${base}/members`)
      await screen.findByRole('heading', { level: 2, name: 'Пригласить сотрудника' })
      await fill(/^Почта сотрудника/, 'kolya@example.ru')
      await userEvent.selectOptions(field(/^Роль ·/), 'unit_head')
      const unit = field(/^Подразделение/)
      const options = within(unit).getAllByRole('option').map((o) => o.textContent)
      expect(options).toEqual(['Назначу позже', dept.name])
      await userEvent.selectOptions(unit, dept.id)
      await press('Отправить приглашение')
      await screen.findByText('Приглашение отправлено на new@example.ru')
      expect(api.called('POST', `/api/organizations/${SLUG}/invitations`)[0].body).toEqual({ email: 'kolya@example.ru', role: 'unit_head', unit_id: dept.id })
    })

    it('sends no unit when "decide later" stays selected', async () => {
      const api = stubApi(asOwner({ [`POST /api/organizations/${SLUG}/invitations`]: reply(201, { invitation: pending }) }))
      renderApp(`${base}/members`)
      await screen.findByRole('heading', { level: 2, name: 'Пригласить сотрудника' })
      await fill(/^Почта сотрудника/, 'kolya@example.ru')
      await userEvent.selectOptions(field(/^Роль ·/), 'unit_head')
      await press('Отправить приглашение')
      await screen.findByText('Приглашение отправлено на new@example.ru')
      expect(api.called('POST', `/api/organizations/${SLUG}/invitations`)[0].body).toMatchObject({ role: 'unit_head', unit_id: null })
    })

    it('checks the address and the role before asking the server', async () => {
      const api = stubApi(asOwner())
      renderApp(`${base}/members`)
      await screen.findByRole('heading', { level: 2, name: 'Пригласить сотрудника' })
      await press('Отправить приглашение')
      expect(screen.getByText('Укажите почту')).toBeInTheDocument()
      expect(screen.getByText('Выберите роль', { selector: 'p' })).toBeInTheDocument()
      await fill(/^Почта сотрудника/, 'не-почта')
      await press('Отправить приглашение')
      expect(screen.getByText(/опечатка/)).toBeInTheDocument()
      expect(api.called('POST', `/api/organizations/${SLUG}/invitations`)).toHaveLength(0)
    })

    it('shows the server\'s objection under the address, and a general failure above the form', async () => {
      let n = 0
      stubApi(
        asOwner({
          [`POST /api/organizations/${SLUG}/invitations`]: () =>
            n++ === 0
              ? apiError(422, 'validation_failed', 'x', { fields: { email: 'Этот человек уже работает в вашей организации' } })
              : apiError(429, 'rate_limited', 'Слишком много попыток', { retry_after: 120 }),
        }),
      )
      renderApp(`${base}/members`)
      await screen.findByRole('heading', { level: 2, name: 'Пригласить сотрудника' })
      await fill(/^Почта сотрудника/, 'boris@example.ru')
      await userEvent.selectOptions(field(/^Роль ·/), 'hr')
      await press('Отправить приглашение')
      expect(await screen.findByText('Этот человек уже работает в вашей организации')).toBeInTheDocument()
      await press('Отправить приглашение')
      expect(await screen.findByText(/Слишком много попыток/)).toBeInTheDocument()
    })
  })

  describe('roles and removal', () => {
    it('changes a role', async () => {
      const api = stubApi(asOwner({ [`PATCH /api/organizations/${SLUG}/members/${members[1].user_id}`]: reply(204) }))
      renderApp(`${base}/members`)
      const select = await screen.findByRole('combobox', { name: 'Роль: Борис Кадров' })
      await userEvent.selectOptions(select, 'owner')
      expect(await screen.findByText('Роль изменена')).toBeInTheDocument()
      expect(api.called('PATCH', `/api/organizations/${SLUG}/members/${members[1].user_id}`)[0].body).toEqual({ role: 'owner' })
    })

    it('explains when a role cannot be changed', async () => {
      stubApi(
        asOwner({
          [`PATCH /api/organizations/${SLUG}/members/${members[0].user_id}`]: apiError(409, 'last_owner', 'В организации должен остаться хотя бы один владелец'),
        }),
      )
      renderApp(`${base}/members`)
      const select = await screen.findByRole('combobox', { name: 'Роль: Анна Смирнова' })
      await userEvent.selectOptions(select, 'hr')
      expect(await screen.findByText('Не удалось изменить роль')).toBeInTheDocument()
      expect(screen.getByText('В организации должен остаться хотя бы один владелец')).toBeInTheDocument()
      expect(select).toHaveValue('owner')
    })

    it('removes a person after confirming, and not before', async () => {
      const api = stubApi(asOwner({ [`DELETE /api/organizations/${SLUG}/members/${members[1].user_id}`]: reply(204) }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Убрать' }))
      const dialog = screen.getByRole('dialog', { name: 'Убрать Борис Кадров из организации?' })
      expect(api.calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
      await userEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Убрать' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Убрать' }))
      expect(await screen.findByText('Сотрудник убран')).toBeInTheDocument()
      expect(api.called('DELETE', `/api/organizations/${SLUG}/members/${members[1].user_id}`)).toHaveLength(1)
    })

    it('reports a failed removal', async () => {
      stubApi(asOwner({ [`DELETE /api/organizations/${SLUG}/members/${members[1].user_id}`]: apiError(500, 'internal', 'Что-то сломалось на сервере') }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Убрать' }))
      await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Убрать' }))
      expect(await screen.findByText('Не удалось убрать сотрудника')).toBeInTheDocument()
    })
  })

  describe('pending invitations', () => {
    it('revokes an invitation', async () => {
      const api = stubApi(asOwner({ [`DELETE /api/organizations/${SLUG}/invitations/${pending.id}`]: reply(204) }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Отозвать' }))
      expect(await screen.findByText('Приглашение отозвано')).toBeInTheDocument()
      expect(api.called('DELETE', `/api/organizations/${SLUG}/invitations/${pending.id}`)).toHaveLength(1)
    })

    it('reports a failed revoke', async () => {
      stubApi(asOwner({ [`DELETE /api/organizations/${SLUG}/invitations/${pending.id}`]: apiError(404, 'not_found', 'Такой страницы нет') }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Отозвать' }))
      expect(await screen.findByText('Не удалось отозвать приглашение')).toBeInTheDocument()
    })

    it('sends it again with the same role and unit', async () => {
      const api = stubApi(asOwner({ [`POST /api/organizations/${SLUG}/invitations`]: reply(201, { invitation: pending }) }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Отправить снова' }))
      expect(await screen.findByText('Приглашение отправлено на new@example.ru')).toBeInTheDocument()
      expect(api.called('POST', `/api/organizations/${SLUG}/invitations`)[0].body).toEqual({ email: 'new@example.ru', role: 'unit_head', unit_id: lab.id })
    })

    it('shows a general error when sending again fails', async () => {
      stubApi(asOwner({ [`POST /api/organizations/${SLUG}/invitations`]: apiError(429, 'rate_limited', 'Слишком много попыток', { retry_after: 120 }) }))
      renderApp(`${base}/members`)
      await userEvent.click(await screen.findByRole('button', { name: 'Отправить снова' }))
      expect(await screen.findByText(/Слишком много попыток/)).toBeInTheDocument()
    })

    it('shows an invitation without a unit', async () => {
      stubApi(asOwner({ ...membersRoute(members, [{ ...pending, unit_id: null, unit_name: null, role: 'hr' }]) }))
      renderApp(`${base}/members`)
      await screen.findByText('new@example.ru')
      expect(screen.queryByText(/подразделение «/)).not.toBeInTheDocument()
    })
  })
})

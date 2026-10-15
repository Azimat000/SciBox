import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { members, membersRoute, org, orgRoutes, SLUG, viewers } from '../../test/orgs'
import { renderApp } from '../../test/render'
import type { Template } from './templates'

// Страница правки шаблонов в разделе организации, список заблокированных организаций и письма о сообщениях в настройках (срез 36).

const me = { ...ann, id: members[0].user_id }
const PAGE = `/my-organization/${SLUG}/chat-templates`
const LIST = `GET /api/orgs/${org.id}/chat-templates`

const tpl = (n: number, title: string, text: string): Template => ({ id: `90000000-0000-4000-8000-${String(n).padStart(12, '0')}`, title, text, updated_at: '2026-10-11T09:00:00Z' })
const A = tpl(1, 'Приглашение на собеседование', 'Добрый день! Предлагаем встретиться онлайн.')
const B = tpl(2, 'Нужны документы', 'Пришлите, пожалуйста, скан диплома.')

const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })

beforeEach(() => {
  window.localStorage.clear()
  window.sessionStorage.clear()
})
afterEach(() => vi.useRealTimers())

function pageApi(viewer: keyof typeof viewers, list: Route, extra: Record<string, Route> = {}) {
  return stubApi({ ...signedInAs(me), ...orgRoutes(viewers[viewer]), ...membersRoute(), [LIST]: list, ...extra })
}

const row = (title: string) => screen.getByRole('heading', { level: 3, name: title }).closest('li') as HTMLElement

describe('templates page', () => {
  it('shows the templates of the organization with the count and the dates', async () => {
    pageApi('owner', reply(200, { items: [A, B], can_edit: true, limit: 50 }))
    renderApp(PAGE)
    expect(await screen.findByText('Шаблонов: 2 из 50')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Шаблоны ответов' })).toHaveAttribute('aria-current', 'page')
    expect(within(row(A.title)).getByText('Добрый день! Предлагаем встретиться онлайн.')).toBeInTheDocument()
    expect(within(row(A.title)).getByText(/Изменён 11 окт\. 2026/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: `Изменить шаблон «${A.title}»` })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: `Удалить шаблон «${B.title}»` })).toBeInTheDocument()
  })

  it('checks the new template on the page, creates it and thanks', async () => {
    const user = userEvent.setup()
    let items: Template[] = [A]
    const { calls } = pageApi('owner', () => reply(200, { items, can_edit: true, limit: 50 }), {
      [`POST /api/orgs/${org.id}/chat-templates`]: (call) => {
        const body = call.body as { title: string; text: string }
        items = [...items, tpl(3, body.title, body.text)]
        return reply(201, { template: items[items.length - 1] })
      },
    })
    renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: 'Добавить шаблон' }))
    expect(screen.getByLabelText(/Название/)).toHaveFocus()
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await screen.findByText('Назовите шаблон')).toBeInTheDocument()
    expect(screen.getByText('Введите текст шаблона')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(/Название/), { target: { value: 'Н'.repeat(121) } })
    fireEvent.change(screen.getByLabelText(/Текст/), { target: { value: 'я'.repeat(4001) } })
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await screen.findByText('Название не длиннее 120 знаков')).toBeInTheDocument()
    expect(screen.getByText('Сократите сообщение на 1 зн.')).toBeInTheDocument()
    expect(screen.getByText('4001 из 4000')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST')).toBe(false)
    fireEvent.change(screen.getByLabelText(/Название/), { target: { value: '  Условия работы ' } })
    fireEvent.change(screen.getByLabelText(/Текст/), { target: { value: 'Ставка 1,0.\n' } })
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await toast('Шаблон добавлен')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ title: 'Условия работы', text: 'Ставка 1,0.' })
    expect(await screen.findByRole('heading', { level: 3, name: 'Условия работы' })).toBeInTheDocument()
    expect(screen.getByText('Шаблонов: 2 из 50')).toBeInTheDocument()
    // Фокус вернулся на «Добавить шаблон»
    expect(screen.getByRole('button', { name: 'Добавить шаблон' })).toHaveFocus()
  })

  it('puts focus on the first field with an error', async () => {
    const user = userEvent.setup()
    pageApi('owner', reply(200, { items: [], can_edit: true, limit: 50 }))
    renderApp(PAGE)
    await user.click((await screen.findAllByRole('button', { name: 'Добавить шаблон' }))[0])
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await screen.findByText('Назовите шаблон')).toBeInTheDocument()
    expect(screen.getByLabelText(/Название/)).toHaveFocus()
    await user.type(screen.getByLabelText(/Название/), 'Есть название')
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    await waitFor(() => expect(screen.getByLabelText(/Текст/)).toHaveFocus())
  })

  it('asks before leaving a started template and stays when told so', async () => {
    const user = userEvent.setup()
    pageApi('owner', reply(200, { items: [A], can_edit: true, limit: 50 }))
    const { router } = renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: 'Добавить шаблон' }))
    await user.type(screen.getByLabelText(/Название/), 'Начал писать')
    await user.click(screen.getByRole('link', { name: 'Данные' }))
    const dialog = await screen.findByRole('dialog', { name: 'Уйти без сохранения?' })
    await user.click(within(dialog).getByRole('button', { name: 'Остаться' }))
    expect(router.state.location.pathname).toBe(PAGE)
    expect(screen.getByLabelText(/Название/)).toHaveValue('Начал писать')
  })

  it('shows what the server said about fields and about the rest', async () => {
    const user = userEvent.setup()
    let answer = apiError(422, 'validation_failed', 'Проверьте поля', { fields: { title: 'Такое название уже есть' } })
    pageApi('owner', reply(200, { items: [A], can_edit: true, limit: 50 }), { [`POST /api/orgs/${org.id}/chat-templates`]: () => answer })
    renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: 'Добавить шаблон' }))
    await user.type(screen.getByLabelText(/Название/), 'Дубль')
    await user.type(screen.getByLabelText(/Текст/), 'Текст')
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await screen.findByText('Такое название уже есть')).toBeInTheDocument()
    answer = apiError(409, 'limit_reached', 'У организации уже 50 шаблонов')
    await user.click(screen.getByRole('button', { name: 'Добавить' }))
    expect(await screen.findByText('Не удалось сохранить шаблон')).toBeInTheDocument()
    expect(screen.getByText('У организации уже 50 шаблонов')).toBeInTheDocument()
    // Отмена закрывает форму и возвращает фокус на кнопку
    await user.click(screen.getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByLabelText(/Название/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить шаблон' })).toHaveFocus()
  })

  it('does not allow more than the limit and says so', async () => {
    const full = Array.from({ length: 50 }, (_, i) => tpl(i + 1, `Шаблон ${i + 1}`, 'Текст'))
    pageApi('owner', reply(200, { items: full, can_edit: true, limit: 50 }))
    renderApp(PAGE)
    expect(await screen.findByText('Шаблонов: 50 из 50')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить шаблон' })).toBeDisabled()
    expect(screen.getByText(/Достигнут предел в 50 шаблонов/)).toBeInTheDocument()
  })

  it('edits a template in place and saves it', async () => {
    const user = userEvent.setup()
    const { calls } = pageApi('hr', reply(200, { items: [A, B], can_edit: true, limit: 50 }), {
      [`PATCH /api/orgs/${org.id}/chat-templates/${B.id}`]: (call) => reply(200, { template: { ...B, ...(call.body as object) } }),
    })
    renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: `Изменить шаблон «${B.title}»` }))
    const title = screen.getByLabelText(/Название/)
    expect(title).toHaveValue(B.title)
    expect(screen.getByLabelText(/Текст/)).toHaveValue(B.text)
    await user.clear(title)
    await user.type(title, 'Документы');
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await toast('Шаблон сохранён')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ title: 'Документы', text: B.text })
    // Форма правки закрыта, фокус на кнопке правки того же шаблона
    await waitFor(() => expect(screen.queryByLabelText(/Название/)).not.toBeInTheDocument())
  })

  it('cancels an edit without saving', async () => {
    const user = userEvent.setup()
    const { calls } = pageApi('owner', reply(200, { items: [A], can_edit: true, limit: 50 }))
    renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: `Изменить шаблон «${A.title}»` }))
    await user.click(screen.getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByLabelText(/Название/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: `Изменить шаблон «${A.title}»` })).toHaveFocus()
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
  })

  it('deletes after the confirmation and tells when the server refuses', async () => {
    const user = userEvent.setup()
    let items = [A, B]
    let fail = true
    const { calls } = pageApi('owner', () => reply(200, { items, can_edit: true, limit: 50 }), {
      [`DELETE /api/orgs/${org.id}/chat-templates/${A.id}`]: () => {
        if (fail) return apiError(500, 'internal', 'Сбой на сервере')
        items = [B]
        return reply(204)
      },
    })
    renderApp(PAGE)
    await user.click(await screen.findByRole('button', { name: `Удалить шаблон «${A.title}»` }))
    const dialog = await screen.findByRole('dialog', { name: 'Удалить шаблон?' })
    expect(within(dialog).getByText(/исчезнет у всех сотрудников организации/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Удалить' }))
    expect(await toast('Не удалось удалить шаблон')).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: `Удалить шаблон «${A.title}»` }))
    await user.click(within(await screen.findByRole('dialog', { name: 'Удалить шаблон?' })).getByRole('button', { name: 'Удалить' }))
    expect(await toast('Шаблон удалён')).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(2)
    await waitFor(() => expect(screen.queryByRole('heading', { level: 3, name: A.title })).not.toBeInTheDocument())
  })

  it('lets a person who may not edit only read', async () => {
    pageApi('head', reply(200, { items: [A], can_edit: false, limit: 50 }))
    renderApp(PAGE)
    expect(await screen.findByRole('heading', { level: 3, name: A.title })).toBeInTheDocument()
    expect(screen.getByText(/Править шаблоны могут владелец организации и кадровик/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Добавить шаблон' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Изменить шаблон/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Удалить шаблон/ })).not.toBeInTheDocument()
  })

  it('invites to add the first template and tells a reader to wait', async () => {
    const user = userEvent.setup()
    pageApi('owner', reply(200, { items: [], can_edit: true, limit: 50 }))
    const first = renderApp(PAGE)
    expect(await screen.findByRole('heading', { level: 2, name: 'Шаблонов пока нет' })).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: 'Добавить шаблон' })[1])
    expect(await screen.findByLabelText(/Название/)).toBeInTheDocument()
    first.unmount()
    pageApi('head', reply(200, { items: [], can_edit: false, limit: 50 }))
    renderApp(PAGE)
    expect(await screen.findByText(/Когда владелец или кадровик добавят шаблоны/)).toBeInTheDocument()
  })

  it('says templates are unavailable when the server closes them', async () => {
    pageApi('head', apiError(403, 'forbidden', 'Недостаточно прав'))
    renderApp(PAGE)
    expect(await screen.findByRole('heading', { level: 2, name: 'Шаблоны недоступны' })).toBeInTheDocument()
  })

  it('shows a load error and loads again', async () => {
    const user = userEvent.setup()
    let fail = true
    pageApi('owner', () => (fail ? apiError(500, 'internal', 'Сбой на сервере') : reply(200, { items: [A], can_edit: true, limit: 50 })))
    renderApp(PAGE)
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить шаблоны' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('heading', { level: 3, name: A.title })).toBeInTheDocument()
  })
})

describe('blocked organizations in the account', () => {
  const BLOCKS = 'GET /api/chat-blocks'
  const blockA = { org_id: 'o-1', org_name: 'Нейрофотоника', org_slug: 'neurofotonika', created_at: '2026-10-14T11:00:00Z' }
  const blockB = { org_id: 'o-2', org_name: 'Технопарк «Академ-Север»', org_slug: 'akadem-sever', created_at: '2026-09-01T11:00:00Z' }
  const section = async () => within(await screen.findByRole('region', { name: 'Заблокированные организации' }))

  it('lists the blocked organizations with the date and a link', async () => {
    stubApi({ ...signedInAs(ann), [BLOCKS]: reply(200, { items: [blockA, blockB] }) })
    renderApp('/account')
    const blocked = await section()
    expect(await blocked.findByRole('link', { name: 'Нейрофотоника' })).toHaveAttribute('href', '/organizations/neurofotonika')
    expect(blocked.getByText('Заблокирована 14 октября 2026 г.')).toBeInTheDocument()
    expect(blocked.getByRole('button', { name: 'Разблокировать организацию «Технопарк «Академ-Север»»' })).toBeInTheDocument()
  })

  it('unblocks, thanks and refreshes the list', async () => {
    const user = userEvent.setup()
    let items = [blockA, blockB]
    const { calls } = stubApi({
      ...signedInAs(ann),
      [BLOCKS]: () => reply(200, { items }),
      'DELETE /api/chat-blocks/o-1': () => {
        items = [blockB]
        return reply(204)
      },
    })
    renderApp('/account')
    const blocked = await section()
    await user.click(await blocked.findByRole('button', { name: 'Разблокировать организацию «Нейрофотоника»' }))
    expect(await toast('Блокировка снята')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'DELETE' && c.path === '/api/chat-blocks/o-1')).toBe(true)
    await waitFor(() => expect(blocked.queryByRole('link', { name: 'Нейрофотоника' })).not.toBeInTheDocument())
    expect(blocked.getByRole('link', { name: 'Технопарк «Академ-Север»' })).toBeInTheDocument()
  })

  it('tells when the server refuses and refreshes quietly when the block is already gone', async () => {
    const user = userEvent.setup()
    let answer = apiError(500, 'internal', 'Сбой на сервере')
    const { calls } = stubApi({ ...signedInAs(ann), [BLOCKS]: reply(200, { items: [blockA] }), 'DELETE /api/chat-blocks/o-1': () => answer })
    renderApp('/account')
    const blocked = await section()
    await user.click(await blocked.findByRole('button', { name: /Разблокировать организацию/ }))
    expect(await toast('Не удалось снять блокировку')).toBeInTheDocument()
    answer = apiError(404, 'not_found', 'Нет такой блокировки')
    const before = calls.filter((c) => c.path === '/api/chat-blocks' && c.method === 'GET').length
    await user.click(blocked.getByRole('button', { name: /Разблокировать организацию/ }))
    await waitFor(() => expect(calls.filter((c) => c.path === '/api/chat-blocks' && c.method === 'GET').length).toBeGreaterThan(before))
  })

  it('says nobody is blocked', async () => {
    stubApi({ ...signedInAs(ann), [BLOCKS]: reply(200, { items: [] }) })
    renderApp('/account')
    expect(await (await section()).findByText('Вы никого не блокировали.')).toBeInTheDocument()
  })

  it('is absent while the server does not know chat blocks', async () => {
    stubApi({ ...signedInAs(ann) })
    renderApp('/account')
    await screen.findByRole('region', { name: 'Письма' })
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Заблокированные организации' })).not.toBeInTheDocument())
  })

  it('shows a load error and loads again', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi({ ...signedInAs(ann), [BLOCKS]: () => (fail ? apiError(500, 'internal', 'Сбой на сервере') : reply(200, { items: [blockA] })) })
    renderApp('/account')
    const blocked = await section()
    expect(await blocked.findByText('Не удалось загрузить список')).toBeInTheDocument()
    fail = false
    await user.click(blocked.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await blocked.findByRole('link', { name: 'Нейрофотоника' })).toBeInTheDocument()
  })
})

describe('mail about messages', () => {
  const setup = (settings: Record<string, boolean>, extra: Record<string, Route> = {}) => stubApi({ ...signedInAs(ann), 'GET /api/notification-settings': reply(200, settings), ...extra })
  const section = async () => within(await screen.findByRole('region', { name: 'Письма' }))

  it('is not shown while the server does not send the field', async () => {
    setup({ email_new_vacancies: true, email_deadlines: true })
    renderApp('/account')
    const mail = await section()
    await waitFor(() => expect(mail.getByRole('checkbox', { name: /Напоминания о сроке подачи/ })).toBeChecked())
    expect(mail.queryByRole('checkbox', { name: /Письма о новых сообщениях/ })).not.toBeInTheDocument()
  })

  it('switches the letters on and off and keeps the other values', async () => {
    const user = userEvent.setup()
    const { calls } = setup(
      { email_new_vacancies: true, email_deadlines: false, email_messages: true },
      { 'PUT /api/notification-settings': (call) => reply(200, call.body) },
    )
    renderApp('/account')
    const mail = await section()
    const box = await waitFor(() => {
      const el = mail.getByRole('checkbox', { name: /Письма о новых сообщениях/ })
      expect(el).toBeChecked()
      return el
    })
    expect(mail.getByText(/В письме нет текста сообщения/)).toBeInTheDocument()
    await user.click(box)
    expect(await toast('Настройки писем сохранены')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ email_new_vacancies: true, email_deadlines: false, email_messages: false })
    expect(mail.getByRole('checkbox', { name: /Письма о новых сообщениях/ })).not.toBeChecked()
  })
})

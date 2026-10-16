import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { chatHeaders, chatLists, chatMessages, mineMessage, textMessage, unreadCounts } from '../../test/chat'
import { renderApp } from '../../test/render'
import { ROLE_STORAGE_KEY } from '../shell/role-context'
import type { ChatHeader, Message } from './api'
import { chatHub } from './hub'

// Инструменты чата (срез 36): шаблоны у поля ввода, закрытая переписка с недописанным текстом, архив, «Ещё»
// (жалоба, блокировка), строка списка, устойчивость окна к ошибкам и потере прав.

const tpl = (id: number, title: string, text: string) => ({ id: `90000000-0000-4000-8000-${String(id).padStart(12, '0')}`, title, text, updated_at: '2026-10-10T09:00:00Z' })
const TEMPLATES = [tpl(1, 'Нужны документы', 'Пришлите, пожалуйста, скан диплома.'), tpl(2, 'Приглашение', 'Добрый день! Приглашаем на собеседование.')]

const orgHeader = (over: Partial<ChatHeader> = {}): ChatHeader => ({ ...chatHeaders.orgAnonymousOffer(), ...over })
const sciHeader = (over: Partial<ChatHeader> = {}): ChatHeader => ({ ...chatHeaders.scientistInvited(), ...over })
const ORG_ID = orgHeader().viewer.org_id as string
const TEMPLATES_URL = `GET /api/orgs/${ORG_ID}/chat-templates`

function windowApi(header: ChatHeader, items: Message[] = [textMessage(1)], extra: Record<string, Route> = {}) {
  return stubApi({
    ...signedInAs(ann),
    'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
    'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
    'GET /api/chats?*': reply(200, chatLists.scientistAll()),
    [`GET /api/chats/${header.id}`]: reply(200, { chat: header }),
    [`GET /api/chats/${header.id}/messages*`]: reply(200, { items, has_more: false }),
    [`POST /api/chats/${header.id}/read`]: reply(204),
    ...extra,
  })
}

async function open(header: ChatHeader, items?: Message[], extra?: Record<string, Route>) {
  const api = windowApi(header, items, extra)
  const view = renderApp(`/chats/${header.id}`)
  await screen.findByRole('region', { name: 'Сообщения переписки' })
  return { ...api, ...view }
}

const box = () => screen.getByRole('textbox', { name: 'Сообщение' }) as HTMLTextAreaElement
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })

beforeEach(() => {
  window.localStorage.clear()
  window.sessionStorage.clear()
})
afterEach(() => {
  vi.useRealTimers()
  window.localStorage.clear()
  window.sessionStorage.clear()
})

describe('templates menu', () => {
  it('is absent for a scientist and for an organization without access', async () => {
    await open(sciHeader())
    expect(screen.queryByRole('button', { name: 'Шаблоны' })).not.toBeInTheDocument()
  })

  it('inserts a template into the empty field and lets the person edit it before sending', async () => {
    const user = userEvent.setup()
    const { calls } = await open(orgHeader(), [textMessage(1)], {
      [TEMPLATES_URL]: reply(200, { items: TEMPLATES, can_edit: true, limit: 50 }),
      [`POST /api/chats/${orgHeader().id}/messages`]: (call) => reply(201, { message: mineMessage(2, { text: (call.body as { text: string }).text }) }),
    })
    const trigger = screen.getByRole('button', { name: 'Шаблоны' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    await user.click(trigger)
    const menu = await screen.findByRole('dialog', { name: 'Шаблоны ответов' })
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    // Две записи без поиска: поиск появляется с шестой
    expect(within(menu).queryByRole('searchbox')).not.toBeInTheDocument()
    await user.click(within(menu).getByRole('button', { name: /Нужны документы/ }))
    expect(screen.queryByRole('dialog', { name: 'Шаблоны ответов' })).not.toBeInTheDocument()
    expect(box().value).toBe('Пришлите, пожалуйста, скан диплома.')
    expect(box()).toHaveFocus()
    await user.type(box(), ' Спасибо!')
    await user.click(screen.getByRole('button', { name: 'Отправить' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path.endsWith('/messages'))).toBe(true))
    const sent = calls.find((c) => c.method === 'POST' && c.path.endsWith('/messages'))
    expect(sent?.body).toMatchObject({ text: 'Пришлите, пожалуйста, скан диплома. Спасибо!' })
  })

  it('puts the template where the caret is', async () => {
    const user = userEvent.setup()
    await open(orgHeader(), [textMessage(1)], { [TEMPLATES_URL]: reply(200, { items: [tpl(1, 'Коротко', 'ВСТАВКА')], can_edit: true, limit: 50 }) })
    await user.type(box(), 'Начало конец')
    box().setSelectionRange(7, 7)
    await user.click(screen.getByRole('button', { name: 'Шаблоны' }))
    await user.click(await screen.findByRole('button', { name: /Коротко/ }))
    expect(box().value).toBe('Начало ВСТАВКАконец')
    expect(box().selectionStart).toBe('Начало ВСТАВКА'.length)
  })

  it('shows search from six templates, filters ignoring case and ё, and does not send on Enter', async () => {
    const user = userEvent.setup()
    const many = [
      tpl(1, 'Приглашение', 'Приглашаем'),
      tpl(2, 'Документы', 'Пришлите диплом'),
      tpl(3, 'Отказ', 'К сожалению, выбрали другого'),
      tpl(4, 'Условия', 'Ставка 1,0'),
      tpl(5, 'Ёлка', 'Новогодний текст'),
      tpl(6, 'Время', 'Удобно ли вам в четверг?'),
    ]
    const { calls } = await open(chatHeaders.orgApplication(), [textMessage(1)], { [TEMPLATES_URL]: reply(200, { items: many, can_edit: false, limit: 50 }) })
    await user.type(box(), 'Черновик')
    await user.click(screen.getByRole('button', { name: 'Шаблоны' }))
    const search = await screen.findByRole('searchbox', { name: 'Найти шаблон' })
    await user.type(search, 'ЕЛКА{Enter}')
    const menu = screen.getByRole('dialog', { name: 'Шаблоны ответов' })
    expect(within(menu).getAllByRole('button')).toHaveLength(1)
    expect(within(menu).getByRole('button', { name: /Ёлка/ })).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path.endsWith('/messages'))).toBe(false)
    await user.clear(search)
    await user.type(search, 'нет такого')
    expect(within(menu).getByText('Ничего не нашлось')).toBeInTheDocument()
    // Без права править ссылки на страницу нет
    expect(within(menu).queryByRole('link', { name: 'Править шаблоны' })).not.toBeInTheDocument()
  })

  it('tells there are no templates and links the editor to the page', async () => {
    const user = userEvent.setup()
    await open(orgHeader(), [textMessage(1)], { [TEMPLATES_URL]: reply(200, { items: [], can_edit: true, limit: 50 }) })
    await user.click(screen.getByRole('button', { name: 'Шаблоны' }))
    const menu = await screen.findByRole('dialog', { name: 'Шаблоны ответов' })
    expect(await within(menu).findByText(/^Шаблонов пока нет\. Сохраните ответы/)).toBeInTheDocument()
    expect(within(menu).getByRole('link', { name: 'Добавить шаблон' })).toHaveAttribute('href', '/my-organization/tsentr-izucheniya-arktiki/chat-templates')
  })

  it('tells a reader that others add templates', async () => {
    const user = userEvent.setup()
    await open(chatHeaders.orgApplication(), [textMessage(1)], { [TEMPLATES_URL]: reply(200, { items: [], can_edit: false, limit: 50 }) })
    await user.click(screen.getByRole('button', { name: 'Шаблоны' }))
    expect(await screen.findByText(/Добавить их могут владелец организации и кадровик/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Править шаблоны' })).not.toBeInTheDocument()
  })

  it('shows a load error and loads again', async () => {
    const user = userEvent.setup()
    let fail = true
    await open(orgHeader(), [textMessage(1)], {
      [TEMPLATES_URL]: () => (fail ? apiError(500, 'internal', 'Сбой на сервере') : reply(200, { items: TEMPLATES, can_edit: true, limit: 50 })),
    })
    await user.click(screen.getByRole('button', { name: 'Шаблоны' }))
    expect(await screen.findByText(/Не удалось загрузить шаблоны: Сбой на сервере/)).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('button', { name: /Нужны документы/ })).toBeInTheDocument()
  })

  it('closes with Escape and returns focus to the button, and closes on an outside click', async () => {
    const user = userEvent.setup()
    await open(orgHeader(), [textMessage(1)], { [TEMPLATES_URL]: reply(200, { items: TEMPLATES, can_edit: true, limit: 50 }) })
    const trigger = screen.getByRole('button', { name: 'Шаблоны' })
    await user.click(trigger)
    await screen.findByRole('button', { name: /Нужны документы/ })
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Шаблоны ответов' })).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    await user.click(trigger)
    await screen.findByRole('button', { name: /Нужны документы/ })
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('dialog', { name: 'Шаблоны ответов' })).not.toBeInTheDocument()
    // Нажатие на саму кнопку переключает меню и не закрывает его «снаружи»
    await user.click(trigger)
    expect(await screen.findByRole('dialog', { name: 'Шаблоны ответов' })).toBeInTheDocument()
    await user.click(trigger)
    expect(screen.queryByRole('dialog', { name: 'Шаблоны ответов' })).not.toBeInTheDocument()
  })
})

describe('closed conversation', () => {
  it('says why in the words of the side', async () => {
    await open(sciHeader({ can_write: false, closed_reason: 'blocked' }))
    expect(screen.getByText(/Вы заблокировали организацию, поэтому переписка только для чтения/)).toBeInTheDocument()
  })

  it('uses the common words for the organization and for reasons without own words', async () => {
    await open(orgHeader({ can_write: false, closed_reason: 'blocked' }))
    expect(screen.getByText('Переписка закрыта: соискатель ограничил переписку с организацией.')).toBeInTheDocument()
  })

  it('keeps the typed text visible and copyable when the conversation is closed under the person', async () => {
    window.sessionStorage.setItem(`scibox.chatDraft.v1:${ann.id}:${chatHeaders.scientistClosed().id}`, 'Недописанный ответ')
    await open(chatHeaders.scientistClosed())
    expect(screen.getByText('Вы не успели отправить этот текст. Его можно скопировать.')).toBeInTheDocument()
    const copy = screen.getByLabelText('Вы не успели отправить этот текст. Его можно скопировать.') as HTMLTextAreaElement
    expect(copy).toHaveAttribute('readonly')
    expect(copy.value).toBe('Недописанный ответ')
    expect(screen.queryByRole('textbox', { name: 'Сообщение' })).not.toBeInTheDocument()
  })

  it('does not show the unsent block when nothing was typed', async () => {
    await open(chatHeaders.scientistClosed())
    expect(screen.queryByText(/Вы не успели отправить/)).not.toBeInTheDocument()
  })
})

describe('archive in the window', () => {
  it('is a button for the organization', async () => {
    const user = userEvent.setup()
    const header = orgHeader()
    const { calls } = await open(header, [textMessage(1)], { [`POST /api/chats/${header.id}/archive`]: reply(204) })
    await user.click(screen.getByRole('button', { name: 'В архив' }))
    expect(await toast('Переписка в архиве')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/chats/${header.id}/archive`)).toBe(true)
  })

  it('shows the label of the way back for an archived conversation of the organization', async () => {
    await open(orgHeader({ archived: true }))
    expect(screen.getByRole('button', { name: 'Вернуть' })).toBeInTheDocument()
  })

  it('moves the conversation to the archive and back through the menu', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    let archived = false
    const { calls } = await open(header, [textMessage(1)], {
      [`GET /api/chats/${header.id}`]: () => reply(200, { chat: { ...header, archived } }),
      [`POST /api/chats/${header.id}/archive`]: () => {
        archived = true
        return reply(204)
      },
      [`POST /api/chats/${header.id}/unarchive`]: () => {
        archived = false
        return reply(204)
      },
    })
    const more = () => user.click(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' }))
    await more()
    await user.click(screen.getByRole('button', { name: 'В архив' }))
    expect(await toast('Переписка в архиве')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/chats/${header.id}/archive`)).toBe(true)
    await waitFor(async () => {
      await more()
      expect(await screen.findByRole('button', { name: 'Вернуть' })).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: 'Вернуть' }))
    expect(await toast('Переписка возвращена')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/chats/${header.id}/unarchive`)).toBe(true)
  })

  it('tells when the server refused', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    await open(header, [textMessage(1)], { [`POST /api/chats/${header.id}/archive`]: apiError(500, 'internal', 'Сбой на сервере') })
    await user.click(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' }))
    await user.click(screen.getByRole('button', { name: 'В архив' }))
    expect(await toast('Не удалось изменить архив')).toBeInTheDocument()
  })
})

describe('complaint and block', () => {
  const header = sciHeader()
  const BLOCK = `POST /api/chats/${header.id}/block`
  const COMPLAINT = `POST /api/chats/${header.id}/complaints`
  const openMore = async () => {
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' }))
    return user
  }

  it('is absent for the organization side', async () => {
    await open(orgHeader())
    expect(screen.queryByRole('button', { name: 'Ещё: другие действия с перепиской' })).not.toBeInTheDocument()
  })

  it('offers only the actions the viewer may use', async () => {
    await open({ ...header, viewer: { ...header.viewer, can_block: false } })
    await openMore()
    expect(screen.getByRole('button', { name: 'Пожаловаться' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Заблокировать организацию' })).not.toBeInTheDocument()
  })

  it('offers only the block when the complaint is not allowed', async () => {
    await open({ ...header, viewer: { ...header.viewer, can_complain: false } })
    await openMore()
    expect(screen.queryByRole('button', { name: 'Пожаловаться' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Заблокировать организацию' })).toBeInTheDocument()
  })

  it('closes the menu with Escape and with an outside click', async () => {
    await open(header)
    const user = await openMore()
    const more = screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' })
    expect(more).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('button', { name: 'Пожаловаться' })).not.toBeInTheDocument()
    expect(more).toHaveFocus()
    await user.click(more)
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('button', { name: 'Пожаловаться' })).not.toBeInTheDocument()
  })

  it('checks the complaint on the page, sends it and thanks', async () => {
    const { calls } = await open(header, [textMessage(1)], { [COMPLAINT]: reply(201, { complaint: { id: '8', created_at: '2026-10-14T11:05:00Z' } }) })
    const user = await openMore()
    await user.click(screen.getByRole('button', { name: 'Пожаловаться' }))
    const dialog = await screen.findByRole('dialog', { name: 'Пожаловаться на организацию' })
    // Без причины отправить нельзя
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await within(dialog).findByText('Выберите причину', { selector: '.field-message' })).toBeInTheDocument()
    expect(within(dialog).getByLabelText(/Причина/)).toHaveFocus()
    expect(calls.some((c) => c.path.endsWith('/complaints'))).toBe(false)
    // «Другое» требует пояснения
    await user.selectOptions(within(dialog).getByLabelText(/Причина/), 'other')
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await within(dialog).findByText('Опишите, что произошло', { selector: '.field-message' })).toBeInTheDocument()
    await user.selectOptions(within(dialog).getByLabelText(/Причина/), 'spam')
    await user.type(within(dialog).getByLabelText(/Что случилось/), 'Пишут про платные курсы')
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await toast('Жалоба отправлена')).toBeInTheDocument()
    expect(calls.find((c) => c.path.endsWith('/complaints'))?.body).toEqual({ reason: 'spam', note: 'Пишут про платные курсы' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    // Фокус вернулся на «Ещё»
    expect(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' })).toHaveFocus()
  })

  it('shows field errors of the server and a general refusal', async () => {
    let answer = apiError(422, 'validation_failed', 'Проверьте поля', { fields: { note: 'Пояснение не длиннее 2000 знаков' } })
    await open(header, [textMessage(1)], { [COMPLAINT]: () => answer })
    const user = await openMore()
    await user.click(screen.getByRole('button', { name: 'Пожаловаться' }))
    const dialog = await screen.findByRole('dialog', { name: 'Пожаловаться на организацию' })
    await user.selectOptions(within(dialog).getByLabelText(/Причина/), 'abuse')
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await within(dialog).findByText('Пояснение не длиннее 2000 знаков')).toBeInTheDocument()
    answer = apiError(429, 'rate_limited', 'Слишком часто')
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await within(dialog).findByText('Не удалось отправить жалобу')).toBeInTheDocument()
    // Отмена очищает форму и ошибку
    await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' }))
    await user.click(screen.getByRole('button', { name: 'Пожаловаться' }))
    const again = await screen.findByRole('dialog', { name: 'Пожаловаться на организацию' })
    expect(within(again).getByLabelText(/Причина/)).toHaveValue('')
    expect(within(again).queryByText('Не удалось отправить жалобу')).not.toBeInTheDocument()
  })

  it('rejects a too long note before sending', async () => {
    const { calls } = await open(header)
    const user = await openMore()
    await user.click(screen.getByRole('button', { name: 'Пожаловаться' }))
    const dialog = await screen.findByRole('dialog', { name: 'Пожаловаться на организацию' })
    await user.selectOptions(within(dialog).getByLabelText(/Причина/), 'other')
    fireEvent.change(within(dialog).getByLabelText(/Что случилось/), { target: { value: 'ы'.repeat(2050) } })
    expect(within(dialog).getByText('2050 из 2000')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Отправить жалобу' }))
    expect(await within(dialog).findByText('Сократите сообщение на 50 зн.')).toBeInTheDocument()
    expect(calls.some((c) => c.path.endsWith('/complaints'))).toBe(false)
  })

  it('explains the consequences, blocks and refreshes the header', async () => {
    let closed = false
    const { calls } = await open(header, [textMessage(1)], {
      [`GET /api/chats/${header.id}`]: () => reply(200, { chat: closed ? { ...header, can_write: false, closed_reason: 'blocked' } : header }),
      [BLOCK]: () => {
        closed = true
        return reply(201, { block: { org_id: 'o1', org_name: header.vacancy.org_name, org_slug: header.vacancy.org_slug, created_at: '2026-10-14T11:00:00Z' } })
      },
    })
    const user = await openMore()
    await user.click(screen.getByRole('button', { name: 'Заблокировать организацию' }))
    const dialog = await screen.findByRole('dialog', { name: 'Заблокировать организацию?' })
    expect(within(dialog).getByText(/«Сибирский институт квантовых материалов» больше не сможет писать вам/)).toBeInTheDocument()
    expect(within(dialog).getByText(/Снять блокировку можно в настройках аккаунта в любой момент/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Заблокировать' }))
    expect(await toast('Организация заблокирована')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/chats/${header.id}/block`)).toBe(true)
    expect(await screen.findByText(/Вы заблокировали организацию, поэтому переписка только для чтения/)).toBeInTheDocument()
  })

  it('shows the refusal of the server in the window and lets the person cancel', async () => {
    await open(header, [textMessage(1)], { [BLOCK]: apiError(409, 'already_blocked', 'Эта организация уже заблокирована') })
    const user = await openMore()
    await user.click(screen.getByRole('button', { name: 'Заблокировать организацию' }))
    const dialog = await screen.findByRole('dialog', { name: 'Заблокировать организацию?' })
    await user.click(within(dialog).getByRole('button', { name: 'Заблокировать' }))
    expect(await within(dialog).findByText('Эта организация уже заблокирована')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    // Повторное открытие начинается без прежней ошибки
    await user.click(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' }))
    await user.click(screen.getByRole('button', { name: 'Заблокировать организацию' }))
    expect(within(await screen.findByRole('dialog', { name: 'Заблокировать организацию?' })).queryByText('Эта организация уже заблокирована')).not.toBeInTheDocument()
  })
})

describe('archive in the list', () => {
  const employer = () => window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')

  it('archives a row from the list without opening it', async () => {
    const user = userEvent.setup()
    const { calls } = stubApi({
      ...signedInAs(ann),
      'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
      'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
      'GET /api/chats?*': reply(200, chatLists.scientistAll()),
      'POST /api/chats/c0000000-0000-4000-8000-000000000001/archive': reply(204),
    })
    const { router } = renderApp('/chats')
    const button = await screen.findByRole('button', { name: 'Убрать в архив: Сибирский институт квантовых материалов' })
    await user.click(button)
    expect(await toast('Переписка в архиве')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path.endsWith('-000000000001/archive'))).toBe(true)
    expect(router.state.location.pathname).toBe('/chats')
  })

  it('restores a row in the archive tab', async () => {
    const user = userEvent.setup()
    employer()
    const archive = chatLists.orgArchive()
    const first = archive.items[0]
    const { calls } = stubApi({
      ...signedInAs(ann),
      'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
      'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
      'GET /api/chats/vacancies': reply(200, { items: [] }),
      'GET /api/chats?*': reply(200, archive),
      [`POST /api/chats/${first.id}/unarchive`]: reply(204),
    })
    renderApp('/chats?box=archive')
    const button = await screen.findByRole('button', { name: /^Вернуть из архива: / })
    await user.click(button)
    expect(await toast('Переписка возвращена')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/chats/${first.id}/unarchive`)).toBe(true)
  })
})

describe('resilience of the window', () => {
  it('shows that access is lost when the server stops knowing the conversation after a failed send', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    let gone = false
    await open(header, [textMessage(1)], {
      [`GET /api/chats/${header.id}`]: () => (gone ? apiError(404, 'not_found', 'Такой переписки нет') : reply(200, { chat: header })),
      [`POST /api/chats/${header.id}/messages`]: () => {
        gone = true
        return apiError(404, 'not_found', 'Такой переписки нет')
      },
    })
    await user.type(box(), 'Текст')
    await user.click(screen.getByRole('button', { name: 'Отправить' }))
    expect(await screen.findByText('Доступ к этой переписке пропал')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Все переписки' })).toBeInTheDocument()
  })

  it('keeps the window when only the network failed on re-reading the header', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    let down = false
    await open(header, [textMessage(1)], {
      [`GET /api/chats/${header.id}`]: () => (down ? apiError(500, 'internal', 'Сбой') : reply(200, { chat: header })),
      [`POST /api/chats/${header.id}/messages`]: () => {
        down = true
        return apiError(500, 'internal', 'Сбой')
      },
    })
    await user.type(box(), 'Текст')
    await user.click(screen.getByRole('button', { name: 'Отправить' }))
    expect(await screen.findByText('Не отправлено')).toBeInTheDocument()
    expect(screen.queryByText('Доступ к этой переписке пропал')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Повторить' })).toBeInTheDocument()
  })

  it('takes away the retry when the conversation was closed while sending', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    let closed = false
    await open(header, [textMessage(1)], {
      [`GET /api/chats/${header.id}`]: () => reply(200, { chat: closed ? { ...header, can_write: false, closed_reason: 'rejected' } : header }),
      [`POST /api/chats/${header.id}/messages`]: () => {
        closed = true
        return apiError(409, 'chat_closed', 'Переписка закрыта')
      },
    })
    await user.type(box(), 'Не успел отправить')
    await user.click(screen.getByRole('button', { name: 'Отправить' }))
    expect(await screen.findByText('Не отправлено')).toBeInTheDocument()
    expect(await screen.findByText(/принято решение «Отказ»/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Повторить' })).not.toBeInTheDocument()
    // Текст остаётся в ленте, его можно выделить, а убрать его решает человек
    expect(screen.getByText('Не успел отправить')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Убрать' })).toBeInTheDocument()
  })

  it('re-reads the header when an edit is refused because the conversation closed', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    const { calls } = await open(header, [mineMessage(1, { text: 'Старый текст' })], {
      [`PATCH /api/chats/${header.id}/messages/1`]: apiError(409, 'chat_closed', 'Переписка закрыта'),
    })
    const headerCalls = () => calls.filter((c) => c.method === 'GET' && c.path === `/api/chats/${header.id}`).length
    const before = headerCalls()
    await user.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await user.click(screen.getByRole('button', { name: 'Изменить' }))
    const edit = screen.getByRole('textbox', { name: 'Текст сообщения' })
    await user.clear(edit)
    await user.type(edit, 'Новый текст')
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(await screen.findByText(/Не удалось сохранить сообщение: Переписка закрыта/)).toBeInTheDocument()
    await waitFor(() => expect(headerCalls()).toBeGreaterThan(before))
  })

  it('re-reads the header when a deletion finds the conversation gone', async () => {
    const user = userEvent.setup()
    const header = sciHeader()
    const { calls } = await open(header, [mineMessage(1, { text: 'Моё' })], {
      [`DELETE /api/chats/${header.id}/messages/1`]: apiError(404, 'not_found', 'Нет'),
    })
    const headerCalls = () => calls.filter((c) => c.method === 'GET' && c.path === `/api/chats/${header.id}`).length
    const before = headerCalls()
    await user.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await user.click(screen.getByRole('button', { name: 'Удалить' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить' }))
    expect(await toast('Не удалось удалить сообщение')).toBeInTheDocument()
    await waitFor(() => expect(headerCalls()).toBeGreaterThan(before))
  })

  it('re-reads the header when marking as read finds the conversation gone', async () => {
    const header = sciHeader()
    // Прочитано до пятого, поэтому шестое сообщение уходит на отметку
    const { calls } = await open(header, [textMessage(6), textMessage(7)], {
      [`POST /api/chats/${header.id}/read`]: apiError(404, 'not_found', 'Нет'),
    })
    await waitFor(() => expect(calls.filter((c) => c.method === 'GET' && c.path === `/api/chats/${header.id}`).length).toBeGreaterThan(1))
  })

  it('reads several incoming messages aloud once', async () => {
    await open(sciHeader(), [textMessage(1)])
    act(() => {
      for (const seq of [2, 3, 4]) chatHub.emit({ type: 'message.created', chat_id: sciHeader().id, side: 'scientist', message: textMessage(seq, { text: `Новое ${seq}` }) })
    })
    expect(await screen.findByText('Новых сообщений: 3')).toBeInTheDocument()
  })

  it('moves focus to the conversation title when it opens', async () => {
    await open(sciHeader())
    const title = screen.getByRole('heading', { level: 2, name: 'Сибирский институт квантовых материалов' })
    expect(title).toHaveFocus()
    expect(title).toHaveAttribute('tabindex', '-1')
  })

  it('shows the conversation and the list of messages from the contract on a fresh open', async () => {
    await open(sciHeader(), chatMessages.scientistHistory().items)
    expect(screen.getByRole('button', { name: 'Ещё: другие действия с перепиской' })).toBeInTheDocument()
  })
})

import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Call, type Route } from '../../test/api'
import { chatHeaders, chatLists, chatMessages, mineMessage, systemMessage, textMessage, unreadCounts } from '../../test/chat'
import { renderApp } from '../../test/render'
import { ROLE_STORAGE_KEY } from '../shell/role-context'
import type { ChatHeader, Message, MessagePage } from './api'
import { chatHub } from './hub'

// Окно переписки (срез 35): лента, системные строки, поле ввода, отправка, правка, удаление, живые события.

const ID = chatHeaders.scientistInvited().id
const HEADER = `GET /api/chats/${ID}`
const MESSAGES = `GET /api/chats/${ID}/messages*`
const SEND = `POST /api/chats/${ID}/messages`
const READ = `POST /api/chats/${ID}/read`
const CLIENT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

const page = (items: Message[], more = false): MessagePage => ({ items, has_more: more })

function windowApi(items: Message[], header: ChatHeader = chatHeaders.scientistInvited(), extra: Record<string, Route> = {}, more = false) {
  return stubApi({
    ...signedInAs(ann),
    'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
    'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
    'GET /api/chats?*': reply(200, chatLists.scientistAll()),
    [`GET /api/chats/${header.id}`]: reply(200, { chat: header }),
    [`GET /api/chats/${header.id}/messages*`]: reply(200, page(items, more)),
    [`POST /api/chats/${header.id}/read`]: reply(204),
    ...extra,
  })
}

const sends = (calls: Call[]) => calls.filter((c) => c.method === 'POST' && c.path === `/api/chats/${ID}/messages`)
const feed = () => document.querySelector('.chat-feed') as HTMLElement

async function open(items: Message[], header?: ChatHeader, extra?: Record<string, Route>, more = false) {
  const api = windowApi(items, header, extra, more)
  const view = renderApp(`/chats/${header?.id ?? ID}`)
  await screen.findByRole('region', { name: 'Сообщения переписки' })
  return { ...api, ...view }
}

beforeEach(() => {
  window.localStorage.clear()
  window.sessionStorage.clear()
})
afterEach(() => {
  vi.useRealTimers()
  window.localStorage.clear()
  window.sessionStorage.clear()
})

describe('feed', () => {
  it('shows text, ticks, day separators, deleted and edited messages from the contract', async () => {
    await open(chatMessages.scientistHistory().items)
    const log = within(feed())
    expect(log.getByText(/Ваш опыт с плёнками нам очень подходит/)).toBeInTheDocument()
    // свои: удалённое и со ссылкой
    expect(log.getByText('Сообщение удалено')).toBeInTheDocument()
    const link = log.getByRole('link', { name: 'https://doi.org/10.5555/12345678' })
    expect(link).toHaveAttribute('href', 'https://doi.org/10.5555/12345678')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer nofollow ugc')
    expect(log.getAllByText(/^Организация просмотрела отклик$|^Отклик просмотрен$/).length).toBeGreaterThanOrEqual(0)
    // подпись автора и название организации рядом
    expect(log.getAllByText('Елена Орлова').length).toBeGreaterThan(0)
    // разделитель дня
    expect(document.querySelectorAll('.chat-day').length).toBeGreaterThan(0)
  })

  it('draws every system line of the contract in the organisation view', async () => {
    const header = chatHeaders.orgApplication()
    await open(chatMessages.orgSystemLines().items, { ...header, can_write: true })
    const log = within(feed())
    for (const title of [
      'Приглашение на вакансию отправлено',
      'Соискатель ответил «Интересно»',
      'Соискатель отправил отклик',
      'Соискатель предложил другое время',
      'Время собеседования согласовано',
      'Соискатель оставил контакты',
      'Решение по отклику',
      'Соискатель отозвал отклик',
      'Соискатель ответил «Не сейчас»',
      'Приглашение на вакансию отозвано',
      'Соискатель ограничил переписку с организацией',
      'Переписка снова открыта',
    ]) {
      expect(log.getAllByText(title).length, title).toBeGreaterThan(0)
    }
    expect(log.getByText('Отказ')).toBeInTheDocument()
    expect(log.getByText('Принят')).toBeInTheDocument()
    expect(log.getAllByText(/Приглашение: /).length).toBeGreaterThanOrEqual(3)
    // записка к отказу и ответ на просьбу о контактах
    expect(log.getByText(/выбрали кандидата с опытом работы на синхротроне/)).toBeInTheDocument()
    expect(log.getAllByText('Как связаться: +7 921 555-77-88').length).toBeGreaterThan(0)
  })

  it('shows the cover letter collapsed when it is long and expands it', async () => {
    const letter = 'Пишу вам по поводу вакансии. '.repeat(30)
    await open([systemMessage(1, 'application.sent', { application_id: 'a', cover_letter: letter })])
    const text = document.querySelector('.chat-event-quote-text') as HTMLElement
    expect(text).toHaveAttribute('data-clamped', 'true')
    const more = screen.getByRole('button', { name: 'Показать полностью' })
    await userEvent.click(more)
    expect(text).not.toHaveAttribute('data-clamped')
    expect(screen.getByRole('button', { name: 'Свернуть' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows unknown events and bare lines without breaking', async () => {
    await open([systemMessage(1, 'something.new'), systemMessage(2, 'invitation.created', {}), systemMessage(3, 'offer.created', {})])
    expect(screen.getAllByText('Событие по отклику')).toHaveLength(3)
  })

  it('says the conversation is empty and invites to write first', async () => {
    await open([])
    expect(screen.getByText('Сообщений пока нет. Напишите первым.')).toBeInTheDocument()
  })

  it('shows a line before the first unread message', async () => {
    const header = { ...chatHeaders.scientistInvited(), read: { mine: 1, theirs: 1 } }
    await open([textMessage(1), textMessage(2), textMessage(3)], header)
    expect(screen.getByText('Новые сообщения')).toBeInTheDocument()
  })

  it('marks the shown messages as read once and does not ask again for the same place', async () => {
    const { calls } = await open([textMessage(1), textMessage(2), textMessage(3)], { ...chatHeaders.scientistInvited(), read: { mine: 1, theirs: 0 } })
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST' && c.path === `/api/chats/${ID}/read`)).toHaveLength(1))
    expect(calls.find((c) => c.path === `/api/chats/${ID}/read`)?.body).toEqual({ up_to: 3 })
  })

  it('does not mark anything read when everything is already read', async () => {
    const { calls } = await open([textMessage(1), textMessage(2)], { ...chatHeaders.scientistInvited(), read: { mine: 2, theirs: 0 } })
    await new Promise((r) => setTimeout(r, 400))
    expect(calls.filter((c) => c.path === `/api/chats/${ID}/read`)).toHaveLength(0)
  })

  it('tries again later when marking read failed', async () => {
    const { calls } = await open([textMessage(1)], { ...chatHeaders.scientistInvited(), read: { mine: 0, theirs: 0 } }, { [READ]: apiError(500, 'internal', 'Ошибка') })
    await waitFor(() => expect(calls.filter((c) => c.path === `/api/chats/${ID}/read`).length).toBeGreaterThan(0))
    fireEvent.scroll(feed())
    await waitFor(() => expect(calls.filter((c) => c.path === `/api/chats/${ID}/read`).length).toBeGreaterThan(1))
  })

  it('shows the error with a retry when the messages do not load', async () => {
    let fail = true
    stubApi({
      ...signedInAs(ann),
      'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
      'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
      'GET /api/chats?*': reply(200, chatLists.scientistAll()),
      [HEADER]: reply(200, { chat: chatHeaders.scientistInvited() }),
      [MESSAGES]: () => (fail ? apiError(500, 'internal', 'Сервер споткнулся') : reply(200, page([textMessage(1)]))),
    })
    renderApp(`/chats/${ID}`)
    expect(await screen.findByText('Не удалось загрузить сообщения')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByText('Сообщение 1')).toBeInTheDocument()
  })
})

describe('older messages', () => {
  const older = (calls: Call[]) => calls.filter((c) => c.method === 'GET' && c.path.includes('before='))

  it('loads the previous page when scrolled to the top and keeps the place', async () => {
    let first = true
    const { calls } = await open(
      [textMessage(31), textMessage(32)],
      undefined,
      {
        [MESSAGES]: (call) => {
          if (call.path.includes('before=31')) return reply(200, page([textMessage(29), textMessage(30)], false))
          const r = reply(200, page([textMessage(31), textMessage(32)], first))
          first = false
          return r
        },
      },
      true,
    )
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => (el.querySelectorAll('li[data-seq]').length > 2 ? 900 : 500) })
    Object.defineProperty(el, 'clientHeight', { configurable: true, value: 300 })
    el.scrollTop = 40
    fireEvent.scroll(el)
    await waitFor(() => expect(older(calls)).toHaveLength(1))
    expect(older(calls)[0].path).toContain('before=31')
    expect(await screen.findByText('Сообщение 29')).toBeInTheDocument()
    // место сохраняется: прежняя высота + прирост
    await waitFor(() => expect(el.scrollTop).toBe(440))
  })

  it('shows a retry when the page does not load and loads it on retry', async () => {
    let fail = true
    const { calls } = await open(
      [textMessage(31)],
      undefined,
      {
        [MESSAGES]: (call) => {
          if (!call.path.includes('before=')) return reply(200, page([textMessage(31)], true))
          return fail ? apiError(500, 'internal', 'нет') : reply(200, page([textMessage(30)], false))
        },
      },
      true,
    )
    fireEvent.scroll(feed())
    expect(await screen.findByText('Не удалось загрузить прежние сообщения.')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить ещё раз' }))
    expect(await screen.findByText('Сообщение 30')).toBeInTheDocument()
    expect(older(calls).length).toBe(2)
  })

  it('does not ask again while a page is loading and not when nothing is older', async () => {
    const { calls } = await open([textMessage(1)])
    fireEvent.scroll(feed())
    fireEvent.scroll(feed())
    await new Promise((r) => setTimeout(r, 100))
    expect(older(calls)).toHaveLength(0)
  })
})

describe('sending', () => {
  const type = async (text: string) => {
    const box = screen.getByRole('textbox', { name: 'Сообщение' })
    await userEvent.type(box, text)
    return box
  }

  it('sends on Enter with a client id, shows it as sending and then as delivered', async () => {
    let resolve: (r: ReturnType<typeof reply>) => void = () => {}
    const gate = new Promise<ReturnType<typeof reply>>((r) => (resolve = r))
    const { calls } = await open([textMessage(1)], undefined, { [SEND]: () => gate as never })
    const box = await type('Добрый день!{Enter}')
    expect(box).toHaveValue('')
    expect(await screen.findByText('Отправляется')).toBeInTheDocument()
    expect(screen.getByText('Добрый день!')).toBeInTheDocument()
    const body = sends(calls)[0].body as { text: string; client_id: string }
    expect(body.text).toBe('Добрый день!')
    expect(body.client_id).toMatch(CLIENT_ID)
    await act(async () => {
      resolve(reply(201, { message: mineMessage(6, { text: 'Добрый день!', client_id: body.client_id }) }))
      await gate
    })
    await waitFor(() => expect(screen.queryByText('Отправляется')).not.toBeInTheDocument())
    expect(screen.getAllByText('Добрый день!')).toHaveLength(1)
    expect(screen.getByText('Доставлено')).toBeInTheDocument()
  })

  it('does not duplicate when the live event for the same message comes before the answer', async () => {
    let resolve: (r: ReturnType<typeof reply>) => void = () => {}
    const gate = new Promise<ReturnType<typeof reply>>((r) => (resolve = r))
    const { calls } = await open([textMessage(1)], undefined, { [SEND]: () => gate as never })
    await type('Привет{Enter}')
    await screen.findByText('Отправляется')
    const clientId = (sends(calls)[0].body as { client_id: string }).client_id
    const mine = mineMessage(2, { text: 'Привет', client_id: clientId })
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: mine }))
    await waitFor(() => expect(screen.queryByText('Отправляется')).not.toBeInTheDocument())
    await act(async () => {
      resolve(reply(201, { message: mine }))
      await gate
    })
    expect(screen.getAllByText('Привет')).toHaveLength(1)
  })

  it('shows a failed send with the reason, retries with the same client id and can drop it', async () => {
    let ok = false
    const { calls } = await open([textMessage(1)], undefined, {
      [SEND]: (call) => (ok ? reply(201, { message: mineMessage(2, { text: 'Привет', client_id: (call.body as { client_id: string }).client_id }) }) : apiError(409, 'chat_closed', 'Переписка закрыта')),
    })
    await type('Привет{Enter}')
    expect(await screen.findByText('Не отправлено')).toBeInTheDocument()
    expect(screen.getByText('Переписка закрыта')).toBeInTheDocument()
    const first = (sends(calls)[0].body as { client_id: string }).client_id
    ok = true
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    await waitFor(() => expect(screen.queryByText('Не отправлено')).not.toBeInTheDocument())
    expect((sends(calls)[1].body as { client_id: string }).client_id).toBe(first)
    // и «Убрать»
    ok = false
    await type('Второе{Enter}')
    await screen.findByText('Не отправлено')
    await userEvent.click(screen.getByRole('button', { name: 'Убрать' }))
    expect(screen.queryByText('Второе')).not.toBeInTheDocument()
  })

  it('shows the reason when the server stops spam limits (T-99): wait for a reply, repeated number, too many a day', async () => {
    const answer = { current: apiError(409, 'awaiting_reply', 'Вы написали несколько сообщений подряд. Дождитесь ответа собеседника') }
    await open([textMessage(1)], undefined, { [SEND]: () => answer.current })
    await type('Привет{Enter}')
    expect(await screen.findByText('Не отправлено')).toBeInTheDocument()
    expect(screen.getByText('Вы написали несколько сообщений подряд. Дождитесь ответа собеседника')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Убрать' }))
    answer.current = apiError(409, 'client_id_reused', 'Этот номер отправки уже использован для другого текста. Повторите отправку')
    await type('Второе{Enter}')
    expect(await screen.findByText('Этот номер отправки уже использован для другого текста. Повторите отправку')).toBeInTheDocument()
  })

  it('Shift+Enter makes a new line and an empty or blank message cannot be sent', async () => {
    const { calls } = await open([textMessage(1)])
    const send = screen.getByRole('button', { name: 'Отправить' })
    expect(send).toBeDisabled()
    const box = await type('  ')
    expect(send).toBeDisabled()
    await userEvent.type(box, 'a{Shift>}{Enter}{/Shift}b')
    expect(box).toHaveValue('  a\nb')
    expect(sends(calls)).toHaveLength(0)
    await userEvent.type(box, '{Control>}{Enter}{/Control}')
    await waitFor(() => expect(sends(calls)).toHaveLength(1))
    expect((sends(calls)[0].body as { text: string }).text).toBe('a\nb')
  })

  it('lets Enter make a new line on a touch screen and sends with the button', async () => {
    const original = window.matchMedia
    window.matchMedia = vi.fn().mockReturnValue({ matches: true }) as never
    try {
      const { calls } = await open([textMessage(1)])
      const box = await type('Строка{Enter}вторая')
      expect(box).toHaveValue('Строка\nвторая')
      expect(sends(calls)).toHaveLength(0)
      expect(screen.getByText('Отправка кнопкой, Enter переносит строку.')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Отправить' }))
      await waitFor(() => expect(sends(calls)).toHaveLength(1))
    } finally {
      window.matchMedia = original
    }
  })

  it('does not send while an input method is composing', async () => {
    const { calls } = await open([textMessage(1)])
    const box = screen.getByRole('textbox', { name: 'Сообщение' })
    fireEvent.change(box, { target: { value: 'тест' } })
    fireEvent.keyDown(box, { key: 'Enter', isComposing: true })
    expect(sends(calls)).toHaveLength(0)
  })

  it('counts close to the limit and blocks sending above it', async () => {
    await open([textMessage(1)])
    const box = screen.getByRole('textbox', { name: 'Сообщение' })
    fireEvent.change(box, { target: { value: 'а'.repeat(3600) } })
    expect(screen.getByText('3600 из 4000')).toBeInTheDocument()
    fireEvent.change(box, { target: { value: 'а'.repeat(4005) } })
    expect(screen.getByText('Сократите сообщение на 5 зн.')).toBeInTheDocument()
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('button', { name: 'Отправить' })).toBeDisabled()
  })

  it('saves the draft and restores it, clears it after sending', async () => {
    await open([textMessage(1)])
    await type('Недописанное')
    await waitFor(() => expect(window.sessionStorage.length).toBe(1))
    expect(Object.values(window.sessionStorage)[0]).toBe('Недописанное')
    // вернулись в переписку: черновик на месте
    document.body.innerHTML = ''
    await open([textMessage(1)])
    expect(screen.getByRole('textbox', { name: 'Сообщение' })).toHaveValue('Недописанное')
    await userEvent.type(screen.getByRole('textbox', { name: 'Сообщение' }), '{Enter}')
    await waitFor(() => expect(window.sessionStorage.length).toBe(0))
  })

  it('works without browser storage', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('нет доступа')
    })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('нет доступа')
    })
    try {
      await open([textMessage(1)])
      await type('Работает')
      expect(screen.getByRole('textbox', { name: 'Сообщение' })).toHaveValue('Работает')
    } finally {
      getItem.mockRestore()
      setItem.mockRestore()
    }
  })

  it('tells the other side that I am typing, not more often than every 3 seconds', async () => {
    const { calls } = await open([textMessage(1)], undefined, { [`POST /api/chats/${ID}/typing`]: reply(204) })
    await type('abc')
    expect(calls.filter((c) => c.path === `/api/chats/${ID}/typing`)).toHaveLength(1)
  })

  it('ignores a failing typing signal', async () => {
    const { calls } = await open([textMessage(1)], undefined, { [`POST /api/chats/${ID}/typing`]: apiError(409, 'chat_closed', 'закрыта') })
    await type('a')
    expect(calls.filter((c) => c.path === `/api/chats/${ID}/typing`)).toHaveLength(1)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

describe('closed conversation', () => {
  it('replaces the field with the reason and keeps the history readable', async () => {
    await open(chatMessages.scientistHistory().items, chatHeaders.scientistClosed(), {})
    expect(screen.getByText('Переписка закрыта: по отклику принято решение «Отказ».')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Сообщение' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Подтвердить время' })).not.toBeInTheDocument()
  })

  it('says it generally when no reason is given', async () => {
    await open([textMessage(1)], { ...chatHeaders.scientistClosed(), closed_reason: null })
    expect(screen.getByText('Писать в эту переписку нельзя.')).toBeInTheDocument()
  })

  it('says the closed conversation has no messages', async () => {
    await open([], chatHeaders.scientistClosed())
    expect(screen.getByText('Сообщений в этой переписке нет.')).toBeInTheDocument()
  })
})

describe('edit and delete', () => {
  const own = (over: Partial<Message> = {}) => mineMessage(7, { text: 'Исходный текст', ...over })

  it('edits my message in place and marks it edited', async () => {
    const { calls } = await open([textMessage(1), own()], undefined, {
      [`PATCH /api/chats/${ID}/messages/7`]: (call) => reply(200, { message: own({ text: (call.body as { text: string }).text, edited_at: '2026-10-14T11:00:00Z' }) }),
    })
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }))
    const box = screen.getByRole('textbox', { name: 'Текст сообщения' })
    expect(box).toHaveValue('Исходный текст')
    await userEvent.clear(box)
    await userEvent.type(box, 'Новый текст{Enter}')
    expect(await screen.findByText('Новый текст')).toBeInTheDocument()
    expect(screen.getByText('изменено')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ text: 'Новый текст' })
  })

  it('cancels with Escape and does not send an unchanged text', async () => {
    const { calls } = await open([own()])
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Текст сообщения' }), '{Escape}')
    expect(screen.getByText('Исходный текст')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }))
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(calls.filter((c) => c.method === 'PATCH')).toHaveLength(0)
    expect(screen.queryByRole('textbox', { name: 'Текст сообщения' })).not.toBeInTheDocument()
  })

  it('does not let me save an empty or too long text and shows the server error', async () => {
    await open([own()], undefined, { [`PATCH /api/chats/${ID}/messages/7`]: apiError(409, 'edit_window_closed', 'Прошло больше суток') })
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }))
    const box = screen.getByRole('textbox', { name: 'Текст сообщения' })
    await userEvent.clear(box)
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
    fireEvent.change(box, { target: { value: 'а'.repeat(4001) } })
    expect(screen.getByText('Сократите сообщение на 1 зн.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeDisabled()
    fireEvent.change(box, { target: { value: 'Другой текст' } })
    await userEvent.type(box, '{Enter}')
    expect(await screen.findByText(/Не удалось сохранить сообщение: Прошло больше суток/)).toBeInTheDocument()
    // форма осталась открытой, повторить можно
    expect(screen.getByRole('textbox', { name: 'Текст сообщения' })).not.toBeDisabled()
  })

  it('asks before deleting and shows the deleted line', async () => {
    const { calls } = await open([own()], undefined, { [`DELETE /api/chats/${ID}/messages/7`]: reply(204) })
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Удалить' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Собеседник увидит, что сообщение удалено. Вернуть его нельзя.')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Удалить' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true))
    expect(await within(feed()).findByText('Сообщение удалено')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Действия с сообщением' })).not.toBeInTheDocument()
  })

  it('shows a toast when the deletion fails', async () => {
    await open([own()], undefined, { [`DELETE /api/chats/${ID}/messages/7`]: apiError(409, 'edit_window_closed', 'Прошло больше суток') })
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Удалить' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить' }))
    expect(await screen.findByText('Не удалось удалить сообщение')).toBeInTheDocument()
    expect(screen.getByText('Исходный текст')).toBeInTheDocument()
  })

  it('offers no actions after 24 hours, for others, for deleted and in a closed chat', async () => {
    await open([own({ editable_until: '2020-01-01T00:00:00Z' }), textMessage(8), own({ deleted: true, text: '', editable_until: null })])
    expect(screen.queryByRole('button', { name: 'Действия с сообщением' })).not.toBeInTheDocument()
  })

  it('offers no actions in a closed conversation', async () => {
    await open([own()], chatHeaders.scientistClosed())
    expect(screen.queryByRole('button', { name: 'Действия с сообщением' })).not.toBeInTheDocument()
  })
})

describe('live updates', () => {
  it('adds a new incoming message and reads it aloud politely', async () => {
    await open([textMessage(1)])
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(2, { text: 'Свежее сообщение' }) }))
    expect(await within(feed()).findByText('Свежее сообщение')).toBeInTheDocument()
    expect(await screen.findByText('Новое сообщение от Елена Орлова: Свежее сообщение')).toBeInTheDocument()
  })

  it('ignores events of other conversations and shows edits and deletions', async () => {
    await open([textMessage(1), textMessage(2)])
    act(() => chatHub.emit({ type: 'message.created', chat_id: 'other', side: 'scientist', message: textMessage(3, { text: 'Чужое' }) }))
    expect(screen.queryByText('Чужое')).not.toBeInTheDocument()
    act(() => chatHub.emit({ type: 'message.edited', chat_id: ID, side: 'scientist', message: textMessage(2, { text: 'Исправлено', edited_at: '2026-10-14T12:00:00Z' }) }))
    expect(await screen.findByText('Исправлено')).toBeInTheDocument()
    expect(screen.getByText('изменено')).toBeInTheDocument()
    act(() => chatHub.emit({ type: 'message.deleted', chat_id: ID, side: 'scientist', message: textMessage(2, { text: '', deleted: true }) }))
    expect(await within(feed()).findByText('Сообщение удалено')).toBeInTheDocument()
    // повтор того же события не даёт дубля
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(1) }))
    expect(screen.getAllByText('Сообщение 1')).toHaveLength(1)
  })

  it('does not announce my own message or a system line', async () => {
    await open([textMessage(1)])
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: mineMessage(2, { text: 'Моё' }) }))
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: systemMessage(3, 'application.viewed') }))
    expect(await screen.findByText('Моё')).toBeInTheDocument()
    expect(screen.queryByText(/Новое сообщение от/)).not.toBeInTheDocument()
  })

  it('shows who is typing and hides it when the message comes', async () => {
    await open([textMessage(1)])
    act(() => chatHub.emit({ type: 'chat.typing', chat_id: ID, side: 'scientist', author: { side: 'org', name: 'Елена Орлова', anonymous: false } }))
    expect(await screen.findByText('Елена Орлова печатает')).toBeInTheDocument()
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(2) }))
    await waitFor(() => expect(screen.queryByText('Елена Орлова печатает')).not.toBeInTheDocument())
    act(() => chatHub.emit({ type: 'chat.typing', chat_id: ID, side: 'org', author: { side: 'scientist', name: '', anonymous: true } }))
    expect(await screen.findByText('Анонимный соискатель печатает')).toBeInTheDocument()
    act(() => chatHub.emit({ type: 'chat.typing', chat_id: 'other', side: 'org', author: { side: 'scientist', name: 'Чужой', anonymous: false } }))
    expect(screen.queryByText('Чужой печатает')).not.toBeInTheDocument()
  })

  it('stops showing typing after a pause', async () => {
    await open([textMessage(1)])
    vi.useFakeTimers({ shouldAdvanceTime: true })
    act(() => chatHub.emit({ type: 'chat.typing', chat_id: ID, side: 'scientist', author: { side: 'org', name: 'Елена Орлова', anonymous: false } }))
    expect(screen.getByText('Елена Орлова печатает')).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5200)
    })
    expect(screen.queryByText('Елена Орлова печатает')).not.toBeInTheDocument()
  })

  it('catches up what was missed after the stream reconnected', async () => {
    let calledAfter = 0
    const { calls } = await open([textMessage(1), textMessage(2)], undefined, {
      [MESSAGES]: (call) => {
        if (call.path.includes('after=')) {
          calledAfter += 1
          return reply(200, page([textMessage(3, { text: 'Пропущенное' })], false))
        }
        return reply(200, page([textMessage(1), textMessage(2)]))
      },
    })
    act(() => chatHub.emit({ type: 'reconnected' }))
    expect(await screen.findByText('Пропущенное')).toBeInTheDocument()
    expect(calledAfter).toBe(1)
    expect(calls.some((c) => c.path.includes('after=2'))).toBe(true)
  })

  it('reads several pages while the server says there is more', async () => {
    let n = 0
    await open([textMessage(1)], undefined, {
      [MESSAGES]: (call) => {
        if (!call.path.includes('after=')) return reply(200, page([textMessage(1)]))
        n += 1
        return n === 1 ? reply(200, page([textMessage(2)], true)) : reply(200, page([textMessage(3)], false))
      },
    })
    act(() => chatHub.emit({ type: 'reconnected' }))
    expect(await screen.findByText('Сообщение 3')).toBeInTheDocument()
    expect(screen.getByText('Сообщение 2')).toBeInTheDocument()
  })

  it('applies events that arrive while the feed is loading', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    stubApi({
      ...signedInAs(ann),
      'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
      'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
      'GET /api/chats?*': reply(200, chatLists.scientistAll()),
      [HEADER]: reply(200, { chat: chatHeaders.scientistInvited() }),
      [MESSAGES]: async (call) => {
        await gate
        return call.path.includes('after=') ? reply(200, page([])) : reply(200, page([textMessage(1)]))
      },
    })
    renderApp(`/chats/${ID}`)
    await waitFor(() => expect(document.querySelector('.chat-window')).not.toBeNull())
    await screen.findByRole('textbox', { name: 'Сообщение' })
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(2, { text: 'Пока грузилось' }) }))
    act(() => chatHub.emit({ type: 'reconnected' }))
    release()
    expect(await screen.findByText('Пока грузилось')).toBeInTheDocument()
    expect(screen.getByText('Сообщение 1')).toBeInTheDocument()
  })

  it('shows a button to the newest when I scrolled up and a message arrived', async () => {
    await open([textMessage(1), textMessage(2)])
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, value: 300 })
    el.scrollTop = 100
    fireEvent.scroll(el)
    const button = await screen.findByRole('button', { name: 'Вниз' })
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(3) }))
    expect(await screen.findByRole('button', { name: 'Новых сообщений: 1' })).toBeInTheDocument()
    expect(button).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Новых сообщений: 1' }))
    el.scrollTop = 700
    fireEvent.scroll(el)
    await waitFor(() => expect(screen.queryByRole('button', { name: /Новых сообщений|Вниз/ })).not.toBeInTheDocument())
  })

  it('shows 99+ new messages on the button', async () => {
    await open([textMessage(1)])
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, value: 300 })
    el.scrollTop = 0
    fireEvent.scroll(el)
    await screen.findByRole('button', { name: 'Вниз' })
    for (let i = 2; i < 104; i += 1) act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(i) }))
    expect(await screen.findByText('99+')).toBeInTheDocument()
  })

  it('scrolls to the newest on its own when I am at the bottom', async () => {
    await open([textMessage(1)])
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 800 })
    act(() => chatHub.emit({ type: 'message.created', chat_id: ID, side: 'scientist', message: textMessage(2) }))
    await waitFor(() => expect(el.scrollTop).toBe(800))
  })

  it('goes to the newest with reduced motion without smooth scrolling', async () => {
    const original = window.matchMedia
    window.matchMedia = vi.fn().mockReturnValue({ matches: true }) as never
    const scrollTo = vi.fn()
    try {
      await open([textMessage(1)])
      const el = feed()
      el.scrollTo = scrollTo as never
      Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 1000 })
      Object.defineProperty(el, 'clientHeight', { configurable: true, value: 300 })
      el.scrollTop = 0
      fireEvent.scroll(el)
      await userEvent.click(await screen.findByRole('button', { name: 'Вниз' }))
      expect(scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: 'auto' })
    } finally {
      window.matchMedia = original
    }
  })
})

describe('answers from the conversation', () => {
  const invitation = (over: Record<string, unknown> = {}) => ({
    id: 'f0000000-0000-4000-8000-000000000001',
    kind: 'interview',
    status: 'pending',
    message: 'Приглашаем на собеседование.',
    starts_at: '2026-10-22T08:00:00Z',
    place_kind: 'online',
    place: 'https://telemost.yandex.ru/j/12345678901234',
    contact_name: '',
    contact_email: '',
    contact_phone: '',
    answer: null,
    created_at: '2026-10-14T09:40:00Z',
    can_answer: true,
    can_cancel: false,
    can_accept_proposal: false,
    ...over,
  })
  const created = (over: Record<string, unknown> = {}) => systemMessage(2, 'invitation.created', { application_id: 'b0000000-0000-4000-8000-000000000001', invitation: invitation(over) })
  const INV = 'f0000000-0000-4000-8000-000000000001'
  const answerPath = `POST /api/applications/b0000000-0000-4000-8000-000000000001/invitations/${INV}/answer`

  it('confirms the time from the conversation and the buttons go away', async () => {
    const calls: string[] = []
    const { fetch } = await open([textMessage(1), created()], undefined, {
      [answerPath]: (call) => {
        calls.push(JSON.stringify(call.body))
        return reply(204)
      },
      [`GET /api/chats/${ID}/messages?limit=30&after=2`]: reply(
        200,
        page([systemMessage(3, 'invitation.answered', { application_id: 'b0000000-0000-4000-8000-000000000001', invitation_id: INV, kind: 'interview', answer: 'confirmed', proposed_at: null, note: '', contact: '', time: '' })]),
      ),
    })
    expect(fetch).toBeTruthy()
    await userEvent.click(await screen.findByRole('button', { name: 'Подтвердить время' }))
    await waitFor(() => expect(calls).toEqual(['{"action":"confirm"}']))
    expect(await screen.findByText('Вы подтвердили время')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Подтвердить время' })).not.toBeInTheDocument())
    expect(screen.getByText('Подтверждено')).toBeInTheDocument()
  })

  it('answers with another time from the dialog of the applications page', async () => {
    const bodies: unknown[] = []
    await open([created()], undefined, {
      [answerPath]: (call) => {
        bodies.push(call.body)
        return reply(204)
      },
    })
    await userEvent.click(await screen.findByRole('button', { name: 'Предложить другое время' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText(/Дата/), { target: { value: '2026-10-23' } })
    fireEvent.change(within(dialog).getByLabelText(/Время/), { target: { value: '15:30' } })
    await userEvent.click(within(dialog).getByRole('button', { name: /Отправить|Предложить/ }))
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toMatchObject({ action: 'propose', proposed_at: '2026-10-23T15:30:00+03:00' })
  })

  it('shows the organisation its own buttons: accept the proposed time and cancel', async () => {
    const header = { ...chatHeaders.orgApplication(), id: 'c0000000-0000-4000-8000-000000000055' }
    const proposed = created({ status: 'proposed', answer: { proposed_at: '2026-10-23T09:00:00Z', note: 'Удобнее позже', contact: '', time: '', answered_at: '2026-10-15T09:00:00Z' }, can_answer: false, can_cancel: true, can_accept_proposal: true })
    const accept = vi.fn(() => reply(204))
    await open([proposed], header, { [`POST /api/applications/b0000000-0000-4000-8000-000000000001/invitations/${INV}/accept-proposal`]: accept })
    expect((await screen.findAllByText(/Предложено другое время/)).length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: 'Подтвердить время' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Принять предложенное время' }))
    await waitFor(() => expect(accept).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: 'Отменить приглашение' })).toBeInTheDocument()
  })

  it('answers an offer to a vacancy with «Интересно» through the same form as the offer page', async () => {
    const OFFER = 'd0000000-0000-4000-8000-000000000003'
    const bodies: unknown[] = []
    const header: ChatHeader = { ...chatHeaders.scientistInvited(), application_id: null, offer_id: OFFER, status: { source: 'offer', value: 'pending' }, needs_answer: true }
    await open([systemMessage(1, 'offer.created', { offer_id: OFFER, message: 'Посмотрите нашу вакансию', status: 'pending', can_answer: true })], header, {
      [`POST /api/offers/${OFFER}/answer`]: (call) => {
        bodies.push(call.body)
        return reply(204)
      },
      'GET /api/profile': reply(404, { error: { code: 'not_found', message: 'нет' } }),
    })
    expect(document.querySelector('.chat-event .tag')).toHaveTextContent('Ждёт вашего ответа')
    await userEvent.click(screen.getByRole('button', { name: 'Ответить на приглашение' }))
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByRole('textbox'), 'Спасибо, интересно')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Интересно' }))
    await waitFor(() => expect(bodies).toEqual([{ action: 'interested', note: 'Спасибо, интересно' }]))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows the organisation that the offer was sent and an answered offer without buttons', async () => {
    const OFFER = 'd0000000-0000-4000-8000-000000000003'
    const header: ChatHeader = { ...chatHeaders.orgAnonymousOffer(), offer_id: OFFER }
    await open(
      [
        systemMessage(1, 'offer.created', { offer_id: OFFER, message: 'Приглашаем', status: 'pending', can_answer: false }),
        systemMessage(2, 'offer.answered', { offer_id: OFFER, answer: 'interested', note: 'Интересно, вернусь с откликом' }),
      ],
      header,
    )
    expect(screen.getByText('Приглашение на вакансию отправлено')).toBeInTheDocument()
    expect(screen.getByText('Соискатель ответил «Интересно»')).toBeInTheDocument()
    expect(screen.getByText('Интересно, вернусь с откликом')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ответить на приглашение' })).not.toBeInTheDocument()
  })
})

describe('header and layout', () => {
  it('keeps the header, shows the author with the organisation and the anonymous label', async () => {
    const org = ROLE_STORAGE_KEY
    window.localStorage.setItem(org, 'employer')
    await open([textMessage(1, { author: { side: 'scientist', name: '', is_me: false, anonymous: true, former: false } })], chatHeaders.orgAnonymousOffer(), {})
    expect(screen.getAllByText('Анонимный соискатель').length).toBeGreaterThan(0)
  })

  it('names a colleague of my side and a former employee', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    const colleague = { side: 'org' as const, name: 'Иван Петров', is_me: false, anonymous: false, former: false }
    const former = { side: 'org' as const, name: '', is_me: false, anonymous: false, former: true }
    await open([textMessage(1, { author: colleague }), textMessage(2, { author: former })], chatHeaders.orgApplication())
    expect(screen.getByText('Иван Петров')).toBeInTheDocument()
    expect(screen.getByText('Сотрудник организации')).toBeInTheDocument()
  })
})

describe('browser behaviour', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
  })

  it('keeps the feed at the bottom when the field grows', async () => {
    let callback: () => void = () => {}
    class FakeObserver {
      constructor(cb: () => void) {
        callback = cb
      }
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal('ResizeObserver', FakeObserver)
    await open([textMessage(1)])
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 900 })
    callback()
    expect(el.scrollTop).toBe(900)
  })

  it('does not read while the tab is hidden and reads when it comes back', async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    const { calls } = await open([textMessage(1), textMessage(2)], { ...chatHeaders.scientistInvited(), read: { mine: 0, theirs: 0 } })
    await new Promise((r) => setTimeout(r, 400))
    expect(calls.filter((c) => c.path === `/api/chats/${ID}/read`)).toHaveLength(0)
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await waitFor(() => expect(calls.filter((c) => c.path === `/api/chats/${ID}/read`)).toHaveLength(1))
  })

  it('checks again on window focus', async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    const { calls } = await open([textMessage(1)], { ...chatHeaders.scientistInvited(), read: { mine: 0, theirs: 0 } })
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    act(() => {
      window.dispatchEvent(new Event('focus'))
    })
    await waitFor(() => expect(calls.some((c) => c.path === `/api/chats/${ID}/read`)).toBe(true))
  })

  it('hides the edit actions once the 24 hours run out while the page is open', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const soon = new Date(Date.now() + 30_000).toISOString()
    await open([mineMessage(7, { editable_until: soon })])
    expect(screen.getByRole('button', { name: 'Действия с сообщением' })).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(61_000)
    })
    expect(screen.queryByRole('button', { name: 'Действия с сообщением' })).not.toBeInTheDocument()
  })

  it('scrolls to the bottom by assignment when the element cannot scrollTo', async () => {
    await open([textMessage(1)])
    const el = feed()
    Object.defineProperty(el, 'scrollHeight', { configurable: true, value: 1000 })
    Object.defineProperty(el, 'clientHeight', { configurable: true, value: 300 })
    el.scrollTop = 0
    fireEvent.scroll(el)
    ;(el as unknown as { scrollTo: unknown }).scrollTo = undefined
    await userEvent.click(await screen.findByRole('button', { name: 'Вниз' }))
    expect(el.scrollTop).toBe(1000)
  })

  it('closes the delete question and the offer answer window without doing anything', async () => {
    const OFFER = 'd0000000-0000-4000-8000-000000000003'
    const header: ChatHeader = { ...chatHeaders.scientistInvited(), application_id: null, offer_id: OFFER, status: { source: 'offer', value: 'pending' }, needs_answer: true }
    const { calls } = await open(
      [mineMessage(1, { text: 'Своё' }), systemMessage(2, 'offer.created', { offer_id: OFFER, message: 'Приглашаем', status: 'pending', can_answer: true })],
      header,
      { 'GET /api/profile': reply(404, { error: { code: 'not_found', message: 'нет' } }) },
    )
    await userEvent.click(screen.getByRole('button', { name: 'Действия с сообщением' }))
    await userEvent.click(screen.getByRole('button', { name: 'Удалить' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: 'Ответить на приглашение' }))
    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(calls.filter((c) => c.method === 'DELETE' || c.path.includes('/answer'))).toHaveLength(0)
  })

  it('shows the older block without a button while the page loads', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => (release = r))
    await open([textMessage(31)], undefined, {
      [MESSAGES]: async (call) => {
        if (call.path.includes('before=')) {
          await gate
          return reply(200, page([textMessage(30)], false))
        }
        return reply(200, page([textMessage(31)], true))
      },
    })
    fireEvent.scroll(feed())
    expect(await screen.findByText('Загружаем прежние сообщения')).toBeInTheDocument()
    release()
    expect(await screen.findByText('Сообщение 30')).toBeInTheDocument()
  })

  it('shows the not found state for someone else’s conversation and the retry for a failing header', async () => {
    stubApi({
      ...signedInAs(ann),
      'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
      'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
      'GET /api/chats?*': reply(200, chatLists.scientistAll()),
      [HEADER]: apiError(404, 'not_found', 'нет'),
    })
    renderApp(`/chats/${ID}`)
    expect(await screen.findByText('Такой переписки нет')).toBeInTheDocument()
  })
})

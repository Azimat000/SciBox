import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Call, type Route } from '../../test/api'
import { chatHeaders, chatLists, chatVacancies, oneChat, unreadCounts } from '../../test/chat'
import { renderApp } from '../../test/render'
import { ROLE_STORAGE_KEY } from '../shell/role-context'
import { POLL_MS, type ChatList } from './api'

// Страница «Сообщения» (срез 34): список переписок по стороне режима, вкладки, поиск, отбор, «Показать ещё», состояния,
// шапка открытой переписки; значок в шапке сайта. Ответы сервера — примеры из contracts/chat.

const LIST = 'GET /api/chats?*'
const sciChat = 'c0000000-0000-4000-8000-000000000001'

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
  'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
  ...extra,
})

const employer = () => window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')

beforeEach(() => window.localStorage.clear())
afterEach(() => {
  vi.useRealTimers()
  window.localStorage.clear()
})

const listCalls = (calls: Call[]) => calls.filter((c) => c.method === 'GET' && c.path.startsWith('/api/chats?')).map((c) => new URL(c.path, 'http://x').searchParams)
async function rows() {
  await waitFor(() => expect(document.querySelector('ul.chat-list')).not.toBeNull())
  return within(document.querySelector('ul.chat-list') as HTMLElement).getAllByRole('link')
}

describe('chats page', () => {
  it('sends a visitor to sign in and back', async () => {
    stubApi()
    const { router } = renderApp('/chats')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe(`?next=${encodeURIComponent('/chats')}`)
  })

  it('shows the scientist side as rows of who, vacancy, state and last message', async () => {
    const { calls } = stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()) }))
    renderApp('/chats')
    expect(await screen.findByRole('heading', { level: 1, name: 'Сообщения' })).toBeInTheDocument()
    expect(screen.getByText('Переписка с организациями по вашим откликам и приглашениям.')).toBeInTheDocument()
    const [first, second, third, fourth] = await rows()
    expect(first).toHaveAttribute('href', `/chats/${sciChat}`)
    expect(within(first).getByText('Сибирский институт квантовых материалов')).toBeInTheDocument()
    expect(within(first).getByText('Младший научный сотрудник лаборатории спинтроники')).toBeInTheDocument()
    expect(within(first).getByText('Ждёт вашего ответа')).toBeInTheDocument()
    expect(within(first).getByText('Приглашение от организации')).toBeInTheDocument()
    expect(within(first).getByText('Непрочитанных сообщений: 1')).toBeInTheDocument()
    expect(first.querySelector('time')).toHaveAttribute('dateTime', '2026-10-14T09:40:00Z')
    expect(within(second).getByText(/Подскажите, когда вам удобно созвониться/)).toBeInTheDocument()
    expect(within(second).getByText('Отклик просмотрен')).toBeInTheDocument()
    expect(within(second).getByText('Непрочитанных сообщений: 2')).toBeInTheDocument()
    expect(within(third).getByText('Приглашение на вакансию', { selector: '.chat-row-preview' })).toBeInTheDocument()
    expect(within(fourth).getByText('Отказ')).toBeInTheDocument()
    expect(within(fourth).getByText('Решение по отклику')).toBeInTheDocument()
    expect(within(fourth).queryByText(/Непрочитанных/)).not.toBeInTheDocument()
    const q = listCalls(calls)[0]
    expect(Object.fromEntries(q)).toEqual({ side: 'scientist', box: 'all', limit: '30' })
    // Справа подсказка выбрать переписку
    expect(screen.getByText(/Выберите переписку в списке/)).toBeInTheDocument()
  })

  it('shows counts on tabs and switches boxes through the address', async () => {
    const { calls } = stubApi(
      signedIn({
        [LIST]: (call) => {
          const box = new URL(call.path, 'http://x').searchParams.get('box')
          return reply(200, box === 'unread' ? chatLists.scientistUnread() : box === 'archive' ? { ...chatLists.empty(), counts: { unread: 3, archive: 0 } } : chatLists.scientistAll())
        },
      }),
    )
    const { router } = renderApp('/chats')
    const tabs = await screen.findByRole('navigation', { name: 'Какие переписки показать' })
    expect(within(tabs).getByRole('link', { name: 'Все' })).toHaveAttribute('aria-current', 'page')
    expect(await within(tabs).findByRole('link', { name: 'Непрочитанные: 3' })).toBeInTheDocument()
    // Пустой архив без числа
    expect(within(tabs).getByRole('link', { name: 'Архив' })).toBeInTheDocument()
    await userEvent.click(within(tabs).getByRole('link', { name: 'Непрочитанные: 3' }))
    await waitFor(async () => expect(await rows()).toHaveLength(3))
    expect(router.state.location.search).toBe('?box=unread')
    await userEvent.click(within(tabs).getByRole('link', { name: 'Архив' }))
    expect(await screen.findByText('В архиве пусто')).toBeInTheDocument()
    await userEvent.click(within(tabs).getByRole('link', { name: 'Все' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(listCalls(calls).map((p) => p.get('box'))).toEqual(['all', 'unread', 'archive'])
  })

  it('says everything is read on the unread tab', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.empty()) }))
    renderApp('/chats?box=unread')
    expect(await screen.findByText('Всё прочитано')).toBeInTheDocument()
  })

  it('ignores an unknown box in the address', async () => {
    const { calls } = stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()) }))
    renderApp('/chats?box=trash')
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
    expect(listCalls(calls)[0].get('box')).toBe('all')
  })

  it('tells each side how a chat appears when there is none', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.empty()) }))
    renderApp('/chats')
    expect(await screen.findByText('Переписка появляется, когда вы откликаетесь на вакансию или организация приглашает вас на неё.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Найти вакансии' })).toHaveAttribute('href', '/vacancies')
  })

  it('tells an organization where chats come from', async () => {
    employer()
    stubApi(signedIn({ [LIST]: reply(200, chatLists.empty()), 'GET /api/chats/vacancies': reply(200, { items: [] }) }))
    renderApp('/chats')
    expect(await screen.findByText(/когда соискатель откликается на вашу вакансию/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Найти соискателей' })).toHaveAttribute('href', '/scientists')
    // Без вакансий отбора нет
    expect(screen.queryByLabelText('Вакансия')).not.toBeInTheDocument()
  })

  it('shows the organization side with an anonymous scientist and a vacancy filter', async () => {
    employer()
    const { calls } = stubApi(signedIn({ [LIST]: reply(200, chatLists.orgAll()), 'GET /api/chats/vacancies': reply(200, chatVacancies()) }))
    const { router } = renderApp('/chats')
    expect(await screen.findByText('Переписка с соискателями по вакансиям, которые вы ведёте.')).toBeInTheDocument()
    const [first, second, third] = await rows()
    expect(within(first).getByText('Дмитрий Королёв')).toBeInTheDocument()
    expect(within(first).getByText('Отклик отправлен')).toBeInTheDocument()
    expect(within(second).getByText('Анонимный соискатель')).toHaveAttribute('data-anonymous', 'true')
    expect(within(third).getByText('Ответ на приглашение')).toBeInTheDocument()
    const filter = await screen.findByLabelText('Вакансия')
    expect(within(filter).getByRole('option', { name: 'Научный сотрудник группы спектроскопии льда (3)' })).toBeInTheDocument()
    await userEvent.selectOptions(filter, 'a0000000-0000-4000-8000-000000000005')
    await waitFor(() => expect(router.state.location.search).toBe('?vacancy=a0000000-0000-4000-8000-000000000005'))
    await waitFor(() => expect(listCalls(calls).at(-1)?.get('vacancy')).toBe('a0000000-0000-4000-8000-000000000005'))
    expect(listCalls(calls).at(-1)?.get('side')).toBe('org')
  })

  it('does not send a vacancy filter on the scientist side', async () => {
    const { calls } = stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()) }))
    renderApp('/chats?vacancy=a0000000-0000-4000-8000-000000000005')
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
    expect(listCalls(calls)[0].has('vacancy')).toBe(false)
    expect(screen.queryByLabelText('Вакансия')).not.toBeInTheDocument()
  })

  it('searches after typing stops, at once on Enter, and resets from the empty result', async () => {
    const { calls } = stubApi(
      signedIn({
        [LIST]: (call) => {
          const q = new URL(call.path, 'http://x').searchParams.get('q')
          return reply(200, q ? chatLists.empty() : chatLists.scientistAll())
        },
      }),
    )
    const { router } = renderApp('/chats')
    const field = await screen.findByRole('searchbox', { name: 'Найти переписку' })
    expect(field).toHaveAttribute('placeholder', 'Вакансия или организация')
    await userEvent.type(field, '  арктика ')
    await waitFor(() => expect(router.state.location.search).toBe('?q=%D0%B0%D1%80%D0%BA%D1%82%D0%B8%D0%BA%D0%B0'))
    expect(await screen.findByText('Ничего не нашлось')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Сбросить поиск' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(field).toHaveValue('')
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
    await userEvent.type(field, 'спин{Enter}')
    await waitFor(() => expect(listCalls(calls).at(-1)?.get('q')).toBe('спин'))
  })

  it('keeps the search field when the role changes the side', async () => {
    employer()
    stubApi(signedIn({ [LIST]: reply(200, chatLists.orgAll()), 'GET /api/chats/vacancies': reply(200, chatVacancies()) }))
    renderApp('/chats')
    const field = await screen.findByRole('searchbox', { name: 'Найти переписку' })
    expect(field).toHaveAttribute('placeholder', 'Вакансия или имя')
  })

  it('loads more by cursor and retries a failed page in place', async () => {
    const first: ChatList = { ...chatLists.scientistAll() }
    const second: ChatList = { ...chatLists.orgArchive(), items: chatLists.orgArchive().items.map((c) => ({ ...c, side: 'scientist' })), next_cursor: null }
    let failNext = true
    const { calls } = stubApi(
      signedIn({
        [LIST]: (call) => {
          const cursor = new URL(call.path, 'http://x').searchParams.get('cursor')
          if (!cursor) return reply(200, first)
          if (failNext) {
            failNext = false
            return apiError(500, 'internal', 'Сбой')
          }
          return reply(200, second)
        },
      }),
    )
    renderApp('/chats')
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
    expect(await rows()).toHaveLength(4)
    await userEvent.click(screen.getByRole('button', { name: 'Показать ещё' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить следующие переписки.')
    expect(await rows()).toHaveLength(4)
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить ещё раз' }))
    await waitFor(async () => expect(await rows()).toHaveLength(5))
    expect(screen.queryByRole('button', { name: 'Показать ещё' })).not.toBeInTheDocument()
    expect(listCalls(calls).at(-1)?.get('cursor')).toBe(first.next_cursor)
  })

  it('shows a load error with a retry', async () => {
    let fail = true
    stubApi(signedIn({ [LIST]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, chatLists.scientistAll())) }))
    renderApp('/chats')
    expect(await screen.findByText('Не удалось загрузить переписки')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findAllByRole('link', { name: /Сибирский институт/ })).not.toHaveLength(0)
  })

  it('shows placeholders while the list loads', async () => {
    const waiting: ((r: ReturnType<typeof reply>) => void)[] = []
    stubApi(signedIn({ [LIST]: () => new Promise((resolve) => waiting.push(resolve)) }))
    renderApp('/chats')
    await waitFor(() => expect(waiting.length).toBeGreaterThan(0))
    expect(document.querySelector('.chat-list[role="status"]')).toHaveAttribute('aria-busy', 'true')
    act(() => waiting.forEach((release) => release(reply(200, chatLists.scientistAll()))))
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
  })

  it('handles worst-case rows: huge counts, empty names, deleted and unknown events, no messages', async () => {
    employer()
    const base = chatLists.orgAll().items[0]
    const list: ChatList = {
      items: [
        { ...base, id: 'w1', unread: 1500, counterpart: { ...base.counterpart, name: '' } },
        { ...base, id: 'w2', unread: 0, last_message: null },
        { ...base, id: 'w3', unread: 0, last_message: { ...base.last_message!, deleted: true, text: '' } },
        { ...base, id: 'w4', unread: 0, last_message: { ...base.last_message!, kind: 'system', author: null, text: '', event_type: 'something.new' } },
        { ...base, id: 'w5', unread: 0, last_message: { ...base.last_message!, author: { side: 'org', name: 'Иван Петров', is_me: true, anonymous: false, former: false }, text: 'Пришлите\n\nдиплом' } },
        { ...base, id: 'w6', unread: 0, last_message: { ...base.last_message!, author: { side: 'org', name: 'Анна Белова', is_me: false, anonymous: false, former: false }, text: 'Посмотрела' } },
        { ...base, id: 'w7', unread: 0, last_message: { ...base.last_message!, author: { side: 'org', name: '', is_me: false, anonymous: false, former: true }, text: 'Старое' } },
        { ...base, id: 'w8', unread: 0, status: { source: 'application', value: 'mystery' } },
      ],
      next_cursor: null,
      counts: { unread: 1, archive: 0 },
    }
    stubApi(signedIn({ [LIST]: reply(200, list), 'GET /api/chats/vacancies': reply(200, { items: [] }) }))
    renderApp('/chats')
    await screen.findByText('99+')
    const r = await rows()
    expect(within(r[0]).getByText('Анонимный соискатель')).toBeInTheDocument()
    expect(within(r[0]).getByText('Непрочитанных сообщений: 1500')).toBeInTheDocument()
    expect(within(r[1]).getByText('Сообщений пока нет')).toBeInTheDocument()
    expect(r[1].querySelector('time')).toHaveAttribute('dateTime', base.updated_at)
    expect(within(r[2]).getByText('Сообщение удалено')).toBeInTheDocument()
    expect(within(r[3]).getByText('Новое событие по отклику')).toBeInTheDocument()
    expect(within(r[4]).getByText('Вы: Пришлите диплом')).toBeInTheDocument()
    expect(within(r[5]).getByText('Анна Белова: Посмотрела')).toBeInTheDocument()
    expect(within(r[6]).getByText('Сотрудник организации: Старое')).toBeInTheDocument()
    expect(within(r[7]).getByText('mystery')).toBeInTheDocument()
  })

  it('points to unread chats of the other role and switches to it', async () => {
    const { calls } = stubApi(
      signedIn({
        'GET /api/chats/unread-count': reply(200, { total: 2, scientist: 0, org: 2 }),
        [LIST]: (call) => reply(200, new URL(call.path, 'http://x').searchParams.get('side') === 'org' ? chatLists.orgAll() : chatLists.scientistAll()),
        'GET /api/chats/vacancies': reply(200, chatVacancies()),
      }),
    )
    renderApp('/chats')
    expect(await screen.findByText(/В режиме «Нанимаю» есть непрочитанные переписки: 2\./)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Переключиться' }))
    await waitFor(() => expect(listCalls(calls).at(-1)?.get('side')).toBe('org'))
    expect(await screen.findByText('Анонимный соискатель')).toBeInTheDocument()
    expect(window.localStorage.getItem(ROLE_STORAGE_KEY)).toBe('employer')
    // Со стороны организации о своих переписках учёного не говорится: там непрочитанных нет
    expect(screen.queryByText(/В режиме «Ищу работу»/)).not.toBeInTheDocument()
  })

  it('points the organization to unread scientist chats too', async () => {
    employer()
    stubApi(signedIn({ 'GET /api/chats/unread-count': reply(200, { total: 1, scientist: 1, org: 0 }), [LIST]: reply(200, chatLists.orgAll()), 'GET /api/chats/vacancies': reply(200, chatVacancies()) }))
    renderApp('/chats')
    expect(await screen.findByText(/В режиме «Ищу работу» есть непрочитанные переписки: 1\./)).toBeInTheDocument()
  })

  it('refreshes the list every minute', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { calls } = stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()) }))
    renderApp('/chats')
    await screen.findAllByRole('link', { name: /Сибирский институт/ })
    const before = listCalls(calls).length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS)
    })
    await waitFor(() => expect(listCalls(calls).length).toBeGreaterThan(before))
  })
})

describe('open chat', () => {
  const header = (path: string, chat = chatHeaders.scientistInvited()) => ({ [`GET /api/chats/${path}`]: reply(200, { chat }) })

  it('marks the row, shows the header and the way to the application', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()), ...header(sciChat) }))
    renderApp(`/chats/${sciChat}?box=all`)
    expect(await screen.findByRole('heading', { level: 2, name: 'Сибирский институт квантовых материалов' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Младший научный сотрудник лаборатории спинтроники' })).toHaveAttribute('href', '/vacancies/a0000000-0000-4000-8000-000000000001')
    expect(screen.getByText('Лаборатория спинтроники', { selector: '.chat-window-unit' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Открыть отклик' })).toHaveAttribute('href', '/applications/b0000000-0000-4000-8000-000000000001')
    expect(screen.getByRole('link', { name: 'Все переписки' })).toHaveAttribute('href', '/chats?box=all')
    expect(await screen.findByRole('link', { current: 'page', name: /Сибирский институт/ })).toHaveAttribute('href', `/chats/${sciChat}?box=all`)
    await waitFor(() => expect(document.title).toBe('Сибирский институт квантовых материалов — Сообщения — ИОН'))
    expect(screen.queryByText(/Выберите переписку/)).not.toBeInTheDocument()
  })

  it('says why a closed chat is closed', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()), ...header('c4', chatHeaders.scientistClosed()) }))
    renderApp('/chats/c4')
    expect(await screen.findByText('Переписка закрыта: по отклику принято решение «Отказ».')).toBeInTheDocument()
    expect(screen.getByText('Отказ', { selector: '.chat-window-meta .tag' })).toBeInTheDocument()
  })

  it('leads the organization to the candidate card or its sent offers', async () => {
    employer()
    stubApi(signedIn({ [LIST]: reply(200, chatLists.orgAll()), 'GET /api/chats/vacancies': reply(200, chatVacancies()), ...header('e', chatHeaders.orgApplication()), ...header('f', chatHeaders.orgAnonymousOffer()) }))
    const { router } = renderApp('/chats/e')
    expect(await screen.findByRole('link', { name: 'Открыть отклик' })).toHaveAttribute('href', '/candidates/b0000000-0000-4000-8000-000000000005')
    await act(() => router.navigate('/chats/f'))
    expect(await screen.findByRole('heading', { level: 2, name: 'Анонимный соискатель' })).toHaveAttribute('data-anonymous', 'true')
    expect(screen.getByRole('link', { name: 'Открыть приглашение' })).toHaveAttribute('href', '/sent-offers')
  })

  it('leads the scientist to the offer, and shows nothing when the chat has neither', async () => {
    const offerChat = { ...chatHeaders.scientistInvited(), application_id: null, offer_id: 'd1' }
    const bare = { ...chatHeaders.scientistInvited(), application_id: null, offer_id: null, needs_answer: false, vacancy: { ...chatHeaders.scientistInvited().vacancy, unit_name: '' } }
    stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()), ...header('o', offerChat), ...header('b', bare) }))
    const { router } = renderApp('/chats/o')
    expect(await screen.findByRole('link', { name: 'Открыть приглашение' })).toHaveAttribute('href', '/offers/d1')
    await act(() => router.navigate('/chats/b'))
    await screen.findByText('Приглашение', { selector: '.chat-window-meta .tag' })
    expect(screen.queryByRole('link', { name: /Открыть/ })).not.toBeInTheDocument()
    expect(document.querySelector('.chat-window-unit')).toBeNull()
  })

  it('treats an unknown chat as missing and a failure as an error with retry', async () => {
    let fail = true
    stubApi(
      signedIn({
        [LIST]: reply(200, chatLists.scientistAll()),
        'GET /api/chats/nope': apiError(404, 'not_found', 'Такой переписки нет'),
        'GET /api/chats/flaky': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { chat: chatHeaders.scientistInvited() })),
      }),
    )
    const { router } = renderApp('/chats/nope')
    expect(await screen.findByText('Такой переписки нет')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Все переписки' })).toHaveAttribute('href', '/chats')
    await act(() => router.navigate('/chats/flaky'))
    expect(await screen.findByText('Не удалось открыть переписку')).toBeInTheDocument()
    fail = false
    await userEvent.click(within(document.querySelector('.chats-pane') as HTMLElement).getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('heading', { level: 2, name: 'Сибирский институт квантовых материалов' })).toBeInTheDocument()
  })

  it('shows placeholders while the chat loads', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()), 'GET /api/chats/slow': () => new Promise(() => {}) }))
    renderApp('/chats/slow')
    await waitFor(() => expect(document.querySelector('.chats-pane [role="status"]')).not.toBeNull())
    expect(document.querySelector('.chats-pane [role="status"]')).toHaveAttribute('aria-busy', 'true')
  })

  it('marks the open chat on the page for the phone layout', async () => {
    stubApi(signedIn({ [LIST]: reply(200, oneChat(chatLists.scientistAll().items[0])), ...header(sciChat) }))
    renderApp(`/chats/${sciChat}`)
    await screen.findByRole('heading', { level: 2, name: 'Сибирский институт квантовых материалов' })
    expect(document.querySelector('.chats-page')).toHaveAttribute('data-open', 'true')
  })
})

describe('messages item in the header', () => {
  const item = (name: RegExp | string = /^Сообщения/) => screen.findByRole('link', { name })

  it('is not shown to a visitor and asks nothing', async () => {
    const { calls } = stubApi()
    renderApp('/')
    await screen.findByRole('link', { name: 'Войти' })
    expect(screen.queryByRole('link', { name: /^Сообщения/ })).not.toBeInTheDocument()
    expect(calls.filter((c) => c.path.startsWith('/api/chats'))).toHaveLength(0)
  })

  it('counts chats with unread messages of both sides', async () => {
    stubApi(signedIn({ 'GET /api/chats/unread-count': reply(200, unreadCounts.some()) }))
    renderApp('/')
    const link = await item('Сообщения, непрочитанных переписок: 4')
    expect(link).toHaveAttribute('href', '/chats')
    expect(within(link).getByText('4')).toBeInTheDocument()
  })

  it('has a plain name with nothing new, caps big numbers and stays quiet on errors', async () => {
    stubApi(signedIn())
    renderApp('/')
    const link = await item('Сообщения')
    expect(within(link).queryByText('0')).not.toBeInTheDocument()
  })

  it('caps a huge number', async () => {
    stubApi(signedIn({ 'GET /api/chats/unread-count': reply(200, { total: 1234, scientist: 1234, org: 0 }) }))
    renderApp('/')
    expect(within(await item(/переписок: 1234/)).getByText('99+')).toBeInTheDocument()
  })

  it('stays quiet when the count cannot be loaded', async () => {
    stubApi(signedIn({ 'GET /api/chats/unread-count': apiError(404, 'not_found', 'Нет') }))
    renderApp('/')
    expect(within(await item('Сообщения')).queryByText(/\d/)).not.toBeInTheDocument()
  })

  it('is marked current on the chats pages', async () => {
    stubApi(signedIn({ [LIST]: reply(200, chatLists.scientistAll()) }))
    renderApp('/chats')
    expect(await item('Сообщения')).toHaveAttribute('aria-current', 'page')
  })

  it('asks again every minute', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { called } = stubApi(signedIn())
    renderApp('/')
    await item('Сообщения')
    const before = called('GET', '/api/chats/unread-count').length
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_MS)
    })
    await waitFor(() => expect(called('GET', '/api/chats/unread-count').length).toBeGreaterThan(before))
  })
})

import { renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, securityApi, sessionsFixture, signedInAs, stubApi, type Call } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'
import { deviceLabel, formatDay, formatMoment } from './labels'
import { useReauth } from './reauth-context'

const pending = { ...ann, deletion_due_at: '2026-10-22T09:00:00Z' }
const reauthNeeded = apiError(403, 'reauth_required', 'Для этого действия введите пароль ещё раз')

/** Сервер просит пароль один раз, потом отвечает ok. */
function onceReauth(ok: ReturnType<typeof reply>) {
  let asked = false
  return () => {
    if (asked) return ok
    asked = true
    return reauthNeeded
  }
}

async function openSecurity(routes: Parameters<typeof stubApi>[0] = {}, user = ann) {
  const api = stubApi({ ...securityApi(user), ...routes })
  const view = renderApp('/account/security')
  await screen.findByRole('heading', { level: 2, name: 'Где вы вошли' })
  await screen.findByText('это устройство')
  return { api, ...view }
}

describe('account tabs', () => {
  it('lead from «Аккаунт» to «Безопасность» and mark the current one', async () => {
    stubApi(securityApi())
    const { router } = renderApp('/account')
    const tabs = await screen.findByRole('navigation', { name: 'Разделы настроек' })
    expect(within(tabs).getByRole('link', { name: 'Аккаунт' })).toHaveAttribute('aria-current', 'page')
    await userEvent.click(within(tabs).getByRole('link', { name: 'Безопасность' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/account/security'))
    expect(await screen.findByRole('heading', { level: 2, name: 'Удаление аккаунта' })).toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: 'Разделы настроек' })).getByRole('link', { name: 'Безопасность' })).toHaveAttribute('aria-current', 'page')
  })

  it('send a visitor to sign in and back to the security tab', async () => {
    stubApi()
    const { router } = renderApp('/account/security')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=/account/security')
  })
})

describe('sessions', () => {
  it('list devices: this one marked, the phone can be ended', async () => {
    await openSecurity()
    const section = screen.getByRole('heading', { level: 2, name: 'Где вы вошли' }).closest('section')!
    const list = within(section).getByRole('list')
    const rows = within(list).getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('Chrome, macOS')
    expect(rows[0]).toHaveTextContent('это устройство')
    expect(rows[0]).toHaveTextContent('Вход 2 октября, 12:05 · заходили 8 октября, 14:00')
    expect(within(rows[0]).queryByRole('button')).not.toBeInTheDocument()
    expect(rows[1]).toHaveTextContent('Safari, iOS · телефон')
    expect(within(rows[1]).getByRole('button', { name: 'Завершить сеанс: Safari, iOS · телефон' })).toBeInTheDocument()
  })

  it('end one session and refresh the list', async () => {
    let ended = false
    const { api } = await openSecurity({
      'GET /api/account/sessions': () => reply(200, { sessions: ended ? sessionsFixture.slice(0, 1) : sessionsFixture }),
      'DELETE /api/account/sessions/s-phone': () => {
        ended = true
        return reply(204)
      },
    })
    await press('Завершить сеанс: Safari, iOS · телефон')
    expect(await screen.findByText('Сеанс завершён')).toBeInTheDocument()
    expect(api.called('DELETE', '/api/account/sessions/s-phone')).toHaveLength(1)
    expect(await screen.findByText('Аккаунт открыт только здесь.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Выйти на всех других устройствах' })).not.toBeInTheDocument()
  })

  it('say so when ending failed', async () => {
    await openSecurity({ 'DELETE /api/account/sessions/s-phone': reply(500) })
    await press('Завершить сеанс: Safari, iOS · телефон')
    expect(await screen.findByText('Не удалось завершить сеанс')).toBeInTheDocument()
  })

  it('sign out everywhere else', async () => {
    const { api } = await openSecurity({ 'POST /api/account/sessions/revoke-others': reply(204) })
    await press('Выйти на всех других устройствах')
    expect(await screen.findByText('Готово: на других устройствах вы вышли из аккаунта')).toBeInTheDocument()
    expect(api.called('POST', '/api/account/sessions/revoke-others')).toHaveLength(1)
  })

  it('say so when signing out elsewhere failed', async () => {
    await openSecurity({ 'POST /api/account/sessions/revoke-others': reply(500) })
    await press('Выйти на всех других устройствах')
    expect(await screen.findByText('Не удалось выйти на других устройствах')).toBeInTheDocument()
  })

  it('hold the place while loading, then recover from an error', async () => {
    let n = 0
    stubApi({
      ...securityApi(),
      'GET /api/account/sessions': () => (n++ === 0 ? reply(500) : reply(200, { sessions: sessionsFixture })),
    })
    renderApp('/account/security')
    const section = (await screen.findByRole('heading', { level: 2, name: 'Где вы вошли' })).closest('section')!
    expect(await within(section).findByText('Не удалось загрузить список устройств')).toBeInTheDocument()
    await userEvent.click(within(section).getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await within(section).findByText('Chrome, macOS')).toBeInTheDocument()
  })

  it('show a skeleton while the list is on its way', async () => {
    stubApi({ ...securityApi(), 'GET /api/account/sessions': () => new Promise(() => {}) as never })
    renderApp('/account/security')
    const section = (await screen.findByRole('heading', { level: 2, name: 'Где вы вошли' })).closest('section')!
    expect(within(section).getByRole('status', { name: 'Загрузка' })).toHaveAttribute('aria-busy', 'true')
  })
})

describe('email change', () => {
  it('asks for the password, repeats the request and says where the link went', async () => {
    const { api } = await openSecurity({
      'POST /api/account/email': onceReauth(reply(202, { status: 'ok' })),
      'POST /api/account/reauth': (call: Call) =>
        (call.body as { password: string }).password === 'кофе на рассвете'
          ? reply(204)
          : apiError(422, 'validation_failed', 'x', { fields: { password: 'Пароль указан неверно' } }),
    })
    expect(within(screen.getByRole('main')).getByText('anna@example.ru')).toBeInTheDocument()
    await fill(/^Новая почта/, 'anna.new@example.ru')
    await press('Прислать ссылку')

    const dialog = await screen.findByRole('dialog', { name: 'Подтвердите, что это вы' })
    expect(within(dialog).getByLabelText(/^Пароль/)).toHaveFocus()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить' }))
    expect(within(dialog).getByText('Введите пароль')).toBeInTheDocument()
    await userEvent.type(within(dialog).getByLabelText(/^Пароль/), 'не тот')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(dialog).findByText('Пароль указан неверно')).toBeInTheDocument()
    await userEvent.clear(within(dialog).getByLabelText(/^Пароль/))
    await userEvent.type(within(dialog).getByLabelText(/^Пароль/), 'кофе на рассвете')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить' }))

    expect(await screen.findByText('Ссылка отправлена на anna.new@example.ru')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.called('POST', '/api/account/email')).toHaveLength(2)
    expect(api.called('POST', '/api/account/email')[1].body).toEqual({ email: 'anna.new@example.ru' })

    await press('Указать другой адрес')
    expect(field(/^Новая почта/)).toHaveValue('')
  })

  it('ends with the server message if the person closes the password window', async () => {
    const { api } = await openSecurity({ 'POST /api/account/email': reauthNeeded })
    await fill(/^Новая почта/, 'anna.new@example.ru')
    await press('Прислать ссылку')
    const dialog = await screen.findByRole('dialog', { name: 'Подтвердите, что это вы' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Для этого действия введите пароль ещё раз')
    expect(api.called('POST', '/api/account/reauth')).toHaveLength(0)
  })

  it('shows trouble with the password check itself inside the window', async () => {
    await openSecurity({
      'POST /api/account/email': reauthNeeded,
      'POST /api/account/reauth': apiError(429, 'rate_limited', 'x', { retry_after: 600 }),
    })
    await fill(/^Новая почта/, 'anna.new@example.ru')
    await press('Прислать ссылку')
    const dialog = await screen.findByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText(/^Пароль/), 'кофе на рассвете')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('через 10 минут')
  })

  it('checks the address before sending and shows the server objection under the field', async () => {
    const { api } = await openSecurity({
      'POST /api/account/email': apiError(422, 'validation_failed', 'x', { fields: { email: 'Это и так почта вашего аккаунта' } }),
    })
    await press('Прислать ссылку')
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    expect(api.called('POST', '/api/account/email')).toHaveLength(0)
    await fill(/^Новая почта/, 'anna@example.ru')
    await press('Прислать ссылку')
    expect(await screen.findByText('Это и так почта вашего аккаунта')).toBeInTheDocument()
  })
})

describe('account deletion', () => {
  it('explains what goes and schedules deletion after the password, then signs out', async () => {
    const { api, router } = await openSecurity({
      'POST /api/account/deletion': onceReauth(reply(200, { due_at: '2026-10-22T09:00:00Z' })),
      'POST /api/account/reauth': reply(204),
    })
    const section = screen.getByRole('heading', { level: 2, name: 'Удаление аккаунта' }).closest('section')!
    await within(section).findByRole('button', { name: 'Удалить аккаунт' })
    expect(section).toHaveTextContent('через 14 дней')
    expect(section).toHaveTextContent('не ваше имя, письмо, профиль и файлы')
    await userEvent.click(within(section).getByRole('button', { name: 'Удалить аккаунт' }))

    const confirm = await screen.findByRole('dialog', { name: 'Удалить аккаунт?' })
    expect(confirm).toHaveTextContent('Данные сотрутся')
    await userEvent.click(within(confirm).getByRole('button', { name: 'Удалить аккаунт' }))
    const pass = await screen.findByRole('dialog', { name: 'Подтвердите, что это вы' })
    await userEvent.type(within(pass).getByLabelText(/^Пароль/), 'кофе на рассвете')
    await userEvent.click(within(pass).getByRole('button', { name: 'Подтвердить' }))

    expect(await screen.findByText('Аккаунт будет удалён 22 октября 2026')).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(api.called('POST', '/api/account/deletion')).toHaveLength(2)
  })

  it('shows what the server said inside the confirmation window and lets it close', async () => {
    await openSecurity({ 'POST /api/account/deletion': apiError(409, 'deletion_blocked', 'Сначала сделайте владельцем кого-то из сотрудников') })
    await screen.findByRole('button', { name: 'Удалить аккаунт' })
    await press('Удалить аккаунт')
    const confirm = await screen.findByRole('dialog', { name: 'Удалить аккаунт?' })
    await userEvent.click(within(confirm).getByRole('button', { name: 'Удалить аккаунт' }))
    expect(await within(confirm).findByRole('alert')).toHaveTextContent('Сначала сделайте владельцем')
    await userEvent.click(within(confirm).getByRole('button', { name: 'Отмена' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await press('Удалить аккаунт')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('asks to appoint an owner first when the person is the only owner of a team', async () => {
    await openSecurity({
      'GET /api/account/deletion': reply(200, { due_at: null, grace_days: 14, blocking: [{ slug: 'arktika', name: 'Центр изучения Арктики' }], orphaned: [] }),
    })
    const section = (await screen.findByText('Сначала назначьте владельца')).closest('section')!
    expect(within(section).getByRole('link', { name: 'Сотрудники организации «Центр изучения Арктики»' })).toHaveAttribute('href', '/my-organization/arktika/members')
    expect(within(section).queryByRole('button', { name: 'Удалить аккаунт' })).not.toBeInTheDocument()
  })

  it('warns that a one-person organization will stay without an owner', async () => {
    await openSecurity({
      'GET /api/account/deletion': reply(200, { due_at: null, grace_days: 1, blocking: [], orphaned: [{ slug: 'odin', name: 'Лаборатория одного' }] }),
    })
    const section = screen.getByRole('heading', { level: 2, name: 'Удаление аккаунта' }).closest('section')!
    expect(await within(section).findByText(/останутся без владельца/)).toBeInTheDocument()
    expect(within(section).getByRole('link', { name: 'Сотрудники организации «Лаборатория одного»' })).toBeInTheDocument()
    expect(section).toHaveTextContent('через 1 день')
  })

  it('recovers when the plan could not be loaded', async () => {
    let n = 0
    await openSecurity({
      'GET /api/account/deletion': () => (n++ === 0 ? reply(500) : reply(200, { due_at: null, grace_days: 14, blocking: [], orphaned: [] })),
    })
    expect(await screen.findByText('Не удалось проверить, можно ли удалить аккаунт')).toBeInTheDocument()
    const section = screen.getByRole('heading', { level: 2, name: 'Удаление аккаунта' }).closest('section')!
    await userEvent.click(within(section).getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await within(section).findByRole('button', { name: 'Удалить аккаунт' })).toBeInTheDocument()
  })

  it('holds the place while the plan loads', async () => {
    await openSecurity({ 'GET /api/account/deletion': () => new Promise(() => {}) as never })
    const section = screen.getByRole('heading', { level: 2, name: 'Удаление аккаунта' }).closest('section')!
    expect(within(section).getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
  })

  it('offers to cancel a scheduled deletion and confirms it', async () => {
    let user: typeof pending | typeof ann = pending
    const api = stubApi({
      ...securityApi(),
      'GET /api/auth/me': () => reply(200, { user }),
      'POST /api/account/deletion/cancel': () => {
        user = ann
        return reply(204)
      },
    })
    renderApp('/account/security')
    const heading = await screen.findByRole('heading', { level: 2, name: /^Аккаунт будет удалён 22\sоктября 2026$/ })
    // На этой странице своя кнопка отмены: полосы под шапкой нет.
    expect(screen.queryByRole('link', { name: 'Отменить удаление' })).not.toBeInTheDocument()
    await userEvent.click(within(heading.closest('section')!).getByRole('button', { name: 'Отменить удаление' }))
    expect(await screen.findByText('Удаление отменено')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { level: 2, name: 'Удаление аккаунта' })).toBeInTheDocument()
    expect(api.called('POST', '/api/account/deletion/cancel')).toHaveLength(1)
  })

  it('says so when cancelling failed', async () => {
    stubApi({ ...securityApi(), ...signedInAs(pending), 'POST /api/account/deletion/cancel': reply(500) })
    renderApp('/account/security')
    await screen.findByRole('button', { name: 'Отменить удаление' })
    await press('Отменить удаление')
    expect(await screen.findByText('Не удалось отменить удаление')).toBeInTheDocument()
  })

  it('reminds on every other page with a banner that leads to the settings', async () => {
    stubApi({ ...securityApi(), ...signedInAs(pending), 'GET /api/landing': reply(500) })
    const { router } = renderApp('/privacy')
    const banner = await screen.findByText(/Аккаунт будет удалён/, { selector: '.deletion-banner-inner' })
    expect(banner).toHaveTextContent('Аккаунт будет удалён 22 октября 2026. Отменить удаление')
    await userEvent.click(within(banner).getByRole('link', { name: 'Отменить удаление' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/account/security'))
  })
})

describe('email change links', () => {
  it('confirm the new address for the person signed in here', async () => {
    const api = stubApi({ ...signedInAs(), 'POST /api/auth/confirm-email-change': reply(200, { user: { ...ann, email: 'anna.new@example.ru' } }) })
    renderApp('/confirm-email-change?token=abc')
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта изменена' })).toBeInTheDocument()
    expect(screen.getByText(/Теперь входите по адресу anna.new@example.ru/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К настройкам' })).toHaveAttribute('href', '/account/security')
    expect(api.called('POST', '/api/auth/confirm-email-change')[0].body).toEqual({ token: 'abc' })
  })

  it('confirm the new address in a browser where nobody is signed in', async () => {
    stubApi({ 'POST /api/auth/confirm-email-change': reply(200, { user: { ...ann, email: 'anna.new@example.ru' } }) })
    renderApp('/confirm-email-change?token=abc')
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта изменена' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Войти' }).some((l) => l.closest('main'))).toBe(true)
  })

  it.each([
    ['without a token', '/confirm-email-change', undefined, 'Ссылка больше не работает'],
    ['used or expired', '/confirm-email-change?token=old', apiError(400, 'invalid_token', 'x'), 'Ссылка больше не работает'],
    ['taken meanwhile', '/confirm-email-change?token=abc', apiError(409, 'email_taken', 'x'), 'Эта почта уже занята'],
  ])('explain a link that did not work: %s', async (_, path, answer, title) => {
    stubApi(answer ? { 'POST /api/auth/confirm-email-change': answer } : {})
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: title })).toBeInTheDocument()
  })

  it('let the person retry when the server did not answer', async () => {
    let n = 0
    stubApi({
      'POST /api/auth/confirm-email-change': () => (n++ === 0 ? reply(500) : reply(200, { user: { ...ann, email: 'anna.new@example.ru' } })),
    })
    renderApp('/confirm-email-change?token=abc')
    expect(await screen.findByText('Не удалось сменить почту')).toBeInTheDocument()
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта изменена' })).toBeInTheDocument()
  })

  it('show the unreachable-server text when there is no answer at all', async () => {
    stubApi({ 'POST /api/auth/confirm-email-change': () => Promise.reject(new TypeError('offline')) })
    renderApp('/confirm-email-change?token=abc')
    expect(await screen.findByText(/Ссылка из письма не сгорела/)).toBeInTheDocument()
  })

  it('revert the address only after a press, then sign out everywhere', async () => {
    const api = stubApi({ ...signedInAs(), 'POST /api/auth/revert-email-change': reply(204) })
    renderApp('/revert-email-change?token=undo')
    expect(await screen.findByRole('heading', { level: 1, name: 'Вернуть прежнюю почту' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/revert-email-change')).toHaveLength(0)
    await press('Вернуть почту и выйти везде')
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта возвращена' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/revert-email-change')[0].body).toEqual({ token: 'undo' })
    expect((await screen.findAllByRole('link', { name: 'Войти' })).length).toBeGreaterThan(0)
  })

  it.each([
    ['without a token', '/revert-email-change', undefined, 'Ссылка больше не работает'],
    ['used or expired', '/revert-email-change?token=old', apiError(400, 'invalid_token', 'x'), 'Ссылка больше не работает'],
    ['old address taken', '/revert-email-change?token=undo', apiError(409, 'email_taken', 'x'), 'Прежняя почта уже занята'],
  ])('explain a revert link that did not work: %s', async (_, path, answer, title) => {
    stubApi(answer ? { 'POST /api/auth/revert-email-change': answer } : {})
    renderApp(path)
    if (answer) {
      await screen.findByRole('button', { name: 'Вернуть почту и выйти везде' })
      await press('Вернуть почту и выйти везде')
    }
    expect(await screen.findByRole('heading', { level: 1, name: title })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Восстановить пароль' })).toHaveAttribute('href', '/forgot-password')
  })

  it('show other revert trouble above the button', async () => {
    stubApi({ 'POST /api/auth/revert-email-change': reply(500) })
    renderApp('/revert-email-change?token=undo')
    await screen.findByRole('button', { name: 'Вернуть почту и выйти везде' })
    await press('Вернуть почту и выйти везде')
    expect(await screen.findByRole('alert')).toBeInTheDocument()
  })
})

describe('labels', () => {
  it('name devices in words', () => {
    expect(deviceLabel({ browser: 'yandex', os: 'android', mobile: true })).toBe('Яндекс Браузер, Android · телефон')
    expect(deviceLabel({ browser: 'firefox', os: '', mobile: false })).toBe('Firefox')
    expect(deviceLabel({ browser: '', os: 'linux', mobile: false })).toBe('Linux')
    expect(deviceLabel({ browser: '', os: '', mobile: false })).toBe('Неизвестное устройство')
  })

  it('write dates and moments by Moscow time', () => {
    expect(formatDay('2026-10-21T22:30:00Z')).toBe('22\u00a0октября 2026')
    expect(formatMoment('2026-10-02T09:05:00Z')).toBe('2\u00a0октября, 12:05')
  })

  it('let an action through untouched outside the password provider', async () => {
    const { result } = renderHook(() => useReauth())
    await expect(result.current(() => Promise.resolve(7))).resolves.toBe(7)
  })
})

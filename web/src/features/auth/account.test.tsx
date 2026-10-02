import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'

describe('AccountPage', () => {
  it('sends a visitor to sign in and brings them back afterwards', async () => {
    stubApi()
    const { router } = renderApp('/account')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe('?next=/account')
    expect(await screen.findByText('Войдите, чтобы открыть эту страницу.')).toBeInTheDocument()
  })

  it('holds the place while it finds out who is signed in', () => {
    stubApi({ 'GET /api/auth/me': () => new Promise(() => {}) as never })
    renderApp('/account')
    expect(screen.getByRole('heading', { level: 1, name: 'Настройки аккаунта' })).toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'Загрузка' })).toHaveAttribute('aria-busy', 'true')
  })

  it('says so when the server does not answer, and recovers on retry', async () => {
    let n = 0
    stubApi({ 'GET /api/auth/me': () => (n++ === 0 ? reply(500) : reply(200, { user: ann })) })
    renderApp('/account')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить аккаунт')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 2, name: 'Профиль' })).toBeInTheDocument()
  })

  it('shows the profile: confirmed address and the current name', async () => {
    stubApi(signedInAs())
    renderApp('/account')
    expect(await screen.findByRole('heading', { level: 2, name: 'Профиль' })).toBeInTheDocument()
    const main = within(screen.getByRole('main'))
    expect(main.getByText('anna@example.ru')).toBeInTheDocument()
    expect(main.getByText('подтверждена')).toBeInTheDocument()
    expect(field(/^Имя и фамилия/)).toHaveValue('Анна Смирнова')
  })

  it('does not call an unconfirmed address confirmed', async () => {
    stubApi(signedInAs({ ...ann, email_confirmed: false }))
    renderApp('/account')
    await within(screen.getByRole('main')).findByText('anna@example.ru')
    expect(screen.queryByText('подтверждена')).not.toBeInTheDocument()
  })

  it('lists the four coming-soon services in their own section', async () => {
    stubApi(signedInAs())
    renderApp('/account')
    const section = (await screen.findByRole('heading', { name: 'Вход через другие сервисы' })).closest('section')!
    expect(within(section).getAllByRole('button')).toHaveLength(4)
    for (const b of within(section).getAllByRole('button')) expect(b).toBeDisabled()
  })

  describe('name', () => {
    it('is saved, announced and shown in the header', async () => {
      const api = stubApi({
        ...signedInAs(),
        'PATCH /api/account': () => reply(200, { user: { ...ann, name: 'Анна Петрова' } }),
      })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Профиль' })
      await userEvent.clear(field(/^Имя и фамилия/))
      await fill(/^Имя и фамилия/, ' Анна Петрова ')
      await press('Сохранить имя')

      expect(await screen.findByText('Имя сохранено')).toBeInTheDocument()
      expect(api.called('PATCH', '/api/account')[0].body).toEqual({ name: 'Анна Петрова' })
      expect(within(screen.getAllByRole('banner')[0]).getAllByText('Анна Петрова').length).toBeGreaterThan(0)
    })

    it('cannot be empty', async () => {
      const api = stubApi(signedInAs())
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Профиль' })
      await userEvent.clear(field(/^Имя и фамилия/))
      await press('Сохранить имя')
      expect(screen.getByText('Укажите, как вас зовут')).toBeInTheDocument()
      expect(field(/^Имя и фамилия/)).toHaveFocus()
      expect(api.called('PATCH', '/api/account')).toHaveLength(0)
    })

    it('shows the server\'s objection under the field', async () => {
      stubApi({
        ...signedInAs(),
        'PATCH /api/account': apiError(422, 'validation_failed', 'x', { fields: { name: 'В имени должны быть буквы' } }),
      })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Профиль' })
      await userEvent.clear(field(/^Имя и фамилия/))
      await fill(/^Имя и фамилия/, '123')
      await press('Сохранить имя')
      expect(await screen.findByText('В имени должны быть буквы')).toBeInTheDocument()
    })

    it('shows other trouble above the form', async () => {
      stubApi({ ...signedInAs(), 'PATCH /api/account': reply(500) })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Профиль' })
      await press('Сохранить имя')
      expect(await screen.findByRole('alert')).toHaveTextContent('Сервер не отвечает')
    })
  })

  describe('password', () => {
    async function open() {
      const api = stubApi({ ...signedInAs(), 'POST /api/account/password': reply(204) })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Пароль' })
      return api
    }

    it('asks for both passwords and checks the new one', async () => {
      const api = await open()
      await press('Сменить пароль')
      expect(screen.getAllByText('Введите пароль')).toHaveLength(2)
      expect(field(/^Текущий пароль/)).toHaveFocus()
      await fill(/^Текущий пароль/, 'старый пароль')
      await fill(/^Новый пароль/, 'коротко')
      await press('Сменить пароль')
      expect(screen.getByText(/слишком короткий/)).toBeInTheDocument()
      expect(api.called('POST', '/api/account/password')).toHaveLength(0)
    })

    it('changes the password, clears the fields and says it is done', async () => {
      const api = await open()
      await fill(/^Текущий пароль/, 'старый пароль')
      await fill(/^Новый пароль/, 'кофе на рассвете')
      await press('Сменить пароль')

      expect(await screen.findByText('Пароль изменён')).toBeInTheDocument()
      expect(api.called('POST', '/api/account/password')[0].body).toEqual({
        current_password: 'старый пароль',
        new_password: 'кофе на рассвете',
      })
      expect(field(/^Текущий пароль/)).toHaveValue('')
      expect(field(/^Новый пароль/)).toHaveValue('')
    })

    it('puts "wrong current password" under the current-password field', async () => {
      stubApi({
        ...signedInAs(),
        'POST /api/account/password': apiError(422, 'validation_failed', 'x', { fields: { current_password: 'Текущий пароль указан неверно' } }),
      })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Пароль' })
      await fill(/^Текущий пароль/, 'не тот пароль')
      await fill(/^Новый пароль/, 'кофе на рассвете')
      await press('Сменить пароль')
      expect(await screen.findByText('Текущий пароль указан неверно')).toBeInTheDocument()
      expect(field(/^Текущий пароль/)).toHaveFocus()
      expect(field(/^Новый пароль/)).not.toBeInvalid()
    })

    it('puts the verdict on the new password under the new-password field', async () => {
      stubApi({
        ...signedInAs(),
        'POST /api/account/password': apiError(422, 'validation_failed', 'x', { fields: { new_password: 'Новый пароль совпадает со старым' } }),
      })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Пароль' })
      await fill(/^Текущий пароль/, 'кофе на рассвете')
      await fill(/^Новый пароль/, 'кофе на рассвете')
      await press('Сменить пароль')
      expect(await screen.findByText('Новый пароль совпадает со старым')).toBeInTheDocument()
      expect(field(/^Новый пароль/)).toHaveFocus()
    })

    it('tells how long to wait after too many wrong guesses', async () => {
      stubApi({ ...signedInAs(), 'POST /api/account/password': apiError(429, 'rate_limited', 'x', { retry_after: 840 }) })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Пароль' })
      await fill(/^Текущий пароль/, 'кофе на рассвете')
      await fill(/^Новый пароль/, 'другой пароль 1')
      await press('Сменить пароль')
      expect(await screen.findByRole('alert')).toHaveTextContent('через 14 минут')
    })
  })

  describe('devices and sign-out', () => {
    it('signs out everywhere else and says so', async () => {
      const api = stubApi({ ...signedInAs(), 'POST /api/account/sessions/revoke-others': reply(204) })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Устройства' })
      await press('Выйти на других устройствах')
      expect(await screen.findByText('Готово: на других устройствах вы вышли из аккаунта')).toBeInTheDocument()
      expect(api.called('POST', '/api/account/sessions/revoke-others')).toHaveLength(1)
    })

    it('says so when that failed', async () => {
      stubApi({ ...signedInAs(), 'POST /api/account/sessions/revoke-others': reply(500) })
      renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Устройства' })
      await press('Выйти на других устройствах')
      expect(await screen.findByText('Не удалось выйти на других устройствах')).toBeInTheDocument()
    })

    it('signs out of this device and goes home, not to a sign-in page asking to sign in', async () => {
      const api = stubApi({ ...signedInAs(), 'POST /api/auth/logout': reply(204) })
      const { router } = renderApp('/account')
      await screen.findByRole('heading', { level: 2, name: 'Профиль' })
      await press('Выйти из аккаунта')

      expect(await screen.findByText('Вы вышли из аккаунта')).toBeInTheDocument()
      expect(router.state.location.pathname).toBe('/')
      expect(api.called('POST', '/api/auth/logout')).toHaveLength(1)
      expect((await screen.findAllByRole('link', { name: 'Войти' })).length).toBeGreaterThan(0)
    })
  })
})

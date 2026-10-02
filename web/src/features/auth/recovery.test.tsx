import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StrictMode } from 'react'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, stubApi } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'

describe('ForgotPasswordPage', () => {
  it('asks for a valid address first', async () => {
    const api = stubApi()
    renderApp('/forgot-password')
    await press('Отправить ссылку')
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    expect(field(/^Почта/)).toHaveFocus()
    await fill(/^Почта/, 'anna')
    await press('Отправить ссылку')
    expect(screen.getByText(/опечатка/)).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/forgot-password')).toHaveLength(0)
  })

  it('answers the same way for any address and offers the way back', async () => {
    const api = stubApi({ 'POST /api/auth/forgot-password': reply(202, { status: 'ok' }) })
    const { router } = renderApp('/forgot-password')
    await fill(/^Почта/, 'nobody@example.ru')
    await press('Отправить ссылку')

    expect(await screen.findByRole('heading', { level: 1, name: 'Проверьте почту' })).toBeInTheDocument()
    expect(screen.getByText(/Если аккаунт с адресом nobody@example.ru есть/)).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/forgot-password')[0].body).toEqual({ email: 'nobody@example.ru' })

    await press('Указать другой адрес')
    expect(screen.getByRole('heading', { level: 1, name: 'Забыли пароль?' })).toBeInTheDocument()
    await fill(/^Почта/, 'x')
    await press('Отправить ссылку')
    await screen.findByText(/Если аккаунт с адресом/)
    await userEvent.click(screen.getByRole('link', { name: 'Вернуться ко входу' }))
    expect(router.state.location.pathname).toBe('/login')
  })

  it('shows the link back to sign-in on the form too', () => {
    stubApi()
    renderApp('/forgot-password')
    expect(screen.getByRole('link', { name: 'Вернуться ко входу' })).toHaveAttribute('href', '/login')
  })

  it('reports trouble', async () => {
    stubApi({ 'POST /api/auth/forgot-password': apiError(429, 'rate_limited', 'x', { retry_after: 3000 }) })
    renderApp('/forgot-password')
    await fill(/^Почта/, 'anna@example.ru')
    await press('Отправить ссылку')
    expect(await screen.findByRole('alert')).toHaveTextContent('через 50 минут')
  })
})

describe('ResetPasswordPage', () => {
  it('needs the code from the mail', async () => {
    stubApi()
    renderApp('/reset-password')
    expect(screen.getByRole('heading', { level: 1, name: 'Ссылка неполная' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Запросить новую ссылку' })).toHaveAttribute('href', '/forgot-password')
  })

  it('checks the new password before sending it', async () => {
    const api = stubApi()
    renderApp('/reset-password?token=abc')
    await press('Сохранить пароль')
    expect(screen.getByText('Введите пароль')).toBeInTheDocument()
    await fill(/^Новый пароль/, 'коротко')
    await press('Сохранить пароль')
    expect(screen.getByText(/слишком короткий/)).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/reset-password')).toHaveLength(0)
  })

  it('saves the password and sends the person to sign in with a confirmation', async () => {
    const api = stubApi({ 'POST /api/auth/reset-password': reply(204) })
    const { router } = renderApp('/reset-password?token=abc_DEF-1')
    await fill(/^Новый пароль/, 'кофе на рассвете')
    await press('Сохранить пароль')

    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(api.called('POST', '/api/auth/reset-password')[0].body).toEqual({ token: 'abc_DEF-1', password: 'кофе на рассвете' })
    expect(await screen.findByText('Пароль изменён. Войдите с новым паролем.')).toBeInTheDocument()
  })

  it('shows the server\'s verdict on a weak password under the field', async () => {
    stubApi({
      'POST /api/auth/reset-password': apiError(422, 'validation_failed', 'x', { fields: { password: 'Этот пароль слишком простой' } }),
    })
    renderApp('/reset-password?token=abc')
    await fill(/^Новый пароль/, '1234567890')
    await press('Сохранить пароль')
    expect(await screen.findByText('Этот пароль слишком простой')).toBeInTheDocument()
    expect(field(/^Новый пароль/)).toHaveFocus()
  })

  it('explains a link that stopped working and offers a new one', async () => {
    stubApi({ 'POST /api/auth/reset-password': apiError(400, 'invalid_token', 'x') })
    renderApp('/reset-password?token=old')
    await fill(/^Новый пароль/, 'кофе на рассвете')
    await press('Сохранить пароль')
    expect(await screen.findByRole('heading', { level: 1, name: 'Ссылка больше не работает' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Запросить новую ссылку' })).toBeInTheDocument()
  })

  it('shows other server trouble above the form', async () => {
    stubApi({ 'POST /api/auth/reset-password': reply(500) })
    renderApp('/reset-password?token=abc')
    await fill(/^Новый пароль/, 'кофе на рассвете')
    await press('Сохранить пароль')
    expect(await screen.findByRole('alert')).toHaveTextContent('Сервер не отвечает')
  })
})

describe('ConfirmEmailPage', () => {
  it('confirms the address, signs the person in and shows who they are', async () => {
    const api = stubApi({ 'POST /api/auth/confirm-email': reply(200, { user: ann }) })
    renderApp('/confirm-email?token=abc')
    expect(screen.getByRole('status', { name: 'Подтверждаем почту…' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта подтверждена' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/confirm-email')[0].body).toEqual({ token: 'abc' })
    expect(await screen.findAllByText('Анна Смирнова')).not.toHaveLength(0)
    expect(screen.getByRole('link', { name: 'На главную' })).toHaveAttribute('href', '/')
  })

  it('uses the link only once even when the page is mounted twice (development mode)', async () => {
    const api = stubApi({ 'POST /api/auth/confirm-email': reply(200, { user: ann }) })
    const { render } = await import('@testing-library/react')
    const { App } = await import('../../app/App')
    const { createMemoryRouter } = await import('react-router')
    const { routes } = await import('../../app/routes')
    render(
      <StrictMode>
        <App router={createMemoryRouter(routes, { initialEntries: ['/confirm-email?token=abc'] })} />
      </StrictMode>,
    )
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта подтверждена' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/confirm-email')).toHaveLength(1)
  })

  it('offers a new link when the old one is dead, and then says to check the mail', async () => {
    const api = stubApi({
      'POST /api/auth/confirm-email': apiError(400, 'invalid_token', 'x'),
      'POST /api/auth/resend-confirmation': reply(202, { status: 'ok' }),
    })
    renderApp('/confirm-email?token=old')
    expect(await screen.findByRole('heading', { level: 1, name: 'Ссылка больше не работает' })).toBeInTheDocument()

    await press('Отправить новую ссылку')
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    await fill(/^Почта/, 'anna@example.ru')
    await press('Отправить новую ссылку')

    expect(await screen.findByRole('heading', { level: 1, name: 'Проверьте почту' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/resend-confirmation')[0].body).toEqual({ email: 'anna@example.ru' })
    expect(screen.getByRole('link', { name: 'Перейти ко входу' })).toHaveAttribute('href', '/login')
  })

  it('offers the same when there is no code in the address at all', async () => {
    const api = stubApi()
    renderApp('/confirm-email')
    expect(screen.getByRole('heading', { level: 1, name: 'Ссылка больше не работает' })).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/confirm-email')).toHaveLength(0)
  })

  it('shows trouble with a resend request on the form', async () => {
    stubApi({ 'POST /api/auth/resend-confirmation': apiError(429, 'rate_limited', 'x', { retry_after: 60 }) })
    renderApp('/confirm-email')
    await fill(/^Почта/, 'anna@example.ru')
    await press('Отправить новую ссылку')
    expect(await screen.findByRole('alert')).toHaveTextContent('через 1 минуту')
  })

  it('does not burn the person when the server is down: says the link is still good and retries', async () => {
    let attempts = 0
    stubApi({ 'POST /api/auth/confirm-email': () => (attempts++ === 0 ? reply(500) : reply(200, { user: ann })) })
    renderApp('/confirm-email?token=abc')
    expect(await screen.findByRole('heading', { name: 'Не удалось подтвердить почту' })).toBeInTheDocument()
    expect(screen.getByText(/Ссылка из письма не сгорела/)).toBeInTheDocument()
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Почта подтверждена' })).toBeInTheDocument()
  })

  it('shows a server message when the failure is not about the link', async () => {
    stubApi({ 'POST /api/auth/confirm-email': apiError(500, 'internal', 'Что-то сломалось на сервере') })
    renderApp('/confirm-email?token=abc')
    expect(await screen.findByText('Что-то сломалось на сервере')).toBeInTheDocument()
  })
})

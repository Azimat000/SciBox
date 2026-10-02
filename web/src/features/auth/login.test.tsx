import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'

async function fillValid() {
  await fill(/^Почта/, 'anna@example.ru')
  await fill(/^Пароль/, 'кофе на рассвете')
}

describe('LoginPage', () => {
  it('checks the fields before asking the server', async () => {
    const api = stubApi()
    renderApp('/login')
    await press('Войти')
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    expect(screen.getByText('Введите пароль')).toBeInTheDocument()
    expect(field(/^Почта/)).toHaveFocus()
    expect(api.called('POST', '/api/auth/login')).toHaveLength(0)
  })

  it('signs in, shows the person in the header and goes home', async () => {
    const api = stubApi({ 'POST /api/auth/login': reply(200, { user: ann }) })
    const { router } = renderApp('/login')
    await fillValid()
    await press('Войти')

    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(api.called('POST', '/api/auth/login')[0].body).toEqual({ email: 'anna@example.ru', password: 'кофе на рассвете' })
    expect(await screen.findAllByText('Анна Смирнова')).not.toHaveLength(0)
    // Данные о человеке уже в памяти: заново про него никто не спрашивает
    expect(api.called('GET', '/api/auth/me')).toHaveLength(1)
  })

  it('returns to the page the person came from, but never to another site', async () => {
    stubApi({ 'POST /api/auth/login': reply(200, { user: ann }) })
    const { router, unmount } = renderApp('/login?next=/account')
    await fillValid()
    await press('Войти')
    await waitFor(() => expect(router.state.location.pathname).toBe('/account'))
    unmount()

    stubApi({ 'POST /api/auth/login': reply(200, { user: ann }) })
    const second = renderApp('/login?next=https://evil.example')
    await fillValid()
    await press('Войти')
    await waitFor(() => expect(second.router.state.location.pathname).toBe('/'))
  })

  it('does not say which of the two was wrong', async () => {
    stubApi({ 'POST /api/auth/login': apiError(401, 'invalid_credentials', 'Почта или пароль указаны неверно') })
    renderApp('/login')
    await fillValid()
    await press('Войти')
    expect(await screen.findByRole('alert')).toHaveTextContent('Почта или пароль указаны неверно. Проверьте раскладку клавиатуры')
    // Поля не помечены ошибкой: нельзя угадать, где опечатка
    expect(field(/^Почта/)).not.toBeInvalid()
    expect(field(/^Пароль/)).not.toBeInvalid()
  })

  it('offers to resend the confirmation mail when the address is not confirmed yet', async () => {
    const api = stubApi({
      'POST /api/auth/login': apiError(403, 'email_not_confirmed', 'x'),
      'POST /api/auth/resend-confirmation': reply(202, { status: 'ok' }),
    })
    renderApp('/login')
    await fillValid()
    await press('Войти')
    expect(await screen.findByRole('alert')).toHaveTextContent('Почта ещё не подтверждена')
    await press('Отправить письмо ещё раз')
    expect(await screen.findByText('Запрос принят. Письмо придёт в течение минуты.')).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/resend-confirmation')[0].body).toEqual({ email: 'anna@example.ru' })
  })

  it('reports a failed resend instead of staying silent', async () => {
    stubApi({
      'POST /api/auth/login': apiError(403, 'email_not_confirmed', 'x'),
      'POST /api/auth/resend-confirmation': reply(500),
    })
    renderApp('/login')
    await fillValid()
    await press('Войти')
    await press('Отправить письмо ещё раз')
    expect(await screen.findAllByText('Сервер не отвечает')).not.toHaveLength(0)
  })

  it('tells how long to wait when there were too many attempts', async () => {
    stubApi({ 'POST /api/auth/login': apiError(429, 'rate_limited', 'x', { retry_after: 900 }) })
    renderApp('/login')
    await fillValid()
    await press('Войти')
    expect(await screen.findByRole('alert')).toHaveTextContent('Слишком много попыток. Попробуйте снова через 15 минут')
  })

  it('goes back to the form after an error and lets the person try again', async () => {
    let tries = 0
    stubApi({ 'POST /api/auth/login': () => (tries++ === 0 ? apiError(401, 'invalid_credentials', 'x') : reply(200, { user: ann })) })
    const { router } = renderApp('/login')
    await fillValid()
    await press('Войти')
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    await press('Войти')
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
  })

  it('shows the notices other pages ask for', async () => {
    stubApi()
    const { router } = renderApp('/login')
    await router.navigate('/login', { state: { notice: 'reset-done' } })
    expect(await screen.findByText('Пароль изменён. Войдите с новым паролем.')).toBeInTheDocument()
    await router.navigate('/login?x=1', { state: { notice: 'need-login' } })
    expect(await screen.findByText('Войдите, чтобы открыть эту страницу.')).toBeInTheDocument()
  })

  it('links to password recovery and registration', async () => {
    stubApi()
    const { router } = renderApp('/login')
    expect(screen.getByRole('link', { name: 'Забыли пароль?' })).toHaveAttribute('href', '/forgot-password')
    await userEvent.click(within(screen.getByRole('main')).getByRole('link', { name: 'Зарегистрироваться' }))
    expect(router.state.location.pathname).toBe('/register')
  })

  it('shows the other ways to sign in as disabled "soon" buttons', () => {
    stubApi()
    renderApp('/login')
    expect(screen.getByRole('heading', { name: 'Другие способы входа' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /ORCID/ })).toBeDisabled()
  })

  it('does not show the form to someone who is already signed in', async () => {
    stubApi(signedInAs())
    const { router } = renderApp('/login?next=/account')
    await waitFor(() => expect(router.state.location.pathname).toBe('/account'))
  })
})

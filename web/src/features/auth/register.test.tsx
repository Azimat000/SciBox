import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, signedInAs, stubApi } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'

async function fillValid() {
  await fill(/^Имя и фамилия/, '  Анна Смирнова ')
  await fill(/^Почта/, ' anna@example.ru ')
  await fill(/^Пароль/, 'кофе на рассвете')
  await userEvent.click(screen.getByRole('checkbox'))
}

describe('RegisterPage', () => {
  it('asks for everything, explains each problem and focuses the first one', async () => {
    const api = stubApi()
    renderApp('/register')
    await press('Создать аккаунт')

    expect(screen.getByText('Укажите, как вас зовут')).toBeInTheDocument()
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    expect(screen.getByText('Введите пароль')).toBeInTheDocument()
    expect(screen.getByText(/Без согласия на обработку персональных данных/)).toBeInTheDocument()
    expect(field(/^Имя и фамилия/)).toHaveFocus()
    expect(field(/^Имя и фамилия/)).toBeInvalid()
    expect(api.called('POST', '/api/auth/register')).toHaveLength(0)
  })

  it('removes a problem as soon as the person starts fixing that field', async () => {
    stubApi()
    renderApp('/register')
    await press('Создать аккаунт')
    expect(screen.getByText('Укажите почту')).toBeInTheDocument()
    await fill(/^Почта/, 'a')
    expect(screen.queryByText('Укажите почту')).not.toBeInTheDocument()
    expect(screen.getByText('Введите пароль')).toBeInTheDocument()
  })

  it('checks the address shape and password length in the browser', async () => {
    stubApi()
    renderApp('/register')
    await fill(/^Имя и фамилия/, 'Анна')
    await fill(/^Почта/, 'anna@')
    await fill(/^Пароль/, 'коротко')
    await press('Создать аккаунт')
    expect(screen.getByText(/опечатка/)).toBeInTheDocument()
    expect(screen.getByText(/слишком короткий/)).toBeInTheDocument()
  })

  it('registers and tells the person to check the mail', async () => {
    const api = stubApi({ 'POST /api/auth/register': reply(202, { email: 'anna@example.ru' }) })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')

    expect(await screen.findByRole('heading', { level: 1, name: 'Проверьте почту' })).toBeInTheDocument()
    expect(screen.getByText(/Мы отправили письмо на anna@example.ru/)).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/register')[0].body).toEqual({
      name: 'Анна Смирнова',
      email: 'anna@example.ru',
      password: 'кофе на рассвете',
      consent: true,
    })
  })

  it('can send the mail again and go back to correct the address', async () => {
    const api = stubApi({
      'POST /api/auth/register': reply(202, { email: 'anna@example.ru' }),
      'POST /api/auth/resend-confirmation': reply(202, { status: 'ok' }),
    })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')
    await press('Отправить письмо ещё раз')
    expect(await screen.findByText('Запрос принят. Письмо придёт в течение минуты.')).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/resend-confirmation')[0].body).toEqual({ email: 'anna@example.ru' })

    await press('Указать другой адрес')
    expect(screen.getByRole('heading', { level: 1, name: 'Создать аккаунт' })).toBeInTheDocument()
    expect(field(/^Почта/)).toHaveValue('anna@example.ru')
  })

  it('says so when the mail could not be requested again', async () => {
    stubApi({
      'POST /api/auth/register': reply(202, { email: 'anna@example.ru' }),
      'POST /api/auth/resend-confirmation': apiError(429, 'rate_limited', 'x', { retry_after: 600 }),
    })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')
    await press('Отправить письмо ещё раз')
    expect(await screen.findByText('Слишком много попыток. Попробуйте снова через 10 минут')).toBeInTheDocument()
  })

  it('puts the server\'s field messages next to the fields, and clears one when edited', async () => {
    stubApi({
      'POST /api/auth/register': apiError(422, 'validation_failed', 'Проверьте поля формы', {
        fields: { password: 'Этот пароль слишком простой', email: 'Адрес не подходит' },
      }),
    })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')

    expect(await screen.findByText('Этот пароль слишком простой')).toBeInTheDocument()
    expect(screen.getByText('Адрес не подходит')).toBeInTheDocument()
    expect(field(/^Почта/)).toHaveFocus()
    // Подробности только у полей, общего сообщения над формой нет
    expect(screen.queryByText('Проверьте отмеченные поля')).not.toBeInTheDocument()

    await fill(/^Почта/, 'x')
    expect(screen.queryByText('Адрес не подходит')).not.toBeInTheDocument()
    expect(screen.getByText('Этот пароль слишком простой')).toBeInTheDocument()
  })

  it('shows trouble that belongs to the whole form above the form', async () => {
    stubApi({ 'POST /api/auth/register': apiError(429, 'rate_limited', 'x') })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')
    expect(await screen.findByRole('alert')).toHaveTextContent('Слишком много попыток. Подождите немного и попробуйте снова')
  })

  it('reports an unreachable server', async () => {
    stubApi({ 'POST /api/auth/register': reply(500) })
    renderApp('/register')
    await fillValid()
    await press('Создать аккаунт')
    expect(await screen.findByRole('alert')).toHaveTextContent('Сервер не отвечает')
  })

  it('opens the privacy policy in a new tab without losing the form', () => {
    stubApi()
    renderApp('/register')
    const link = screen.getByRole('link', { name: 'политики конфиденциальности' })
    expect(link).toHaveAttribute('href', '/privacy')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('offers the other ways to sign in as "coming soon" and keeps them disabled', () => {
    stubApi()
    renderApp('/register')
    for (const name of ['ORCID', 'Яндекс ID', 'VK ID', 'Госуслуги']) {
      expect(screen.getByRole('button', { name: new RegExp(name) })).toBeDisabled()
    }
    expect(screen.getAllByText('скоро')).toHaveLength(4)
  })

  it('sends a signed-in person home instead of showing the form', async () => {
    stubApi(signedInAs())
    const { router } = renderApp('/register')
    await waitFor(() => expect(router.state.location.pathname).toBe('/'))
    expect(screen.queryByRole('heading', { name: 'Создать аккаунт' })).not.toBeInTheDocument()
  })

  it('links to sign-in for people who already have an account', async () => {
    stubApi()
    const { router } = renderApp('/register')
    await userEvent.click(within(screen.getByRole('main')).getByRole('link', { name: 'Войти' }))
    expect(router.state.location.pathname).toBe('/login')
  })
})

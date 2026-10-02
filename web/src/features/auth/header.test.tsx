import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, reply, signedInAs, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { renderApp } from '../../test/render'

const banner = () => screen.getAllByRole('banner')[0]
const menuButton = () => screen.findByRole('button', { name: /Меню аккаунта: Анна Смирнова/ })

describe('header while nobody is signed in', () => {
  it('offers sign-in and registration', async () => {
    stubApi()
    renderApp('/')
    expect(await within(banner()).findByRole('link', { name: 'Войти' })).toHaveAttribute('href', '/login')
    expect(within(banner()).getByRole('link', { name: 'Зарегистрироваться' })).toHaveAttribute('href', '/register')
    expect(screen.queryByRole('button', { name: /Меню аккаунта/ })).not.toBeInTheDocument()
  })

  it('offers sign-in when it cannot find out (the server is down)', async () => {
    stubApi({ 'GET /api/auth/me': reply(500) })
    renderApp('/')
    expect(await within(banner()).findByRole('link', { name: 'Войти' })).toBeInTheDocument()
  })

  it('shows the same two buttons in the mobile menu', async () => {
    stubApi()
    renderApp('/')
    await within(banner()).findByRole('link', { name: 'Войти' })
    await userEvent.click(screen.getByRole('button', { name: 'Открыть меню' }))
    const menu = document.getElementById('mobile-menu')!
    expect(within(menu).getByRole('link', { name: 'Войти' })).toBeInTheDocument()
    expect(within(menu).getByRole('link', { name: 'Зарегистрироваться' })).toBeInTheDocument()
  })

  it('shows nothing in the sign-in slot until it knows', () => {
    stubApi({ 'GET /api/auth/me': () => new Promise(() => {}) as never })
    renderApp('/')
    expect(within(banner()).queryByRole('link', { name: 'Войти' })).not.toBeInTheDocument()
    document.getElementById('mobile-menu')
    expect(document.querySelector('.header-auth-placeholder')).toBeInTheDocument()
    expect(document.querySelector('.mobile-menu-auth')).not.toBeInTheDocument()
  })
})

describe('user menu', () => {
  it('shows the name and opens with name, address and the two commands', async () => {
    stubApi(signedInAs())
    renderApp('/')
    const button = await menuButton()
    expect(button).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText('anna@example.ru', { selector: '.user-menu *' })).not.toBeVisible()

    await userEvent.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'true')
    const panel = document.getElementById('user-menu-panel')!
    expect(panel).not.toHaveAttribute('hidden')
    expect(within(panel).getByText('anna@example.ru')).toBeVisible()
    expect(within(panel).getByRole('link', { name: 'Настройки аккаунта' })).toHaveAttribute('href', '/account')
    expect(within(panel).getByRole('button', { name: 'Выйти' })).toBeInTheDocument()
  })

  it('closes with Escape and gives the focus back to the button', async () => {
    stubApi(signedInAs())
    renderApp('/')
    const button = await menuButton()
    await userEvent.click(button)
    await userEvent.keyboard('{Escape}')
    expect(button).toHaveAttribute('aria-expanded', 'false')
    expect(button).toHaveFocus()
  })

  it('ignores other keys', async () => {
    stubApi(signedInAs())
    renderApp('/')
    const button = await menuButton()
    await userEvent.click(button)
    await userEvent.keyboard('a')
    expect(button).toHaveAttribute('aria-expanded', 'true')
  })

  it('closes when the person clicks elsewhere, but not when they click inside it', async () => {
    stubApi(signedInAs())
    renderApp('/')
    const button = await menuButton()
    await userEvent.click(button)
    await userEvent.click(within(document.getElementById('user-menu-panel')!).getByText('anna@example.ru'))
    expect(button).toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(screen.getByRole('main'))
    expect(button).toHaveAttribute('aria-expanded', 'false')
  })

  it('toggles with the button and closes after following a link', async () => {
    stubApi(signedInAs())
    const { router } = renderApp('/')
    const button = await menuButton()
    await userEvent.click(button)
    await userEvent.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(button)
    await userEvent.click(screen.getByRole('link', { name: 'Настройки аккаунта' }))
    expect(router.state.location.pathname).toBe('/account')
    await waitFor(() => expect(screen.getByRole('button', { name: /Меню аккаунта/ })).toHaveAttribute('aria-expanded', 'false'))
  })

  it('signs out from the menu: the server forgets the session, the page forgets the person', async () => {
    const api = stubApi({ ...signedInAs(), 'POST /api/auth/logout': reply(204) })
    renderApp('/')
    await userEvent.click(await menuButton())
    await userEvent.click(within(document.getElementById('user-menu-panel')!).getByRole('button', { name: 'Выйти' }))

    expect(await screen.findByText('Вы вышли из аккаунта')).toBeInTheDocument()
    expect(api.called('POST', '/api/auth/logout')).toHaveLength(1)
    expect(await within(banner()).findByRole('link', { name: 'Войти' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Меню аккаунта/ })).not.toBeInTheDocument()
  })

  it('stays signed in and says so when sign-out fails', async () => {
    stubApi({ ...signedInAs(), 'POST /api/auth/logout': reply(500) })
    renderApp('/')
    await userEvent.click(await menuButton())
    await userEvent.click(within(document.getElementById('user-menu-panel')!).getByRole('button', { name: 'Выйти' }))
    expect(await screen.findByText('Не удалось выйти')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Меню аккаунта/ })).toBeInTheDocument()
  })

  it('copes with a very long name without breaking the header', async () => {
    const long = 'Александр Константинович Вишневецкий-Сорокопудов-Нижегородский'
    stubApi(signedInAs({ ...ann, name: long }))
    renderApp('/')
    const button = await screen.findByRole('button', { name: new RegExp(long) })
    expect(within(button).getByText(long)).toHaveClass('user-menu-name')
  })
})

describe('mobile menu for a signed-in person', () => {
  it('shows who is signed in with the same two commands', async () => {
    const api = stubApi({ ...signedInAs(), 'POST /api/auth/logout': reply(204) })
    renderApp('/')
    await menuButton()
    await userEvent.click(screen.getByRole('button', { name: 'Открыть меню' }))
    const menu = document.getElementById('mobile-menu')!
    expect(within(menu).getByText('Анна Смирнова')).toBeInTheDocument()
    expect(within(menu).getByRole('link', { name: 'Настройки аккаунта' })).toBeInTheDocument()
    expect(within(menu).queryByRole('link', { name: 'Войти' })).not.toBeInTheDocument()
    await press('Выйти')
    await screen.findByText('Вы вышли из аккаунта')
    expect(api.called('POST', '/api/auth/logout')).toHaveLength(1)
  })
})

describe('privacy policy', () => {
  it('is a real page with a visible draft notice and the version the server records', async () => {
    stubApi()
    renderApp('/privacy')
    expect(screen.getByRole('heading', { level: 1, name: 'Политика конфиденциальности' })).toBeInTheDocument()
    expect(screen.getByText('Версия 2026-10-draft')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Это черновик')
    expect(screen.getAllByRole('heading', { level: 2 }).length).toBeGreaterThanOrEqual(8)
    expect(screen.getByRole('heading', { name: 'Файлы cookie' })).toBeInTheDocument()
    expect(screen.getByText(/152-ФЗ/)).toBeInTheDocument()
  })
})

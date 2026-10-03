import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reply, stubApi } from '../../test/api'
import { renderApp } from '../../test/render'
import { navFor } from './nav'
import { parseRole, ROLE_STORAGE_KEY } from './role-context'

const healthy = { status: 'ok', product: 'SciBox', version: 'dev', database: { schema_version: 1, server_version: '16.15' } }

beforeEach(() => {
  window.localStorage.clear()
  stubApi({ 'GET /api/health': reply(200, healthy) })
})

const desktopNav = () => screen.getAllByRole('navigation', { name: 'Основное меню' })[0]

describe('parseRole and navFor', () => {
  it('falls back to seeker for anything unknown', () => {
    expect(parseRole('employer')).toBe('employer')
    expect(parseRole('seeker')).toBe('seeker')
    expect(parseRole(null)).toBe('seeker')
    expect(parseRole('admin')).toBe('seeker')
  })

  it('gives each role its own menu', () => {
    expect(navFor('seeker').map((i) => i.label)).toEqual(['Вакансии', 'Учёные', 'Организации', 'Избранное', 'Мои отклики'])
    expect(navFor('employer').map((i) => i.label)).toEqual(['Мои вакансии', 'Отклики', 'Каталог учёных', 'Организация'])
  })
})

describe('site shell', () => {
  it('renders header, main area, skip link and footer', async () => {
    renderApp('/')
    expect(screen.getByRole('link', { name: 'К содержимому' })).toHaveAttribute('href', '#main')
    expect(screen.getByRole('banner')).toBeInTheDocument()
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('contentinfo')).toHaveTextContent('Сервис бесплатный')
    expect(within(screen.getByRole('contentinfo')).getByRole('link', { name: 'Политика конфиденциальности' })).toHaveAttribute('href', '/privacy')
    expect((await screen.findAllByRole('link', { name: 'Войти', hidden: true }))[0]).toHaveAttribute('href', '/login')
    expect(screen.getAllByRole('link', { name: 'Зарегистрироваться', hidden: true })[0]).toHaveAttribute('href', '/register')
  })

  it('shows the seeker menu and marks the current page', async () => {
    renderApp('/vacancies')
    const link = within(desktopNav()).getByRole('link', { name: 'Вакансии' })
    expect(link).toHaveAttribute('aria-current', 'page')
    expect(within(desktopNav()).getByRole('link', { name: 'Учёные' })).not.toHaveAttribute('aria-current')
    expect(await screen.findByRole('heading', { level: 1, name: 'Вакансии в науке' })).toBeInTheDocument()
  })

  it('switches to the employer menu, remembers the choice and restores it', async () => {
    const first = renderApp('/')
    const switcher = screen.getAllByRole('group', { name: 'Режим' })[0]
    expect(within(switcher).getByRole('button', { name: 'Ищу работу' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(within(switcher).getByRole('button', { name: 'Нанимаю' }))
    expect(within(switcher).getByRole('button', { name: 'Нанимаю' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(desktopNav()).getByRole('link', { name: 'Мои вакансии' })).toBeInTheDocument()
    expect(window.localStorage.getItem(ROLE_STORAGE_KEY)).toBe('employer')
    first.unmount()

    renderApp('/')
    expect(within(desktopNav()).getByRole('link', { name: 'Мои вакансии' })).toBeInTheDocument()
  })

  it('works when browser storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    renderApp('/')
    expect(within(desktopNav()).getByRole('link', { name: 'Вакансии' })).toBeInTheDocument()
    await userEvent.click(screen.getAllByRole('button', { name: 'Нанимаю' })[0])
    expect(within(desktopNav()).getByRole('link', { name: 'Отклики' })).toBeInTheDocument()
  })

  it('opens and closes the mobile menu with the button and with Escape', async () => {
    renderApp('/')
    const menu = document.getElementById('mobile-menu')!
    const toggle = screen.getByRole('button', { name: 'Открыть меню' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(menu).toHaveAttribute('hidden')

    await userEvent.click(toggle)
    expect(screen.getByRole('button', { name: 'Закрыть меню' })).toHaveAttribute('aria-expanded', 'true')
    expect(menu).not.toHaveAttribute('hidden')

    await userEvent.keyboard('{Escape}')
    expect(menu).toHaveAttribute('hidden')

    await userEvent.click(screen.getByRole('button', { name: 'Открыть меню' }))
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть меню' }))
    expect(menu).toHaveAttribute('hidden')
  })

  it('ignores other keys while the mobile menu is open', async () => {
    renderApp('/')
    await userEvent.click(screen.getByRole('button', { name: 'Открыть меню' }))
    await userEvent.keyboard('a')
    expect(screen.getByRole('button', { name: 'Закрыть меню' })).toBeInTheDocument()
  })

  it('closes the mobile menu after following a link', async () => {
    renderApp('/')
    await userEvent.click(screen.getByRole('button', { name: 'Открыть меню' }))
    const mobileNav = screen.getAllByRole('navigation', { name: 'Основное меню' })[1]
    await userEvent.click(within(mobileNav).getByRole('link', { name: 'Учёные' }))
    expect(await screen.findByRole('heading', { name: 'Раздел готовится' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Открыть меню' })).toHaveAttribute('aria-expanded', 'false')
    expect(document.getElementById('mobile-menu')).toHaveAttribute('hidden')
  })
})

describe('placeholder pages', () => {
  it.each(['/scientists', '/favorites', '/candidates'])(
    '%s says the section is coming',
    async (path) => {
      renderApp(path)
      expect(await screen.findByRole('heading', { level: 2, name: 'Раздел готовится' })).toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'На главную' })).toHaveAttribute('href', '/')
    },
  )

  it('unknown address shows the 404 page inside the shell', async () => {
    renderApp('/nothing-here')
    expect(await screen.findByRole('heading', { name: 'Такой страницы нет' })).toBeInTheDocument()
    expect(screen.getByRole('banner')).toBeInTheDocument()
  })
})

describe('useRole', () => {
  it('refuses to work outside the provider', async () => {
    const { renderHook } = await import('@testing-library/react')
    const { useRole } = await import('./useRole')
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => renderHook(() => useRole())).toThrow('RoleProvider')
    error.mockRestore()
  })
})

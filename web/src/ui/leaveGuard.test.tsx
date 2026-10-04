import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { createMemoryRouter, Link, RouterProvider, useNavigate } from 'react-router'
import { describe, expect, it } from 'vitest'
import { useLeaveGuard } from './useLeaveGuard'

function Draft() {
  const [text, setText] = useState('')
  const navigate = useNavigate()
  const guard = useLeaveGuard(text !== '')
  return (
    <div>
      {guard.prompt}
      <label>
        Письмо
        <textarea value={text} onChange={(e) => setText(e.target.value)} />
      </label>
      <Link to="/elsewhere">Уйти по ссылке</Link>
      <Link to="/draft?tab=2">Та же страница</Link>
      <button
        type="button"
        onClick={() => {
          guard.release()
          void navigate('/saved')
        }}
      >
        Сохранить
      </button>
    </div>
  )
}

function setup() {
  const router = createMemoryRouter(
    [
      { path: '/draft', element: <Draft /> },
      { path: '/elsewhere', element: <h1>Другая страница</h1> },
      { path: '/saved', element: <h1>Сохранено</h1> },
    ],
    { initialEntries: ['/draft'] },
  )
  render(<RouterProvider router={router} />)
  return { router, user: userEvent.setup() }
}

describe('useLeaveGuard', () => {
  it('lets you leave freely while nothing is typed', async () => {
    const { user } = setup()
    await user.click(screen.getByRole('link', { name: 'Уйти по ссылке' }))
    expect(screen.getByRole('heading', { name: 'Другая страница' })).toBeInTheDocument()
  })

  it('asks before leaving a started form; «Остаться» keeps the text', async () => {
    const { user, router } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Начало письма')
    await user.click(screen.getByRole('link', { name: 'Уйти по ссылке' }))

    const dialog = screen.getByRole('dialog', { name: 'Уйти без сохранения?' })
    expect(dialog).toHaveTextContent('введённое пропадёт')
    await user.click(screen.getByRole('button', { name: 'Остаться' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/draft')
    expect(screen.getByLabelText('Письмо')).toHaveValue('Начало письма')
  })

  it('«Уйти» goes where the person was heading', async () => {
    const { user } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Черновик')
    await user.click(screen.getByRole('link', { name: 'Уйти по ссылке' }))
    await user.click(screen.getByRole('button', { name: 'Уйти' }))
    expect(screen.getByRole('heading', { name: 'Другая страница' })).toBeInTheDocument()
  })

  it('Esc in the question means «stay»', async () => {
    const { user, router } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Черновик')
    await user.click(screen.getByRole('link', { name: 'Уйти по ссылке' }))
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/draft')
  })

  it('does not ask when only the address query changes', async () => {
    const { user, router } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Черновик')
    await user.click(screen.getByRole('link', { name: 'Та же страница' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.state.location.search).toBe('?tab=2')
  })

  it('does not ask after release (the form was saved)', async () => {
    const { user } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Готовое письмо')
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Сохранено' })).toBeInTheDocument()
  })

  it('asks the browser to confirm closing the tab only while the form is started', async () => {
    const { user } = setup()
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)

    await user.type(screen.getByLabelText('Письмо'), 'Черновик')
    const started = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(started)
    expect(started.defaultPrevented).toBe(true)
  })

  it('stops asking the browser after release', async () => {
    const { user } = setup()
    await user.type(screen.getByLabelText('Письмо'), 'Черновик')
    await user.click(screen.getByRole('button', { name: 'Сохранить' }))
    const after = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })
})

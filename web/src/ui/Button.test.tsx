import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { Button, ButtonLink } from './Button'

describe('Button', () => {
  it('renders a primary button by default and fires onClick', async () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Откликнуться</Button>)
    const button = screen.getByRole('button', { name: 'Откликнуться' })
    expect(button).toHaveClass('btn', 'btn-primary')
    expect(button).toHaveAttribute('type', 'button')
    await userEvent.click(button)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('supports variants and the small size', () => {
    render(
      <Button variant="danger" size="sm" className="extra">
        Удалить
      </Button>,
    )
    expect(screen.getByRole('button')).toHaveClass('btn-danger', 'btn-sm', 'extra')
  })

  it('is disabled and announces busy state while loading', async () => {
    const onClick = vi.fn()
    render(
      <Button loading onClick={onClick}>
        Отправляем
      </Button>,
    )
    const button = screen.getByRole('button')
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText('Подождите…')).toBeInTheDocument()
    await userEvent.click(button)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('does not fire when disabled', async () => {
    const onClick = vi.fn()
    render(
      <Button disabled onClick={onClick}>
        Нельзя
      </Button>,
    )
    await userEvent.click(screen.getByRole('button'))
    expect(onClick).not.toHaveBeenCalled()
  })

  it('can submit a form', () => {
    render(<Button type="submit">Отправить</Button>)
    expect(screen.getByRole('button')).toHaveAttribute('type', 'submit')
  })
})

describe('ButtonLink', () => {
  it('renders a link that looks like a button', () => {
    const router = createMemoryRouter([{ path: '/', element: <ButtonLink to="/x" variant="secondary" size="sm">Дальше</ButtonLink> }])
    render(<RouterProvider router={router} />)
    const link = screen.getByRole('link', { name: 'Дальше' })
    expect(link).toHaveAttribute('href', '/x')
    expect(link).toHaveClass('btn-secondary', 'btn-sm')
  })
})

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { Alert } from './Alert'
import { Checkbox } from './Checkbox'
import { PasswordField } from './PasswordField'

describe('Alert', () => {
  it('announces errors immediately and other kinds politely', () => {
    const { rerender } = render(<Alert kind="error">Не вышло</Alert>)
    expect(screen.getByRole('alert')).toHaveTextContent('Не вышло')
    rerender(<Alert kind="success">Готово</Alert>)
    expect(screen.getByRole('status')).toHaveTextContent('Готово')
    rerender(<Alert>Заметьте</Alert>)
    expect(screen.getByRole('status')).toHaveAttribute('data-kind', 'info')
  })

  it('shows an optional title and action', () => {
    render(
      <Alert kind="error" title="Заголовок" action={<button type="button">Исправить</button>}>
        Текст
      </Alert>,
    )
    expect(screen.getByText('Заголовок')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Исправить' })).toBeInTheDocument()
  })

  it('renders with nothing but a title', () => {
    render(<Alert title="Только заголовок" />)
    expect(screen.getByRole('status')).toHaveTextContent('Только заголовок')
  })
})

describe('Checkbox', () => {
  function Harness({ error }: { error?: string }) {
    const [on, setOn] = useState(false)
    return <Checkbox label="Согласен" checked={on} onChange={(e) => setOn(e.target.checked)} error={error} />
  }

  it('toggles from the label as well as the box', async () => {
    render(<Harness />)
    const box = screen.getByRole('checkbox', { name: 'Согласен' })
    expect(box).not.toBeChecked()
    await userEvent.click(screen.getByText('Согласен'))
    expect(box).toBeChecked()
    await userEvent.click(box)
    expect(box).not.toBeChecked()
  })

  it('links the error to the box and marks it invalid', () => {
    render(<Harness error="Нужно согласие" />)
    const box = screen.getByRole('checkbox')
    expect(box).toBeInvalid()
    expect(box).toHaveAccessibleDescription('Нужно согласие')
  })

  it('has no error wiring when there is no error', () => {
    render(<Harness />)
    const box = screen.getByRole('checkbox')
    expect(box).not.toHaveAttribute('aria-invalid')
    expect(box).not.toHaveAttribute('aria-describedby')
  })
})

describe('PasswordField', () => {
  it('hides the password by default and shows it on demand', async () => {
    render(<PasswordField label="Пароль" defaultValue="секрет" />)
    const input = screen.getByLabelText('Пароль')
    expect(input).toHaveAttribute('type', 'password')
    const toggle = screen.getByRole('button', { name: 'Показать пароль' })
    expect(toggle).toHaveAttribute('aria-pressed', 'false')
    expect(toggle).toHaveTextContent('Показать')
    await userEvent.click(toggle)
    expect(input).toHaveAttribute('type', 'text')
    expect(screen.getByRole('button', { name: 'Скрыть пароль' })).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(screen.getByRole('button', { name: 'Скрыть пароль' }))
    expect(input).toHaveAttribute('type', 'password')
  })

  it('shows a hint and an error and turns off autocorrection', () => {
    render(<PasswordField label="Пароль" hint="Не меньше 10 знаков" error="Слишком короткий" required />)
    const input = screen.getByLabelText(/Пароль/)
    expect(input).toBeInvalid()
    expect(input).toHaveAccessibleDescription('Слишком короткий Не меньше 10 знаков')
    expect(input).toHaveAttribute('autocapitalize', 'none')
    expect(input).toHaveAttribute('spellcheck', 'false')
  })
})

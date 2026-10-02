import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { Field } from './Field'
import { Select } from './Select'
import { TextArea, TextField } from './TextField'

describe('Field', () => {
  it('links label, hint and error to the control', () => {
    render(
      <Field label="Почта" hint="Рабочая" error="Опечатка" required>
        {(c) => <input {...c} />}
      </Field>,
    )
    const input = screen.getByLabelText(/Почта/)
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAttribute('aria-required', 'true')
    expect(input).toHaveAccessibleDescription('Опечатка Рабочая')
    expect(screen.getByText(/обязательно/)).toBeInTheDocument()
  })

  it('has no description or flags when none are given', () => {
    render(<Field label="Имя">{(c) => <input {...c} />}</Field>)
    const input = screen.getByLabelText('Имя')
    expect(input).not.toHaveAttribute('aria-describedby')
    expect(input).not.toHaveAttribute('aria-invalid')
    expect(screen.queryByText(/обязательно/)).not.toBeInTheDocument()
  })

  it('marks optional fields, unless they are required', () => {
    const { rerender } = render(
      <Field label="Отчество" optional>
        {(c) => <input {...c} />}
      </Field>,
    )
    expect(screen.getByText(/необязательно/)).toBeInTheDocument()
    rerender(
      <Field label="Отчество" optional required>
        {(c) => <input {...c} />}
      </Field>,
    )
    expect(screen.queryByText(/необязательно/)).not.toBeInTheDocument()
  })
})

describe('TextField and TextArea', () => {
  it('accepts typing', async () => {
    render(<TextField label="Название" placeholder="Например" />)
    const input = screen.getByLabelText('Название')
    await userEvent.type(input, 'Постдок')
    expect(input).toHaveValue('Постдок')
  })

  it('shows an error and keeps the control reachable', () => {
    render(<TextField label="Почта" error="Нет @" defaultValue="ivanov" />)
    expect(screen.getByLabelText('Почта')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText('Нет @')).toBeInTheDocument()
  })

  it('passes required to the native input', () => {
    render(<TextField label="Имя" required />)
    expect(screen.getByLabelText(/Имя/)).toBeRequired()
  })

  it('renders a textarea with a row count', async () => {
    render(<TextArea label="Письмо" rows={8} optional />)
    const area = screen.getByLabelText(/Письмо/)
    expect(area.tagName).toBe('TEXTAREA')
    expect(area).toHaveAttribute('rows', '8')
    await userEvent.type(area, 'Здравствуйте')
    expect(area).toHaveValue('Здравствуйте')
  })

  it('uses 5 rows by default and supports required', () => {
    render(<TextArea label="Письмо" required />)
    expect(screen.getByLabelText(/Письмо/)).toHaveAttribute('rows', '5')
    expect(screen.getByLabelText(/Письмо/)).toBeRequired()
  })
})

describe('Select', () => {
  const options = [
    { value: 'a', label: 'Очно' },
    { value: 'b', label: 'Гибрид' },
  ]

  it('lists options and changes value', async () => {
    render(<Select label="Формат" options={options} />)
    const select = screen.getByLabelText('Формат')
    await userEvent.selectOptions(select, 'Гибрид')
    expect(select).toHaveValue('b')
    expect(screen.queryByRole('option', { name: 'Не выбрано' })).not.toBeInTheDocument()
  })

  it('adds an empty option with the default text', () => {
    render(<Select label="Формат" options={options} placeholder="" />)
    expect(screen.getByRole('option', { name: 'Не выбрано' })).toHaveValue('')
  })

  it('uses custom placeholder text and error', () => {
    render(<Select label="Формат" options={options} placeholder="Выберите" error="Нужно выбрать" hint="Подсказка" required />)
    expect(screen.getByRole('option', { name: 'Выберите' })).toBeInTheDocument()
    expect(screen.getByLabelText(/Формат/)).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText(/Формат/)).toBeRequired()
  })
})

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Combobox } from './Combobox'

const options = [
  { value: 'msk', label: 'Москва' },
  { value: 'spb', label: 'Санкт-Петербург' },
  { value: 'yol', label: 'Йошкар-Ола' },
  { value: 'tsk', label: 'Томск' },
]

function Harness({ initial = null, onChange }: { initial?: string | null; onChange?: (v: string | null) => void }) {
  const [value, setValue] = useState<string | null>(initial)
  return (
    <Combobox
      label="Город"
      options={options}
      value={value}
      onChange={(v) => {
        setValue(v)
        onChange?.(v)
      }}
      placeholder="Начните вводить"
      hint="Список сократится"
      error={undefined}
    />
  )
}

const input = () => screen.getByRole('combobox', { name: /^Город/ })

describe('Combobox', () => {
  it('starts closed, with the selected label in the field', () => {
    render(<Harness initial="tsk" />)
    expect(input()).toHaveValue('Томск')
    expect(input()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('filters while typing, ignoring case and ё/е', async () => {
    render(<Harness />)
    await userEvent.type(input(), 'йош')
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { name: 'Йошкар-Ола' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Найдено вариантов: 1')
    await userEvent.clear(input())
    await userEvent.type(input(), 'ТОМ')
    expect(screen.getAllByRole('option')).toHaveLength(1)
  })

  it('says when nothing matches', async () => {
    render(<Harness />)
    await userEvent.type(input(), 'яяя')
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(screen.getAllByText('Ничего не нашлось').length).toBeGreaterThan(0)
    expect(input()).not.toHaveAttribute('aria-activedescendant')
    await userEvent.keyboard('{Enter}')
    expect(input()).toHaveValue('яяя')
  })

  it('picks with the mouse', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    await userEvent.click(input())
    await userEvent.type(input(), 'м')
    await userEvent.click(screen.getByRole('option', { name: 'Москва' }))
    expect(onChange).toHaveBeenLastCalledWith('msk')
    expect(input()).toHaveValue('Москва')
    expect(input()).toHaveAttribute('aria-expanded', 'false')
  })

  it('navigates and picks with the keyboard', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    input().focus()
    await userEvent.keyboard('{ArrowDown}')
    expect(input()).toHaveAttribute('aria-expanded', 'true')
    expect(input()).toHaveAttribute('aria-activedescendant', screen.getByRole('option', { name: 'Москва' }).id)
    await userEvent.keyboard('{ArrowDown}{ArrowDown}')
    expect(screen.getByRole('option', { name: 'Йошкар-Ола' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('option', { name: 'Санкт-Петербург' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{End}')
    expect(screen.getByRole('option', { name: 'Томск' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('option', { name: 'Томск' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{Home}')
    expect(screen.getByRole('option', { name: 'Москва' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('option', { name: 'Москва' })).toHaveAttribute('data-active')
    await userEvent.keyboard('{End}{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('tsk')
    expect(input()).toHaveValue('Томск')
  })

  it('opens with ArrowUp and ignores Home/End/Enter while closed', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    input().focus()
    await userEvent.keyboard('{Home}{End}{Enter}')
    expect(onChange).not.toHaveBeenCalled()
    expect(input()).toHaveAttribute('aria-expanded', 'false')
    await userEvent.keyboard('{ArrowUp}')
    expect(input()).toHaveAttribute('aria-expanded', 'true')
  })

  it('Escape closes and restores the selected text; other keys do nothing special', async () => {
    render(<Harness initial="msk" />)
    input().focus()
    await userEvent.keyboard('{Escape}')
    expect(input()).toHaveValue('Москва')
    await userEvent.type(input(), 'ква')
    expect(input()).toHaveAttribute('aria-expanded', 'true')
    await userEvent.keyboard('{Escape}')
    expect(input()).toHaveAttribute('aria-expanded', 'false')
    expect(input()).toHaveValue('Москва')
  })

  it('clearing the field clears the selection', async () => {
    const onChange = vi.fn()
    render(<Harness initial="msk" onChange={onChange} />)
    await userEvent.clear(input())
    expect(onChange).toHaveBeenLastCalledWith(null)
  })

  it('clearing an already empty field does not report a change', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    await userEvent.type(input(), 'а')
    await userEvent.clear(input())
    expect(onChange).not.toHaveBeenCalled()
  })

  it('closes on blur and drops unfinished text', async () => {
    render(
      <>
        <Harness initial="spb" />
        <button type="button">Дальше</button>
      </>,
    )
    await userEvent.type(input(), 'xx')
    await userEvent.click(screen.getByRole('button', { name: 'Дальше' }))
    expect(input()).toHaveValue('Санкт-Петербург')
    expect(input()).toHaveAttribute('aria-expanded', 'false')
  })

  it('marks the selected option', async () => {
    render(<Harness initial="spb" />)
    await userEvent.click(screen.getByRole('button', { name: 'Показать варианты' }))
    expect(screen.getByRole('option', { name: 'Санкт-Петербург' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('option', { name: 'Москва' })).toHaveAttribute('aria-selected', 'false')
  })

  it('toggle button opens and closes the list', async () => {
    render(<Harness />)
    const toggle = screen.getByRole('button', { name: 'Показать варианты' })
    await userEvent.click(toggle)
    expect(input()).toHaveAttribute('aria-expanded', 'true')
    expect(input()).toHaveFocus()
    await userEvent.click(toggle)
    expect(input()).toHaveAttribute('aria-expanded', 'false')
  })

  it('hovering an option makes it active', async () => {
    render(<Harness />)
    await userEvent.click(input())
    await userEvent.keyboard('{ArrowDown}')
    await userEvent.hover(screen.getByRole('option', { name: 'Томск' }))
    expect(screen.getByRole('option', { name: 'Томск' })).toHaveAttribute('data-active')
  })

  it('shows the whole list when the typed text is blank spaces', async () => {
    render(<Harness />)
    await userEvent.type(input(), ' ')
    expect(screen.getAllByRole('option')).toHaveLength(4)
  })

  it('shows hint, error and required flag', () => {
    render(<Combobox label="Город" options={options} value={null} onChange={() => {}} error="Выберите город" required optional />)
    expect(input()).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText('Выберите город')).toBeInTheDocument()
  })
})

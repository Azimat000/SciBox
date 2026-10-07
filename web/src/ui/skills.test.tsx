import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { SkillsInput } from './SkillsInput'
import { addSkills } from './skills'

function Harness({ start = [], max = 3 }: { start?: string[]; max?: number }) {
  const [value, setValue] = useState<string[]>(start)
  return <SkillsInput label="Навыки" hint="По одному" placeholder="Например, Excel" value={value} onChange={setValue} max={max} maxLength={60} />
}

describe('skills input', () => {
  it('adds phrases without blanks and repeats and never past the limit', () => {
    expect(addSkills(['Excel'], ['  word  ', '', 'EXCEL', 'Word', 'R', 'SQL'], 3)).toEqual(['Excel', 'word', 'R'])
    expect(addSkills(['a', 'b'], ['c'], 2)).toEqual(['a', 'b'])
  })

  it('locks the field when the list is full and opens it again after a removal', async () => {
    render(<Harness start={['Excel', 'Word']} />)
    const input = screen.getByLabelText(/^Навыки ·/)
    await userEvent.type(input, 'R{Enter}')
    expect(input).toBeDisabled()
    expect(input).toHaveAttribute('placeholder', 'Список полон: уберите что-нибудь, чтобы добавить новое')
    expect(screen.getByText('3 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить' })).toBeDisabled()
    await userEvent.click(screen.getByRole('button', { name: 'Убрать навык «Word»' }))
    expect(input).toBeEnabled()
    expect(input).toHaveAttribute('placeholder', 'Например, Excel')
  })

  it('waits for the word while a keyboard is composing it, and ignores an empty entry', async () => {
    render(<Harness />)
    const input = screen.getByLabelText(/^Навыки ·/)
    await userEvent.type(input, 'Python')
    fireEvent.keyDown(input, { key: 'Enter', isComposing: true })
    expect(input).toHaveValue('Python')
    await userEvent.clear(input)
    await userEvent.type(input, '   {Enter}')
    fireEvent.blur(input)
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить' })).toBeDisabled()
  })
})

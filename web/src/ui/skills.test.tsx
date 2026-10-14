import { QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { createQueryClient } from '../app/queryClient'
import { SkillsInput } from './SkillsInput'
import { addSkills, skillKey, type SkillSearch, type SkillSuggestion } from './skills'

function Harness({ start = [], max = 3, search }: { start?: string[]; max?: number; search?: SkillSearch }) {
  const [value, setValue] = useState<string[]>(start)
  return (
    <QueryClientProvider client={createQueryClient()}>
      <SkillsInput label="Навыки" hint="По одному" placeholder="Например, Excel" value={value} onChange={setValue} max={max} maxLength={60} search={search} searchKey="t" />
    </QueryClientProvider>
  )
}

const pcr: SkillSuggestion = { name: 'ПЦР', group: 'Методы', via: 'Полимеразная цепная реакция' }
const rt: SkillSuggestion = { name: 'ПЦР в реальном времени', group: 'Методы' }
const chips = () => screen.queryAllByRole('listitem').map((li) => li.textContent)
const input = () => screen.getByRole('combobox')

describe('skills input', () => {
  it('adds phrases without blanks and repeats and never past the limit', () => {
    expect(addSkills(['Excel'], ['  word  ', '', 'EXCEL', 'Word', 'R', 'SQL'], 3)).toEqual(['Excel', 'word', 'R'])
    expect(addSkills(['a', 'b'], ['c'], 2)).toEqual(['a', 'b'])
    expect(addSkills(['Бухгалтерский учёт'], ['бухгалтерский учет', 'Масс‑спектрометрия', 'масс-спектрометрия'], 5)).toEqual(['Бухгалтерский учёт', 'Масс‑спектрометрия'])
    expect(skillKey('  Ёлка ‑ Б ')).toBe('елка - б')
  })

  it('locks the field when the list is full and opens it again after a removal', async () => {
    render(<Harness start={['Excel', 'Word']} />)
    const field = screen.getByLabelText(/^Навыки ·/)
    await userEvent.type(field, 'R{Enter}')
    expect(field).toBeDisabled()
    expect(field).toHaveAttribute('placeholder', 'Список полон: уберите что-нибудь, чтобы добавить новое')
    expect(screen.getByText('3 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить' })).toBeDisabled()
    await userEvent.click(screen.getByRole('button', { name: 'Убрать навык «Word»' }))
    expect(field).toBeEnabled()
    expect(field).toHaveAttribute('placeholder', 'Например, Excel')
  })

  it('waits for the word while a keyboard is composing it, and ignores an empty entry', async () => {
    render(<Harness />)
    const field = screen.getByLabelText(/^Навыки ·/)
    await userEvent.type(field, 'Python')
    fireEvent.keyDown(field, { key: 'Enter', isComposing: true })
    expect(field).toHaveValue('Python')
    await userEvent.clear(field)
    await userEvent.type(field, '   {Enter}')
    fireEvent.blur(field)
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить' })).toBeDisabled()
  })
})

describe('skills suggestions', () => {
  const reply = (items: SkillSuggestion[], fuzzy = false) => vi.fn<SkillSearch>(async () => ({ items, fuzzy }))

  it('shows the dictionary under the field with the group, the synonym and the «add your own» line', async () => {
    const search = reply([pcr, rt])
    render(<Harness search={search} />)
    await userEvent.type(input(), 'полимераз')
    const options = await screen.findAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['ПЦР«Полимеразная цепная реакция» · Методы', 'ПЦР в реальном времениМетоды', 'Не нашли? Добавить «полимераз»'])
    expect(input()).toHaveAttribute('aria-expanded', 'true')
    expect(search).toHaveBeenCalledTimes(1)
    expect(search.mock.calls[0][0]).toBe('полимераз')
  })

  it('picks a suggestion with the mouse and puts its canonical name into the list', async () => {
    render(<Harness search={reply([pcr])} />)
    await userEvent.type(input(), 'полимер')
    await userEvent.click(await screen.findByRole('option', { name: /^ПЦР/ }))
    expect(chips()).toEqual(['ПЦР'])
    expect(input()).toHaveValue('')
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('walks the list with arrows, picks by Enter, closes by Escape, and Enter without a choice adds the typed text', async () => {
    render(<Harness search={reply([pcr, rt])} max={5} />)
    await userEvent.type(input(), 'цепная')
    await screen.findAllByRole('option')
    await userEvent.keyboard('{ArrowDown}')
    // первая стрелка при открытом списке выбирает первую строку
    expect(input()).toHaveAttribute('aria-activedescendant', screen.getAllByRole('option')[0].id)
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}')
    expect(input()).toHaveAttribute('aria-activedescendant', screen.getAllByRole('option')[2].id)
    await userEvent.keyboard('{ArrowUp}{Enter}')
    expect(chips()).toEqual(['ПЦР в реальном времени'])

    await userEvent.type(input(), 'мой метод')
    await screen.findAllByRole('option')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
    expect(input()).toHaveValue('мой метод')
    await userEvent.keyboard('{Enter}')
    expect(chips()).toEqual(['ПЦР в реальном времени', 'мой метод'])
  })

  it('«Не нашли? Добавить» adds the typed text as it is', async () => {
    render(<Harness search={reply([])} />)
    await userEvent.type(input(), 'Криоэлектронная томография')
    expect(await screen.findByText('В справочнике такого нет. Можно добавить как есть.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', { name: 'Не нашли? Добавить «Криоэлектронная томография»' }))
    expect(chips()).toEqual(['Криоэлектронная томография'])
  })

  it('does not offer what is already in the list and hides «add» for an exact name', async () => {
    render(<Harness start={['ПЦР']} search={reply([pcr, rt])} />)
    await userEvent.type(input(), 'ПЦР в реальном времени')
    const options = await screen.findAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['ПЦР в реальном времениМетоды'])
    await userEvent.click(options[0])
    expect(chips()).toEqual(['ПЦР', 'ПЦР в реальном времени'])
    // навык уже в списке: ни подсказки, ни «Добавить»
    await userEvent.type(input(), 'пцр')
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveAttribute('aria-expanded', 'false'))
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('turns a typed name or synonym into the dictionary name when Enter is pressed', async () => {
    render(<Harness search={reply([{ name: 'Microsoft Excel', group: 'Офис', via: 'Excel' }])} />)
    await userEvent.type(input(), 'excel')
    await screen.findByRole('option', { name: /Microsoft Excel/ })
    await userEvent.keyboard('{Enter}')
    expect(chips()).toEqual(['Microsoft Excel'])
  })

  it('says it when the dictionary offers similar spellings, or cannot be reached, and still lets the person add the text', async () => {
    render(<Harness search={reply([{ name: 'Python', group: 'Программы' }], true)} />)
    await userEvent.type(input(), 'pyton')
    expect(await screen.findByText('Точных совпадений нет. Возможно, вы искали:')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', { name: /^Python/ }))
    expect(chips()).toEqual(['Python'])
  })

  it('stays usable when the suggestions fail', async () => {
    const failing = vi.fn<SkillSearch>(async () => {
      throw new Error('boom')
    })
    render(<Harness search={failing} />)
    await userEvent.type(input(), 'ПЦР')
    expect(await screen.findByText('Подсказки сейчас недоступны. Навык можно добавить как есть.')).toBeInTheDocument()
    await userEvent.keyboard('{Enter}')
    expect(chips()).toEqual(['ПЦР'])
  })

  it('still splits a pasted list and a comma into separate skills', async () => {
    render(<Harness search={reply([])} max={5} />)
    await userEvent.type(input(), 'Word, Excel;')
    expect(chips()).toEqual(['Word', 'Excel'])
    await userEvent.click(input())
    await userEvent.paste('R, SQL')
    expect(chips()).toEqual(['Word', 'Excel', 'R', 'SQL'])
  })

  it('adds the unfinished text when the field loses focus and closes the list', async () => {
    render(<Harness search={reply([pcr])} />)
    await userEvent.type(input(), 'Рентген')
    await screen.findAllByRole('option')
    fireEvent.blur(input())
    expect(chips()).toEqual(['Рентген'])
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })

  it('arrow down opens a closed list and arrow up stops at the field', async () => {
    render(<Harness search={reply([pcr])} />)
    await userEvent.type(input(), 'цеп')
    await screen.findAllByRole('option')
    await userEvent.keyboard('{Escape}{ArrowDown}')
    expect(input()).toHaveAttribute('aria-expanded', 'true')
    await userEvent.keyboard('{ArrowDown}{ArrowUp}{ArrowUp}')
    expect(input()).not.toHaveAttribute('aria-activedescendant')
  })
})

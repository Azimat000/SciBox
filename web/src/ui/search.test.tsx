import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { ResultsHeader } from './ResultsHeader'
import { SearchBar } from './SearchBar'

const regions = [
  { value: 'nsk', label: 'Новосибирск' },
  { value: 'kzn', label: 'Казань' },
]

function Harness({ onSubmit = () => {}, withRegions = true }: { onSubmit?: () => void; withRegions?: boolean }) {
  const [query, setQuery] = useState('')
  const [region, setRegion] = useState('')
  return (
    <SearchBar
      query={query}
      onQueryChange={setQuery}
      regions={withRegions ? regions : undefined}
      region={region}
      onRegionChange={setRegion}
      onSubmit={onSubmit}
    />
  )
}

describe('SearchBar', () => {
  it('is a labelled search landmark with query, region and a submit button', async () => {
    render(<Harness />)
    expect(screen.getByRole('search')).toBeInTheDocument()
    const input = screen.getByRole('searchbox', { name: 'Что ищете' })
    await userEvent.type(input, 'геофизика')
    expect(input).toHaveValue('геофизика')
    const select = screen.getByRole('combobox', { name: 'Регион' })
    expect(select).toHaveValue('')
    await userEvent.selectOptions(select, 'Казань')
    expect(select).toHaveValue('kzn')
    expect(screen.getByRole('option', { name: 'Все регионы' })).toBeInTheDocument()
  })

  it('submits with the button and with Enter', async () => {
    const onSubmit = vi.fn()
    render(<Harness onSubmit={onSubmit} />)
    await userEvent.click(screen.getByRole('button', { name: 'Найти' }))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    await userEvent.type(screen.getByRole('searchbox'), 'химия{Enter}')
    expect(onSubmit).toHaveBeenCalledTimes(2)
  })

  it('works without a region list', () => {
    render(<Harness withRegions={false} />)
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.getByRole('searchbox')).toBeInTheDocument()
  })

  it('tolerates a missing region handler and a missing region value', async () => {
    render(<SearchBar query="" onQueryChange={() => {}} regions={regions} onSubmit={() => {}} />)
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Регион' }), 'Казань')
    expect(screen.getByRole('combobox', { name: 'Регион' })).toBeInTheDocument()
  })
})

describe('ResultsHeader', () => {
  it.each([
    [1, 'Найдена 1 вакансия'],
    [4, 'Найдено 4 вакансии'],
    [11, 'Найдено 11 вакансий'],
    [0, 'Найдено 0 вакансий'],
  ])('%d → %s', (count, text) => {
    render(<ResultsHeader count={count} />)
    expect(screen.getByText(text)).toBeInTheDocument()
  })

  it('shows the sort order when given', () => {
    render(<ResultsHeader count={3} sortedBy="сначала ближайший срок" />)
    expect(screen.getByText(/сначала ближайший срок/)).toBeInTheDocument()
  })
})

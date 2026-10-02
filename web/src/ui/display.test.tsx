import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { Deadline } from './Deadline'
import { EmptyState } from './EmptyState'
import { Skeleton, VacancyEntrySkeleton, VacancyListSkeleton } from './Skeleton'
import { FilterChip, Tag } from './Tag'
import { VacancyEntry } from './VacancyEntry'

const now = new Date(2026, 9, 2, 12)

function inRouter(ui: React.ReactElement) {
  return render(<RouterProvider router={createMemoryRouter([{ path: '/', element: ui }])} />)
}

describe('Tag and FilterChip', () => {
  it('renders tags in both tones', () => {
    render(
      <>
        <Tag>Конкурс</Tag>
        <Tag tone="accent">R3</Tag>
      </>,
    )
    expect(screen.getByText('Конкурс')).toHaveClass('tag-neutral')
    expect(screen.getByText('R3')).toHaveClass('tag-accent')
  })

  it('toggles state visibly and accessibly', async () => {
    const onClick = vi.fn()
    const { rerender } = render(
      <FilterChip pressed={false} onClick={onClick}>
        ППС
      </FilterChip>,
    )
    const chip = screen.getByRole('button', { name: 'ППС' })
    expect(chip).toHaveAttribute('aria-pressed', 'false')
    expect(chip.querySelector('svg')).toBeNull()
    await userEvent.click(chip)
    expect(onClick).toHaveBeenCalledTimes(1)
    rerender(
      <FilterChip pressed className="x">
        ППС
      </FilterChip>,
    )
    expect(screen.getByRole('button', { name: 'ППС' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'ППС' }).querySelector('svg')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'ППС' })).toHaveClass('chip', 'x')
  })
})

describe('Deadline', () => {
  it('marks an urgent deadline', () => {
    render(<Deadline date="2026-10-09" now={now} align="end" />)
    const time = screen.getByText('Заявки до 9 октября')
    expect(time.tagName).toBe('TIME')
    expect(time).toHaveAttribute('datetime', '2026-10-09')
    expect(time).toHaveClass('mark')
    expect(screen.getByText('осталось 7 дней')).toBeInTheDocument()
  })

  it('does not mark a far deadline', () => {
    render(<Deadline date="2026-11-14" now={now} />)
    expect(screen.getByText('Заявки до 14 ноября')).not.toHaveClass('mark')
  })

  it('says applications are closed when the deadline has passed', () => {
    const { container } = render(<Deadline date="2026-09-29" now={now} />)
    expect(screen.getByText('Приём заявок закончен')).not.toHaveClass('mark')
    expect(container.firstElementChild).toHaveAttribute('data-state', 'expired')
  })

  it('renders nothing for a broken date', () => {
    const { container } = render(<Deadline date="завтра" now={now} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('uses the current date when none is given', () => {
    vi.useFakeTimers({ now: new Date(2026, 9, 2, 12), toFake: ['Date'] })
    try {
      render(<Deadline date="2026-10-03" />)
      expect(screen.getByText('остался 1 день')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('VacancyEntry', () => {
  it('shows every part of a full entry', () => {
    inRouter(
      <VacancyEntry
        to="/vacancies/1"
        title="Постдок: геофизика"
        organization="Центр вычислительных наук"
        city="Екатеринбург"
        abstract="Проект по сейсмическим данным."
        level={['R2', 'признанный исследователь']}
        facts={['Науки о Земле', 'Контракт 2 года']}
        competition
        deadline="2026-10-09"
        now={now}
        footer={<p>Подвал</p>}
      />,
    )
    expect(screen.getByRole('heading', { level: 3, name: 'Постдок: геофизика' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Постдок: геофизика' })).toHaveAttribute('href', '/vacancies/1')
    expect(screen.getByText('Центр вычислительных наук, Екатеринбург')).toBeInTheDocument()
    expect(screen.getByText('Проект по сейсмическим данным.')).toBeInTheDocument()
    expect(screen.getByText('R2')).toBeInTheDocument()
    expect(screen.getByText('Науки о Земле')).toBeInTheDocument()
    expect(screen.getByText('Конкурс')).toBeInTheDocument()
    expect(screen.getByText('Заявки до 9 октября')).toBeInTheDocument()
    expect(screen.getByText('Подвал')).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Основные факты' })).toBeInTheDocument()
  })

  it('works with the minimum of data and a custom heading level', () => {
    inRouter(<VacancyEntry to="/v/2" title="Аспирант" organization="ИМС" city="Дубна" headingLevel={2} />)
    expect(screen.getByRole('heading', { level: 2, name: 'Аспирант' })).toBeInTheDocument()
    expect(screen.queryByText(/Заявки/)).not.toBeInTheDocument()
    expect(screen.queryByText('Конкурс')).not.toBeInTheDocument()
  })
})

describe('EmptyState', () => {
  it('explains and offers a way out', () => {
    render(<EmptyState title="Ничего не нашлось" text="Уберите фильтры" action={<button type="button">Сбросить</button>} />)
    expect(screen.getByRole('heading', { level: 3, name: 'Ничего не нашлось' })).toBeInTheDocument()
    expect(screen.getByText('Уберите фильтры')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Сбросить' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('announces an error immediately', () => {
    render(<EmptyState tone="error" headingLevel={2} title="Сбой" />)
    expect(screen.getByRole('alert')).toHaveTextContent('Сбой')
    expect(screen.getByRole('heading', { level: 2 })).toBeInTheDocument()
  })
})

describe('Skeleton', () => {
  it('is hidden from assistive technology', () => {
    const { container } = render(<Skeleton width="50%" height="2rem" className="x" />)
    const el = container.firstElementChild as HTMLElement
    expect(el).toHaveAttribute('aria-hidden', 'true')
    expect(el).toHaveStyle({ width: '50%', height: '32px' })
  })

  it('uses full width by default', () => {
    const { container } = render(<Skeleton />)
    expect(container.firstElementChild).toHaveStyle({ width: '100%' })
  })

  it('renders one entry placeholder', () => {
    const { container } = render(<VacancyEntrySkeleton />)
    expect(container.querySelectorAll('.skeleton').length).toBeGreaterThan(3)
  })

  it('announces loading once for the whole list', () => {
    const { container } = render(<VacancyListSkeleton count={4} />)
    expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelectorAll('.entry-skeleton')).toHaveLength(4)
  })

  it('shows three placeholders by default', () => {
    const { container } = render(<VacancyListSkeleton />)
    expect(container.querySelectorAll('.entry-skeleton')).toHaveLength(3)
  })
})

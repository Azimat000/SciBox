import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useActiveTabInView } from './useActiveTabInView'

function Tabs({ current }: { current: number }) {
  const ref = useActiveTabInView<HTMLElement>()
  return (
    <nav ref={ref} data-testid="tabs">
      {[0, 1, 2, 3].map((i) => (
        <a key={i} href={`#${i}`} aria-current={i === current ? 'page' : undefined}>
          {i}
        </a>
      ))}
    </nav>
  )
}

/** jsdom ничего не раскладывает: размеры ряда и вкладок задаём сами. */
function layout(scrollWidth: number) {
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(scrollWidth)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(300)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    if (this.tagName === 'NAV') return { left: 0, width: 300 } as DOMRect
    const i = Number(this.textContent)
    return { left: i * 150, width: 100 } as DOMRect
  })
}

describe('useActiveTabInView', () => {
  afterEach(() => vi.restoreAllMocks())

  it('brings the current tab to the middle of a row that scrolls sideways', () => {
    layout(600)
    const { getByTestId } = render(
      <MemoryRouter>
        <Tabs current={3} />
      </MemoryRouter>,
    )
    // Вкладка 3 начинается с 450 пикселей; середина ряда 150 — сдвиг 450 - (300 - 100) / 2 = 350
    expect(getByTestId('tabs').scrollLeft).toBe(350)
  })

  it('leaves a row that fits alone', () => {
    layout(300)
    const { getByTestId } = render(
      <MemoryRouter>
        <Tabs current={3} />
      </MemoryRouter>,
    )
    expect(getByTestId('tabs').scrollLeft).toBe(0)
  })

  it('does nothing without a current tab', () => {
    layout(600)
    const { getByTestId } = render(
      <MemoryRouter>
        <Tabs current={-1} />
      </MemoryRouter>,
    )
    expect(getByTestId('tabs').scrollLeft).toBe(0)
  })
})

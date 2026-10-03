import { describe, expect, it } from 'vitest'
import {
  PAGE_SIZE,
  activeFilters,
  change,
  defaultSort,
  effectiveSort,
  emptyCatalog,
  filterCount,
  parseCatalog,
  removeFilter,
  toApiParams,
  toParams,
  toggleList,
  withoutFilters,
} from './params'

const parse = (qs: string) => parseCatalog(new URLSearchParams(qs))

describe('parseCatalog', () => {
  it('reads everything from the address', () => {
    const s = parse('q=катализ&region=54&field=1.4&field=1.4.4&degree=doctor&title=professor&open=1&h_min=20&sort=h_index&page=3')
    expect(s).toEqual({ q: 'катализ', region: '54', field: ['1.4', '1.4.4'], degree: ['doctor'], title: ['professor'], open: true, hMin: 20, sort: 'h_index', page: 3 })
  })

  it('drops what it does not know and keeps defaults', () => {
    const s = parse('region=5&field=abc&field=1..4&degree=phd&title=king&open=no&h_min=900&sort=best&page=-2&q=%20%20')
    expect(s).toEqual(emptyCatalog())
    expect(parse('h_min=x').hMin).toBe(0)
    expect(parse('h_min=0').hMin).toBe(0)
    expect(parse('page=x').page).toBe(1)
  })

  it('collapses spaces in the words, trims and cuts them to 200 characters', () => {
    expect(parse('q=%20%20%D0%BA%D0%B0%D1%82%D0%B0%D0%BB%D0%B8%D0%B7%20%20%D1%85%D0%B8%D0%BC%D0%B8%D1%8F%20').q).toBe('катализ химия')
    expect(parse(`q=${'я'.repeat(300)}`).q).toHaveLength(200)
  })

  it('removes repeated values and caps the list at 30', () => {
    expect(parse('degree=doctor&degree=doctor').degree).toEqual(['doctor'])
    const many = Array.from({ length: 40 }, (_, i) => `field=1.${i + 1}`).join('&')
    expect(parse(many).field).toHaveLength(30)
  })

  it('understands open=true too', () => {
    expect(parse('open=true').open).toBe(true)
  })
})

describe('sort', () => {
  it('uses relevance with words and recent updates without', () => {
    expect(defaultSort({ q: 'химия' })).toBe('relevance')
    expect(defaultSort({ q: '' })).toBe('updated')
    expect(effectiveSort({ q: '', sort: 'relevance' })).toBe('updated')
    expect(effectiveSort({ q: 'x', sort: 'relevance' })).toBe('relevance')
    expect(effectiveSort({ q: '', sort: 'name' })).toBe('name')
    expect(effectiveSort({ q: '', sort: '' })).toBe('updated')
  })
})

describe('writing the address and the request', () => {
  it('writes only what differs from the defaults', () => {
    expect(toParams(emptyCatalog()).toString()).toBe('')
    expect(toParams({ ...emptyCatalog(), q: 'химия', sort: 'relevance' }).toString()).toBe('q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F')
    expect(toParams({ ...emptyCatalog(), sort: 'name', page: 2 }).toString()).toBe('sort=name&page=2')
    const all = toParams({ q: 'a', region: '54', field: ['1.4'], degree: ['doctor'], title: ['docent'], open: true, hMin: 5, sort: '', page: 1 })
    expect(all.toString()).toBe('q=a&field=1.4&degree=doctor&title=docent&region=54&open=1&h_min=5')
  })

  it('sends the page size, the offset and the chosen order to the server', () => {
    expect(toApiParams(emptyCatalog()).toString()).toBe(`limit=${PAGE_SIZE}`)
    const p = toApiParams({ ...emptyCatalog(), sort: 'h_index', page: 3 })
    expect(p.get('sort')).toBe('h_index')
    expect(p.get('limit')).toBe('20')
    expect(p.get('offset')).toBe('40')
  })
})

describe('changing the search', () => {
  it('returns to the first page after any change', () => {
    const s = { ...emptyCatalog(), page: 4 }
    expect(change(s, { q: 'x' }).page).toBe(1)
    expect(change(s, { page: 2 }).page).toBe(2)
  })

  it('toggles one value in a list', () => {
    const on = toggleList(emptyCatalog(), 'degree', 'doctor')
    expect(on.degree).toEqual(['doctor'])
    expect(toggleList(on, 'degree', 'doctor').degree).toEqual([])
    expect(toggleList(on, 'degree', 'candidate').degree).toEqual(['doctor', 'candidate'])
  })

  it('lists the active filters in order and counts them', () => {
    const s = { ...emptyCatalog(), field: ['1.4'], degree: ['doctor'], title: ['docent'], region: '54', open: true, hMin: 10, q: 'x', sort: 'name' as const }
    expect(activeFilters(s).map((f) => f.kind)).toEqual(['list', 'list', 'list', 'region', 'open', 'hIndex'])
    expect(filterCount(s)).toBe(6)
    expect(filterCount(emptyCatalog())).toBe(0)
  })

  it('removes every kind of filter by itself', () => {
    const s = { ...emptyCatalog(), field: ['1.4'], degree: ['doctor'], title: ['docent'], region: '54', open: true, hMin: 10 }
    expect(removeFilter(s, { kind: 'list', key: 'field', value: '1.4' }).field).toEqual([])
    expect(removeFilter(s, { kind: 'list', key: 'title', value: 'docent' }).title).toEqual([])
    expect(removeFilter(s, { kind: 'region' }).region).toBe('')
    expect(removeFilter(s, { kind: 'open' }).open).toBe(false)
    expect(removeFilter(s, { kind: 'hIndex' }).hMin).toBe(0)
  })

  it('resets the filters but keeps the words and the order', () => {
    const s = { ...emptyCatalog(), q: 'химия', sort: 'name' as const, degree: ['doctor'], open: true, page: 3 }
    expect(withoutFilters(s)).toEqual({ ...emptyCatalog(), q: 'химия', sort: 'name' })
  })
})

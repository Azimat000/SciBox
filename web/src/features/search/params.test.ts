import { describe, expect, it } from 'vitest'
import {
  PAGE_SIZE,
  activeFilters,
  change,
  defaultSort,
  effectiveSort,
  emptySearch,
  filterCount,
  parseSearch,
  removeFilter,
  toApiParams,
  toParams,
  toggleMulti,
  withoutFilters,
  type FilterRef,
} from './params'

const parse = (qs: string) => parseSearch(new URLSearchParams(qs))

describe('reading the address', () => {
  it('reads every kind of parameter', () => {
    const s = parse(
      'q=%20органическая%20%20химия%20&field=1.4&field=1.3.8&region=54&type=research&type=teaching&level=3&format=remote&degree=doctor&org_kind=institute&funding=grant&rate=50&term=short&salary_min=80000&housing=1&competition=true&deadline=week&sort=deadline&page=3',
    )
    expect(s).toEqual({
      q: 'органическая химия',
      region: '54',
      multi: { field: ['1.4', '1.3.8'], type: ['research', 'teaching'], level: ['3'], format: ['remote'], degree: ['doctor'], org_kind: ['institute'], funding: ['grant'], rate: ['50'], term: ['short'] },
      salaryMin: 80000,
      housing: true,
      competition: true,
      deadline: 'week',
      sort: 'deadline',
      page: 3,
    })
  })

  it('an empty address means «no filters»', () => {
    expect(parse('')).toEqual(emptySearch())
  })

  it('drops everything it does not know instead of failing', () => {
    const s = parse(
      'type=zzz&type=research&type=research&field=1.x&field=abc&field=2&level=9&format=moon&region=5&region=ab&salary_min=-5&housing=no&competition=0&deadline=tomorrow&sort=random&page=-4',
    )
    expect(s.multi.type).toEqual(['research']) // повтор убран
    expect(s.multi.field).toEqual(['2'])
    expect(s.multi.level).toEqual([])
    expect(s.multi.format).toEqual([])
    expect(s.region).toBe('')
    expect(s.salaryMin).toBe(0)
    expect(s.housing).toBe(false)
    expect(s.competition).toBe(false)
    expect(s.deadline).toBe('')
    expect(s.sort).toBe('')
    expect(s.page).toBe(1)
  })

  it('limits the length of the query, the number of values and the salary', () => {
    expect(parse(`q=${'я'.repeat(500)}`).q).toHaveLength(200)
    const many = Array.from({ length: 40 }, (_, i) => `field=1.${i + 1}`).join('&')
    expect(parse(many).multi.field).toHaveLength(30)
    expect(parse('salary_min=999999999999').salaryMin).toBe(0)
    expect(parse('salary_min=abc').salaryMin).toBe(0)
    expect(parse('page=abc').page).toBe(1)
  })
})

describe('writing the address', () => {
  it('writes only what differs from the defaults', () => {
    expect(toParams(emptySearch()).toString()).toBe('')
    const s = { ...emptySearch(), q: 'химия', sort: 'relevance' as const } // порядок по умолчанию в адрес не попадает
    expect(toParams(s).toString()).toBe('q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F')
    expect(toParams({ ...s, sort: 'new' }).get('sort')).toBe('new')
    expect(toParams({ ...emptySearch(), sort: 'new' }).has('sort')).toBe(false)
  })

  it('round-trips a complex search', () => {
    const s = parse('q=химия&field=1.4&field=1.3&region=54&type=research&format=hybrid&salary_min=80000&housing=1&competition=1&deadline=month&sort=salary&page=2')
    expect(parseSearch(toParams(s))).toEqual(s)
  })

  it('asks the server for one page: limit, offset and an explicit order', () => {
    expect(toApiParams(emptySearch()).toString()).toBe(`limit=${PAGE_SIZE}`)
    const p = toApiParams({ ...emptySearch(), q: 'a', page: 3, sort: 'deadline', multi: { ...emptySearch().multi, level: ['2', '3'] }, housing: true })
    expect(p.getAll('level')).toEqual(['2', '3'])
    expect(p.get('offset')).toBe(String(2 * PAGE_SIZE))
    expect(p.get('sort')).toBe('deadline')
    expect(p.get('housing')).toBe('1')
  })
})

describe('order', () => {
  it('by relevance with words and newest first without them', () => {
    expect(defaultSort({ q: 'химия' })).toBe('relevance')
    expect(defaultSort({ q: '' })).toBe('new')
    expect(effectiveSort({ q: 'химия', sort: '' })).toBe('relevance')
    expect(effectiveSort({ q: '', sort: '' })).toBe('new')
    expect(effectiveSort({ q: '', sort: 'relevance' })).toBe('new') // «по совпадению» без слов — это «новые»
    expect(effectiveSort({ q: 'x', sort: 'salary' })).toBe('salary')
  })
})

describe('changing the search', () => {
  it('any change goes back to the first page', () => {
    expect(change({ ...emptySearch(), page: 4 }, { region: '54' })).toMatchObject({ region: '54', page: 1 })
    expect(change({ ...emptySearch(), page: 4 }, { page: 2 }).page).toBe(2)
  })

  it('toggles one value of a filter', () => {
    const on = toggleMulti(emptySearch(), 'type', 'research')
    expect(on.multi.type).toEqual(['research'])
    expect(toggleMulti(on, 'type', 'teaching').multi.type).toEqual(['research', 'teaching'])
    expect(toggleMulti(on, 'type', 'research').multi.type).toEqual([])
  })

  it('counts and lists the active filters, without the words and the order', () => {
    expect(filterCount(emptySearch())).toBe(0)
    const s = parse('q=химия&sort=deadline&field=1.4&type=research&type=teaching&region=54&salary_min=1000&housing=1&competition=1&deadline=none')
    expect(filterCount(s)).toBe(8)
    const refs = activeFilters(s)
    expect(refs).toHaveLength(8)
    expect(refs.map((r) => r.kind)).toEqual(['multi', 'multi', 'multi', 'region', 'salary', 'housing', 'competition', 'deadline'])
  })

  it('removes each kind of filter', () => {
    const s = parse('field=1.4&type=research&region=54&salary_min=1000&housing=1&competition=1&deadline=none&page=3')
    const refs: FilterRef[] = [
      { kind: 'multi', key: 'type', value: 'research' },
      { kind: 'region' },
      { kind: 'salary' },
      { kind: 'housing' },
      { kind: 'competition' },
      { kind: 'deadline' },
    ]
    let now = s
    for (const ref of refs) {
      now = removeFilter(now, ref)
      expect(now.page).toBe(1)
    }
    expect(filterCount(now)).toBe(1) // осталась область науки
    expect(removeFilter(now, { kind: 'multi', key: 'field', value: '1.4' }).multi.field).toEqual([])
  })

  it('«reset» keeps the words and the order only', () => {
    const s = withoutFilters(parse('q=химия&sort=deadline&type=research&region=54&page=2'))
    expect(s).toEqual({ ...emptySearch(), q: 'химия', sort: 'deadline' })
  })
})

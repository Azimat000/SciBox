import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { renderApp } from '../../test/render'
import { book, emptyProfile, otherPage, ownPage, profile, publication } from '../../test/profile'
import type { FoundJournal, Item, Profile } from './api'

// Срез 14: квартили журналов у публикаций, счётчик «статей в Q1–Q2», подпись источника и выбор журнала из справочника.

const own = (p: Profile, extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/profile': reply(200, ownPage(p)),
  ...extra,
})

const nature = { title: 'Nature', issn: '0028-0836', quartile: 1, year: 2025 }
const q1: Item = { ...publication, id: 'q1', issn: '0028-0836', journal: nature }
const q3: Item = { ...publication, id: 'q3', title: 'Статья в третьем квартиле', doi: '10.1234/q3', issn: '1063-7834', journal: { title: 'Physics of the Solid State', issn: '1063-7834', quartile: 3, year: 2025 } }
const unranked: Item = { ...publication, id: 'nq', title: 'Журнал без квартиля', doi: '10.1234/nq', issn: '2041-1723', journal: { title: 'New', issn: '2041-1723', quartile: null, year: 2025 } }
// Запись только с названием: строки под заголовком пусты, квартиль стоит отдельной строкой.
const bare: Item = { id: 'bare', kind: 'publication', title: 'Только название', issn: '0028-0836', journal: nature }

const withPubs = (publications: Item[], quartiles: Profile['quartiles']): Profile => ({ ...profile, sections: { ...profile.sections, publications }, quartiles })

const pubSection = async () => within((await screen.findByRole('heading', { level: 2, name: 'Публикации' })).closest('section')!)

describe('quartiles on the profile page', () => {
  it('marks publications with the quartile of their journal and counts Q1–Q2 articles', async () => {
    stubApi(own(withPubs([q1, q3, unranked, book, bare], { q12_total: 4, q12_recent: 2, recent_from: 2022, year: 2025 })))
    renderApp('/profile')
    const pubs = await pubSection()
    expect(pubs.getByText('4 статьи в журналах Q1–Q2')).toBeInTheDocument()
    expect(pubs.getByText('из них 2 за 2022–2026')).toBeInTheDocument()
    // Q1 синим, Q3 нейтрально; у журнала без квартиля и у книги без журнала метки нет.
    const marks = pubs.getAllByText(/^Q\d$/)
    // Экранный диктор читает «квартиль журнала Q1».
    expect(marks.map((m) => m.textContent)).toEqual(['квартиль журнала Q1', 'квартиль журнала Q3', 'квартиль журнала Q1'])
    expect(marks[0].closest('.tag')).toHaveClass('tag-accent')
    expect(marks[1].closest('.tag')).toHaveClass('tag-neutral')
    expect(marks[0].closest('.profile-quartile')).toHaveAttribute('title', 'Лучший квартиль журнала по данным SCImago Journal Rank 2025')
    expect(pubs.getAllByText('квартиль журнала')).toHaveLength(3)
    expect(pubs.getByText('Только название').closest('li')!.querySelectorAll('.profile-item-line')).toHaveLength(1)
    // Источник квартилей подписан со ссылкой.
    expect(pubs.getByText(/данные SCImago Journal & Country Rank за 2025 год/)).toBeInTheDocument()
    expect(pubs.getByRole('link', { name: 'scimagojr.com' })).toHaveAttribute('href', 'https://www.scimagojr.com')
  })

  it('says when there are no recent Q1–Q2 articles and hides the count when there are none at all', async () => {
    stubApi(own(withPubs([q1], { q12_total: 1, q12_recent: 0, recent_from: 2022, year: 2025 })))
    const { unmount } = renderApp('/profile')
    expect(await screen.findByText('1 статья в журналах Q1–Q2')).toBeInTheDocument()
    expect(screen.getByText('за 2022–2026 пока ни одной')).toBeInTheDocument()
    unmount()

    stubApi(own(withPubs([q3], { q12_total: 0, q12_recent: 0, recent_from: 2022, year: 2025 })))
    renderApp('/profile')
    const pubs = await pubSection()
    expect(pubs.queryByText(/в журналах Q1–Q2/)).not.toBeInTheDocument()
    expect(pubs.getByText(/SCImago Journal & Country Rank/)).toBeInTheDocument()
  })

  it('has no count and no source when no publication is in the catalog, or in an old snapshot without quartiles', async () => {
    stubApi(own(withPubs([publication], { q12_total: 0, q12_recent: 0, recent_from: 2022, year: null })))
    const { unmount } = renderApp('/profile')
    const pubs = await pubSection()
    expect(pubs.queryByText(/Q1–Q2/)).not.toBeInTheDocument()
    expect(pubs.queryByText(/SCImago/)).not.toBeInTheDocument()
    unmount()

    stubApi({ 'GET /api/auth/me': reply(200, { user: null }), 'GET /api/scientists/*': reply(200, otherPage(withPubs([q1], undefined))) })
    renderApp(`/scientists/${profile.id}`)
    const other = await pubSection()
    expect(other.getByText('Q1')).toBeInTheDocument()
    expect(other.queryByText(/в журналах Q1–Q2/)).not.toBeInTheDocument()
    expect(other.queryByText(/SCImago Journal & Country Rank/)).not.toBeInTheDocument()
  })
})

const found = (over: Partial<FoundJournal> = {}): FoundJournal => ({
  id: 1, title: 'Physical Review B', publisher: 'American Physical Society', issn: '2469-9950', issns: ['2469-9950', '2469-9969'], quartile: 1, year: 2025, ...over,
})

describe('journal picker in the publication window', () => {
  const addPublication = async () => {
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Публикации»' }))
    return within(await screen.findByRole('dialog'))
  }

  it('finds a journal by words, fills the empty venue and saves its ISSN', async () => {
    const api = stubApi(
      own(emptyProfile, {
        'GET /api/journals*': reply(200, { items: [found(), found({ id: 2, title: 'Physics', issn: '2624-8174', issns: ['2624-8174'], quartile: null, publisher: '' })] }),
        'POST /api/profile/items': reply(201, { item: publication }),
      }),
    )
    renderApp('/profile')
    const dialog = await addPublication()
    const search = dialog.getByRole('searchbox', { name: /Журнал из справочника/ })
    await userEvent.type(search, 'p')
    expect(await dialog.findByText('Введите хотя бы две буквы')).toBeInTheDocument()
    await userEvent.type(search, 'hysical review{Enter}')
    const list = within(await dialog.findByRole('list', { name: 'Найденные журналы' }))
    expect(api.calls.filter((c) => c.path.startsWith('/api/journals')).at(-1)?.path).toBe('/api/journals?q=physical%20review')
    expect(list.getByText('ISSN 2469-9950, 2469-9969')).toBeInTheDocument()
    expect(list.getByText('American Physical Society')).toBeInTheDocument()
    expect(list.getByText('без квартиля')).toBeInTheDocument()
    // Enter в поиске не отправил форму.
    expect(api.called('POST', '/api/profile/items')).toHaveLength(0)

    await userEvent.click(list.getByRole('button', { name: 'Выбрать журнал Physical Review B' }))
    expect(dialog.queryByRole('searchbox', { name: /Журнал из справочника/ })).not.toBeInTheDocument()
    expect(dialog.getByText('Physical Review B')).toBeInTheDocument()
    expect(dialog.getByText('ISSN 2469-9950')).toBeInTheDocument()
    expect(dialog.getByText('Q1')).toBeInTheDocument()
    expect(dialog.getByLabelText(/Журнал, сборник/)).toHaveValue('Physical Review B')

    await userEvent.type(dialog.getByLabelText(/Название/), 'Статья')
    await userEvent.type(dialog.getByLabelText(/Авторы/), 'Орлова Е. А.')
    await userEvent.type(dialog.getByLabelText(/Год/), '2025')
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Запись добавлена')
    expect(api.called('POST', '/api/profile/items')[0].body).toMatchObject({ kind: 'publication', issn: '2469-9950', venue: 'Physical Review B' })
  })

  it('keeps a venue the person already typed', async () => {
    stubApi(own(emptyProfile, { 'GET /api/journals*': reply(200, { items: [found()] }) }))
    renderApp('/profile')
    const dialog = await addPublication()
    await userEvent.type(dialog.getByLabelText(/Журнал, сборник/), 'Физический журнал')
    await userEvent.type(dialog.getByRole('searchbox', { name: /Журнал из справочника/ }), 'physical')
    await userEvent.click(await dialog.findByRole('button', { name: 'Выбрать журнал Physical Review B' }))
    expect(dialog.getByLabelText(/Журнал, сборник/)).toHaveValue('Физический журнал')
  })

  it('says when nothing is found and when the search fails', async () => {
    stubApi(own(emptyProfile, { 'GET /api/journals*': (call) => (call.path.includes('broken') ? apiError(500, 'internal', 'Ошибка') : reply(200, { items: [] })) }))
    renderApp('/profile')
    const dialog = await addPublication()
    const search = dialog.getByRole('searchbox', { name: /Журнал из справочника/ })
    await userEvent.type(search, 'nothing')
    expect(await dialog.findByText(/В справочнике такого журнала нет/)).toBeInTheDocument()
    await userEvent.clear(search)
    await userEvent.type(search, 'broken')
    expect(await dialog.findByText('Не удалось найти журналы. Попробуйте ещё раз.')).toBeInTheDocument()
  })

  it('shows the saved journal, lets choose another one or go back, and removes it', async () => {
    const api = stubApi(
      own(withPubs([q1], { q12_total: 1, q12_recent: 1, recent_from: 2022, year: 2025 }), {
        'GET /api/journals*': reply(200, { items: [found()] }),
        'PUT /api/profile/items/q1': reply(200, { item: publication }),
      }),
    )
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: `Изменить: ${q1.title}` }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByText('Nature')).toBeInTheDocument()
    expect(dialog.getByText('ISSN 0028-0836')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Другой журнал' }))
    expect(dialog.getByRole('searchbox', { name: /Журнал из справочника/ })).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Оставить прежний журнал' }))
    expect(dialog.getByText('Nature')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Убрать' }))
    expect(dialog.getByRole('searchbox', { name: /Журнал из справочника/ })).toBeInTheDocument()
    // Без журнала возвращаться не к чему.
    expect(dialog.queryByRole('button', { name: 'Оставить прежний журнал' })).not.toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Запись сохранена')
    expect(api.called('PUT', '/api/profile/items/q1')[0].body).not.toHaveProperty('issn')
  })

  it('tells that a journal with this ISSN is not in the catalog', async () => {
    const unknown: Item = { ...publication, id: 'u1', issn: '1234-5679' }
    stubApi(own(withPubs([unknown], { q12_total: 0, q12_recent: 0, recent_from: 2022, year: null })))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: `Изменить: ${unknown.title}` }))
    const dialog = within(await screen.findByRole('dialog'))
    expect(dialog.getByText(/ISSN 1234-5679: этого журнала нет в справочнике SCImago/)).toBeInTheDocument()
  })

  it('shows an ISSN problem the server found, both with a chosen journal and in the search', async () => {
    const bad = apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { issn: 'ISSN записывают так: 0028-0836' } })
    stubApi(own(withPubs([q1], { q12_total: 1, q12_recent: 1, recent_from: 2022, year: 2025 }), { 'PUT /api/profile/items/q1': bad }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: `Изменить: ${q1.title}` }))
    const dialog = within(await screen.findByRole('dialog'))
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    expect(await dialog.findByText('ISSN записывают так: 0028-0836')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Другой журнал' }))
    expect(dialog.getByText('ISSN записывают так: 0028-0836')).toBeInTheDocument()
  })
})

describe('journal from Crossref', () => {
  const work = { doi: '10.1038/nature12373', title: 'Статья', authors: 'Kucsko G.', venue: 'Nature', year: 2013, type: 'article', volume: '', issue: '', pages: '' }
  const lookup = async () => {
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Публикации»' }))
    const dialog = within(await screen.findByRole('dialog'))
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), '10.1038/nature12373')
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    await dialog.findByText('Нашли. Проверьте поля ниже и сохраните.')
    return dialog
  }

  it('puts the journal found by the ISSN from Crossref into the form', async () => {
    const api = stubApi(own(emptyProfile, { 'GET /api/profile/doi*': reply(200, { work: { ...work, issn: '1476-4687', journal: { ...nature, issn: '1476-4687' } } }), 'POST /api/profile/items': reply(201, { item: publication }) }))
    renderApp('/profile')
    const dialog = await lookup()
    expect(dialog.getByText('ISSN 1476-4687')).toBeInTheDocument()
    expect(dialog.getByText('Q1')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Сохранить' }))
    await screen.findByText('Запись добавлена')
    expect(api.called('POST', '/api/profile/items')[0].body).toMatchObject({ issn: '1476-4687', source: 'crossref' })
  })

  it('keeps the ISSN from Crossref when the journal is not in the catalog', async () => {
    stubApi(own(emptyProfile, { 'GET /api/profile/doi*': reply(200, { work: { ...work, issn: '1234-5679', journal: null } }) }))
    renderApp('/profile')
    const dialog = await lookup()
    expect(dialog.getByText(/ISSN 1234-5679: этого журнала нет в справочнике SCImago/)).toBeInTheDocument()
  })

  it('leaves a chosen journal alone when Crossref knows no ISSN', async () => {
    stubApi(own(emptyProfile, { 'GET /api/journals*': reply(200, { items: [found()] }), 'GET /api/profile/doi*': reply(200, { work: { ...work, issn: '' } }) }))
    renderApp('/profile')
    await userEvent.click(await screen.findByRole('button', { name: 'Добавить в раздел «Публикации»' }))
    const dialog = within(await screen.findByRole('dialog'))
    await userEvent.type(dialog.getByRole('searchbox', { name: /Журнал из справочника/ }), 'physical')
    await userEvent.click(await dialog.findByRole('button', { name: 'Выбрать журнал Physical Review B' }))
    await userEvent.type(dialog.getByLabelText(/Найти по DOI/), '10.1038/nature12373')
    await userEvent.click(dialog.getByRole('button', { name: 'Найти' }))
    await dialog.findByText('Нашли. Проверьте поля ниже и сохраните.')
    await waitFor(() => expect(dialog.getByText('ISSN 2469-9950')).toBeInTheDocument())
  })
})

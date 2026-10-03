import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { savedSearch } from '../../test/matching'
import { renderApp } from '../../test/render'
import { card, reference } from '../../test/vacancies'
import { ROLE_STORAGE_KEY } from '../shell/role-context'
import type { Reference } from '../vacancies/api'
import { conditionsOfQuery, conditionsOfSearch, frequencyHint, frequencyLabel, reasonLabel, suggestName } from './labels'
import { emptySearch } from '../search/params'

beforeEach(() => window.localStorage.clear())

const results = reply(200, { items: [card], total: 1, fuzzy: false })
const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/reference': reply(200, reference),
  'GET /api/favorites/ids': reply(200, { ids: [] }),
  'GET /api/vacancies?*': results,
  ...extra,
})
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })
const QUERY = '/vacancies?q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F&region=54&field=1.4'

describe('save search button', () => {
  it('is not there while nothing is asked for', async () => {
    stubApi(signedIn())
    renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    expect(screen.queryByRole('button', { name: 'Сохранить поиск' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Войти, чтобы сохранить поиск' })).not.toBeInTheDocument()
  })

  it('is not there in the hiring mode', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    stubApi(signedIn())
    renderApp(QUERY)
    await screen.findByRole('link', { name: card.title })
    expect(screen.queryByRole('button', { name: 'Сохранить поиск' })).not.toBeInTheDocument()
  })

  it('leads a visitor to sign in and back to the same search', async () => {
    stubApi({ 'GET /api/reference': reply(200, reference), 'GET /api/vacancies?*': results })
    renderApp(QUERY)
    const link = await screen.findByRole('link', { name: 'Войти, чтобы сохранить поиск' })
    expect(link).toHaveAttribute('href', `/login?next=${encodeURIComponent(QUERY)}`)
  })

  it('also shows itself when nothing was found: that is when a person wants to be told later', async () => {
    stubApi(signedIn({ 'GET /api/vacancies?*': reply(200, { items: [], total: 0, fuzzy: false }) }))
    renderApp('/vacancies?q=%D1%85%D0%B8%D0%BC%D0%B8%D1%8F')
    expect(await screen.findByRole('button', { name: 'Сохранить поиск' })).toBeInTheDocument()
  })

  it('waits while it is not known who is signed in', () => {
    stubApi({ 'GET /api/auth/me': () => new Promise(() => {}) as never, 'GET /api/reference': reply(200, reference), 'GET /api/vacancies?*': results })
    renderApp(QUERY)
    expect(screen.queryByRole('button', { name: 'Сохранить поиск' })).not.toBeInTheDocument()
  })

  it('opens a window with the conditions, a suggested name and the daily frequency', async () => {
    const user = userEvent.setup()
    stubApi(signedIn())
    renderApp(QUERY)
    await user.click(await screen.findByRole('button', { name: 'Сохранить поиск' }))
    const dialog = await screen.findByRole('dialog', { name: 'Сохранить поиск' })
    expect(within(dialog).getByText('Слова: химия')).toBeInTheDocument()
    expect(within(dialog).getByText('Новосибирская область')).toBeInTheDocument()
    expect(within(dialog).getByText('1.4 Химические науки')).toBeInTheDocument()
    expect(within(dialog).getByRole('textbox', { name: /Название/ })).toHaveValue('химия · 1.4 Химические науки · Новосибирская область')
    expect(within(dialog).getByRole('combobox', { name: 'Как сообщать' })).toHaveValue('daily')
    expect(within(dialog).getByText('Одно сообщение утром (9:00 по Москве), если есть новые вакансии')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('saves the conditions without the page, with the chosen name and frequency', async () => {
    const user = userEvent.setup()
    const { calls } = stubApi(signedIn({ 'POST /api/saved-searches': reply(201, { search: savedSearch({ frequency: 'weekly' }) }), 'GET /api/saved-searches': reply(200, { items: [] }) }))
    renderApp(`${QUERY}&page=3&sort=deadline`)
    await user.click(await screen.findByRole('button', { name: 'Сохранить поиск' }))
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByRole('textbox', { name: /Название/ })
    await user.clear(name)
    await user.type(name, '  Моя химия ')
    await user.selectOptions(within(dialog).getByRole('combobox', { name: 'Как сообщать' }), 'weekly')
    expect(within(dialog).getByText('Одно сообщение в понедельник утром, если есть новые вакансии')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить поиск' }))
    expect(await toast('Поиск сохранён')).toBeInTheDocument()
    expect(screen.getByText('Сообщать: раз в неделю. Поиски собраны в разделе «Избранное».', { selector: '.toast *' })).toBeInTheDocument()
    const sent = calls.find((c) => c.method === 'POST')!.body as { name: string; query: string; frequency: string }
    expect(sent.name).toBe('Моя химия')
    expect(sent.frequency).toBe('weekly')
    const query = new URLSearchParams(sent.query)
    expect(query.get('q')).toBe('химия')
    expect(query.get('region')).toBe('54')
    expect(query.get('field')).toBe('1.4')
    expect(query.get('sort')).toBe('deadline')
    expect(query.has('page')).toBe(false)
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('checks the name before sending', async () => {
    const user = userEvent.setup()
    const { calls } = stubApi(signedIn())
    renderApp(QUERY)
    await user.click(await screen.findByRole('button', { name: 'Сохранить поиск' }))
    const dialog = await screen.findByRole('dialog')
    const name = within(dialog).getByRole('textbox', { name: /Название/ })
    await user.clear(name)
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить поиск' }))
    expect(await within(dialog).findByText('Назовите поиск')).toBeInTheDocument()
    await user.type(name, 'я'.repeat(121))
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить поиск' }))
    expect(await within(dialog).findByText('Название длиннее 120 знаков')).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
  })

  it('shows what the server says: a general failure, a refused condition, a field', async () => {
    const user = userEvent.setup()
    const refuse = (reply: ReturnType<typeof apiError>) => stubApi(signedIn({ 'POST /api/saved-searches': reply }))
    for (const [answer, text] of [
      [apiError(409, 'too_many_searches', 'Сохранено слишком много поисков. Удалите ненужные'), 'Сохранено слишком много поисков. Удалите ненужные'],
      [apiError(422, 'validation_failed', 'Проверьте поиск', { fields: { query: 'Неизвестное значение: x' } }), 'Неизвестное значение: x'],
    ] as const) {
      refuse(answer)
      const view = renderApp(QUERY)
      await user.click(await screen.findByRole('button', { name: 'Сохранить поиск' }))
      const dialog = await screen.findByRole('dialog')
      await user.click(within(dialog).getByRole('button', { name: 'Сохранить поиск' }))
      expect(await within(dialog).findByRole('alert')).toHaveTextContent(text)
      view.unmount()
    }
    refuse(apiError(422, 'validation_failed', 'Проверьте поиск', { fields: { name: 'Название уже занято' } }))
    renderApp(QUERY)
    await user.click(await screen.findByRole('button', { name: 'Сохранить поиск' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Сохранить поиск' }))
    expect(await within(dialog).findByText('Название уже занято')).toBeInTheDocument()
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })
})

describe('labels of the section', () => {
  const noReference: Reference | undefined = undefined

  it('names frequencies and reasons, and shows an unknown one as it is', () => {
    expect(frequencyLabel('instant')).toBe('Сразу')
    expect(frequencyLabel('later')).toBe('later')
    expect(frequencyHint('off')).toBe('Поиск сохранён, но сообщений по нему не будет')
    expect(frequencyHint('later')).toBe('')
    expect(reasonLabel('region')).toBe('Ваш регион')
    expect(reasonLabel('new_reason')).toBe('new_reason')
  })

  it('writes conditions: words first, then filters; codes while the reference is not loaded', () => {
    expect(conditionsOfQuery('q=химия&format=remote&field=1.4&region=54&housing=1', reference)).toEqual([
      'Слова: химия',
      '1.4 Химические науки',
      'Удалённо',
      'Новосибирская область',
      'Предоставляется жильё',
    ])
    expect(conditionsOfQuery('field=1.4&region=54', noReference)).toEqual(['1.4', '54'])
    expect(conditionsOfQuery('page=2&sort=new', reference)).toEqual([])
    expect(conditionsOfSearch(emptySearch(), reference)).toEqual([])
  })

  it('suggests a name from the words and filters, never longer than 120 signs', () => {
    expect(suggestName({ ...emptySearch(), q: 'геномика' }, reference)).toBe('геномика')
    expect(suggestName({ ...emptySearch(), region: '54' }, reference)).toBe('Новосибирская область')
    expect(suggestName({ ...emptySearch(), q: 'химия', region: '54' }, reference)).toBe('химия · Новосибирская область')
    expect(suggestName(emptySearch(), reference)).toBe('Мой поиск')
    const long = suggestName({ ...emptySearch(), q: 'слово '.repeat(40).trim() }, reference)
    expect(long).toHaveLength(120)
    expect(long.endsWith('…')).toBe(true)
  })
})

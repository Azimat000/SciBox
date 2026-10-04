import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { bareCard, catalogCard, catalogResult } from '../../test/offers'
import { renderApp } from '../../test/render'
import { reference } from '../../test/vacancies'
import { ROLE_STORAGE_KEY } from '../shell/role-context'

beforeEach(() => window.localStorage.clear())

/** Подставной сервер: справочники и каталог, который запоминает запросы. */
function setup(over: Record<string, Route> = {}, result: Parameters<typeof catalogResult>[1] & { items?: Parameters<typeof catalogResult>[0] } = {}) {
  const searches: URLSearchParams[] = []
  const { items, ...extra } = result
  const api = stubApi({
    'GET /api/reference': reply(200, reference),
    'GET /api/scientists?*': (call) => {
      searches.push(new URLSearchParams(call.path.split('?')[1]))
      return reply(200, catalogResult(items, extra))
    },
    ...over,
  })
  const last = () => searches[searches.length - 1]
  return { ...api, searches, last }
}

const filters = () => screen.getByRole('complementary', { name: 'Уточнить поиск' })
const chipsList = () => screen.queryByRole('list', { name: 'Выбранные фильтры' })

describe('catalog page', () => {
  it('lists scientists as journal entries: name, post, facts, specialties, open-to-offers mark', async () => {
    const { last } = setup()
    renderApp('/scientists')
    expect(await screen.findByRole('heading', { level: 1, name: 'Каталог учёных' })).toBeInTheDocument()
    const link = await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(link).toHaveAttribute('href', `/scientists/${catalogCard.id}`)
    const entry = link.closest('article') as HTMLElement
    const e = within(entry)
    expect(e.getByText('Старший научный сотрудник, лаборатория катализа')).toBeInTheDocument()
    expect(e.getByText('Доктор наук')).toBeInTheDocument()
    expect(e.getByText('Профессор')).toBeInTheDocument()
    expect(e.getByText('Новосибирск, Новосибирская область')).toBeInTheDocument()
    expect(e.getByText('h-index 15')).toBeInTheDocument()
    expect(e.getByText('12 публикаций')).toBeInTheDocument()
    expect(e.getByText('Открыт к предложениям')).toBeInTheDocument()
    expect(within(e.getByRole('list', { name: 'Научные специальности' })).getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByText('Найдено 2 учёных')).toBeInTheDocument()
    expect(last().get('limit')).toBe('20')
    expect(last().has('q')).toBe(false)
    expect(chipsList()).not.toBeInTheDocument()
  })

  it('leaves out the facts a scientist did not give', async () => {
    setup({}, { items: [bareCard] })
    renderApp('/scientists')
    const entry = (await screen.findByRole('link', { name: 'Борис Лапин' })).closest('article') as HTMLElement
    expect(within(entry).queryByRole('list', { name: 'Основные сведения' })).not.toBeInTheDocument()
    expect(within(entry).queryByRole('list', { name: 'Научные специальности' })).not.toBeInTheDocument()
    expect(within(entry).queryByText('Открыт к предложениям')).not.toBeInTheDocument()
    expect(screen.getByText('Найден 1 учёный')).toBeInTheDocument()
  })

  it('shows only the facts that exist, even when the degree is missing', async () => {
    setup({}, { items: [{ ...bareCard, city: 'Казань', publications: 1, academic_title: 'docent' }] })
    renderApp('/scientists')
    const entry = (await screen.findByRole('link', { name: 'Борис Лапин' })).closest('article') as HTMLElement
    const facts = within(entry).getByRole('list', { name: 'Основные сведения' })
    expect(within(facts).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Доцент', 'Казань', '1 публикация'])
  })

  it('searches by words on «Найти», writes them to the address and sorts by relevance', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/scientists')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    await user.type(screen.getByRole('searchbox', { name: 'Кого ищете' }), '  катализ ')
    expect(last().has('q')).toBe(false)
    await user.click(screen.getByRole('button', { name: 'Найти' }))
    await waitFor(() => expect(last().get('q')).toBe('катализ'))
    expect(router.state.location.search).toBe('?q=%D0%BA%D0%B0%D1%82%D0%B0%D0%BB%D0%B8%D0%B7')
    expect(screen.getByRole('combobox', { name: 'Порядок' })).toHaveValue('relevance')
  })

  it('reads everything from the address and drops what it does not know', async () => {
    const { last } = setup()
    renderApp('/scientists?q=химия&degree=doctor&degree=bogus&title=professor&region=54&open=1&h_min=20&field=1.4&page=2')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(screen.getByRole('searchbox', { name: 'Кого ищете' })).toHaveValue('химия')
    expect(last().getAll('degree')).toEqual(['doctor'])
    expect(last().getAll('title')).toEqual(['professor'])
    expect(last().get('region')).toBe('54')
    expect(last().get('open')).toBe('1')
    expect(last().get('h_min')).toBe('20')
    expect(last().getAll('field')).toEqual(['1.4'])
    expect(last().get('offset')).toBe('20')
    expect(screen.getByRole('combobox', { name: 'Регион' })).toHaveValue('54')
  })

  it('turns every filter into the request and into a removable tag', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/scientists')
    await screen.findByRole('link', { name: 'Елена Орлова' })

    await user.click(within(filters()).getByRole('checkbox', { name: 'Только открытые к предложениям' }))
    await waitFor(() => expect(last().get('open')).toBe('1'))
    await user.click(within(filters()).getByRole('checkbox', { name: 'Доктор наук' }))
    await user.click(within(filters()).getByRole('checkbox', { name: 'Профессор' }))
    await user.click(within(filters()).getByText('Область науки'))
    await user.click(within(filters()).getByText('Естественные науки'))
    await user.click(within(filters()).getByRole('checkbox', { name: /Химические науки/ }))
    await user.selectOptions(within(filters()).getByRole('combobox', { name: 'h-index от' }), '30')
    await user.click(within(filters()).getByText('Статьи в Q1–Q2'))
    await user.selectOptions(within(filters()).getByRole('combobox', { name: 'Статей в Q1–Q2 от' }), '5')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Регион' }), '77')

    await waitFor(() => expect(last().get('region')).toBe('77'))
    expect(last().getAll('degree')).toEqual(['doctor'])
    expect(last().getAll('title')).toEqual(['professor'])
    expect(last().getAll('field')).toEqual(['1.4'])
    expect(last().get('h_min')).toBe('30')
    expect(last().get('q12_min')).toBe('5')
    expect(router.state.location.search).toContain('open=1')

    const chips = within(chipsList() as HTMLElement)
    expect(chips.getAllByRole('button')).toHaveLength(7)
    expect(chips.getByRole('button', { name: 'Убрать фильтр: Статей в Q1–Q2 от 5' })).toBeInTheDocument()
    expect(chips.getByRole('button', { name: 'Убрать фильтр: 1.4 Химические науки' })).toBeInTheDocument()
    expect(chips.getByRole('button', { name: 'Убрать фильтр: Москва' })).toBeInTheDocument()
    expect(chips.getByRole('button', { name: 'Убрать фильтр: h-index от 30' })).toBeInTheDocument()

    await user.click(chips.getByRole('button', { name: 'Убрать фильтр: Доктор наук' }))
    await waitFor(() => expect(last().getAll('degree')).toEqual([]))
    await user.click(screen.getByRole('button', { name: 'Сбросить всё' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(chipsList()).not.toBeInTheDocument()
  })

  it('keeps an odd h-index from the address among the choices', async () => {
    setup()
    renderApp('/scientists?h_min=17')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    const select = within(filters()).getByRole('combobox', { name: 'h-index от' })
    expect(select).toHaveValue('17')
    expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['Любой', 'h-index от 5', 'h-index от 10', 'h-index от 17', 'h-index от 20', 'h-index от 30', 'h-index от 50'])
    await userEvent.selectOptions(select, '')
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Убрать фильтр: h-index от 17' })).not.toBeInTheDocument())
  })

  it('keeps an odd Q1–Q2 count from the address among the choices', async () => {
    setup()
    renderApp('/scientists?q12_min=7')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    const select = within(filters()).getByRole('combobox', { name: 'Статей в Q1–Q2 от' })
    expect(select).toHaveValue('7')
    expect(within(select).getAllByRole('option').map((o) => (o as HTMLOptionElement).value)).toEqual(['', '1', '3', '5', '7', '10', '20'])
    await userEvent.selectOptions(select, '')
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Убрать фильтр: Статей в Q1–Q2 от 7' })).not.toBeInTheDocument())
  })

  it('shows the Q1–Q2 count with the recent part only when there is one', async () => {
    setup({}, { items: [catalogCard, { ...bareCard, q12_total: 2, q12_recent: 0 }] })
    renderApp('/scientists')
    const first = (await screen.findByRole('link', { name: 'Елена Орлова' })).closest('article') as HTMLElement
    expect(within(first).getByText('5 статей в Q1–Q2, 3 с 2022 г.')).toBeInTheDocument()
    const second = screen.getByRole('link', { name: 'Борис Лапин' }).closest('article') as HTMLElement
    expect(within(second).getByText('2 статьи в Q1–Q2')).toBeInTheDocument()
  })

  it('puts the chosen order in the address unless it is the default', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/scientists')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    const sort = screen.getByRole('combobox', { name: 'Порядок' })
    expect(sort).toHaveValue('updated')
    expect(within(sort).queryByRole('option', { name: 'Сначала подходящие' })).not.toBeInTheDocument()
    await user.selectOptions(sort, 'h_index')
    await waitFor(() => expect(last().get('sort')).toBe('h_index'))
    expect(router.state.location.search).toBe('?sort=h_index')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Порядок' }), 'updated')
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('says when only similar words were found', async () => {
    setup({}, { fuzzy: true })
    renderApp('/scientists?q=Орлвоа')
    expect(await screen.findByText('Точных совпадений нет')).toBeInTheDocument()
    expect(screen.getByText(/Проверьте, как написан запрос «Орлвоа»/)).toBeInTheDocument()
    expect(screen.getByText('Похожих: 2 учёных')).toBeInTheDocument()
  })

  it('explains an empty result for filters, for words and for an empty catalog', async () => {
    setup({}, { items: [] })
    const first = renderApp('/scientists?degree=doctor')
    expect(await screen.findByRole('heading', { level: 2, name: 'Никого не нашлось' })).toBeInTheDocument()
    expect(screen.getByText(/Уберите часть фильтров/)).toBeInTheDocument()
    first.unmount()

    setup({}, { items: [] })
    const second = renderApp('/scientists?q=несуществующее')
    expect(await screen.findByText(/Проверьте написание/)).toBeInTheDocument()
    second.unmount()

    setup({}, { items: [] })
    renderApp('/scientists')
    expect(await screen.findByText(/В каталоге пока никого нет/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Сбросить всё' })).not.toBeInTheDocument()
  })

  it('turns pages and says when the page does not exist', async () => {
    const user = userEvent.setup()
    const many = Array.from({ length: 20 }, (_, i) => ({ ...bareCard, id: `s${i}`, name: `Учёный ${i}` }))
    const { last } = setup({}, { items: many, total: 45 })
    const { router } = renderApp('/scientists')
    expect(await screen.findByText('Страница 1 из 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Дальше' }))
    await waitFor(() => expect(last().get('offset')).toBe('20'))
    expect(router.state.location.search).toBe('?page=2')
    expect(await screen.findByText('Страница 2 из 3')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Назад' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('offers the first page when the page is beyond the last', async () => {
    const user = userEvent.setup()
    setup({}, { items: [], total: 3 })
    const { router } = renderApp('/scientists?page=9')
    expect(await screen.findByRole('heading', { level: 2, name: 'Страницы 9 нет' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'К первой странице' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('shows a skeleton while loading and an error with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    setup({ 'GET /api/scientists?*': () => (fail ? apiError(500, 'internal', 'Что-то сломалось на сервере') : reply(200, catalogResult())) })
    renderApp('/scientists')
    expect(screen.getAllByRole('status', { name: 'Загрузка' }).length).toBeGreaterThan(0)
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить каталог' })).toBeInTheDocument()
    expect(screen.getByText('Что-то сломалось на сервере')).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: 'Елена Орлова' })).toBeInTheDocument()
  })

  it('opens the filter column with a button on a narrow screen', async () => {
    const user = userEvent.setup()
    setup()
    renderApp('/scientists?degree=doctor')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    const toggle = screen.getByRole('button', { name: 'Фильтры (1)' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(screen.getByRole('button', { name: 'Показать 2 учёных' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  it('works without the reference books: no regions and no science groups yet', async () => {
    setup({ 'GET /api/reference': apiError(500, 'internal', 'нет справочников') })
    renderApp('/scientists?field=1.4.4&region=54')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    // Пока справочник не загрузился, фильтры показываются кодами.
    expect(screen.getByRole('button', { name: 'Убрать фильтр: 1.4.4' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Убрать фильтр: 54' })).toBeInTheDocument()
  })

  it('shows a very long name in full', async () => {
    setup({}, { items: [{ ...bareCard, name: 'Очень-Очень-Длинная-Фамилия-Через-Дефис Анастасия-Мария-Александровна' }] })
    renderApp('/scientists')
    expect(await screen.findByRole('heading', { level: 2, name: 'Очень-Очень-Длинная-Фамилия-Через-Дефис Анастасия-Мария-Александровна' })).toBeInTheDocument()
  })
})

describe('the invite button in the catalog', () => {
  const signedIn = signedInAs(ann)

  it('is not there for a visitor', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    setup()
    renderApp('/scientists')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(screen.queryByRole('button', { name: /Пригласить на вакансию/ })).not.toBeInTheDocument()
  })

  it('is not there for someone who looks for work', async () => {
    setup(signedIn)
    renderApp('/scientists')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(screen.queryByRole('button', { name: /Пригласить на вакансию/ })).not.toBeInTheDocument()
  })

  it('appears on every entry for a signed-in person in the «Нанимаю» mode', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    setup(signedIn)
    renderApp('/scientists')
    expect(await screen.findByRole('button', { name: 'Пригласить на вакансию: Елена Орлова' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Пригласить на вакансию: Борис Лапин' })).toBeInTheDocument()
  })
})

describe('catalog intro', () => {
  it('talks to the employer about inviting, and to the seeker about being seen', async () => {
    localStorage.setItem('scibox.role', 'employer')
    setup()
    const employer = renderApp('/scientists')
    expect(await screen.findByText(/пригласите его на свою вакансию/)).toBeInTheDocument()
    employer.unmount()
    localStorage.setItem('scibox.role', 'seeker')
    setup()
    renderApp('/scientists')
    expect(await screen.findByText(/Учёные, открывшие свой профиль/)).toBeInTheDocument()
    expect(screen.queryByText(/пригласите его/)).not.toBeInTheDocument()
    localStorage.removeItem('scibox.role')
  })
})

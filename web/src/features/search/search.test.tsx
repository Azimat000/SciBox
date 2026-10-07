import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, stubApi, type Route } from '../../test/api'
import { renderApp } from '../../test/render'
import { card, reference } from '../../test/vacancies'

const second = { ...card, id: 'b', title: 'Постдок: геномика', deadline: '', is_competition: false }
const items = [card, second]

/** Подставной сервер: справочники и поиск, который отвечает по таблице страниц и запоминает запросы. */
function setup(over: Record<string, Route> = {}, result: { items?: unknown[]; total?: number; fuzzy?: boolean } = {}) {
  const searches: URLSearchParams[] = []
  const api = stubApi({
    'GET /api/reference': reply(200, reference),
    'GET /api/vacancies?*': (call) => {
      searches.push(new URLSearchParams(call.path.split('?')[1]))
      return reply(200, { items, total: items.length, fuzzy: false, ...result })
    },
    ...over,
  })
  const last = () => searches[searches.length - 1]
  return { ...api, searches, last }
}

const filters = () => screen.getByRole('complementary', { name: 'Уточнить поиск' })
const chipsList = () => screen.queryByRole('list', { name: 'Выбранные фильтры' })

describe('search page', () => {
  it('opens on /vacancies with the newest first', async () => {
    const { last } = setup()
    renderApp('/vacancies')
    expect(await screen.findByRole('heading', { level: 1, name: 'Вакансии в науке и образовании' })).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: card.title })).toHaveAttribute('href', `/vacancies/${card.id}`)
    expect(screen.getByText('Найдено 2 вакансии')).toBeInTheDocument()
    expect(last().get('limit')).toBe('20')
    expect(last().has('q')).toBe(false)
    expect(chipsList()).not.toBeInTheDocument()
  })

  it('searches by words on «Найти», puts them in the address and sorts by relevance', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    await user.type(screen.getByRole('searchbox', { name: 'Что ищете' }), '  органическая химия ')
    // Пока не нажали «Найти», запроса нет.
    expect(last().has('q')).toBe(false)
    await user.click(screen.getByRole('button', { name: 'Найти' }))
    await waitFor(() => expect(last().get('q')).toBe('органическая химия'))
    expect(router.state.location.search).toBe('?q=%D0%BE%D1%80%D0%B3%D0%B0%D0%BD%D0%B8%D1%87%D0%B5%D1%81%D0%BA%D0%B0%D1%8F+%D1%85%D0%B8%D0%BC%D0%B8%D1%8F')
    // Порядок по умолчанию со словами — «подходящие»; выбор выдачи на сервере не нужен.
    expect(screen.getByRole('combobox', { name: 'Порядок' })).toHaveValue('relevance')
  })

  it('reads everything from the address and drops what it does not know', async () => {
    const { last } = setup()
    renderApp('/vacancies?q=химия&type=research&type=bogus&region=54&salary_min=80000&deadline=week&page=2')
    await screen.findByRole('link', { name: card.title })
    expect(screen.getByRole('searchbox', { name: 'Что ищете' })).toHaveValue('химия')
    expect(last().getAll('type')).toEqual(['research'])
    expect(last().get('region')).toBe('54')
    expect(last().get('offset')).toBe('20')
    expect(screen.getByRole('combobox', { name: 'Регион' })).toHaveValue('54')
  })

  it('turns the checkbox, chip and select filters into the address and into removable tags', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })

    await user.click(within(filters()).getByRole('checkbox', { name: 'Научный работник' }))
    await waitFor(() => expect(last().getAll('type')).toEqual(['research']))
    await user.click(within(filters()).getByRole('button', { name: 'Гибрид' }))
    await user.click(within(filters()).getByRole('button', { name: 'В ближайшие 7 дней' }))
    await user.click(within(filters()).getByRole('checkbox', { name: 'Только конкурсы' }))
    await user.click(within(filters()).getByRole('checkbox', { name: 'Предоставляется жильё' }))
    await user.selectOptions(within(filters()).getByRole('combobox', { name: 'Зарплата от' }), '100000')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Регион' }), '77')

    await waitFor(() => expect(last().get('region')).toBe('77'))
    expect(last().getAll('format')).toEqual(['hybrid'])
    expect(last().get('deadline')).toBe('week')
    expect(last().get('competition')).toBe('1')
    expect(last().get('housing')).toBe('1')
    expect(last().get('salary_min')).toBe('100000')

    const tags = within(chipsList()!)
    for (const label of ['Научный работник', 'Гибрид', 'В ближайшие 7 дней', 'Только конкурсы', 'Предоставляется жильё', 'от 100 000 ₽', 'Москва']) {
      expect(tags.getByText(label)).toBeInTheDocument()
    }

    // Тег убирается нажатием; «Сбросить всё» убирает остальное.
    await user.click(screen.getByRole('button', { name: 'Убрать фильтр: Гибрид' }))
    await waitFor(() => expect(last().has('format')).toBe(false))
    expect(within(filters()).getByRole('button', { name: 'Гибрид' })).toHaveAttribute('aria-pressed', 'false')
    await user.click(within(screen.getByRole('region', { name: 'Результаты поиска' })).getAllByRole('button', { name: 'Сбросить всё' })[0]!)
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(chipsList()).not.toBeInTheDocument()
  })

  it('switches a chip filter off by a second click and the deadline off by choosing it again', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    renderApp('/vacancies?format=remote&rate=50&deadline=month&salary_min=65000')
    await screen.findByRole('link', { name: card.title })
    // Нестандартная зарплата из адреса остаётся в списке и выбрана.
    expect(within(filters()).getByRole('combobox', { name: 'Зарплата от' })).toHaveValue('65000')
    await user.click(within(filters()).getByRole('button', { name: 'Удалённо' }))
    await user.click(within(filters()).getByRole('button', { name: '0,5 ставки' }))
    await user.click(within(filters()).getByRole('button', { name: 'В ближайшие 30 дней' }))
    await user.selectOptions(within(filters()).getByRole('combobox', { name: 'Зарплата от' }), '')
    await waitFor(() => expect(last().has('deadline')).toBe(false))
    expect(last().has('format')).toBe(false)
    expect(last().has('rate')).toBe(false)
    expect(last().has('salary_min')).toBe(false)
  })

  it('shows region and science names on the tags once the reference books arrive', async () => {
    const user = userEvent.setup()
    setup()
    renderApp('/vacancies?field=1.4&field=1.9.9&level=3&degree=doctor&org_kind=institute&funding=grant&term=medium&type=teaching')
    const list = await screen.findByRole('list', { name: 'Выбранные фильтры' })
    expect(await within(list).findByText('1.4 Химические науки')).toBeInTheDocument()
    expect(within(list).getByText('1.9.9')).toBeInTheDocument() // кода нет в справочнике — показываем код
    expect(within(list).getByText('R3 · самостоятельный исследователь')).toBeInTheDocument()
    expect(within(list).getByText('Требуемая степень: доктор наук')).toBeInTheDocument()
    expect(within(list).getByText('НИИ / институт РАН')).toBeInTheDocument()
    expect(within(list).getByText('Грант')).toBeInTheDocument()
    expect(within(list).getByText('Срочный, от года до трёх лет')).toBeInTheDocument()
    expect(within(list).getByText('Преподаватель')).toBeInTheDocument()
    // Группа со сделанным выбором открыта, и выбор виден в её заголовке.
    expect(within(filters()).getByRole('checkbox', { name: /Химические науки/ })).toBeChecked()
    await user.click(within(list).getByRole('button', { name: 'Убрать фильтр: 1.4 Химические науки' }))
    await waitFor(() => expect(within(filters()).getByRole('checkbox', { name: /Химические науки/ })).not.toBeChecked())
  })

  it('opens a filter group by itself when a choice appears and keeps the user\'s own toggling', async () => {
    const user = userEvent.setup()
    setup()
    const { router } = renderApp('/vacancies')
    const level = await screen.findByText('Уровень')
    const group = level.closest('details')!
    expect(group).not.toHaveAttribute('open')
    await user.click(level)
    expect(group).toHaveAttribute('open')
    await user.click(level)
    expect(group).not.toHaveAttribute('open')
    // Выбор приходит из адреса (например, «Назад»): группа раскрывается сама.
    await router.navigate('/vacancies?level=2')
    await waitFor(() => expect(group).toHaveAttribute('open'))
    expect(within(group).getByRole('checkbox', { name: /R2/ })).toBeChecked()
  })

  it('changes the order and goes back to the default one without writing it in the address', async () => {
    const user = userEvent.setup()
    const { last } = setup()
    const { router } = renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    const sort = screen.getByRole('combobox', { name: 'Порядок' })
    // Без слов поиска «по совпадению» нет.
    expect(within(sort).queryByRole('option', { name: 'Сначала подходящие' })).not.toBeInTheDocument()
    await user.selectOptions(sort, 'deadline')
    await waitFor(() => expect(last().get('sort')).toBe('deadline'))
    expect(router.state.location.search).toBe('?sort=deadline')
    await user.selectOptions(sort, 'new')
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('hides the order when there is nothing to order', async () => {
    setup({}, { items: [card], total: 1 })
    renderApp('/vacancies')
    await screen.findByText('Найдена 1 вакансия')
    expect(screen.queryByRole('combobox', { name: 'Порядок' })).not.toBeInTheDocument()
  })

  it('turns the pages: offset follows the page, edges are disabled, the filters keep', async () => {
    const user = userEvent.setup()
    const { last } = setup({}, { total: 45 })
    const { router } = renderApp('/vacancies?type=research')
    await screen.findByText('Страница 1 из 3')
    const pager = screen.getByRole('navigation', { name: 'Страницы результатов' })
    expect(within(pager).getByRole('button', { name: 'Назад' })).toBeDisabled()
    await user.click(within(pager).getByRole('button', { name: 'Дальше' }))
    await screen.findByText('Страница 2 из 3')
    expect(last().get('offset')).toBe('20')
    expect(last().getAll('type')).toEqual(['research'])
    expect(router.state.location.search).toBe('?type=research&page=2')
    await user.click(within(pager).getByRole('button', { name: 'Дальше' }))
    await screen.findByText('Страница 3 из 3')
    expect(within(pager).getByRole('button', { name: 'Дальше' })).toBeDisabled()
    await user.click(within(pager).getByRole('button', { name: 'Назад' }))
    await screen.findByText('Страница 2 из 3')
    // Смена фильтра возвращает на первую страницу.
    await user.click(within(filters()).getByRole('checkbox', { name: 'Преподаватель' }))
    await waitFor(() => expect(router.state.location.search).not.toContain('page'))
  })

  it('says so when the page is beyond the last one, and leads to the first', async () => {
    const user = userEvent.setup()
    const { last } = setup({}, { items: [], total: 7 })
    const { router } = renderApp('/vacancies?page=9')
    expect(await screen.findByRole('heading', { name: 'Страницы 9 нет' })).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Страницы результатов' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'К первой странице' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(last().has('offset')).toBe(false)
  })

  it('says that nothing is found and offers to reset', async () => {
    const user = userEvent.setup()
    setup({}, { items: [], total: 0 })
    const { router } = renderApp('/vacancies?q=нет&type=research')
    expect(await screen.findByRole('heading', { name: 'Ничего не нашлось' })).toBeInTheDocument()
    expect(screen.getByText('Найдено 0 вакансий')).toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: 'Порядок' })).not.toBeInTheDocument()
    const empty = screen.getByRole('heading', { name: 'Ничего не нашлось' }).closest('.empty') as HTMLElement
    await user.click(within(empty).getByRole('button', { name: 'Сбросить всё' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('asks to check the spelling when there are only words and no filters', async () => {
    setup({}, { items: [], total: 0 })
    renderApp('/vacancies?q=zzzz')
    expect(await screen.findByText(/Проверьте написание/)).toBeInTheDocument()
    expect(screen.queryByText(/Уберите часть фильтров/)).not.toBeInTheDocument()
  })

  it('says that there are no vacancies at all when nothing was asked', async () => {
    setup({}, { items: [], total: 0 })
    renderApp('/vacancies')
    expect(await screen.findByText(/Опубликованных вакансий пока нет/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Сбросить всё' })).not.toBeInTheDocument()
  })

  it('tells that only similar words were found', async () => {
    setup({}, { fuzzy: true })
    renderApp('/vacancies?q=геномкии')
    const title = await screen.findByText('Точных совпадений нет')
    expect(title.closest('[role="status"]')).toHaveTextContent('«геномкии»')
    // Счётчик не обещает «найдено»: это похожие.
    expect(screen.getByText('Похожих: 2 вакансии')).toBeInTheDocument()
    expect(screen.queryByText(/^Найдено/)).not.toBeInTheDocument()
  })

  it('shows loading, then an error with a retry that works', async () => {
    const user = userEvent.setup()
    let fail = true
    setup({
      'GET /api/vacancies?*': () => (fail ? apiError(500, 'internal', 'Что-то сломалось на сервере') : reply(200, { items, total: 2, fuzzy: false })),
    })
    renderApp('/vacancies')
    expect(screen.getAllByRole('status', { name: 'Загрузка' })).not.toHaveLength(0)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось найти вакансии')
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: card.title })).toBeInTheDocument()
  })

  it('keeps the old list on screen, dimmed, while a new one loads', async () => {
    const user = userEvent.setup()
    let release: () => void = () => undefined
    let n = 0
    setup({
      'GET /api/vacancies?*': () => {
        n += 1
        if (n === 1) return reply(200, { items, total: 2, fuzzy: false })
        return new Promise((resolve) => {
          release = () => resolve(reply(200, { items: [second], total: 1, fuzzy: false }))
        }) as never
      },
    })
    renderApp('/vacancies')
    await screen.findByRole('link', { name: card.title })
    await user.click(within(filters()).getByRole('checkbox', { name: 'Преподаватель' }))
    await waitFor(() => expect(document.querySelector('.vacancy-list')).toHaveAttribute('aria-busy', 'true'))
    expect(screen.getByRole('link', { name: card.title })).toBeInTheDocument()
    release()
    await waitFor(() => expect(screen.queryByRole('link', { name: card.title })).not.toBeInTheDocument())
    expect(document.querySelector('.vacancy-list')).toHaveAttribute('aria-busy', 'false')
  })

  it('opens and closes the filter column on a narrow screen', async () => {
    const user = userEvent.setup()
    setup()
    renderApp('/vacancies?type=research&format=remote')
    await screen.findByRole('link', { name: card.title })
    const toggle = screen.getByRole('button', { name: 'Фильтры (2)' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(document.getElementById('search-filter-panel')).toHaveAttribute('data-open', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(document.getElementById('search-filter-panel')).toHaveAttribute('data-open', 'true')
    await user.click(screen.getByRole('button', { name: 'Показать 2 вакансии' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(document.getElementById('search-filter-panel')).toHaveAttribute('data-open', 'false')
  })

  it('has no filter count on the button when nothing is chosen', async () => {
    setup()
    renderApp('/vacancies')
    expect(await screen.findByRole('button', { name: 'Фильтры' })).toBeInTheDocument()
  })

  it('works while the reference books are not there: no region list, no science groups', async () => {
    setup({ 'GET /api/reference': apiError(500, 'internal', 'нет') })
    renderApp('/vacancies?region=54&field=1.4')
    await screen.findByRole('link', { name: card.title })
    const list = screen.getByRole('list', { name: 'Выбранные фильтры' })
    // Названий нет: показываем коды, а не пустые теги.
    expect(within(list).getByText('54')).toBeInTheDocument()
    expect(within(list).getByText('1.4')).toBeInTheDocument()
    expect(within(filters()).queryByText('Естественные науки')).not.toBeInTheDocument()
  })

  it('keeps long and awkward data inside the page', async () => {
    const long = { ...card, id: 'z', title: 'Сверхдлинноеназваниеуникальнойдолжности'.repeat(8), organization: { ...card.organization, name: 'Федеральное государственное бюджетное научное учреждение «Очень длинное название института»' } }
    setup({}, { items: [long], total: 1 })
    renderApp('/vacancies')
    expect(await screen.findByRole('link', { name: long.title })).toBeInTheDocument()
    expect(screen.getByText(/Очень длинное название института/)).toBeInTheDocument()
  })
})

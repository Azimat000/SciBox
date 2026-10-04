import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { field, fill, press } from '../../test/forms'
import { lab, SLUG } from '../../test/orgs'
import { renderApp } from '../../test/render'
import { detail, reference, target, VACANCY_ID, vacancyRoute, withViewer } from '../../test/vacancies'

const base = (extra: Record<string, Route> = {}, targets = [target]): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/reference': reply(200, reference),
  'GET /api/my/vacancy-targets': reply(200, { targets }),
  [`GET /api/vacancies/${VACANCY_ID}`]: reply(200, { vacancy: withViewer({}, true, []) }),
  ...extra,
})
const saved = (over = {}) => reply(201, { vacancy: { ...detail, id: 'new-1', status: 'draft', ...over, viewer: { can_manage: true, transitions: ['published'] } } })
const click = async (name: string) => userEvent.click(await screen.findByRole('button', { name }))
const pickType = async (name: string) => userEvent.click(await screen.findByRole('button', { name }))
const choose = (label: string | RegExp, option: string) => userEvent.selectOptions(field(label), option)

describe('new vacancy form', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/my-vacancies/new')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('says there is no organization to create vacancies in', async () => {
    stubApi(base({}, []))
    renderApp('/my-vacancies/new')
    expect(await screen.findByRole('heading', { level: 2, name: 'Нет организации, где вы можете создавать вакансии' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К моим организациям' })).toHaveAttribute('href', '/my-organization')
  })

  it('needs a title, a type and a position before saving, and focuses the first problem', async () => {
    const api = stubApi(base())
    renderApp('/my-vacancies/new')
    await click('Сохранить черновик')
    expect(await screen.findByText('Укажите название вакансии')).toBeInTheDocument()
    expect(field(/Название вакансии/)).toHaveFocus()
    expect(api.called('POST', '/api/vacancies')).toHaveLength(0)
    // Должность видна, только когда выбран тип.
    expect(screen.queryByLabelText(/Должность/)).not.toBeInTheDocument()
    await pickType('Научная должность')
    expect(screen.getByText('Научный сотрудник, заведующий лабораторией, инженер-исследователь')).toBeInTheDocument()
    await click('Сохранить черновик')
    expect(await screen.findByText('Выберите должность', { selector: 'p' })).toBeInTheDocument()
  })

  it('saves a draft with only a title and a position, and opens it', async () => {
    const api = stubApi(base({ 'POST /api/vacancies': saved(), [`GET /api/vacancies/new-1`]: saved() }))
    const { router } = renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    await choose(/Должность/, 'Старший научный сотрудник')
    await fill(/Название вакансии/, 'Старший научный сотрудник в лабораторию')
    await click('Сохранить черновик')
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies/new-1'))
    const body = api.called('POST', '/api/vacancies')[0].body as Record<string, unknown>
    expect(body).toMatchObject({
      organization: SLUG,
      title: 'Старший научный сотрудник в лабораторию',
      position_code: 'senior_researcher',
      unit_id: null,
      career_level: null,
      rate_percent: null,
      salary_from: null,
      contract_months: null,
      is_competition: false,
      specialties: [],
      housing: 'none',
      degree_required: 'none',
      title_required: 'none',
    })
    expect(await screen.findByText('Черновик сохранён')).toBeInTheDocument()
  })

  it('sends every field in the shape the server expects', async () => {
    const api = stubApi(base({ 'POST /api/vacancies': saved(), [`GET /api/vacancies/new-1`]: saved() }))
    renderApp('/my-vacancies/new')
    await screen.findByLabelText(/Подразделение/)
    await choose(/Подразделение/, lab.name)
    await pickType('Научная должность')
    await choose(/Должность/, 'Старший научный сотрудник')
    await fill(/Название вакансии/, 'Старший научный сотрудник')
    await fill(/Аннотация/, 'Исследования активных центров катализаторов.')
    await fill(/^Описание/, 'Работа в команде.')
    await fill(/^Требования/, 'Кандидат наук.')
    await fill(/Направление или проект/, 'Рост плёнок')
    await choose(/Уровень исследователя/, 'R3 · самостоятельный исследователь')
    await choose(/Требуемая степень/, 'Кандидат наук')
    await choose(/Формат работы/, 'Очно')
    await fill(/^Город/, 'Новосибирск')
    await choose(/^Ставка/, '1 ставка')
    await fill(/Зарплата от/, '90000')
    await fill(/Зарплата до/, '120000')
    await choose(/^Договор/, 'Срочный договор')
    await fill(/Срок договора/, '36')
    await choose(/Источник финансирования/, 'Грант')
    await fill(/Уточнение к финансированию/, 'РНФ 24-12-00184')
    await choose(/Жильё/, 'Служебное жильё')
    await userEvent.click(screen.getByRole('checkbox', { name: 'Конкурс на должность' }))
    await fill(/Последний день подачи/, '2099-12-31')
    await userEvent.type(screen.getByRole('combobox', { name: /Регион/ }), 'Новосиб')
    await userEvent.click(await screen.findByRole('option', { name: 'Новосибирская область' }))
    await userEvent.type(screen.getByRole('combobox', { name: /Научные специальности/ }), 'физическая')
    await userEvent.click(await screen.findByRole('option', { name: '1.4.4 Физическая химия' }))
    await click('Сохранить черновик')
    await waitFor(() => expect(api.called('POST', '/api/vacancies')).toHaveLength(1))
    expect(api.called('POST', '/api/vacancies')[0].body).toEqual({
      organization: SLUG,
      title: 'Старший научный сотрудник',
      position_code: 'senior_researcher',
      unit_id: lab.id,
      summary: 'Исследования активных центров катализаторов.',
      description: 'Работа в команде.',
      requirements: 'Кандидат наук.',
      focus: 'Рост плёнок',
      career_level: 3,
      work_format: 'onsite',
      region_code: '54',
      city: 'Новосибирск',
      housing: 'service',
      rate_percent: 100,
      salary_from: 90000,
      salary_to: 120000,
      contract_type: 'fixed',
      contract_months: 36,
      funding_source: 'grant',
      funding_note: 'РНФ 24-12-00184',
      degree_required: 'candidate',
      title_required: 'none',
      is_competition: true,
      deadline: '2099-12-31',
      specialties: ['1.4.4'],
    })
  })

  it('shows only the fields that make sense for the type of position', async () => {
    stubApi(base())
    renderApp('/my-vacancies/new')
    await pickType('ППС')
    expect(screen.getByLabelText(/Требуемое звание/)).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Конкурс на должность' })).toBeInTheDocument()
    expect(screen.getByLabelText(/Преподаваемые дисциплины/)).toBeInTheDocument()
    await choose(/Требуемое звание/, 'Профессор')
    await userEvent.click(screen.getByRole('checkbox', { name: 'Конкурс на должность' }))
    await pickType('Аспирантура, постдок, стажировка')
    expect(screen.queryByLabelText(/Требуемое звание/)).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: 'Конкурс на должность' })).not.toBeInTheDocument()
    expect(screen.getByLabelText(/Тема исследования/)).toBeInTheDocument()
    expect(screen.getByLabelText(/Стипендия от/)).toBeInTheDocument()
    await pickType('Управление наукой')
    expect(screen.getByLabelText(/Сфера ответственности/)).toBeInTheDocument()
    expect(screen.queryByLabelText(/Стипендия от/)).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: 'Конкурс на должность' })).not.toBeInTheDocument()
  })

  it('forgets the position when the type changes to one that does not contain it', async () => {
    stubApi(base())
    renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    await choose(/Должность/, 'Научный сотрудник')
    await pickType('Научная должность')
    expect(field(/Должность/)).toHaveValue('researcher')
    await pickType('ППС')
    expect(field(/Должность/)).toHaveValue('')
    expect(within(field(/Должность/)).getAllByRole('option').map((o) => o.textContent)).toEqual(['Выберите должность', 'Доцент'])
  })

  it('asks for the months only for a fixed-term contract and makes the place optional for remote work', async () => {
    stubApi(base())
    renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    expect(screen.queryByLabelText(/Срок договора/)).not.toBeInTheDocument()
    await choose(/^Договор/, 'Срочный договор')
    expect(screen.getByLabelText(/Срок договора/)).toBeInTheDocument()
    await choose(/^Договор/, 'Бессрочный договор')
    expect(screen.queryByLabelText(/Срок договора/)).not.toBeInTheDocument()
    expect(screen.getByLabelText(/^Город/).closest('.field')).not.toHaveTextContent('необязательно')
    await choose(/Формат работы/, 'Удалённо')
    expect(screen.getByLabelText(/^Город/).closest('.field')).toHaveTextContent('необязательно')
    expect(screen.queryByLabelText(/Уточнение к финансированию/)).not.toBeInTheDocument()
    await choose(/Источник финансирования/, 'Грант')
    expect(screen.getByLabelText(/Уточнение к финансированию/)).toBeInTheDocument()
  })

  it('chooses up to five specialties, lets one be removed and stops offering more at five', async () => {
    stubApi(base())
    renderApp('/my-vacancies/new')
    expect(await screen.findByText('Пока ничего не выбрано')).toBeInTheDocument()
    const add = async (name: string) => {
      await userEvent.type(screen.getByRole('combobox', { name: /Научные специальности/ }), name.slice(0, 6))
      await userEvent.click(await screen.findByRole('option', { name }))
    }
    for (const name of ['1.3.1 Физика космоса, астрономия', '1.3.2 Приборы и методы экспериментальной физики', '1.3.3 Теоретическая физика', '1.3.8 Физика конденсированного состояния', '1.4.3 Органическая химия']) {
      await add(name)
    }
    expect(screen.queryByRole('combobox', { name: /Научные специальности/ })).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem').filter((li) => li.classList.contains('specialty-chip'))).toHaveLength(5)
    await userEvent.click(screen.getByRole('button', { name: 'Убрать специальность Теоретическая физика' }))
    expect(screen.getByRole('combobox', { name: /Научные специальности/ })).toBeInTheDocument()
    expect(screen.queryByText('Теоретическая физика')).not.toBeInTheDocument()
    // Выбранная специальность из списка вариантов исчезает.
    await userEvent.type(screen.getByRole('combobox', { name: /Научные специальности/ }), 'Органическая')
    expect(screen.queryByRole('option', { name: '1.4.3 Органическая химия' })).not.toBeInTheDocument()
  })

  it('offers the organization when the person works in several, and preselects unit rules', async () => {
    const other = { organization: { slug: 'other', name: 'Другой институт', kind: 'institute', city: 'Томск' }, whole_org: false, units: [{ id: 'u-2', name: 'Кафедра физики' }] }
    const api = stubApi(base({ 'POST /api/vacancies': saved(), [`GET /api/vacancies/new-1`]: saved() }, [target, other]))
    renderApp('/my-vacancies/new')
    expect(await screen.findByLabelText(/Организация/)).toHaveValue('')
    expect(screen.queryByLabelText(/Подразделение/)).not.toBeInTheDocument()
    await choose(/Организация/, 'Сибирский институт')
    expect(within(field(/Подразделение/)).getAllByRole('option').map((o) => o.textContent)).toEqual(['Вся организация (без подразделения)', lab.name])
    // У организации, где «всей организации» нет, подразделение выбирается сразу, пустого пункта нет.
    await choose(/Организация/, 'Другой институт')
    expect(field(/Подразделение/)).toHaveValue('u-2')
    expect(within(field(/Подразделение/)).getAllByRole('option').map((o) => o.textContent)).toEqual(['Кафедра физики'])
    await pickType('Научная должность')
    await choose(/Должность/, 'Научный сотрудник')
    await fill(/Название вакансии/, 'Научный сотрудник')
    await click('Сохранить черновик')
    await waitFor(() => expect(api.called('POST', '/api/vacancies')).toHaveLength(1))
    expect(api.called('POST', '/api/vacancies')[0].body).toMatchObject({ organization: 'other', unit_id: 'u-2' })
  })

  it('needs an organization when the person works in several and has not chosen', async () => {
    const other = { organization: { slug: 'other', name: 'Другой институт', kind: 'institute', city: 'Томск' }, whole_org: true, units: [] }
    stubApi(base({}, [target, other]))
    renderApp('/my-vacancies/new')
    await click('Сохранить черновик')
    expect(await screen.findByText('Выберите организацию')).toBeInTheDocument()
  })

  it('takes the organization and unit from the address', async () => {
    stubApi(base())
    renderApp(`/my-vacancies/new?org=${SLUG}&unit=${lab.id}`)
    expect(await screen.findByLabelText(/Подразделение/)).toHaveValue(lab.id)
  })

  it('ignores an unknown unit in the address', async () => {
    stubApi(base())
    renderApp(`/my-vacancies/new?org=${SLUG}&unit=nope`)
    expect(await screen.findByLabelText(/Подразделение/)).toHaveValue('')
  })

  it('puts the server\'s complaints next to the fields', async () => {
    stubApi(
      base({ 'POST /api/vacancies': apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { summary: 'Аннотация слишком короткая: не меньше 20 знаков', deadline: 'Дата должна быть вида 2026-11-14' } }) }),
    )
    renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    await choose(/Должность/, 'Научный сотрудник')
    await fill(/Название вакансии/, 'Научный сотрудник')
    await click('Сохранить черновик')
    expect(await screen.findByText('Аннотация слишком короткая: не меньше 20 знаков')).toBeInTheDocument()
    expect(screen.getByText('Дата должна быть вида 2026-11-14')).toBeInTheDocument()
    expect(field(/Аннотация/)).toHaveFocus()
  })

  it('shows a general error above the form', async () => {
    stubApi(base({ 'POST /api/vacancies': apiError(429, 'rate_limited', 'Слишком много', { retry_after: 600 }) }))
    renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    await choose(/Должность/, 'Научный сотрудник')
    await fill(/Название вакансии/, 'Научный сотрудник')
    await click('Сохранить черновик')
    expect(await screen.findByRole('alert')).toHaveTextContent('Слишком много попыток')
  })

  it('creates the draft once even if publishing fails and the form is sent again', async () => {
    let publishes = 0
    const api = stubApi(
      base({
        'POST /api/vacancies': saved(),
        'PATCH /api/vacancies/new-1': reply(200, { vacancy: { ...detail, id: 'new-1', status: 'draft', viewer: { can_manage: true, transitions: ['published'] } } }),
        'POST /api/vacancies/new-1/status': () => {
          publishes++
          return publishes === 1
            ? apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { description: 'Опишите вакансию: задачи, команду, условия' } })
            : reply(200, { vacancy: { ...detail, id: 'new-1', status: 'published', viewer: { can_manage: true, transitions: ['closed'] } } })
        },
        'GET /api/vacancies/new-1': saved(),
      }),
    )
    const { router } = renderApp('/my-vacancies/new')
    await pickType('Научная должность')
    await choose(/Должность/, 'Научный сотрудник')
    await fill(/Название вакансии/, 'Научный сотрудник')
    await click('Сохранить и опубликовать')
    expect(await screen.findByText('Опишите вакансию: задачи, команду, условия')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/my-vacancies/new')
    await fill(/^Описание/, 'Описание есть')
    await click('Сохранить и опубликовать')
    await waitFor(() => expect(router.state.location.pathname).toBe('/vacancies/new-1'))
    expect(api.called('POST', '/api/vacancies')).toHaveLength(1)
    expect(api.called('PATCH', '/api/vacancies/new-1')).toHaveLength(1)
    expect(await screen.findByText('Вакансия опубликована')).toBeInTheDocument()
  })

  it('goes back to the list on cancel', async () => {
    stubApi({ ...base(), 'GET /api/my/vacancies?limit=50&offset=0': reply(200, { items: [], total: 0, counts: { draft: 0, published: 0, closed: 0, archived: 0 } }) })
    const { router } = renderApp('/my-vacancies/new')
    await click('Отмена')
    await waitFor(() => expect(router.state.location.pathname).toBe('/my-vacancies'))
  })

  it('shows an error with retry when the reference books cannot be loaded', async () => {
    let n = 0
    stubApi(base({ 'GET /api/reference': () => (n++ === 0 ? reply(500) : reply(200, reference)) }))
    renderApp('/my-vacancies/new')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить данные для формы')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Новая вакансия' })).toBeInTheDocument()
  })

  it('shows an error with retry when the places to create vacancies cannot be loaded', async () => {
    let n = 0
    stubApi(base({ 'GET /api/my/vacancy-targets': () => (n++ === 0 ? reply(500) : reply(200, { targets: [target] })) }))
    renderApp('/my-vacancies/new')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить данные для формы')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Новая вакансия' })).toBeInTheDocument()
  })
})

describe('editing a vacancy', () => {
  const editPath = `/my-vacancies/${VACANCY_ID}/edit`
  const draftRoute = (over = {}) => vacancyRoute(withViewer({ status: 'draft', ...over }, true, ['published']))

  it('fills the form from the saved vacancy', async () => {
    stubApi(base(draftRoute()))
    renderApp(editPath)
    expect(await screen.findByRole('heading', { level: 1, name: 'Правка вакансии' })).toBeInTheDocument()
    expect(field(/Название вакансии/)).toHaveValue(detail.title)
    expect(field(/Должность/)).toHaveValue('senior_researcher')
    expect(field(/Подразделение/)).toHaveValue(lab.id)
    expect(field(/Аннотация/)).toHaveValue(detail.summary)
    expect(field(/Уровень исследователя/)).toHaveValue('3')
    expect(screen.getByText('ведёт собственное направление')).toBeInTheDocument()
    expect(field(/^Ставка/)).toHaveValue('100')
    expect(field(/Зарплата от/)).toHaveValue(95000)
    expect(field(/Срок договора/)).toHaveValue(36)
    expect(field(/Источник финансирования/)).toHaveValue('grant')
    expect(screen.getByRole('combobox', { name: /Регион/ })).toHaveValue('Новосибирская область')
    expect(screen.getByRole('checkbox', { name: 'Конкурс на должность' })).toBeChecked()
    expect(screen.getByText('Физическая химия')).toBeInTheDocument()
    // Организацию у созданной вакансии не меняют.
    expect(screen.queryByLabelText(/Организация/)).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Назад к вакансии' })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}`)
  })

  it('shows empty optional fields as empty', async () => {
    stubApi(base(draftRoute({ unit: null, region: null, career_level: null, rate_percent: null, salary_from: null, salary_to: null, contract_months: null, contract_type: 'permanent', deadline: '', is_competition: false, specialties: [] })))
    renderApp(editPath)
    expect(await screen.findByLabelText(/Зарплата от/)).toHaveValue(null)
    expect(field(/Уровень исследователя/)).toHaveValue('')
    expect(field(/Подразделение/)).toHaveValue('')
    expect(screen.getByRole('combobox', { name: /Регион/ })).toHaveValue('')
  })

  it('saves the changes of a draft and can also publish it', async () => {
    const patched = reply(200, { vacancy: { ...detail, status: 'draft', viewer: { can_manage: true, transitions: ['published'] } } })
    const published = reply(200, { vacancy: { ...detail, status: 'published', viewer: { can_manage: true, transitions: ['closed'] } } })
    const api = stubApi(base({ ...draftRoute(), [`PATCH /api/vacancies/${VACANCY_ID}`]: patched, [`POST /api/vacancies/${VACANCY_ID}/status`]: published }))
    const { router } = renderApp(editPath)
    await screen.findByRole('heading', { level: 1, name: 'Правка вакансии' })
    await fill(/Название вакансии/, ' (обновлено)')
    await click('Сохранить и опубликовать')
    await waitFor(() => expect(router.state.location.pathname).toBe(`/vacancies/${VACANCY_ID}`))
    expect((api.called('PATCH', `/api/vacancies/${VACANCY_ID}`)[0].body as { title: string }).title).toBe(`${detail.title} (обновлено)`)
    expect(api.called('POST', `/api/vacancies/${VACANCY_ID}/status`)[0].body).toEqual({ status: 'published' })
    expect(await screen.findByText('Вакансия опубликована')).toBeInTheDocument()
  })

  it('only saves with the plain save button, and says so', async () => {
    const api = stubApi(base({ ...draftRoute(), [`PATCH /api/vacancies/${VACANCY_ID}`]: reply(200, { vacancy: { ...detail, status: 'draft', viewer: { can_manage: true, transitions: ['published'] } } }) }))
    const { router } = renderApp(editPath)
    await click('Сохранить')
    await waitFor(() => expect(router.state.location.pathname).toBe(`/vacancies/${VACANCY_ID}`))
    expect(api.called('POST', `/api/vacancies/${VACANCY_ID}/status`)).toHaveLength(0)
    expect(await screen.findByText('Изменения сохранены')).toBeInTheDocument()
  })

  it('has a single save button for a published vacancy', async () => {
    stubApi(base(vacancyRoute(withViewer({ status: 'published' }, true, ['closed']))))
    renderApp(editPath)
    await screen.findByRole('heading', { level: 1, name: 'Правка вакансии' })
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Сохранить и опубликовать' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Для черновика достаточно/)).not.toBeInTheDocument()
  })

  it('does not let a person edit a vacancy they cannot manage', async () => {
    stubApi(base(vacancyRoute(withViewer({}, false))))
    renderApp(editPath)
    expect(await screen.findByRole('heading', { level: 2, name: 'Нет прав на эту вакансию' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Назад к вакансии' })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}`)
    expect(screen.queryByRole('button', { name: 'Сохранить' })).not.toBeInTheDocument()
  })

  it('says the vacancy does not exist', async () => {
    stubApi(base({ [`GET /api/vacancies/${VACANCY_ID}`]: apiError(404, 'not_found', 'Такой вакансии нет') }))
    renderApp(editPath)
    expect(await screen.findByRole('heading', { level: 2, name: 'Такой вакансии нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К моим вакансиям' })).toHaveAttribute('href', '/my-vacancies')
  })

  it('shows an error with retry when the vacancy cannot be loaded', async () => {
    let n = 0
    stubApi(base({ [`GET /api/vacancies/${VACANCY_ID}`]: () => (n++ === 0 ? reply(500) : reply(200, { vacancy: withViewer({ status: 'draft' }, true, ['published']) })) }))
    renderApp(editPath)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить данные для формы')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Правка вакансии' })).toBeInTheDocument()
  })

  it('goes back to the vacancy on cancel', async () => {
    stubApi(base(draftRoute()))
    const { router } = renderApp(editPath)
    await click('Отмена')
    await waitFor(() => expect(router.state.location.pathname).toBe(`/vacancies/${VACANCY_ID}`))
  })
})

describe('leaving a started vacancy form', () => {
  it('asks before leaving, and stays on «Остаться»', async () => {
    stubApi(base())
    const { router } = renderApp('/my-vacancies/new')
    await screen.findByLabelText(/Название вакансии/)
    await fill(/Название вакансии/, 'Научный сотрудник в лабораторию')
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Основное меню' })).getAllByRole('link')[0])
    const dialog = await screen.findByRole('dialog', { name: 'Уйти без сохранения?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Остаться' }))
    expect(router.state.location.pathname).toBe('/my-vacancies/new')
    expect(field(/Название вакансии/)).toHaveValue('Научный сотрудник в лабораторию')
  })

  it('does not ask when nothing was typed', async () => {
    stubApi(base())
    const { router } = renderApp('/my-vacancies/new')
    await screen.findByRole('heading', { level: 1 })
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Основное меню' })).getAllByRole('link')[0])
    await waitFor(() => expect(router.state.location.pathname).not.toBe('/my-vacancies/new'))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

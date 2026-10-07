import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { SLUG } from '../../test/orgs'
import { renderApp } from '../../test/render'
import { detail, VACANCY_ID, vacancyRoute, withViewer } from '../../test/vacancies'

const path = `/vacancies/${VACANCY_ID}`
const staff = (extra = {}) => ({ ...signedInAs(ann), ...extra })
/** Нажимает кнопку, дождавшись, пока страница загрузится. */
const click = async (name: string) => userEvent.click(await screen.findByRole('button', { name }))

describe('vacancy page for a visitor', () => {
  it('sets the vacancy as an article: title, organization, summary, facts, specialties, texts', async () => {
    stubApi(vacancyRoute(detail))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: detail.title })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', `/organizations/${SLUG}`)
    expect(screen.getByRole('link', { name: detail.unit!.name })).toHaveAttribute('href', `/organizations/${SLUG}/units/${detail.unit!.id}`)
    expect(screen.getByText('Конкурс')).toBeInTheDocument()
    expect(screen.getByText(detail.summary)).toBeInTheDocument()
    const facts = within(screen.getByRole('heading', { level: 2, name: 'Условия и требования' }).closest('section')!)
    const fact = (name: string) => facts.getByText(name).closest('div')!
    expect(within(fact('Уровень')).getByText('R3 · самостоятельный исследователь')).toBeInTheDocument()
    expect(within(fact('Формат работы')).getByText('Очно')).toBeInTheDocument()
    expect(within(fact('Место')).getByText('Новосибирск, Новосибирская область')).toBeInTheDocument()
    expect(within(fact('Ставка')).getByText('1 ставка')).toBeInTheDocument()
    expect(within(fact('Зарплата')).getByText('95 000 – 130 000 ₽, в месяц до вычета налога')).toBeInTheDocument()
    expect(within(fact('Договор')).getByText('Срочный договор, 3 года')).toBeInTheDocument()
    expect(within(fact('Финансирование')).getByText('Грант, Грант РНФ 24-12-00184')).toBeInTheDocument()
    expect(within(fact('Жильё')).getByText('Служебное жильё')).toBeInTheDocument()
    expect(within(fact('Требуемая степень')).getByText('Кандидат наук')).toBeInTheDocument()
    expect(facts.queryByText('Требуемое звание')).not.toBeInTheDocument()
    // У опубликованной срок стоит в шапке и в условиях не повторяется.
    expect(facts.queryByText('Срок подачи')).not.toBeInTheDocument()
    expect(facts.getByText('Физика конденсированного состояния')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Направление работы' })).toBeInTheDocument()
    expect(screen.getByText('Рост и характеризация плёнок')).toBeInTheDocument()
    expect(screen.getByText(/Лаборатория ведёт четыре проекта/)).toBeInTheDocument()
    expect(screen.getByText('Кандидат наук, опыт эпитаксии.')).toBeInTheDocument()
    expect(screen.getByText(/Заявки до/)).toBeInTheDocument()
    // Посетителю управления нет.
    expect(screen.queryByRole('region', { name: 'Управление вакансией' })).not.toBeInTheDocument()
  })

  it('skips what is not set: no unit, no place, no pay, no requirements, no deadline', async () => {
    stubApi(
      vacancyRoute({
        ...detail,
        unit: null,
        city: '',
        region: null,
        salary_from: null,
        salary_to: null,
        rate_percent: null,
        contract_type: '',
        contract_months: null,
        work_format: '',
        career_level: null,
        housing: 'none',
        funding_source: '',
        degree_required: 'none',
        requirements: '',
        focus: '',
        description: '',
        summary: '',
        specialties: [],
        deadline: '',
        is_competition: false,
        position: { code: 'grant_manager', name: 'Грант-менеджер', type: 'admin' },
      }),
    )
    renderApp(path)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    const facts = within(screen.getByRole('heading', { level: 2, name: 'Условия и требования' }).closest('section')!)
    expect(facts.getAllByRole('term').map((n) => n.textContent)).toEqual(['Должность'])
    expect(screen.getByText('Особых требований не указано.')).toBeInTheDocument()
    expect(screen.queryByText('Конкурс')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: 'Описание' })).not.toBeInTheDocument()
  })

  it('says «кроме степени» when there are no written requirements but a degree is required', async () => {
    stubApi(vacancyRoute({ ...detail, requirements: '', degree_required: 'candidate', title_required: 'none' }))
    renderApp(path)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    expect(screen.getByText('Кроме степени или звания из условий выше, особых требований не указано.')).toBeInTheDocument()
    expect(screen.queryByText('Особых требований не указано.')).not.toBeInTheDocument()
  })

  it('shows the required title for teaching staff and a stipend for postgraduate study', async () => {
    stubApi(vacancyRoute({ ...detail, title_required: 'professor', position: { code: 'phd_student', name: 'Аспирантура', type: 'phd' } }))
    renderApp(path)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    expect(screen.getByText('Стипендия')).toBeInTheDocument()
    expect(screen.getByText('Профессор')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Тема исследования' })).toBeInTheDocument()
  })

  it('keeps the deadline among the facts once the vacancy is not published', async () => {
    stubApi(vacancyRoute({ ...detail, status: 'closed' }))
    renderApp(path)
    expect(await screen.findByText('Срок подачи')).toBeInTheDocument()
    expect(screen.getByText(/31 декабря 2099/)).toBeInTheDocument()
  })

  it('says that a closed vacancy is closed and hides the deadline marker', async () => {
    stubApi(vacancyRoute({ ...detail, status: 'closed' }))
    renderApp(path)
    expect(await screen.findByText('Набор закрыт. Страница осталась доступной по ссылке.')).toBeInTheDocument()
    expect(screen.getByText('Закрыта')).toBeInTheDocument()
    expect(screen.queryByText(/Заявки до/)).not.toBeInTheDocument()
  })

  it('says that it is only visible to staff when a draft or archived one is opened by them', async () => {
    stubApi(staff(vacancyRoute(withViewer({ status: 'draft' }, true, ['published']))))
    renderApp(path)
    expect(await screen.findByText(/Это черновик/)).toBeInTheDocument()
  })

  it('explains an archived vacancy', async () => {
    stubApi(staff(vacancyRoute(withViewer({ status: 'archived' }, true, ['closed']))))
    renderApp(path)
    expect(await screen.findByText(/Вакансия в архиве/)).toBeInTheDocument()
  })

  it('says the vacancy does not exist (also for a hidden draft)', async () => {
    stubApi({ [`GET /api/vacancies/${VACANCY_ID}`]: apiError(404, 'not_found', 'Такой вакансии нет') })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('shows loading, then an error with retry', async () => {
    let n = 0
    stubApi({ [`GET /api/vacancies/${VACANCY_ID}`]: () => (n++ === 0 ? reply(500) : reply(200, { vacancy: detail })) })
    renderApp(path)
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить вакансию')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: detail.title })).toBeInTheDocument()
  })
})

describe('managing a vacancy from its page', () => {
  const statusPath = `POST /api/vacancies/${VACANCY_ID}/status`

  it('offers edit and the allowed status changes, and shows what changed', async () => {
    let current = withViewer({ status: 'draft' }, true, ['published'])
    const api = stubApi({
      ...signedInAs(ann),
      [`GET /api/vacancies/${VACANCY_ID}`]: () => reply(200, { vacancy: current }),
      [statusPath]: () => {
        current = withViewer({ status: 'published' }, true, ['closed'])
        return reply(200, { vacancy: current })
      },
    })
    renderApp(path)
    const bar = within(await screen.findByRole('region', { name: 'Управление вакансией' }))
    expect(bar.getByRole('link', { name: 'Править' })).toHaveAttribute('href', `/my-vacancies/${VACANCY_ID}/edit`)
    expect(bar.getByRole('button', { name: 'Удалить черновик' })).toBeInTheDocument()
    await userEvent.click(bar.getByRole('button', { name: 'Опубликовать' }))
    expect(await screen.findByText('Вакансия опубликована')).toBeInTheDocument()
    expect(api.called('POST', `/api/vacancies/${VACANCY_ID}/status`)[0].body).toEqual({ status: 'published' })
    expect(await within(screen.getByRole('region', { name: 'Управление вакансией' })).findByRole('button', { name: 'Закрыть набор' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Удалить черновик' })).not.toBeInTheDocument()
  })

  it('offers reopening and archiving for a closed vacancy, and restoring for an archived one', async () => {
    stubApi(staff(vacancyRoute(withViewer({ status: 'closed' }, true, ['published', 'archived']))))
    renderApp(path)
    const bar = within(await screen.findByRole('region', { name: 'Управление вакансией' }))
    expect(bar.getByRole('button', { name: 'Открыть снова' })).toBeInTheDocument()
    expect(bar.getByRole('button', { name: 'В архив' })).toBeInTheDocument()
    expect(bar.queryByRole('button', { name: 'Удалить черновик' })).not.toBeInTheDocument()
  })

  it('names what is missing when the vacancy cannot be published yet, and links to the form', async () => {
    stubApi(
      staff({
        ...vacancyRoute(withViewer({ status: 'draft' }, true, ['published'])),
        [statusPath]: apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { city: 'Укажите город', summary: 'Напишите короткую аннотацию', odd: 'Что-то ещё' } }),
      }),
    )
    renderApp(path)
    await click('Опубликовать')
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Вакансию пока нельзя опубликовать')
    expect(alert).toHaveTextContent('Напишите короткую аннотацию; Укажите город; Что-то ещё')
    expect(within(alert).getByRole('link', { name: 'Дополнить вакансию' })).toHaveAttribute('href', `/my-vacancies/${VACANCY_ID}/edit`)
  })

  it('reports another failure with a message and keeps the page', async () => {
    stubApi(staff({ ...vacancyRoute(withViewer({ status: 'draft' }, true, ['published'])), [statusPath]: apiError(409, 'invalid_status_change', 'Так менять статус вакансии нельзя') }))
    renderApp(path)
    await click('Опубликовать')
    expect(await screen.findByText('Не удалось изменить статус')).toBeInTheDocument()
    expect(screen.getByText('Так менять статус вакансии нельзя')).toBeInTheDocument()
    expect(screen.queryByText('Вакансию пока нельзя опубликовать')).not.toBeInTheDocument()
  })

  it('deletes a draft after confirmation and goes to the list', async () => {
    const api = stubApi(
      staff({
        ...vacancyRoute(withViewer({ status: 'draft' }, true, ['published'])),
        [`DELETE /api/vacancies/${VACANCY_ID}`]: reply(204),
        'GET /api/my/vacancies?limit=50&offset=0': reply(200, { items: [], total: 0, counts: { draft: 0, published: 0, closed: 0, archived: 0 } }),
        'GET /api/my/vacancy-targets': reply(200, { targets: [] }),
      }),
    )
    const { router } = renderApp(path)
    await click('Удалить черновик')
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent('Удалить черновик?')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Удалить черновик' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/my-vacancies'))
    expect(api.called('DELETE', `/api/vacancies/${VACANCY_ID}`)).toHaveLength(1)
    expect(await screen.findByText('Черновик удалён')).toBeInTheDocument()
  })

  it('can be backed out of, and a failed deletion says so', async () => {
    stubApi(staff({ ...vacancyRoute(withViewer({ status: 'draft' }, true, ['published'])), [`DELETE /api/vacancies/${VACANCY_ID}`]: apiError(409, 'vacancy_not_draft', 'Удалить можно только черновик') }))
    renderApp(path)
    await click('Удалить черновик')
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await click('Удалить черновик')
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Удалить черновик' }))
    expect(await screen.findByText('Не удалось удалить черновик')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows no management to a signed-in person who cannot manage this vacancy', async () => {
    stubApi(staff(vacancyRoute(detail)))
    renderApp(path)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    expect(screen.queryByRole('region', { name: 'Управление вакансией' })).not.toBeInTheDocument()
  })
})

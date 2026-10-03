import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { APP_ID, application, canApply, stateRoute } from '../../test/applications'
import { fill, press } from '../../test/forms'
import { emptyProfile, ownPage, profile } from '../../test/profile'
import { renderApp } from '../../test/render'
import { detail, VACANCY_ID, vacancyRoute, withViewer } from '../../test/vacancies'
import type { ApplyState } from './api'

const applyPath = `/vacancies/${VACANCY_ID}/apply`
const vacancyPath = `/vacancies/${VACANCY_ID}`

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  ...vacancyRoute(detail),
  ...stateRoute(canApply),
  'GET /api/profile': reply(200, ownPage()),
  ...extra,
})

const cover = 'Здравствуйте! Занимаюсь эпитаксией плёнок и хотела бы работать в вашей лаборатории.'
const pdf = (name = 'Список.pdf', size = 1000) => new File([new Uint8Array(size)], name, { type: 'application/pdf' })
const fileInput = () => document.querySelector<HTMLInputElement>('input[type="file"]')!

describe('apply button on the vacancy page', () => {
  it('sends a visitor to sign in and back to the form', async () => {
    stubApi(vacancyRoute(detail))
    renderApp(vacancyPath)
    const link = await screen.findByRole('link', { name: 'Войти и откликнуться' })
    expect(link).toHaveAttribute('href', `/login?next=${encodeURIComponent(applyPath)}`)
  })

  it('offers the form to a signed-in person who can apply', async () => {
    stubApi(signedIn())
    renderApp(vacancyPath)
    expect(await screen.findByRole('link', { name: 'Откликнуться' })).toHaveAttribute('href', applyPath)
  })

  it('shows the existing application instead of the button', async () => {
    const applied: ApplyState = { can_apply: false, reason: 'applied', application: { id: APP_ID, status: 'sent' } }
    stubApi(signedIn(stateRoute(applied)))
    renderApp(vacancyPath)
    expect(await screen.findByText('Вы уже откликнулись на эту вакансию.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Смотреть отклик' })).toHaveAttribute('href', `/applications/${APP_ID}`)
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
  })

  it('says the deadline has passed and offers nothing', async () => {
    stubApi(signedIn(stateRoute({ can_apply: false, reason: 'deadline_passed', application: null })))
    renderApp(vacancyPath)
    expect(await screen.findByText('Срок подачи прошёл: отклики не принимаются.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
  })

  it.each(['own_vacancy', 'closed'] as const)('shows nothing for the reason %s', async (reason) => {
    const { called } = stubApi(signedIn(stateRoute({ can_apply: false, reason, application: null })))
    renderApp(vacancyPath)
    await screen.findByRole('heading', { level: 1, name: detail.title })
    await waitFor(() => expect(called('GET', `/api/applications/for-vacancy/${VACANCY_ID}`)).toHaveLength(1))
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
    expect(screen.queryByText('Срок подачи прошёл: отклики не принимаются.')).not.toBeInTheDocument()
  })

  it('does not break the vacancy page when the check fails', async () => {
    stubApi(signedIn({ [`GET /api/applications/for-vacancy/${VACANCY_ID}`]: apiError(500, 'internal', 'Что-то сломалось') }))
    renderApp(vacancyPath)
    expect(await screen.findByRole('heading', { level: 1, name: detail.title })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
  })

  it('does not offer to apply to the people who manage the vacancy', async () => {
    const { called } = stubApi(signedIn(vacancyRoute(withViewer({}, true, ['closed']))))
    renderApp(vacancyPath)
    await screen.findByRole('region', { name: 'Управление вакансией' })
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
    expect(called('GET', `/api/applications/for-vacancy/${VACANCY_ID}`)).toHaveLength(0)
  })
})

describe('apply page', () => {
  it('sends a visitor to sign in', async () => {
    stubApi(vacancyRoute(detail))
    const { router } = renderApp(applyPath)
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe(`?next=${encodeURIComponent(applyPath)}`)
  })

  it('says what the organization will see and prefills the contact from the profile', async () => {
    stubApi(signedIn())
    renderApp(applyPath)
    expect(await screen.findByRole('heading', { level: 1, name: 'Отклик на вакансию' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: detail.title })).toHaveAttribute('href', vacancyPath)
    expect(screen.getByText('Сибирский институт')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Что увидит организация' })).toBeInTheDocument()
    expect(screen.getByText(/Профиль уйдёт, даже если он скрыт/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Открыть мой профиль' })).toHaveAttribute('href', '/profile')
    expect(screen.getByLabelText(/Почта для связи/)).toHaveValue(profile.contact_email)
    expect(screen.getByText('Файлов нет. Резюме из профиля приложится само.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Отправить отклик' })).toBeEnabled()
  })

  it('falls back to the account email when the profile has no contact', async () => {
    stubApi(signedIn({ 'GET /api/profile': reply(200, ownPage({ ...profile, contact_email: undefined })) }))
    renderApp(applyPath)
    expect(await screen.findByLabelText(/Почта для связи/)).toHaveValue(ann.email)
  })

  it('refuses an empty profile and sends the person to fill it in', async () => {
    stubApi(signedIn({ 'GET /api/profile': reply(200, ownPage(emptyProfile)) }))
    renderApp(applyPath)
    expect(await screen.findByText('В профиле не указано, кто вы')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Заполнить профиль' })).toHaveAttribute('href', '/profile/edit')
    expect(screen.getByRole('button', { name: 'Отправить отклик' })).toBeDisabled()
  })

  it.each([
    ['own_vacancy', 'Вы ведёте эту вакансию и видите отклики на неё, поэтому откликнуться на неё нельзя.'],
    ['closed', 'Вакансия закрыта: отклики не принимаются.'],
    ['deadline_passed', 'Срок подачи прошёл: отклики не принимаются.'],
  ] as const)('shows why the form is not available (%s)', async (reason, text) => {
    stubApi(signedIn(stateRoute({ can_apply: false, reason, application: null })))
    renderApp(applyPath)
    expect(await screen.findByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отправить отклик' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Назад к вакансии' })).toHaveAttribute('href', vacancyPath)
  })

  it('links to the application when the person has already applied', async () => {
    stubApi(signedIn(stateRoute({ can_apply: false, reason: 'applied', application: { id: APP_ID, status: 'sent' } })))
    renderApp(applyPath)
    expect(await screen.findByText('Вы уже откликнулись на эту вакансию.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Смотреть отклик' })).toHaveAttribute('href', `/applications/${APP_ID}`)
  })

  it('shows not found for an unknown vacancy and a way to retry a failed load', async () => {
    stubApi({ ...signedIn(), [`GET /api/vacancies/${VACANCY_ID}`]: apiError(404, 'not_found', 'Такой вакансии нет') })
    renderApp(applyPath)
    expect(await screen.findByRole('heading', { name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('shows not found when the state check says the vacancy is hidden', async () => {
    stubApi(signedIn({ [`GET /api/applications/for-vacancy/${VACANCY_ID}`]: apiError(404, 'not_found', 'Нет') }))
    renderApp(applyPath)
    expect(await screen.findByRole('heading', { name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it.each([
    ['vacancy', { [`GET /api/vacancies/${VACANCY_ID}`]: apiError(500, 'internal', 'Сбой') }],
    ['state', { [`GET /api/applications/for-vacancy/${VACANCY_ID}`]: apiError(500, 'internal', 'Сбой') }],
    ['profile', { 'GET /api/profile': apiError(500, 'internal', 'Сбой') }],
  ])('offers to retry when the %s does not load', async (_name, routes) => {
    stubApi(signedIn(routes))
    renderApp(applyPath)
    expect(await screen.findByText('Не удалось проверить, можно ли откликнуться')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    // Повтор спрашивает ещё раз то, что не загрузилось.
    await waitFor(() => expect(screen.getByText('Не удалось проверить, можно ли откликнуться')).toBeInTheDocument())
  })

  it('shows the form once the retry succeeds', async () => {
    let fail = true
    stubApi(signedIn({ [`GET /api/applications/for-vacancy/${VACANCY_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, canApply)) }))
    renderApp(applyPath)
    await screen.findByText('Не удалось проверить, можно ли откликнуться')
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('button', { name: 'Отправить отклик' })).toBeInTheDocument()
  })

  it('checks the contact and the letter before sending', async () => {
    const { calls } = stubApi(signedIn())
    renderApp(applyPath)
    const contact = await screen.findByLabelText(/Почта для связи/)
    await userEvent.clear(contact)
    await fill(/Сопроводительное письмо/, 'коротко')
    await press('Отправить отклик')
    expect(await screen.findByText('Укажите почту')).toBeInTheDocument()
    expect(screen.getByText('Письмо слишком короткое: напишите хотя бы 20 знаков')).toBeInTheDocument()
    expect(contact).toHaveFocus()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
    // Счётчик знаков идёт по набранному.
    expect(screen.getByText('7 из 6000')).toBeInTheDocument()
  })

  it('counts the letter and warns when it is too long', async () => {
    stubApi(signedIn())
    renderApp(applyPath)
    const letter = await screen.findByLabelText(/Сопроводительное письмо/)
    await userEvent.click(letter)
    await userEvent.paste('я'.repeat(6001))
    const count = screen.getByText('6001 из 6000')
    expect(count).toHaveAttribute('data-over')
    await press('Отправить отклик')
    expect(await screen.findByText(/^Письмо длиннее 6000 знаков/)).toBeInTheDocument()
  })

  it('sends the application with the letter, files and referees and opens it', async () => {
    const { calls } = stubApi(
      signedIn({
        'POST /api/applications': reply(201, { application }),
        [`GET /api/applications/${APP_ID}`]: reply(200, { application }),
      }),
    )
    const { router } = renderApp(applyPath)
    await screen.findByLabelText(/Сопроводительное письмо/)
    await fill(/Сопроводительное письмо/, cover)
    await userEvent.upload(fileInput(), [pdf('Список публикаций.pdf'), pdf('Диплом.pdf')])
    expect(screen.getByText('Список публикаций.pdf')).toBeInTheDocument()
    expect(screen.getByText('Диплом.pdf')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Добавить рекомендателя' }))
    const group = screen.getByRole('group', { name: 'Рекомендатель 1' })
    await userEvent.type(within(group).getByLabelText(/^Имя/), 'Андрей Козлов')
    await userEvent.type(within(group).getByLabelText(/^Почта/), 'kozlov@example.ru')
    await userEvent.type(within(group).getByLabelText(/Кем он вам приходится/), 'научный руководитель')
    await press('Отправить отклик')

    await waitFor(() => expect(router.state.location.pathname).toBe(`/applications/${APP_ID}`))
    const post = calls.find((c) => c.method === 'POST' && c.path === '/api/applications')!
    expect(post.body).toEqual({
      vacancy_id: VACANCY_ID,
      contact_email: profile.contact_email,
      cover_letter: cover,
      referees: [{ name: 'Андрей Козлов', email: 'kozlov@example.ru', relation: 'научный руководитель' }],
      __files: ['Список публикаций.pdf', 'Диплом.pdf'],
    })
    expect(await screen.findByText('Отклик отправлен', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('refuses files that are not PDF or too big right in the form', async () => {
    stubApi(signedIn())
    renderApp(applyPath)
    await screen.findByLabelText(/Сопроводительное письмо/)
    await userEvent.upload(fileInput(), new File(['x'], 'фото.png', { type: 'image/png' }), { applyAccept: false })
    expect(screen.getByRole('alert')).toHaveTextContent('«фото.png» не PDF')
    await userEvent.upload(fileInput(), pdf('Огромный.pdf', 11 * 1024 * 1024))
    expect(screen.getByRole('alert')).toHaveTextContent('«Огромный.pdf» больше 10 МБ')
    const six = Array.from({ length: 6 }, (_, i) => pdf(`Файл ${i + 1}.pdf`))
    await userEvent.upload(fileInput(), six)
    expect(screen.getByRole('alert')).toHaveTextContent('Можно приложить не больше пяти файлов')
    expect(screen.getByText('Файл 5.pdf')).toBeInTheDocument()
    expect(screen.queryByText('Файл 6.pdf')).not.toBeInTheDocument()
  })

  it('lets the person remove files and referees and caps the referees at three', async () => {
    stubApi(signedIn())
    renderApp(applyPath)
    await userEvent.upload(await screen.findByLabelText(/Сопроводительное письмо/).then(() => fileInput()), [pdf('Один.pdf'), pdf('Два.pdf')])
    await userEvent.click(screen.getByRole('button', { name: 'Убрать файл «Один.pdf»' }))
    expect(screen.queryByText('Один.pdf')).not.toBeInTheDocument()
    expect(screen.getByText('Два.pdf')).toBeInTheDocument()

    const add = () => userEvent.click(screen.getByRole('button', { name: 'Добавить рекомендателя' }))
    await add()
    await add()
    await add()
    expect(screen.getAllByRole('group', { name: /Рекомендатель \d/ })).toHaveLength(3)
    expect(screen.queryByRole('button', { name: 'Добавить рекомендателя' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Убрать рекомендателя 2' }))
    expect(screen.getAllByRole('group', { name: /Рекомендатель \d/ })).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Добавить рекомендателя' })).toBeInTheDocument()
  })

  it('shows what the server did not like in the files, referees and profile', async () => {
    stubApi(
      signedIn({
        'POST /api/applications': apiError(422, 'validation_failed', 'Проверьте отклик', {
          fields: { files: '«a.pdf»: Файл не похож на PDF', referees: 'Рекомендатель 1: укажите, как зовут рекомендателя', profile: 'Профиль слишком пуст' },
        }),
      }),
    )
    renderApp(applyPath)
    await screen.findByLabelText(/Сопроводительное письмо/)
    await fill(/Сопроводительное письмо/, cover)
    await userEvent.click(screen.getByRole('button', { name: 'Добавить рекомендателя' }))
    await press('Отправить отклик')
    expect(await screen.findByText('«a.pdf»: Файл не похож на PDF')).toBeInTheDocument()
    expect(screen.getByText('Рекомендатель 1: укажите, как зовут рекомендателя')).toBeInTheDocument()
    expect(screen.getByText('Профиль слишком пуст')).toBeInTheDocument()
  })

  it('shows a general failure above the form and keeps what was typed', async () => {
    stubApi(signedIn({ 'POST /api/applications': apiError(409, 'already_applied', 'Вы уже откликнулись на эту вакансию') }))
    renderApp(applyPath)
    await screen.findByLabelText(/Сопроводительное письмо/)
    await fill(/Сопроводительное письмо/, cover)
    await press('Отправить отклик')
    expect(await screen.findByText('Не удалось отправить отклик')).toBeInTheDocument()
    expect(screen.getAllByText('Вы уже откликнулись на эту вакансию').length).toBeGreaterThan(0)
    expect(screen.getByLabelText(/Сопроводительное письмо/)).toHaveValue(cover)
  })

  it('stays out of the way when the server is unreachable', async () => {
    stubApi(signedIn({ 'POST /api/applications': reply(500) }))
    renderApp(applyPath)
    await screen.findByLabelText(/Сопроводительное письмо/)
    await fill(/Сопроводительное письмо/, cover)
    await press('Отправить отклик')
    expect(await screen.findByText('Не удалось отправить отклик')).toBeInTheDocument()
    expect(screen.getByText('Сервер не отвечает')).toBeInTheDocument()
  })
})

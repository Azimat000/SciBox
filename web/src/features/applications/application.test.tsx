import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import {
  APP_ID,
  REF_ID,
  application,
  attachment,
  cvFile,
  declinedRef,
  letterFile,
  list,
  pendingRef,
  receivedRef,
  resendableRef,
  staffApplication,
  staffFileOnly,
  staffTextOnly,
  summary,
} from '../../test/applications'
import { renderApp } from '../../test/render'
import type { Detail } from './api'

const click = async (name: string | RegExp) => userEvent.click(await screen.findByRole('button', { name }))
const mine = (extra: Record<string, Route> = {}): Record<string, Route> => ({ ...signedInAs(ann), ...extra })
const one = (d: Detail = application, extra: Record<string, Route> = {}) => mine({ [`GET /api/applications/${d.id}`]: reply(200, { application: d }), ...extra })

describe('my applications', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/applications')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists applications with status, date and how many recommendations arrived', async () => {
    const second = { ...summary, id: '11111111-aaaa-4aaa-8aaa-111111111111', status: 'invited' as const, references: { total: 0, received: 0 }, vacancy: { ...summary.vacancy, title: 'Постдок в лаборатории', status: 'closed' } }
    const third = { ...summary, id: '22222222-aaaa-4aaa-8aaa-222222222222', status: 'withdrawn' as const, vacancy: { ...summary.vacancy, title: 'Доцент кафедры физики' } }
    stubApi(mine({ 'GET /api/applications?*': reply(200, list([summary, second, third])) }))
    renderApp('/applications')
    expect(await screen.findByRole('heading', { level: 1, name: 'Мои отклики' })).toBeInTheDocument()
    expect(screen.getByText('3 отклика')).toBeInTheDocument()
    const entries = screen.getAllByRole('listitem').filter((li) => li.classList.contains('app-entry'))
    expect(entries).toHaveLength(3)
    const first = within(entries[0])
    expect(first.getByRole('link', { name: summary.vacancy.title })).toHaveAttribute('href', `/applications/${APP_ID}`)
    expect(first.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', '/organizations/sibirskiy-institut')
    expect(first.getByText('Отправлен')).toBeInTheDocument()
    expect(first.getByText(/Отклик от 3 октября 2026/)).toBeInTheDocument()
    expect(first.getByText('Рекомендации: 1 из 3')).toBeInTheDocument()
    const secondEntry = within(entries[1])
    expect(secondEntry.getByText('Приглашение')).toBeInTheDocument()
    expect(secondEntry.getByText('Вакансия закрыта')).toBeInTheDocument()
    expect(secondEntry.queryByText(/Рекомендации/)).not.toBeInTheDocument()
    expect(within(entries[2]).getByText('Отозван')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Страницы откликов' })).not.toBeInTheDocument()
  })

  it('says there is nothing yet and points to the search', async () => {
    stubApi(mine({ 'GET /api/applications?*': reply(200, list([])) }))
    renderApp('/applications')
    expect(await screen.findByRole('heading', { name: 'Вы пока никуда не откликались' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Искать вакансии' })).toHaveAttribute('href', '/vacancies')
  })

  it('pages through long lists and keeps the page in the address', async () => {
    const items = Array.from({ length: 20 }, (_, i) => ({ ...summary, id: `00000000-0000-4000-8000-${String(i).padStart(12, '0')}` }))
    const { calls } = stubApi(mine({ 'GET /api/applications?*': reply(200, list(items, 45)) }))
    const { router } = renderApp('/applications')
    await screen.findByText('Страница 1 из 3')
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await click('Дальше')
    expect(await screen.findByText('Страница 2 из 3')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    expect(calls.some((c) => c.path === '/api/applications?limit=20&offset=20')).toBe(true)
    await click('Назад')
    await screen.findByText('Страница 1 из 3')
    expect(router.state.location.search).toBe('')
  })

  it('offers to retry when the list does not load', async () => {
    let fail = true
    stubApi(mine({ 'GET /api/applications?*': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, list([summary]))) }))
    renderApp('/applications')
    expect(await screen.findByText('Не удалось загрузить отклики')).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('link', { name: summary.vacancy.title })).toBeInTheDocument()
  })
})

describe('application page (applicant)', () => {
  it('shows what was sent: letter, contact, files, recommendations, the profile snapshot', async () => {
    stubApi(one())
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByRole('heading', { level: 1, name: application.vacancy.title })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: application.vacancy.title })).toHaveAttribute('href', `/vacancies/${application.vacancy.id}`)
    expect(screen.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', '/organizations/sibirskiy-institut')
    expect(screen.getByText('Отправлен')).toBeInTheDocument()
    expect(screen.getByText(/Здравствуйте!/)).toBeInTheDocument()
    expect(screen.getAllByText('anna.apply@example.ru').length).toBeGreaterThan(0)

    const cv = screen.getByRole('link', { name: /Резюме из профиля, Анна Смирнова — CV.pdf \(28 КБ\)/ })
    expect(cv).toHaveAttribute('href', `/api/applications/${APP_ID}/files/${cvFile.id}`)
    expect(cv).toHaveAttribute('download')
    expect(screen.getByRole('link', { name: 'Список публикаций.pdf (16 КБ)' })).toHaveAttribute('href', `/api/applications/${APP_ID}/files/${attachment.id}`)

    const refs = within(screen.getByRole('heading', { level: 2, name: 'Рекомендации' }).closest('section')!)
    expect(refs.getByText('Содержание писем видит только организация. Вы узнаёте лишь, ответил ли человек.')).toBeInTheDocument()
    expect(refs.getByText('Письмо получено')).toBeInTheDocument()
    expect(refs.getByText('Ждём ответа')).toBeInTheDocument()
    expect(refs.getByText('Отказался')).toBeInTheDocument()
    expect(refs.getByText('коллега по лаборатории · mironova@example.ru')).toBeInTheDocument()
    expect(refs.getByText('mironova@example.ru', { exact: false })).toBeInTheDocument()
    expect(refs.getByText(/ссылка работает до 2 ноября 2026/)).toBeInTheDocument()
    expect(refs.getByText(/Повторить можно/)).toBeInTheDocument()
    // Письма рекомендателей на этой странице нет и быть не может.
    expect(screen.queryByText(/Знаю Анну/)).not.toBeInTheDocument()

    // Профиль спрятан под заголовком, пока его не попросят.
    expect(screen.getByRole('heading', { level: 2, name: 'Профиль, который получила организация' })).toBeInTheDocument()
    expect(screen.getByText('Показать профиль')).toBeInTheDocument()
  })

  it('says there are no extra files and no recommendations', async () => {
    stubApi(one({ ...application, files: [], references: [] }))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByText('Других файлов вы не прикладывали.')).toBeInTheDocument()
    expect(screen.getByText('Вы пока никого не просили о рекомендации.')).toBeInTheDocument()
  })

  it('works without a resume file in the list', async () => {
    stubApi(one({ ...application, cv: null }))
    renderApp(`/applications/${APP_ID}`)
    await screen.findByText('Список публикаций.pdf (16 КБ)')
    expect(screen.queryByText(/Резюме из профиля,/)).not.toBeInTheDocument()
  })

  it('sends the staff to the card meant for the organization', async () => {
    stubApi(one({ ...application, viewer: { role: 'staff', can_withdraw: false, decisions: [], can_invite: false }, references: [] }))
    const { router } = renderApp(`/applications/${APP_ID}`)
    await waitFor(() => expect(router.state.location.pathname).toBe(`/candidates/${APP_ID}`))
  })

  it('shows not found for someone else’s application and offers a retry on failure', async () => {
    stubApi(mine({ [`GET /api/applications/${APP_ID}`]: apiError(404, 'not_found', 'Такого отклика нет') }))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByRole('heading', { name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('offers to retry when the application does not load', async () => {
    let fail = true
    stubApi(mine({ [`GET /api/applications/${APP_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { application })) }))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByText('Не удалось открыть отклик')).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: application.vacancy.title })).toBeInTheDocument()
  })

  it('withdraws the application after confirmation and shows the new status', async () => {
    let withdrawn = false
    const { calls } = stubApi(
      one(application, {
        [`GET /api/applications/${APP_ID}`]: () => reply(200, { application: withdrawn ? { ...application, status: 'withdrawn', viewer: { role: 'applicant', can_withdraw: false } } : application }),
        [`POST /api/applications/${APP_ID}/withdraw`]: () => {
          withdrawn = true
          return reply(204)
        },
      }),
    )
    renderApp(`/applications/${APP_ID}`)
    await click('Отозвать отклик')
    expect(screen.getByRole('dialog', { name: 'Отозвать отклик?' })).toBeInTheDocument()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отозвать отклик' }))
    expect(await screen.findByText('Отклик отозван', { selector: '.toast *' })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Отозвать отклик' })).not.toBeInTheDocument())
    expect(screen.getByText('Отозван', { selector: '.tag' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1)
    // У отозванного отклика рекомендации менять нельзя.
    expect(screen.queryByRole('button', { name: 'Попросить ещё одного' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отменить просьбу' })).not.toBeInTheDocument()
  })

  it('can be cancelled out of the withdraw dialog and reports a failure', async () => {
    stubApi(one(application, { [`POST /api/applications/${APP_ID}/withdraw`]: apiError(409, 'invalid_status_change', 'Этот отклик уже нельзя отозвать') }))
    renderApp(`/applications/${APP_ID}`)
    await click('Отозвать отклик')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await click('Отозвать отклик')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отозвать отклик' }))
    expect(await screen.findByText('Не удалось отозвать отклик', { selector: '.toast *' })).toBeInTheDocument()
    expect(screen.getByText('Этот отклик уже нельзя отозвать', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('does not offer to withdraw once a decision is made', async () => {
    stubApi(one({ ...application, status: 'rejected', viewer: { role: 'applicant', can_withdraw: false, decisions: [], can_invite: false }, references: [receivedRef] }))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByText('Отказ', { selector: '.tag' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отозвать отклик' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Попросить ещё одного' })).not.toBeInTheDocument()
  })
})

describe('recommendations on the application page', () => {
  it('resends the request when the pause is over, and shows a failure', async () => {
    let fail = false
    const { calls } = stubApi(
      one({ ...application, references: [resendableRef] }, {
        [`POST /api/applications/${APP_ID}/references/${resendableRef.id}/resend`]: () => (fail ? apiError(429, 'resend_too_soon', 'Письмо уже отправлено недавно') : reply(200, { reference: resendableRef })),
      }),
    )
    renderApp(`/applications/${APP_ID}`)
    await click('Отправить письмо ещё раз')
    expect(await screen.findByText('Письмо отправлено ещё раз', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1)
    fail = true
    await click('Отправить письмо ещё раз')
    expect(await screen.findByText('Не получилось', { selector: '.toast *' })).toBeInTheDocument()
    expect(screen.getByText('Письмо уже отправлено недавно', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('cancels a pending request after confirmation', async () => {
    const { calls } = stubApi(one({ ...application, references: [pendingRef] }, { [`DELETE /api/applications/${APP_ID}/references/${REF_ID}`]: reply(204) }))
    renderApp(`/applications/${APP_ID}`)
    await click('Отменить просьбу')
    const dialog = screen.getByRole('dialog', { name: 'Отменить просьбу?' })
    expect(within(dialog).getByText(/Ссылка из письма для Мария Миронова перестанет работать/)).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отменить просьбу' }))
    expect(await screen.findByText('Просьба отменена', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(1)
  })

  it('keeps the request when the person changes their mind', async () => {
    const { calls } = stubApi(one({ ...application, references: [pendingRef] }))
    renderApp(`/applications/${APP_ID}`)
    await click('Отменить просьбу')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
    expect(screen.getByText('Мария Миронова')).toBeInTheDocument()
  })

  it('reports a failed cancel and closes the dialog', async () => {
    stubApi(one({ ...application, references: [pendingRef] }, { [`DELETE /api/applications/${APP_ID}/references/${REF_ID}`]: apiError(409, 'reference_not_pending', 'Рекомендатель уже ответил: это действие недоступно') }))
    renderApp(`/applications/${APP_ID}`)
    await click('Отменить просьбу')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отменить просьбу' }))
    expect(await screen.findByText('Рекомендатель уже ответил: это действие недоступно', { selector: '.toast *' })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('does not offer actions on answered requests', async () => {
    stubApi(one({ ...application, references: [receivedRef, declinedRef] }))
    renderApp(`/applications/${APP_ID}`)
    await screen.findByText('Письмо получено')
    expect(screen.queryByRole('button', { name: 'Отменить просьбу' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отправить письмо ещё раз' })).not.toBeInTheDocument()
    expect(screen.getAllByText(/ответил 3 октября/)).toHaveLength(2)
  })

  it('asks one more referee and refreshes the list', async () => {
    const { calls } = stubApi(
      one({ ...application, references: [receivedRef] }, { [`POST /api/applications/${APP_ID}/references`]: reply(201, { reference: pendingRef }) }),
    )
    renderApp(`/applications/${APP_ID}`)
    await click('Попросить ещё одного')
    const dialog = screen.getByRole('dialog', { name: 'Попросить ещё одного рекомендателя' })
    await userEvent.type(within(dialog).getByLabelText(/^Имя/), 'Мария Миронова')
    await userEvent.type(within(dialog).getByLabelText(/^Почта/), 'mironova@example.ru')
    await userEvent.type(within(dialog).getByLabelText(/Кем он вам приходится/), 'коллега')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить просьбу' }))
    expect(await screen.findByText('Просьба отправлена', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ name: 'Мария Миронова', email: 'mironova@example.ru', relation: 'коллега' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('checks the name and the email before asking, then shows what the server says', async () => {
    const { calls } = stubApi(
      one({ ...application, references: [] }, {
        [`POST /api/applications/${APP_ID}/references`]: apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { email: 'Эта почта уже есть среди рекомендателей' } }),
      }),
    )
    renderApp(`/applications/${APP_ID}`)
    await click('Попросить ещё одного')
    const dialog = screen.getByRole('dialog')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить просьбу' }))
    expect(await within(dialog).findByText('Укажите, как зовут рекомендателя')).toBeInTheDocument()
    expect(within(dialog).getByText('Укажите почту')).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
    await userEvent.type(within(dialog).getByLabelText(/^Имя/), 'Мария')
    await userEvent.type(within(dialog).getByLabelText(/^Почта/), 'mironova@example.ru')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить просьбу' }))
    expect(await within(dialog).findByText('Эта почта уже есть среди рекомендателей')).toBeInTheDocument()
  })

  it('shows a general failure inside the dialog and can be closed', async () => {
    stubApi(one({ ...application, references: [] }, { [`POST /api/applications/${APP_ID}/references`]: apiError(409, 'too_many_references', 'В отклике уже три рекомендателя — больше нельзя') }))
    renderApp(`/applications/${APP_ID}`)
    await click('Попросить ещё одного')
    const dialog = screen.getByRole('dialog')
    await userEvent.type(within(dialog).getByLabelText(/^Имя/), 'Мария')
    await userEvent.type(within(dialog).getByLabelText(/^Почта/), 'mironova@example.ru')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отправить просьбу' }))
    expect(await within(dialog).findByText('В отклике уже три рекомендателя — больше нельзя')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    // Окно открывается заново пустым.
    await click('Попросить ещё одного')
    expect(within(screen.getByRole('dialog')).getByLabelText(/^Имя/)).toHaveValue('')
  })

  it('stops offering new referees at three', async () => {
    stubApi(one({ ...application, references: [receivedRef, pendingRef, declinedRef] }))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByText('Три рекомендателя — это предел.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Попросить ещё одного' })).not.toBeInTheDocument()
  })
})

describe('candidate card (organization)', () => {
  const staff = (d: Detail = staffApplication) => one(d)
  const path = `/candidates/${APP_ID}`

  it('shows the applicant, the letter, files, recommendation letters and the profile snapshot', async () => {
    stubApi(staff())
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: 'Анна Смирнова' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: application.vacancy.title })).toHaveAttribute('href', `/vacancies/${application.vacancy.id}`)
    expect(screen.getAllByRole('link', { name: 'anna.apply@example.ru' })[0]).toHaveAttribute('href', 'mailto:anna.apply@example.ru')
    expect(screen.getByText(/Занимаюсь эпитаксией плёнок/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Резюме из профиля/ })).toHaveAttribute('href', `/api/applications/${APP_ID}/files/${cvFile.id}`)
    expect(screen.getByRole('link', { name: 'Список публикаций.pdf (16 КБ)' })).toBeInTheDocument()

    const refs = within(screen.getByRole('heading', { level: 2, name: 'Рекомендательные письма' }).closest('section')!)
    expect(refs.getByText('Кандидат видит только, ответили ли рекомендатели, а не сами письма.')).toBeInTheDocument()
    expect(refs.getByText(/Знаю Анну пять лет/)).toBeInTheDocument()
    expect(refs.getByRole('link', { name: 'Письмо Иванова.pdf (2,3 МБ)' })).toHaveAttribute('href', `/api/applications/${APP_ID}/files/${letterFile.id}`)
    expect(refs.getByText('Письмо получено')).toBeInTheDocument()
    expect(refs.getByText('Ответа пока нет')).toBeInTheDocument()
    expect(refs.getByText('Отказался писать')).toBeInTheDocument()

    const profile = within(screen.getByRole('heading', { level: 2, name: 'Профиль кандидата' }).closest('section')!)
    expect(profile.getByRole('heading', { level: 2, name: 'Анна Смирнова' })).toBeInTheDocument()
    expect(profile.getByRole('heading', { level: 3, name: 'О себе' })).toBeInTheDocument()
  })

  it('shows letters that are only text or only a file, and an empty list', async () => {
    stubApi(staff({ ...staffApplication, files: [], references: [staffTextOnly, staffFileOnly] }))
    renderApp(path)
    expect(await screen.findByText('Только текст')).toBeInTheDocument()
    expect(screen.getByText('Других файлов нет.')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /Письмо Иванова.pdf/ })).toHaveLength(1)
  })

  it('says the applicant named no referees', async () => {
    stubApi(staff({ ...staffApplication, references: [], cv: null }))
    renderApp(path)
    expect(await screen.findByText('Кандидат не указал рекомендателей.')).toBeInTheDocument()
    expect(screen.queryByText(/Резюме из профиля,/)).not.toBeInTheDocument()
  })

  it('sends the applicant to their own page: they never see the letters', async () => {
    stubApi(one({ ...application, references: [receivedRef] }))
    const { router } = renderApp(path)
    await waitFor(() => expect(router.state.location.pathname).toBe(`/applications/${APP_ID}`))
  })

  it('shows not found for people who may not see the application', async () => {
    stubApi(mine({ [`GET /api/applications/${APP_ID}`]: apiError(404, 'not_found', 'Такого отклика нет') }))
    renderApp(path)
    expect(await screen.findByRole('heading', { name: 'Такой страницы нет' })).toBeInTheDocument()
  })

  it('offers to retry when the card does not load', async () => {
    let fail = true
    stubApi(mine({ [`GET /api/applications/${APP_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { application: staffApplication })) }))
    renderApp(path)
    expect(await screen.findByText('Не удалось открыть отклик')).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Анна Смирнова' })).toBeInTheDocument()
  })

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp(path)
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })
})

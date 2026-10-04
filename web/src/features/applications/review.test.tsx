import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import {
  APP_ID,
  INV_ID,
  answeredEmpty,
  application,
  candidate,
  candidateList,
  contactsPhoneOnly,
  contactsShared,
  counts,
  interviewAcceptedProposal,
  interviewBadLink,
  interviewCancelled,
  interviewConfirmed,
  interviewForApplicant,
  interviewForStaff,
  interviewOnsite,
  interviewProposedByApplicant,
  interviewProposedForStaff,
  requestAnswered,
  requestForApplicant,
  reviewedApplication,
  staffApplication,
  summary,
  vacancyCount,
} from '../../test/applications'
import { renderApp } from '../../test/render'
import { VACANCY_ID, card as vacancyCard, mineList, target } from '../../test/vacancies'
import type { Detail, Invitation } from './api'

const click = async (name: string | RegExp) => userEvent.click(await screen.findByRole('button', { name }))
const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({ ...signedInAs(ann), ...extra })
const card = (d: Detail, extra: Record<string, Route> = {}) => signedIn({ [`GET /api/applications/${d.id}`]: reply(200, { application: d }), ...extra })
const withInvitations = (d: Detail, invitations: Invitation[]): Detail => ({ ...d, invitations })
const setDate = (label: RegExp, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
const confirmedForApplicant = { ...interviewConfirmed, can_cancel: false }
const POST = (path: string) => `POST /api/applications/${APP_ID}${path}`

// ---------------------------------------------------------------- список откликов

describe('candidates list', () => {
  const second = { ...candidate, id: '22222222-aaaa-4aaa-8aaa-222222222222', status: 'invited' as const, applicant_name: 'Борис Лапин', headline: '', unit_name: '', pending_invitations: 1, proposed_invitations: 1, references: { total: 0, received: 0 } }
  const route = 'GET /api/my/candidates?*'
  const facet = { 'GET /api/my/candidate-vacancies': reply(200, { items: [vacancyCount, { ...vacancyCount, id: 'v2', title: 'Доцент', total: 1, new: 0 }] }) }

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/candidates')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists applications with status, vacancy, unit, recommendations and what waits for whom', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([candidate, second], { counts: counts({ sent: 1, invited: 1 }) })), ...facet }))
    renderApp('/candidates')
    expect(await screen.findByRole('heading', { level: 1, name: 'Отклики' })).toBeInTheDocument()
    const rows = screen.getAllByRole('listitem').filter((li) => li.classList.contains('cand-row'))
    expect(rows).toHaveLength(2)
    const first = within(rows[0])
    expect(first.getByRole('link', { name: 'Анна Смирнова' })).toHaveAttribute('href', `/candidates/${APP_ID}`)
    expect(first.getByText('Научный сотрудник, лаборатория катализа')).toBeInTheDocument()
    expect(first.getByText(/Старший научный сотрудник: сверхпроводящие плёнки · Лаборатория катализа/)).toBeInTheDocument()
    expect(first.getByText('Отправлен')).toBeInTheDocument()
    expect(first.getByText('Рекомендации: 1 из 3')).toBeInTheDocument()
    expect(first.getByText(/Отклик от 3 октября 2026/)).toBeInTheDocument()
    const other = within(rows[1])
    expect(other.getByText('Должность не указана')).toBeInTheDocument()
    expect(other.getByText(/Вся организация/)).toBeInTheDocument()
    expect(other.getByText('Приглашение')).toBeInTheDocument()
    expect(other.getByText('Кандидат предложил другое время')).toBeInTheDocument()
    expect(other.getByText('Ждём ответа кандидата')).toBeInTheDocument()
    expect(other.queryByText(/Рекомендации/)).not.toBeInTheDocument()
    expect(screen.getByText('2 отклика')).toBeInTheDocument()
  })

  it('shows counts on the tabs and keeps the vacancy and the status in the address', async () => {
    const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate], { counts: counts({ sent: 4, viewed: 2, invited: 1 }) })), ...facet }))
    const { router } = renderApp('/candidates?vacancy=' + VACANCY_ID + '&status=sent')
    await screen.findByRole('link', { name: 'Анна Смирнова' })
    const tabs = within(screen.getByRole('navigation', { name: 'Отклики по статусам' }))
    expect(tabs.getAllByRole('link').map((a) => a.textContent)).toEqual(['Все7', 'Новые4', 'Просмотрены2', 'Приглашены1', 'Приняты0', 'Отказ0', 'Отозваны0'])
    expect(tabs.getByRole('link', { name: 'Новые: 4' })).toHaveAttribute('aria-current', 'page')
    expect(tabs.getByRole('link', { name: 'Все: 7' })).toHaveAttribute('href', `/candidates?vacancy=${VACANCY_ID}`)
    expect(tabs.getByRole('link', { name: 'Приглашены: 1' })).toHaveAttribute('href', `/candidates?vacancy=${VACANCY_ID}&status=invited`)
    expect(calls.some((c) => c.path === `/api/my/candidates?limit=20&offset=0&vacancy=${VACANCY_ID}&status=sent`)).toBe(true)
    // С выбранной вакансией её название в строках не повторяется.
    expect(screen.queryByText(/Лаборатория катализа/, { selector: '.cand-vacancy' })).not.toBeInTheDocument()
    await userEvent.click(tabs.getByRole('link', { name: 'Приглашены: 1' }))
    await waitFor(() => expect(router.state.location.search).toBe(`?vacancy=${VACANCY_ID}&status=invited`))
  })

  it('selects a vacancy from the list of vacancies that have applications', async () => {
    const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), ...facet }))
    const { router } = renderApp('/candidates?status=viewed&page=3')
    const select = await screen.findByLabelText('Вакансия')
    expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual([
      'Все вакансии',
      'Старший научный сотрудник: сверхпроводящие плёнки (3, новых 2)',
      'Доцент (1)',
    ])
    await userEvent.selectOptions(select, VACANCY_ID)
    await waitFor(() => expect(router.state.location.search).toBe(`?status=viewed&vacancy=${VACANCY_ID}`))
    expect(calls.some((c) => c.path.includes(`vacancy=${VACANCY_ID}`))).toBe(true)
    await userEvent.selectOptions(screen.getByLabelText('Вакансия'), '')
    await waitFor(() => expect(router.state.location.search).toBe('?status=viewed'))
  })

  it('works without the list of vacancies when it does not load', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), 'GET /api/my/candidate-vacancies': apiError(500, 'internal', 'Сбой') }))
    renderApp('/candidates')
    expect(await screen.findByRole('link', { name: 'Анна Смирнова' })).toBeInTheDocument()
    expect(screen.queryByLabelText('Вакансия')).not.toBeInTheDocument()
  })

  it('hides the vacancy filter when no vacancy has applications yet', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), 'GET /api/my/candidate-vacancies': reply(200, { items: [] }) }))
    renderApp('/candidates')
    await screen.findByRole('link', { name: 'Анна Смирнова' })
    expect(screen.queryByLabelText('Вакансия')).not.toBeInTheDocument()
  })

  it('says there are no applications at all and points to my vacancies', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([])), ...facet }))
    renderApp('/candidates')
    expect(await screen.findByRole('heading', { name: 'Откликов пока нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Мои вакансии' })).toHaveAttribute('href', '/my-vacancies')
  })

  it('says a tab is empty while the other tabs have people', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([], { counts: counts({ sent: 2 }) })), ...facet }))
    renderApp('/candidates?status=accepted')
    expect(await screen.findByRole('heading', { name: 'В этом списке никого нет' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Отклики по статусам' })).toBeInTheDocument()
  })

  it('keeps the filter visible when a chosen vacancy has no applications', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([])), ...facet }))
    renderApp('/candidates?vacancy=' + VACANCY_ID)
    expect(await screen.findByRole('heading', { name: 'В этом списке никого нет' })).toBeInTheDocument()
    expect(screen.getByLabelText('Вакансия')).toBeInTheDocument()
  })

  it('ignores an unknown status in the address', async () => {
    const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), ...facet }))
    renderApp('/candidates?status=bogus')
    await screen.findByRole('link', { name: 'Анна Смирнова' })
    expect(calls.some((c) => c.path.startsWith('/api/my/candidates') && c.path.includes('status='))).toBe(false)
  })

  it('pages through long lists', async () => {
    const items = Array.from({ length: 20 }, (_, i) => ({ ...candidate, id: `00000000-0000-4000-8000-${String(i).padStart(12, '0')}` }))
    const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList(items, { total: 45, counts: counts({ sent: 45 }) })), ...facet }))
    const { router } = renderApp('/candidates')
    await screen.findByText('Страница 1 из 3')
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await click('Дальше')
    expect(await screen.findByText('Страница 2 из 3')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    expect(calls.some((c) => c.path === '/api/my/candidates?limit=20&offset=20')).toBe(true)
    await click('Назад')
    await screen.findByText('Страница 1 из 3')
    expect(router.state.location.search).toBe('')
  })

  it('offers to retry when the list does not load', async () => {
    let fail = true
    stubApi(signedIn({ [route]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, candidateList([candidate]))), ...facet }))
    renderApp('/candidates')
    expect(await screen.findByText('Не удалось загрузить отклики')).toBeInTheDocument()
    fail = false
    await click('Проверить ещё раз')
    expect(await screen.findByRole('link', { name: 'Анна Смирнова' })).toBeInTheDocument()
  })

  it('copes with a very long name and no headline', async () => {
    const long = { ...candidate, applicant_name: 'Константинопольская-Преображенская'.repeat(4), headline: '' }
    stubApi(signedIn({ [route]: reply(200, candidateList([long])), ...facet }))
    renderApp('/candidates')
    const heading = (await screen.findByRole('link', { name: long.applicant_name })).closest('h2')!
    expect(heading).toHaveAttribute('data-long')
  })
})

// ---------------------------------------------------------------- карточка кандидата: решение

describe('candidate card: decision panel', () => {
  const path = `/candidates/${APP_ID}`

  it('offers to invite, accept and reject while the application is open', async () => {
    stubApi(card(reviewedApplication))
    renderApp(path)
    const panel = within((await screen.findByRole('heading', { level: 2, name: 'Решение по отклику' })).closest('section')!)
    expect(panel.getByRole('button', { name: 'Пригласить' })).toBeInTheDocument()
    expect(panel.getByRole('button', { name: 'Принять' })).toBeInTheDocument()
    expect(panel.getByRole('button', { name: 'Отказать' })).toBeInTheDocument()
    expect(screen.getByText('Вы пока никого не приглашали.')).toBeInTheDocument()
  })

  it('offers only what the status allows', async () => {
    stubApi(card({ ...reviewedApplication, status: 'sent', viewer: { ...reviewedApplication.viewer, decisions: ['rejected'] } }))
    renderApp(path)
    await screen.findByRole('button', { name: 'Пригласить' })
    expect(screen.queryByRole('button', { name: 'Принять' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Отказать' })).toBeInTheDocument()
  })

  it.each([
    ['accepted', /Кандидат принят \(3 октября 2026 г\.\)\. Решение окончательное\./],
    ['rejected', /Вы отказали кандидату \(3 октября 2026 г\.\)/],
    ['withdrawn', /Кандидат отозвал отклик\./],
  ] as const)('shows the outcome for %s and no buttons', async (status, text) => {
    stubApi(card({ ...reviewedApplication, status, decision_note: status === 'withdrawn' ? '' : 'Спасибо за интерес', viewer: { ...reviewedApplication.viewer, decisions: [], can_invite: false } }))
    renderApp(path)
    expect(await screen.findByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Пригласить' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Принять' })).not.toBeInTheDocument()
    if (status !== 'withdrawn') expect(screen.getByText('Спасибо за интерес')).toBeInTheDocument()
    else expect(screen.queryByText(/Записка кандидату/)).not.toBeInTheDocument()
    // Приглашать некого: раздела приглашений без приглашений нет.
    expect(screen.queryByRole('heading', { name: 'Приглашения' })).not.toBeInTheDocument()
  })

  it('accepts with a note', async () => {
    const { calls } = stubApi(card(reviewedApplication, { [POST('/status')]: reply(204) }))
    renderApp(path)
    await click('Принять')
    const dialog = screen.getByRole('dialog', { name: 'Принять кандидата?' })
    await userEvent.type(within(dialog).getByLabelText(/Записка кандидату/), 'Ждём вас в понедельник')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Принять кандидата' }))
    expect(await screen.findByText('Кандидат принят', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ status: 'accepted', note: 'Ждём вас в понедельник' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('rejects without a note after a warning that the decision is final', async () => {
    const { calls } = stubApi(card(reviewedApplication, { [POST('/status')]: reply(204) }))
    renderApp(path)
    await click('Отказать')
    const dialog = screen.getByRole('dialog', { name: 'Отказать кандидату?' })
    expect(within(dialog).getByText(/Решение окончательное/)).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отказать' }))
    expect(await screen.findByText('Отказ отправлен', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ status: 'rejected', note: '' })
  })

  it('shows a failure inside the dialog and a field error under the note', async () => {
    let reject = apiError(409, 'invalid_status_change', 'Для отклика в таком состоянии это действие недоступно. Обновите страницу')
    stubApi(card(reviewedApplication, { [POST('/status')]: () => reject }))
    renderApp(path)
    await click('Принять')
    const dialog = screen.getByRole('dialog')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Принять кандидата' }))
    expect(await within(dialog).findByText('Для отклика в таком состоянии это действие недоступно. Обновите страницу')).toBeInTheDocument()
    reject = apiError(422, 'validation_failed', 'Проверьте', { fields: { note: 'Не больше 1000 знаков' } })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Принять кандидата' }))
    expect(await within(dialog).findByText('Не больше 1000 знаков')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})

// ---------------------------------------------------------------- окно «Пригласить»

describe('invite dialog', () => {
  const path = `/candidates/${APP_ID}`
  const open = async (extra: Record<string, Route> = {}) => {
    const stub = stubApi(card(reviewedApplication, { [POST('/invitations')]: reply(201, { invitation: interviewForStaff }), ...extra }))
    renderApp(path)
    await click('Пригласить')
    return { ...stub, dialog: within(screen.getByRole('dialog', { name: 'Пригласить кандидата' })) }
  }

  it('invites to an online interview at Moscow time', async () => {
    const { calls, dialog } = await open()
    setDate(/^Дата/, '2026-10-15')
    setDate(/^Время/, '14:00')
    await userEvent.type(dialog.getByLabelText(/Ссылка на встречу/), 'https://meet.example.org/room-1')
    await userEvent.type(dialog.getByLabelText(/Сообщение кандидату/), 'Расскажем о проекте')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await screen.findByText('Приглашение отправлено', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({
      kind: 'interview', message: 'Расскажем о проекте', starts_at: '2026-10-15T14:00:00+03:00', place_kind: 'online', place: 'https://meet.example.org/room-1',
    })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('asks for an address when the interview is on site', async () => {
    const { calls, dialog } = await open()
    await userEvent.selectOptions(dialog.getByLabelText(/Формат/), 'onsite')
    expect(dialog.queryByLabelText(/Ссылка на встречу/)).not.toBeInTheDocument()
    setDate(/^Дата/, '2026-10-15')
    setDate(/^Время/, '09:30')
    await userEvent.type(dialog.getByLabelText(/^Адрес/), 'Новосибирск, пр. Лаврентьева, 5')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true))
    expect(calls.find((c) => c.method === 'POST')!.body).toMatchObject({ place_kind: 'onsite', place: 'Новосибирск, пр. Лаврентьева, 5', starts_at: '2026-10-15T09:30:00+03:00' })
  })

  it('does not send an interview without a date, a time and a place', async () => {
    const { calls, dialog } = await open()
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await dialog.findByText('Выберите дату')).toBeInTheDocument()
    expect(dialog.getByText('Укажите время')).toBeInTheDocument()
    expect(dialog.getByText('Вставьте ссылку на встречу')).toBeInTheDocument()
    await userEvent.selectOptions(dialog.getByLabelText(/Формат/), 'onsite')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await dialog.findByText('Укажите адрес')).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(0)
  })

  it('sends own contacts, needing at least a mail or a phone', async () => {
    const { calls, dialog } = await open()
    await userEvent.selectOptions(dialog.getByLabelText(/Что отправить/), 'contacts')
    expect(dialog.queryByLabelText(/^Дата/)).not.toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await dialog.findByText('Укажите почту или телефон')).toBeInTheDocument()
    await userEvent.type(dialog.getByLabelText(/Контактное лицо/), 'Ольга Кузнецова')
    await userEvent.type(dialog.getByLabelText(/^Телефон/), '+7 913 000-00-00')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true))
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ kind: 'contacts', message: '', contact_name: 'Ольга Кузнецова', contact_email: '', contact_phone: '+7 913 000-00-00' })
  })

  it('asks the candidate to leave contacts', async () => {
    const { calls, dialog } = await open()
    await userEvent.selectOptions(dialog.getByLabelText(/Что отправить/), 'request_contacts')
    expect(dialog.getByText(/Кандидат ответит на странице отклика/)).toBeInTheDocument()
    await userEvent.type(dialog.getByLabelText(/Сообщение кандидату/), 'Оставьте телефон')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true))
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ kind: 'request_contacts', message: 'Оставьте телефон' })
  })

  it('shows the server’s field errors and a general failure', async () => {
    let answer = apiError(422, 'validation_failed', 'Проверьте', { fields: { starts_at: 'Это время уже прошло. Выберите время в будущем' } })
    const { dialog } = await open({ [POST('/invitations')]: () => answer })
    setDate(/^Дата/, '2020-01-01')
    setDate(/^Время/, '10:00')
    await userEvent.type(dialog.getByLabelText(/Ссылка на встречу/), 'https://meet.example.org/x')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await dialog.findByText('Это время уже прошло. Выберите время в будущем')).toBeInTheDocument()
    answer = apiError(409, 'too_many_invitations', 'На один отклик можно отправить не больше десяти приглашений')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await dialog.findByText('На один отклик можно отправить не больше десяти приглашений')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})

// ---------------------------------------------------------------- приглашения: показ

describe('invitations on both sides', () => {
  it('shows an interview to the applicant with the time in Moscow and a safe link', async () => {
    stubApi(card(withInvitations(application, [{ ...interviewForApplicant, status: 'pending' }]), {}))
    renderApp(`/applications/${APP_ID}`)
    const section = within((await screen.findByRole('heading', { level: 2, name: 'Приглашения от организации' })).closest('section')!)
    expect(section.getByRole('heading', { level: 3, name: 'Собеседование' })).toBeInTheDocument()
    expect(section.getByText('Ждёт ответа')).toBeInTheDocument()
    expect(section.getByText(/15 октября 2026.*14:00 \(МСК\)/)).toBeInTheDocument()
    const link = section.getByRole('link', { name: 'https://meet.example.org/room-1' })
    expect(link).toHaveAttribute('href', 'https://meet.example.org/room-1')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(section.getByText(/Расскажем о проекте/)).toBeInTheDocument()
    expect(section.getByText(/Отправлено 3 октября 2026/)).toBeInTheDocument()
  })

  it('does not turn a foreign scheme into a link, and shows an address as plain text', async () => {
    stubApi(card(withInvitations(application, [interviewBadLink, interviewOnsite])))
    renderApp(`/applications/${APP_ID}`)
    await screen.findByText(/javascript:alert\(1\)/)
    expect(screen.queryByRole('link', { name: 'javascript:alert(1)' })).not.toBeInTheDocument()
    expect(screen.getByText(/Новосибирск, пр. Лаврентьева, 5/)).toBeInTheDocument()
  })

  it('shows contacts with mail and phone links, or a phone only', async () => {
    stubApi(card(withInvitations(application, [contactsShared, contactsPhoneOnly])))
    renderApp(`/applications/${APP_ID}`)
    expect(await screen.findByText('Ольга Кузнецова')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'hr@example.ru' })).toHaveAttribute('href', 'mailto:hr@example.ru')
    const phones = screen.getAllByRole('link', { name: '+7 913 000-00-00' })
    expect(phones).toHaveLength(2)
    expect(phones[0]).toHaveAttribute('href', 'tel:+79130000000')
    expect(screen.getAllByText('Отправлено')).toHaveLength(2)
    expect(screen.getAllByText(/Отправлено 3 октября/)).toHaveLength(2)
    expect(screen.getByText(/Пишите в любое время/)).toBeInTheDocument()
    expect(screen.queryByText('Почта')).toBeInTheDocument()
  })

  it('shows the staff what the candidate answered, in each way', async () => {
    const proposedElsewhere = { ...interviewProposedForStaff, id: 'a2' }
    stubApi(
      card(
        withInvitations(reviewedApplication, [interviewConfirmed, proposedElsewhere, { ...interviewAcceptedProposal, id: 'a3' }, requestAnswered, answeredEmpty, { ...interviewCancelled }]),
      ),
    )
    renderApp(`/candidates/${APP_ID}`)
    // Назад к списку откликов на ту же вакансию.
    expect(await screen.findByRole('link', { name: 'Все отклики на эту вакансию' })).toHaveAttribute('href', `/candidates?vacancy=${reviewedApplication.vacancy.id}`)
    const section = within((await screen.findByRole('heading', { level: 2, name: 'Приглашения' })).closest('section')!)
    expect(section.getAllByText('Ответ кандидата')).toHaveLength(4)
    expect(section.getByText('Время подходит.')).toBeInTheDocument()
    expect(section.getByText('Комментарий: Буду вовремя')).toBeInTheDocument()
    expect(section.getByText(/Предложено другое время: 17 октября 2026.*15:00 \(МСК\)/)).toBeInTheDocument()
    expect(section.getByText('Комментарий: Занят в этот день')).toBeInTheDocument()
    expect(section.getByText('Вы приняли время, которое предложил кандидат.')).toBeInTheDocument()
    expect(section.getByText('Как связаться: +7 913 555-66-77')).toBeInTheDocument()
    expect(section.getByText('Когда удобно: после 15:00')).toBeInTheDocument()
    // Ответ без единой строки не рисуется пустым блоком.
    expect(section.getAllByText('Ответ кандидата')).toHaveLength(4)
    const cancelled = section.getAllByRole('heading', { level: 3 }).map((h) => h.closest('li')!).find((li) => li.hasAttribute('data-cancelled'))
    expect(cancelled).toBeTruthy()
  })

  it('calls the answer “Ваш ответ” for the applicant and tells whose time was accepted', async () => {
    stubApi(card(withInvitations(application, [confirmedForApplicant, { ...interviewAcceptedProposal, id: 'a3', can_cancel: false }])))
    renderApp(`/applications/${APP_ID}`)
    expect((await screen.findAllByText('Ваш ответ')).length).toBe(2)
    expect(screen.getByText('Организация приняла предложенное вами время.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отменить приглашение' })).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- приглашения: действия соискателя

describe('applicant answers an invitation', () => {
  const path = `/applications/${APP_ID}`
  const answer = (inv: string) => POST(`/invitations/${inv}/answer`)

  it('confirms the interview', async () => {
    const { calls } = stubApi(card(withInvitations(application, [interviewForApplicant]), { [answer(INV_ID)]: reply(204) }))
    renderApp(path)
    await click('Подтвердить время')
    expect(await screen.findByText('Время подтверждено', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ action: 'confirm' })
  })

  it('shows a failure when the confirmation does not go through', async () => {
    stubApi(card(withInvitations(application, [interviewForApplicant]), { [answer(INV_ID)]: apiError(409, 'invalid_invitation_state', 'С этим приглашением это действие уже невозможно: оно изменилось. Обновите страницу') }))
    renderApp(path)
    await click('Подтвердить время')
    expect(await screen.findByText('С этим приглашением это действие уже невозможно: оно изменилось. Обновите страницу', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('proposes another time with a note', async () => {
    const { calls } = stubApi(card(withInvitations(application, [interviewForApplicant]), { [answer(INV_ID)]: reply(204) }))
    renderApp(path)
    await click('Предложить другое время')
    const dialog = within(screen.getByRole('dialog', { name: 'Предложить другое время' }))
    await userEvent.click(dialog.getByRole('button', { name: 'Предложить' }))
    expect(await dialog.findByText('Выберите дату')).toBeInTheDocument()
    expect(dialog.getByText('Укажите время')).toBeInTheDocument()
    setDate(/^Дата/, '2026-10-17')
    setDate(/^Время/, '15:30')
    await userEvent.type(dialog.getByLabelText(/Комментарий/), 'В этот день занят')
    await userEvent.click(dialog.getByRole('button', { name: 'Предложить' }))
    expect(await screen.findByText('Предложение отправлено', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ action: 'propose', proposed_at: '2026-10-17T15:30:00+03:00', note: 'В этот день занят' })
  })

  it('shows the server’s complaint about the proposed time, or a general failure, and can be closed', async () => {
    let result = apiError(422, 'validation_failed', 'Проверьте', { fields: { proposed_at: 'Это время уже прошло. Выберите время в будущем' } })
    stubApi(card(withInvitations(application, [interviewForApplicant]), { [answer(INV_ID)]: () => result }))
    renderApp(path)
    await click('Предложить другое время')
    const dialog = within(screen.getByRole('dialog'))
    setDate(/^Дата/, '2020-01-01')
    setDate(/^Время/, '10:00')
    await userEvent.click(dialog.getByRole('button', { name: 'Предложить' }))
    expect(await dialog.findByText('Это время уже прошло. Выберите время в будущем')).toBeInTheDocument()
    result = apiError(409, 'invalid_invitation_state', 'Приглашение изменилось')
    await userEvent.click(dialog.getByRole('button', { name: 'Предложить' }))
    expect(await dialog.findByText('Приглашение изменилось')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('leaves contacts and a convenient time', async () => {
    const { calls } = stubApi(card(withInvitations(application, [requestForApplicant]), { [answer(requestForApplicant.id)]: reply(204) }))
    renderApp(path)
    expect(screen.queryByRole('button', { name: 'Подтвердить время' })).not.toBeInTheDocument()
    await click('Оставить контакты')
    const dialog = within(screen.getByRole('dialog', { name: 'Оставить контакты' }))
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить' }))
    expect(await dialog.findByText('Укажите, как с вами связаться')).toBeInTheDocument()
    await userEvent.type(dialog.getByLabelText(/Как с вами связаться/), '+7 913 555-66-77')
    await userEvent.type(dialog.getByLabelText(/Когда удобно/), 'после 15:00')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить' }))
    expect(await screen.findByText('Контакты отправлены', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ action: 'reply', contact: '+7 913 555-66-77', time: 'после 15:00' })
  })

  it('shows a failure of the reply inside the dialog', async () => {
    stubApi(card(withInvitations(application, [requestForApplicant]), { [answer(requestForApplicant.id)]: apiError(409, 'invalid_invitation_state', 'Приглашение изменилось') }))
    renderApp(path)
    await click('Оставить контакты')
    const dialog = within(screen.getByRole('dialog'))
    await userEvent.type(dialog.getByLabelText(/Как с вами связаться/), 'x@y.ru')
    await userEvent.click(dialog.getByRole('button', { name: 'Отправить' }))
    expect(await dialog.findByText('Приглашение изменилось')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('does not offer buttons once the invitation is answered or cancelled', async () => {
    stubApi(card(withInvitations(application, [confirmedForApplicant, interviewCancelled, requestAnswered])))
    renderApp(path)
    await screen.findAllByText('Ваш ответ', { selector: '.inv-answer-head' })
    for (const name of ['Подтвердить время', 'Предложить другое время', 'Оставить контакты']) {
      expect(screen.queryByRole('button', { name })).not.toBeInTheDocument()
    }
  })

  it('shows the decision and the organization’s note to the applicant', async () => {
    stubApi(card({ ...application, status: 'rejected', decision_note: 'Выбрали другого кандидата', viewer: { ...application.viewer, can_withdraw: false } }))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Решение организации' })).toBeInTheDocument()
    expect(screen.getByText('Организация не продолжит рассмотрение отклика.')).toBeInTheDocument()
    expect(screen.getByText('Выбрали другого кандидата')).toBeInTheDocument()
  })

  it('shows a positive decision without a note', async () => {
    stubApi(card({ ...application, status: 'accepted', viewer: { ...application.viewer, can_withdraw: false } }))
    renderApp(path)
    expect(await screen.findByText('Организация приняла положительное решение по отклику.')).toBeInTheDocument()
    expect(screen.queryByText(/Сообщение организации/)).not.toBeInTheDocument()
  })

  it('has no decision block while the application is open', async () => {
    stubApi(card(application))
    renderApp(path)
    await screen.findByRole('heading', { level: 1 })
    expect(screen.queryByRole('heading', { name: 'Решение организации' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Приглашения от организации' })).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- приглашения: действия организации

describe('staff acts on an invitation', () => {
  const path = `/candidates/${APP_ID}`

  it('accepts the time proposed by the candidate', async () => {
    const { calls } = stubApi(card(withInvitations(reviewedApplication, [interviewProposedForStaff]), { [POST(`/invitations/${INV_ID}/accept-proposal`)]: reply(204) }))
    renderApp(path)
    await click('Принять предложенное время')
    expect(await screen.findByText('Время принято', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1)
  })

  it('shows a failure when the proposal can no longer be accepted', async () => {
    stubApi(card(withInvitations(reviewedApplication, [interviewProposedForStaff]), { [POST(`/invitations/${INV_ID}/accept-proposal`)]: apiError(409, 'invalid_invitation_state', 'Приглашение изменилось') }))
    renderApp(path)
    await click('Принять предложенное время')
    expect(await screen.findByText('Приглашение изменилось', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('cancels an invitation after a confirmation', async () => {
    const { calls } = stubApi(card(withInvitations(reviewedApplication, [interviewForStaff]), { [`DELETE /api/applications/${APP_ID}/invitations/${INV_ID}`]: reply(204) }))
    renderApp(path)
    await click('Отменить приглашение')
    const dialog = within(screen.getByRole('dialog', { name: 'Отменить приглашение?' }))
    await userEvent.click(dialog.getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(0)
    await click('Отменить приглашение')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отменить приглашение' }))
    expect(await screen.findByText('Приглашение отменено', { selector: '.toast *' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'DELETE')).toHaveLength(1)
  })

  it('reports a failed cancel and closes the dialog', async () => {
    stubApi(card(withInvitations(reviewedApplication, [interviewForStaff]), { [`DELETE /api/applications/${APP_ID}/invitations/${INV_ID}`]: apiError(409, 'invalid_invitation_state', 'Приглашение изменилось') }))
    renderApp(path)
    await click('Отменить приглашение')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отменить приглашение' }))
    expect(await screen.findByText('Приглашение изменилось', { selector: '.toast *' })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('offers no buttons on finished invitations or to a reader without rights', async () => {
    stubApi(card(withInvitations({ ...staffApplication, status: 'rejected', viewer: { ...staffApplication.viewer, decisions: [], can_invite: false } }, [interviewCancelled, contactsShared])))
    renderApp(path)
    await screen.findByRole('heading', { level: 2, name: 'Приглашения' })
    expect(screen.queryByRole('button', { name: 'Отменить приглашение' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Принять предложенное время' })).not.toBeInTheDocument()
  })

  it('shows the proposal exactly once on the proposed invitation', async () => {
    stubApi(card(withInvitations(reviewedApplication, [interviewProposedByApplicant])))
    renderApp(path)
    expect(await screen.findByText(/Предложено другое время: 17 октября 2026/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Принять предложенное время' })).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- «Мои отклики» и «Мои вакансии»

describe('other pages', () => {
  it('tells the applicant that an invitation waits for an answer', async () => {
    stubApi(signedIn({ 'GET /api/applications?*': reply(200, { items: [{ ...summary, status: 'invited', pending_invitations: 1 }, { ...summary, id: 'x2', pending_invitations: 0 }], total: 2 }) }))
    renderApp('/applications')
    expect(await screen.findAllByText('Ждёт вашего ответа')).toHaveLength(1)
  })

  const mineRoutes = (items: Record<string, Route>) =>
    signedIn({ 'GET /api/my/vacancy-targets': reply(200, { targets: [target] }), 'GET /api/my/vacancies?limit=50&offset=0': reply(200, mineList([vacancyCard])), ...items })

  it('shows how many applications each of my vacancies has, with a link to them', async () => {
    stubApi(mineRoutes({ 'GET /api/my/candidate-vacancies': reply(200, { items: [{ ...vacancyCount, id: vacancyCard.id, total: 3, new: 2 }] }) }))
    renderApp('/my-vacancies')
    const row = (await screen.findByRole('link', { name: vacancyCard.title })).closest('.mine-row')! as HTMLElement
    const link = await within(row).findByRole('link', { name: '3 отклика' })
    expect(link).toHaveAttribute('href', `/candidates?vacancy=${vacancyCard.id}`)
    expect(within(row).getByText(/новых 2/)).toBeInTheDocument()
  })

  it('does not mention new applications when all were seen, nor applications when there are none', async () => {
    stubApi(mineRoutes({ 'GET /api/my/candidate-vacancies': reply(200, { items: [{ ...vacancyCount, id: vacancyCard.id, total: 1, new: 0 }] }) }))
    const { unmount } = renderApp('/my-vacancies')
    const row = (await screen.findByRole('link', { name: vacancyCard.title })).closest('.mine-row')! as HTMLElement
    expect(await within(row).findByRole('link', { name: '1 отклик' })).toBeInTheDocument()
    expect(within(row).queryByText(/новых/)).not.toBeInTheDocument()
    unmount()
    stubApi(mineRoutes({ 'GET /api/my/candidate-vacancies': reply(200, { items: [{ ...vacancyCount, id: vacancyCard.id, total: 0, new: 0 }] }) }))
    renderApp('/my-vacancies')
    const empty = (await screen.findByRole('link', { name: vacancyCard.title })).closest('.mine-row')! as HTMLElement
    await waitFor(() => expect(within(empty).queryByRole('link', { name: /отклик/ })).not.toBeInTheDocument())
  })

  it('lists my vacancies even when the numbers of applications do not load', async () => {
    stubApi(mineRoutes({ 'GET /api/my/candidate-vacancies': apiError(500, 'internal', 'Сбой') }))
    renderApp('/my-vacancies')
    const row = (await screen.findByRole('link', { name: vacancyCard.title })).closest('.mine-row')! as HTMLElement
    expect(within(row).queryByRole('link', { name: /отклик/ })).not.toBeInTheDocument()
  })
})

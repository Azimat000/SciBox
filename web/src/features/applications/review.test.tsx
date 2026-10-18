import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ann, apiError, fileReply, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { APP_ID, application, candidate, candidateList, counts, reviewedApplication, staffApplication, vacancyCount } from '../../test/applications'
import { renderApp } from '../../test/render'
import { VACANCY_ID, card as vacancyCard, mineList, target } from '../../test/vacancies'
import type { Detail } from './api'

const click = async (name: string | RegExp) => userEvent.click(await screen.findByRole('button', { name }))
const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({ ...signedInAs(ann), ...extra })
const card = (d: Detail, extra: Record<string, Route> = {}) => signedIn({ [`GET /api/applications/${d.id}`]: reply(200, { application: d }), ...extra })
const POST = (path: string) => `POST /api/applications/${APP_ID}${path}`

// ---------------------------------------------------------------- список откликов

describe('candidates list', () => {
  const second = { ...candidate, id: '22222222-aaaa-4aaa-8aaa-222222222222', status: 'viewed' as const, applicant_name: 'Борис Лапин', headline: '', unit_name: '', references: { total: 0, received: 0 } }
  const route = 'GET /api/my/candidates?*'
  const facet = { 'GET /api/my/candidate-vacancies': reply(200, { items: [vacancyCount, { ...vacancyCount, id: 'v2', title: 'Доцент', total: 1, new: 0 }] }) }

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/candidates')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists applications with status, vacancy, unit and recommendations', async () => {
    stubApi(signedIn({ [route]: reply(200, candidateList([candidate, second], { counts: counts({ sent: 1, viewed: 1 }) })), ...facet }))
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
    expect(other.getByText('Просмотрен')).toBeInTheDocument()
    expect(other.queryByText(/Рекомендации/)).not.toBeInTheDocument()
    expect(screen.getByText('2 отклика')).toBeInTheDocument()
  })

  it('shows counts on the tabs and keeps the vacancy and the status in the address', async () => {
    const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate], { counts: counts({ sent: 4, viewed: 2, accepted: 1 }) })), ...facet }))
    const { router } = renderApp('/candidates?vacancy=' + VACANCY_ID + '&status=sent')
    await screen.findByRole('link', { name: 'Анна Смирнова' })
    const tabs = within(screen.getByRole('navigation', { name: 'Отклики по статусам' }))
    expect(tabs.getAllByRole('link').map((a) => a.textContent)).toEqual(['Все7', 'Новые4', 'Просмотрены2', 'Приняты1', 'Отказ0', 'Отозваны0'])
    expect(tabs.getByRole('link', { name: 'Новые: 4' })).toHaveAttribute('aria-current', 'page')
    expect(tabs.getByRole('link', { name: 'Все: 7' })).toHaveAttribute('href', `/candidates?vacancy=${VACANCY_ID}`)
    expect(tabs.getByRole('link', { name: 'Приняты: 1' })).toHaveAttribute('href', `/candidates?vacancy=${VACANCY_ID}&status=accepted`)
    expect(calls.some((c) => c.path === `/api/my/candidates?limit=20&offset=0&vacancy=${VACANCY_ID}&status=sent`)).toBe(true)
    // С выбранной вакансией её название в строках не повторяется.
    expect(screen.queryByText(/Лаборатория катализа/, { selector: '.cand-vacancy' })).not.toBeInTheDocument()
    await userEvent.click(tabs.getByRole('link', { name: 'Приняты: 1' }))
    await waitFor(() => expect(router.state.location.search).toBe(`?vacancy=${VACANCY_ID}&status=accepted`))
  })

  describe('export to a file', () => {
    let saved: { name: string; href: string }[]
    beforeEach(() => {
      saved = []
      vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:test-csv', revokeObjectURL: vi.fn() }))
      vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
        saved.push({ name: this.download, href: this.href })
      })
    })
    afterEach(() => vi.restoreAllMocks())

    it('explains what goes into the file and saves it under the name the server gave', async () => {
      const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate, second], { counts: counts({ sent: 1, viewed: 1 }) })), ...facet, 'GET /api/my/candidates/export': fileReply('otkliki-2026-10-14.csv', '\uFEFFИмя') }))
      renderApp('/candidates')
      const button = await screen.findByRole('button', { name: 'Выгрузить в Excel' })
      expect(button).toHaveAccessibleDescription(/Все отклики выбранной вакансии и вкладки/)
      expect(screen.getByText(/вкладки, до 5000/)).toBeInTheDocument()
      await userEvent.click(button)
      expect(await screen.findByText('Файл сохранён')).toBeInTheDocument()
      expect(screen.getByText('В файле 2 отклика.')).toBeInTheDocument()
      expect(saved).toEqual([{ name: 'otkliki-2026-10-14.csv', href: 'blob:test-csv' }])
      expect(calls.filter((c) => c.path.startsWith('/api/my/candidates/export')).map((c) => c.path)).toEqual(['/api/my/candidates/export'])
    })

    it('exports the chosen vacancy and tab, not the page', async () => {
      const { calls } = stubApi(signedIn({ [route]: reply(200, candidateList([candidate], { counts: counts({ sent: 4 }) })), ...facet, 'GET /api/my/candidates/export?*': fileReply('', 'x') }))
      renderApp(`/candidates?vacancy=${VACANCY_ID}&status=sent&page=2`)
      await userEvent.click(await screen.findByRole('button', { name: 'Выгрузить в Excel' }))
      await waitFor(() => expect(saved).toHaveLength(1))
      expect(calls.some((c) => c.path === `/api/my/candidates/export?vacancy=${VACANCY_ID}&status=sent`)).toBe(true)
      expect(saved[0]?.name).toBe('otkliki.csv') // сервер имени не дал
    })

    it('says that only the first ones went into the file when there are more than the limit', async () => {
      stubApi(signedIn({ [route]: reply(200, candidateList([candidate], { total: 5200, counts: counts({ sent: 5200 }) })), ...facet, 'GET /api/my/candidates/export': fileReply('o.csv', 'x') }))
      renderApp('/candidates')
      await userEvent.click(await screen.findByRole('button', { name: 'Выгрузить в Excel' }))
      expect(await screen.findByText(/В файл вошли первые 5000 откликов/)).toBeInTheDocument()
    })

    it('tells about the limit of exports and about any other failure, and saves nothing', async () => {
      stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), ...facet, 'GET /api/my/candidates/export': apiError(429, 'rate_limited', 'Слишком много запросов', { retry_after: 30 }) }))
      renderApp('/candidates')
      await userEvent.click(await screen.findByRole('button', { name: 'Выгрузить в Excel' }))
      expect(await screen.findByText('Не удалось выгрузить отклики')).toBeInTheDocument()
      expect(screen.getByText('Выгрузок слишком много за минуту. Подождите немного и попробуйте снова.')).toBeInTheDocument()
      expect(saved).toHaveLength(0)
    })

    it('shows the server text when the export fails for another reason', async () => {
      stubApi(signedIn({ [route]: reply(200, candidateList([candidate])), ...facet, 'GET /api/my/candidates/export': apiError(500, 'internal', 'Что-то сломалось') }))
      renderApp('/candidates')
      await userEvent.click(await screen.findByRole('button', { name: 'Выгрузить в Excel' }))
      expect(await screen.findByText('Не удалось выгрузить отклики')).toBeInTheDocument()
      expect(saved).toHaveLength(0)
    })

    it('has no export button on an empty tab', async () => {
      stubApi(signedIn({ [route]: reply(200, candidateList([], { counts: counts({ sent: 3 }) })), ...facet }))
      renderApp('/candidates?status=rejected')
      await screen.findByRole('heading', { level: 2, name: 'В этом списке никого нет' })
      expect(screen.queryByRole('button', { name: 'Выгрузить в Excel' })).not.toBeInTheDocument()
    })
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

  it('offers to accept and reject while the application is open, and no invitations (D-256)', async () => {
    stubApi(card(reviewedApplication))
    renderApp(path)
    // Решение стоит в колонке справа: она подписана своим заголовком
    const panel = within(await screen.findByRole('complementary', { name: 'Решение по отклику' }))
    expect(panel.getByRole('button', { name: 'Принять кандидата' })).toBeInTheDocument()
    expect(panel.getByRole('button', { name: 'Отказать' })).toBeInTheDocument()
    expect(panel.queryByRole('button', { name: 'Пригласить' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Приглашения' })).not.toBeInTheDocument()
  })

  it('offers only what the status allows', async () => {
    stubApi(card({ ...reviewedApplication, status: 'sent', viewer: { ...reviewedApplication.viewer, decisions: ['rejected'] } }))
    renderApp(path)
    await screen.findByRole('button', { name: 'Отказать' })
    expect(screen.queryByRole('button', { name: 'Принять кандидата' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Отказать' })).toBeInTheDocument()
  })

  it.each([
    ['accepted', /Кандидат принят \(3 октября 2026 г\.\)\. Решение окончательное\./],
    ['rejected', /Вы отказали кандидату \(3 октября 2026 г\.\)/],
    ['withdrawn', /Кандидат отозвал отклик\./],
  ] as const)('shows the outcome for %s and no buttons', async (status, text) => {
    stubApi(card({ ...reviewedApplication, status, decision_note: status === 'withdrawn' ? '' : 'Спасибо за интерес', viewer: { ...reviewedApplication.viewer, decisions: [] } }))
    renderApp(path)
    expect(await screen.findByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Принять кандидата' })).not.toBeInTheDocument()
    if (status !== 'withdrawn') expect(screen.getByText('Спасибо за интерес')).toBeInTheDocument()
    else expect(screen.queryByText(/Записка кандидату/)).not.toBeInTheDocument()
  })

  it('accepts with a note', async () => {
    const { calls } = stubApi(card(reviewedApplication, { [POST('/status')]: reply(204) }))
    renderApp(path)
    await click('Принять кандидата')
    const dialog = screen.getByRole('dialog', { name: 'Принять кандидата?' })
    expect(within(dialog).getByText(/вы берёте кандидата на позицию.*Решение окончательное/)).toBeInTheDocument()
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
    await click('Принять кандидата')
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

// ---------------------------------------------------------------- решение глазами соискателя

describe('decision seen by the applicant', () => {
  const path = `/applications/${APP_ID}`

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
  })
})

// ---------------------------------------------------------------- «Мои отклики» и «Мои вакансии»

describe('other pages', () => {
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

// ---------------------------------------------------------------- соискатель удалил аккаунт (срез 20, D-142)

describe('anonymized application', () => {
  const erased: Detail = {
    ...staffApplication,
    status: 'withdrawn',
    applicant_name: 'Аккаунт удалён',
    anonymized: true,
    contact_email: '',
    cover_letter: '',
    cv: null,
    files: [],
    references: [],
    viewer: { ...staffApplication.viewer, decisions: [] },
  }

  it('shows the card without the letter, files and profile, and says why', async () => {
    stubApi(card(erased))
    renderApp(`/candidates/${APP_ID}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Аккаунт удалён' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Соискатель удалил аккаунт' })).toBeInTheDocument()
    expect(screen.getByText(/стёрты письмо, контакты, профиль, файлы и рекомендации/)).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2, name: 'Сопроводительное письмо' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /mailto/ })).not.toBeInTheDocument()
  })

  it('marks the row in the list', async () => {
    stubApi(
      signedIn({
        'GET /api/my/candidates?*': reply(200, candidateList([{ ...candidate, applicant_name: 'Аккаунт удалён', headline: '', anonymized: true, status: 'withdrawn' }])),
        'GET /api/my/candidate-vacancies': reply(200, { items: [] }),
      }),
    )
    renderApp('/candidates')
    expect(await screen.findByText('Соискатель удалил аккаунт')).toBeInTheDocument()
    expect(screen.queryByText('Должность не указана')).not.toBeInTheDocument()
  })
})

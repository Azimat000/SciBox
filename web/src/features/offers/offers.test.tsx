import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { OFFER_ID, catalogCard, catalogResult, declined, interested, mineList, offer, secondTarget, sentAnswered, sentCancelled, sentList, sentOffer, target } from '../../test/offers'
import { PROFILE_ID, profile } from '../../test/profile'
import { renderApp } from '../../test/render'
import { VACANCY_ID, reference } from '../../test/vacancies'
import { ROLE_STORAGE_KEY } from '../shell/role-context'
import { vacancyOpen } from './labels'

beforeEach(() => window.localStorage.clear())

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({ ...signedInAs(ann), ...extra })
const employer = () => window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })
const TARGETS = `GET /api/scientists/${PROFILE_ID}/offer-targets`

// ---------------------------------------------------------------- «Приглашения» учёного

describe('my offers', () => {
  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/offers')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists offers with what waits for the answer and what was answered', async () => {
    const closed = { ...declined, vacancy: { ...declined.vacancy, status: 'closed', title: 'Закрытая вакансия' }, application_id: 'app-1' }
    stubApi(signedIn({ 'GET /api/offers?*': reply(200, mineList([offer, interested, closed])) }))
    renderApp('/offers')
    expect(await screen.findByRole('heading', { level: 1, name: 'Приглашения' })).toBeInTheDocument()
    const rows = screen.getAllByRole('listitem').filter((li) => li.classList.contains('app-entry'))
    expect(rows).toHaveLength(3)
    const first = within(rows[0])
    expect(first.getByRole('link', { name: offer.vacancy.title })).toHaveAttribute('href', `/offers/${OFFER_ID}`)
    expect(first.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', '/organizations/sibirskiy-institut')
    expect(first.getByText('Ждёт вашего ответа')).toBeInTheDocument()
    expect(first.getByText(/Приглашение от 3 октября 2026/)).toBeInTheDocument()
    expect(within(rows[1]).getByText('Ваш ответ: Интересно')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Ваш ответ: Не сейчас')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Вакансия закрыта')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Вы откликнулись')).toBeInTheDocument()
    expect(screen.getByText('3 приглашения')).toBeInTheDocument()
  })

  it('has the tabs of the section and marks the current one', async () => {
    stubApi(signedIn({ 'GET /api/offers?*': reply(200, mineList()) }))
    renderApp('/offers')
    const tabs = await screen.findByRole('navigation', { name: 'Отклики и приглашения' })
    expect(within(tabs).getByRole('link', { name: 'Приглашения' })).toHaveAttribute('aria-current', 'page')
    expect(within(tabs).getByRole('link', { name: 'Отклики' })).toHaveAttribute('href', '/applications')
    // Пункт меню «Мои отклики» остаётся текущим и на странице приглашений.
    const menu = screen.getAllByRole('navigation', { name: 'Основное меню' })[0]
    expect(within(menu).getByRole('link', { name: 'Мои отклики' })).toHaveAttribute('aria-current', 'page')
  })

  it('explains an empty list and leads to the profile settings', async () => {
    stubApi(signedIn({ 'GET /api/offers?*': reply(200, mineList([])) }))
    renderApp('/offers')
    expect(await screen.findByRole('heading', { level: 2, name: 'Приглашений пока нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Настроить профиль' })).toHaveAttribute('href', '/profile')
  })

  it('turns pages', async () => {
    const user = userEvent.setup()
    const calls: string[] = []
    const many = Array.from({ length: 20 }, (_, i) => ({ ...offer, id: `o${i}` }))
    stubApi(signedIn({ 'GET /api/offers?*': (call) => (calls.push(call.path), reply(200, mineList(many, { total: 25 }))) }))
    const { router } = renderApp('/offers')
    expect(await screen.findByText('Страница 1 из 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Назад' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Дальше' }))
    await waitFor(() => expect(calls.at(-1)).toContain('offset=20'))
    expect(router.state.location.search).toBe('?page=2')
    await user.click(await screen.findByRole('button', { name: 'Назад' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
  })

  it('shows an error with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi(signedIn({ 'GET /api/offers?*': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, mineList())) }))
    renderApp('/offers')
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить приглашения' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: offer.vacancy.title })).toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- страница приглашения

describe('offer page', () => {
  const path = `/offers/${OFFER_ID}`
  const page = (o = offer, extra: Record<string, Route> = {}) => stubApi(signedIn({ [`GET /api/offers/${OFFER_ID}`]: reply(200, { offer: o }), ...extra }))

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp(path)
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('shows the message, the vacancy and the way to apply', async () => {
    page()
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: offer.vacancy.title })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: offer.vacancy.title })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}`)
    expect(screen.getByRole('link', { name: 'Сибирский институт' })).toHaveAttribute('href', '/organizations/sibirskiy-institut')
    expect(screen.getByText(/Ваши работы по катализаторам/)).toBeInTheDocument()
    expect(screen.getByText('Ждёт вашего ответа')).toBeInTheDocument()
    expect(screen.getByText('Лаборатория катализа, Новосибирск')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Открыть вакансию' })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}`)
    expect(screen.getByRole('link', { name: 'Откликнуться' })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}/apply`)
  })

  it('says so when the organization left no message', async () => {
    page({ ...offer, message: '' })
    renderApp(path)
    expect(await screen.findByText(/Организация не оставила сообщения/)).toBeInTheDocument()
  })

  it('answers «Интересно» with a note and tells it to the server', async () => {
    const user = userEvent.setup()
    const { calls } = page(offer, { [`POST /api/offers/${OFFER_ID}/answer`]: reply(204) })
    renderApp(path)
    await user.type(await screen.findByRole('textbox', { name: /Записка для организации/ }), 'Откликнусь на этой неделе')
    await user.click(screen.getByRole('button', { name: 'Интересно' }))
    await toast('Ответ отправлен')
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ action: 'interested', note: 'Откликнусь на этой неделе' })
  })

  it('answers «Не сейчас» without a note', async () => {
    const user = userEvent.setup()
    const { calls } = page(offer, { [`POST /api/offers/${OFFER_ID}/answer`]: reply(204) })
    renderApp(path)
    await user.click(await screen.findByRole('button', { name: 'Не сейчас' }))
    await toast('Ответ отправлен')
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ action: 'declined', note: '' })
  })

  it('shows the answer after answering', async () => {
    const user = userEvent.setup()
    let answered = false
    stubApi(signedIn({
      [`GET /api/offers/${OFFER_ID}`]: () => reply(200, { offer: answered ? interested : offer }),
      [`POST /api/offers/${OFFER_ID}/answer`]: () => ((answered = true), reply(204)),
    }))
    renderApp(path)
    await user.click(await screen.findByRole('button', { name: 'Интересно' }))
    expect(await screen.findByText('Вы ответили: Интересно')).toBeInTheDocument()
    expect(screen.getByText(/Ответ от 4 октября 2026/)).toBeInTheDocument()
    expect(screen.getByText('Откликнусь на этой неделе')).toBeInTheDocument()
    expect(screen.getByText(/Если вакансия вам подходит, откликнитесь на неё/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Не сейчас' })).not.toBeInTheDocument()
  })

  it('refuses a note that is too long before asking the server', async () => {
    const user = userEvent.setup()
    const { called } = page(offer)
    renderApp(path)
    const note = await screen.findByRole('textbox', { name: /Записка для организации/ })
    await user.click(note)
    await user.paste('я'.repeat(1001))
    await user.click(screen.getByRole('button', { name: 'Интересно' }))
    expect(await screen.findByText(/Записка длиннее 1000 знаков/)).toBeInTheDocument()
    expect(called('POST', `/api/offers/${OFFER_ID}/answer`)).toHaveLength(0)
  })

  it('shows what the server says about the note and about everything else', async () => {
    const user = userEvent.setup()
    let body = apiError(422, 'validation_failed', 'Проверьте приглашение', { fields: { note: 'Записка слишком странная' } })
    page(offer, { [`POST /api/offers/${OFFER_ID}/answer`]: () => body })
    renderApp(path)
    await user.click(await screen.findByRole('button', { name: 'Интересно' }))
    expect(await screen.findByText('Записка слишком странная')).toBeInTheDocument()
    body = apiError(409, 'invalid_offer_state', 'Приглашение уже изменилось. Обновите страницу')
    await user.click(screen.getByRole('button', { name: 'Не сейчас' }))
    expect(await screen.findByText('Приглашение уже изменилось. Обновите страницу')).toBeInTheDocument()
  })

  it('links to the application when there is one and does not offer a new one', async () => {
    page({ ...interested, application_id: 'app-7' })
    renderApp(path)
    expect(await screen.findByRole('link', { name: 'Смотреть отклик' })).toHaveAttribute('href', '/applications/app-7')
    expect(screen.getByText('Вы уже откликнулись на эту вакансию.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Если вакансия вам подходит/)).not.toBeInTheDocument()
  })

  it('does not offer to apply when the vacancy is closed or its deadline has passed', async () => {
    const first = page({ ...offer, vacancy: { ...offer.vacancy, status: 'closed' } })
    const view = renderApp(path)
    expect(await screen.findByText('Вакансия закрыта: отклики не принимаются.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Заявки до/)).not.toBeInTheDocument()
    view.unmount()
    void first

    page({ ...offer, vacancy: { ...offer.vacancy, deadline: '2020-01-01' } })
    renderApp(path)
    expect(await screen.findByText('Срок подачи прошёл: отклики не принимаются.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Откликнуться' })).not.toBeInTheDocument()
  })

  it('shows an answered «Не сейчас» without a note or a date', async () => {
    page({ ...declined, answered_at: null })
    renderApp(path)
    expect(await screen.findByText('Вы ответили: Не сейчас')).toBeInTheDocument()
    expect(screen.queryByText(/Ответ от/)).not.toBeInTheDocument()
    expect(screen.queryByText('Ваша записка:')).not.toBeInTheDocument()
  })

  it('handles a vacancy without deadline and a cancelled offer', async () => {
    page({ ...offer, status: 'cancelled', can_answer: false, vacancy: { ...offer.vacancy, deadline: null, unit_name: '', city: '' } })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Ваш ответ' })).toBeInTheDocument()
    expect(screen.getByText('Отозвано', { selector: '.application-muted' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Откликнуться' })).toBeInTheDocument()
  })

  it('says the offer does not exist, with a way back', async () => {
    stubApi(signedIn({ [`GET /api/offers/${OFFER_ID}`]: apiError(404, 'not_found', 'Такого приглашения нет') }))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Такого приглашения нет' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'К приглашениям' })).toHaveAttribute('href', '/offers')
  })

  it('shows another failure with a retry', async () => {
    const user = userEvent.setup()
    let fail = true
    stubApi(signedIn({ [`GET /api/offers/${OFFER_ID}`]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { offer })) }))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить приглашение' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('heading', { level: 1, name: offer.vacancy.title })).toBeInTheDocument()
  })
})

describe('vacancyOpen', () => {
  const at = (iso: string) => new Date(iso)
  it('knows whether the vacancy still takes applications, by the Moscow date', () => {
    expect(vacancyOpen(offer, at('2026-10-03T12:00:00Z'))).toBe('open')
    expect(vacancyOpen({ ...offer, vacancy: { ...offer.vacancy, status: 'archived' } })).toBe('closed')
    const last = { ...offer, vacancy: { ...offer.vacancy, deadline: '2026-10-03' } }
    expect(vacancyOpen(last, at('2026-10-03T20:59:00Z'))).toBe('open')
    expect(vacancyOpen(last, at('2026-10-03T21:00:00Z'))).toBe('deadline')
    expect(vacancyOpen({ ...offer, vacancy: { ...offer.vacancy, deadline: null } }, at('2099-01-01T00:00:00Z'))).toBe('open')
  })
})

// ---------------------------------------------------------------- «Отправленные приглашения»

describe('sent offers', () => {
  const route = 'GET /api/my/sent-offers?*'

  it('sends a visitor to sign in', async () => {
    stubApi()
    const { router } = renderApp('/sent-offers')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('lists whom the organization invited, what they answered, with tabs and counts', async () => {
    employer()
    const items = [sentOffer, sentAnswered, sentCancelled]
    stubApi(signedIn({ [route]: reply(200, sentList(items)) }))
    renderApp('/sent-offers')
    expect(await screen.findByRole('heading', { level: 1, name: 'Отправленные приглашения' })).toBeInTheDocument()
    const rows = screen.getAllByRole('listitem').filter((li) => li.classList.contains('cand-row'))
    expect(rows).toHaveLength(3)
    const first = within(rows[0])
    expect(first.getByRole('link', { name: 'Елена Орлова' })).toHaveAttribute('href', `/scientists/${PROFILE_ID}`)
    expect(first.getByRole('link', { name: offer.vacancy.title })).toHaveAttribute('href', `/vacancies/${VACANCY_ID}`)
    expect(first.getByText(/Лаборатория катализа/)).toBeInTheDocument()
    expect(first.getByText('Ждёт ответа')).toBeInTheDocument()
    expect(first.getByText(/Отправлено 3 октября 2026/)).toBeInTheDocument()
    expect(first.getByText(/Ваши работы по катализаторам/)).toBeInTheDocument()
    expect(first.getByRole('button', { name: 'Отозвать' })).toBeInTheDocument()
    const answered = within(rows[1])
    expect(answered.getByText('Интересно')).toBeInTheDocument()
    expect(answered.getByText(/Ответ 4 октября 2026/)).toBeInTheDocument()
    expect(answered.getByText('Спасибо, откликнусь')).toBeInTheDocument()
    expect(answered.queryByRole('button', { name: 'Отозвать' })).not.toBeInTheDocument()
    expect(within(rows[2]).getByText('Отозвано')).toBeInTheDocument()
    expect(within(rows[2]).queryByText(/Ваше сообщение/)).not.toBeInTheDocument()
    const tabs = screen.getByRole('navigation', { name: 'Состояния приглашений' })
    expect(within(tabs).getByRole('link', { name: 'Все, 3' })).toHaveAttribute('aria-current', 'page')
    expect(within(tabs).getByRole('link', { name: 'Ждут ответа, 1' })).toBeInTheDocument()
    expect(within(tabs).getByRole('link', { name: 'Не сейчас, 0' })).toHaveAttribute('href', '/sent-offers?status=declined')
    expect(within(tabs).getByRole('link', { name: 'Отозваны, 1' })).toBeInTheDocument()
    expect(screen.getByText('3 приглашения')).toBeInTheDocument()
    // Меню «Отклики» остаётся текущим.
    const menu = screen.getAllByRole('navigation', { name: 'Основное меню' })[0]
    expect(within(menu).getByRole('link', { name: 'Отклики' })).toHaveAttribute('aria-current', 'page')
    expect(within(screen.getByRole('navigation', { name: 'Отклики и приглашения' })).getByRole('link', { name: 'Отклики' })).toHaveAttribute('href', '/candidates')
  })

  it('takes the tab from the address and asks the server for it', async () => {
    employer()
    const calls: string[] = []
    stubApi(signedIn({ [route]: (call) => (calls.push(call.path), reply(200, sentList([sentAnswered]))) }))
    renderApp('/sent-offers?status=interested&page=2')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(calls.at(-1)).toContain('status=interested')
    expect(calls.at(-1)).toContain('offset=20')
    expect(screen.getByRole('link', { name: 'Интересно, 1' })).toHaveAttribute('aria-current', 'page')
  })

  it('ignores an unknown tab in the address', async () => {
    employer()
    const calls: string[] = []
    stubApi(signedIn({ [route]: (call) => (calls.push(call.path), reply(200, sentList())) }))
    renderApp('/sent-offers?status=bogus')
    await screen.findByRole('link', { name: 'Елена Орлова' })
    expect(calls.at(-1)).not.toContain('status=')
  })

  it('cancels an offer after a confirmation', async () => {
    const user = userEvent.setup()
    employer()
    const { calls } = stubApi(signedIn({ [route]: reply(200, sentList()), [`POST /api/offers/${OFFER_ID}/cancel`]: reply(204) }))
    renderApp('/sent-offers')
    await user.click(await screen.findByRole('button', { name: 'Отозвать' }))
    const dialog = await screen.findByRole('dialog', { name: 'Отозвать приглашение?' })
    await user.click(within(dialog).getByRole('button', { name: 'Отозвать' }))
    await toast('Приглашение отозвано')
    expect(calls.some((c) => c.method === 'POST' && c.path === `/api/offers/${OFFER_ID}/cancel`)).toBe(true)
  })

  it('does not cancel when the confirmation is declined', async () => {
    const user = userEvent.setup()
    employer()
    const { called } = stubApi(signedIn({ [route]: reply(200, sentList()) }))
    renderApp('/sent-offers')
    await user.click(await screen.findByRole('button', { name: 'Отозвать' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отмена' }))
    expect(called('POST', `/api/offers/${OFFER_ID}/cancel`)).toHaveLength(0)
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('tells why cancelling failed', async () => {
    const user = userEvent.setup()
    employer()
    stubApi(signedIn({ [route]: reply(200, sentList()), [`POST /api/offers/${OFFER_ID}/cancel`]: apiError(409, 'invalid_offer_state', 'Приглашение уже изменилось') }))
    renderApp('/sent-offers')
    await user.click(await screen.findByRole('button', { name: 'Отозвать' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Отозвать' }))
    expect(await toast('Приглашение уже изменилось')).toBeInTheDocument()
  })

  it('explains that nobody was invited yet and leads to the catalog', async () => {
    employer()
    stubApi(signedIn({ [route]: reply(200, sentList([])) }))
    renderApp('/sent-offers')
    expect(await screen.findByRole('heading', { level: 2, name: 'Вы пока никого не приглашали' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Перейти к соискателям' })).toHaveAttribute('href', '/scientists')
    expect(screen.getByRole('navigation', { name: 'Отклики и приглашения' })).toBeInTheDocument()
  })

  it('explains an empty tab', async () => {
    employer()
    stubApi(signedIn({ [route]: reply(200, sentList([], { counts: { pending: 2, interested: 0, declined: 0, cancelled: 0 } })) }))
    renderApp('/sent-offers?status=declined')
    expect(await screen.findByRole('heading', { level: 2, name: 'В этой вкладке ничего нет' })).toBeInTheDocument()
  })

  it('turns pages', async () => {
    const user = userEvent.setup()
    employer()
    const many = Array.from({ length: 20 }, (_, i) => ({ ...sentOffer, id: `s${i}` }))
    stubApi(signedIn({ [route]: reply(200, sentList(many, { total: 25 })) }))
    const { router } = renderApp('/sent-offers')
    expect(await screen.findByText('Страница 1 из 2')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Дальше' }))
    expect(router.state.location.search).toBe('?page=2')
    await user.click(await screen.findByRole('button', { name: 'Назад' }))
    expect(router.state.location.search).toBe('')
  })

  it('shows an error with a retry', async () => {
    const user = userEvent.setup()
    employer()
    let fail = true
    stubApi(signedIn({ [route]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, sentList())) }))
    renderApp('/sent-offers')
    expect(await screen.findByRole('heading', { level: 2, name: 'Не удалось загрузить приглашения' })).toBeInTheDocument()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('link', { name: 'Елена Орлова' })).toBeInTheDocument()
  })

  it('copes with an offer without a scientist in the answer', async () => {
    employer()
    stubApi(signedIn({ [route]: reply(200, sentList([{ ...sentOffer, scientist: undefined, message: '' }])) }))
    renderApp('/sent-offers')
    expect(await screen.findByText('Ждёт ответа', { selector: '.tag' })).toBeInTheDocument()
    expect(screen.queryByText(/Ваше сообщение/)).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- окно «Пригласить на вакансию»

describe('invite window', () => {
  const catalogRoutes = (over: Record<string, Route> = {}) =>
    signedIn({
      'GET /api/reference': reply(200, reference),
      'GET /api/scientists?*': reply(200, catalogResult([catalogCard])),
      ...over,
    })
  const open = async () => {
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Пригласить на вакансию: Елена Орлова' }))
    return { user, dialog: await screen.findByRole('dialog', { name: 'Пригласить на вакансию' }) }
  }

  it('invites to the chosen vacancy with a message', async () => {
    employer()
    const { calls } = stubApi(
      catalogRoutes({
        [TARGETS]: reply(200, { items: [target, secondTarget] }),
        'POST /api/offers': reply(201, { offer: sentOffer }),
      }),
    )
    renderApp('/scientists')
    const { user, dialog } = await open()
    const d = within(dialog)
    expect(await d.findByRole('combobox', { name: /Вакансия/ })).toHaveValue('')
    expect(d.getByText(/Имя сотрудника ему не сообщается/)).toBeInTheDocument()
    await user.selectOptions(d.getByRole('combobox', { name: /Вакансия/ }), secondTarget.id)
    await user.type(d.getByRole('textbox', { name: /Сообщение/ }), 'Нам нужен постдок')
    await user.click(d.getByRole('button', { name: 'Отправить приглашение' }))
    await toast('Приглашение отправлено')
    expect(calls.find((c) => c.method === 'POST' && c.path === '/api/offers')?.body).toEqual({ vacancy_id: secondTarget.id, profile_id: PROFILE_ID, message: 'Нам нужен постдок' })
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('selects the only free vacancy by itself and marks the busy ones', async () => {
    employer()
    stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [{ ...target, offered: true }, secondTarget, { ...target, id: 'v3', title: 'Доцент', applied: true }] }) }))
    renderApp('/scientists')
    const { dialog } = await open()
    const select = await within(dialog).findByRole('combobox', { name: /Вакансия/ })
    expect(select).toHaveValue(secondTarget.id)
    const options = within(select).getAllByRole('option').map((o) => o.textContent)
    expect(options).toContain('Старший научный сотрудник: сверхпроводящие плёнки · Лаборатория катализа — уже приглашён')
    expect(options).toContain('Доцент · Лаборатория катализа — уже откликнулся')
    expect(options).toContain('Постдок: геномика')
    expect(within(select).queryByRole('group')).not.toBeInTheDocument()
  })

  it('groups the vacancies by organization when the person runs several', async () => {
    employer()
    const other = { ...secondTarget, org_name: 'Институт океанологии', org_slug: 'okeanologii', unit_name: '' }
    stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [target, other] }) }))
    renderApp('/scientists')
    const { dialog } = await open()
    const select = await within(dialog).findByRole('combobox', { name: /Вакансия/ })
    const groups = within(select).getAllByRole('group')
    expect(groups.map((g) => g.getAttribute('label'))).toEqual(['Сибирский институт', 'Институт океанологии'])
    expect(within(groups[1]).getByRole('option', { name: 'Постдок: геномика' })).toBeInTheDocument()
  })

  it('does not let a person who is already invited everywhere send anything', async () => {
    employer()
    stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [{ ...target, offered: true }] }) }))
    renderApp('/scientists')
    const { dialog } = await open()
    expect(await within(dialog).findByText(/Этого человека уже приглашали на все ваши открытые вакансии/)).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Отправить приглашение' })).toBeDisabled()
  })

  it('asks for a vacancy and for a shorter message before sending', async () => {
    employer()
    const { called } = stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [target, secondTarget] }) }))
    renderApp('/scientists')
    const { user, dialog } = await open()
    const d = within(dialog)
    await d.findByRole('combobox', { name: /Вакансия/ })
    await user.click(d.getByRole('textbox', { name: /Сообщение/ }))
    await user.paste('я'.repeat(1001))
    await user.click(d.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await d.findByText('Выберите вакансию, на которую зовёте')).toBeInTheDocument()
    expect(d.getByText('Сообщение длиннее 1000 знаков: сократите его')).toBeInTheDocument()
    expect(called('POST', '/api/offers')).toHaveLength(0)
    expect(d.getByText(/1001 из 1000/)).toBeInTheDocument()
  })

  it('shows the answer of the server: a field error and a general one', async () => {
    employer()
    let answer = apiError(422, 'validation_failed', 'Проверьте приглашение', { fields: { message: 'Сообщение слишком странное' } })
    stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [target] }), 'POST /api/offers': () => answer }))
    renderApp('/scientists')
    const { user, dialog } = await open()
    const d = within(dialog)
    await d.findByRole('combobox', { name: /Вакансия/ })
    await user.click(d.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await d.findByText('Сообщение слишком странное')).toBeInTheDocument()
    answer = apiError(409, 'already_offered', 'Этого человека уже приглашали на эту вакансию')
    await user.click(d.getByRole('button', { name: 'Отправить приглашение' }))
    expect(await d.findByText('Этого человека уже приглашали на эту вакансию')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('says there is nowhere to invite and leads to the vacancies', async () => {
    employer()
    stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [] }) }))
    renderApp('/scientists')
    const { dialog } = await open()
    expect(await within(dialog).findByRole('heading', { name: 'Пока некуда приглашать' })).toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: 'Мои вакансии' })).toHaveAttribute('href', '/my-vacancies')
  })

  it('shows a loading state, then an error with a retry', async () => {
    employer()
    let fail = true
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    stubApi(catalogRoutes({ [TARGETS]: async () => (await gate, fail ? apiError(500, 'internal', 'Сбой') : reply(200, { items: [target] })) }))
    renderApp('/scientists')
    const { user, dialog } = await open()
    expect(within(dialog).getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    release()
    expect(await within(dialog).findByRole('heading', { name: 'Не удалось загрузить ваши вакансии' })).toBeInTheDocument()
    fail = false
    await user.click(within(dialog).getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await within(dialog).findByRole('combobox', { name: /Вакансия/ })).toBeInTheDocument()
  })

  it('closes without sending', async () => {
    employer()
    const { called } = stubApi(catalogRoutes({ [TARGETS]: reply(200, { items: [target, secondTarget] }) }))
    renderApp('/scientists')
    const { user, dialog } = await open()
    await user.click(await within(dialog).findByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(called('POST', '/api/offers')).toHaveLength(0)
  })
})

// ---------------------------------------------------------------- страница учёного

describe('scientist page', () => {
  const view = (isOwner: boolean) => reply(200, { profile: { ...profile }, viewer: { is_owner: isOwner, can_see_contacts: true } })

  it('has the invite button in the hiring mode', async () => {
    employer()
    stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: view(false), [TARGETS]: reply(200, { items: [target] }) }))
    renderApp(`/scientists/${PROFILE_ID}`)
    const button = await screen.findByRole('button', { name: 'Пригласить на вакансию: Елена Орлова' })
    await userEvent.click(button)
    expect(await screen.findByRole('dialog', { name: 'Пригласить на вакансию' })).toBeInTheDocument()
  })

  it('has no invite button for the owner of the profile, for a visitor or in the job-seeking mode', async () => {
    employer()
    stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: view(true) }))
    const own = renderApp(`/scientists/${PROFILE_ID}`)
    await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })
    expect(screen.queryByRole('button', { name: /Пригласить на вакансию/ })).not.toBeInTheDocument()
    own.unmount()

    stubApi({ [`GET /api/scientists/${PROFILE_ID}`]: view(false) })
    const visitor = renderApp(`/scientists/${PROFILE_ID}`)
    await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })
    expect(screen.queryByRole('button', { name: /Пригласить на вакансию/ })).not.toBeInTheDocument()
    visitor.unmount()

    window.localStorage.clear()
    stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: view(false) }))
    renderApp(`/scientists/${PROFILE_ID}`)
    await screen.findByRole('heading', { level: 1, name: 'Елена Орлова' })
    expect(screen.queryByRole('button', { name: /Пригласить на вакансию/ })).not.toBeInTheDocument()
  })
})

// ---------------------------------------------------------------- вкладки «Отклики / Приглашения» у обеих сторон

describe('tabs of the applications section', () => {
  it('are on the page of my applications and lead to my offers', async () => {
    stubApi(signedIn({ 'GET /api/applications?*': reply(200, { items: [], total: 0 }) }))
    renderApp('/applications')
    const tabs = await screen.findByRole('navigation', { name: 'Отклики и приглашения' })
    expect(within(tabs).getByRole('link', { name: 'Отклики' })).toHaveAttribute('aria-current', 'page')
    expect(within(tabs).getByRole('link', { name: 'Приглашения' })).toHaveAttribute('href', '/offers')
  })

  it('are on the list of candidates and lead to the sent offers', async () => {
    employer()
    stubApi(signedIn({ 'GET /api/my/candidates?*': reply(200, { items: [], total: 0, counts: { sent: 0, viewed: 0, invited: 0, rejected: 0, accepted: 0, withdrawn: 0 } }), 'GET /api/my/candidate-vacancies': reply(200, { items: [] }) }))
    renderApp('/candidates')
    const tabs = await screen.findByRole('navigation', { name: 'Отклики и приглашения' })
    expect(within(tabs).getByRole('link', { name: 'Приглашения' })).toHaveAttribute('href', '/sent-offers')
  })
})

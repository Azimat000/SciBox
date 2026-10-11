import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { catalogCard, catalogResult, sentList, sentOffer } from '../../test/offers'
import { maskedPage, maskedProfile, ownPage, profile, PROFILE_ID } from '../../test/profile'
import { renderApp } from '../../test/render'
import { ROLE_STORAGE_KEY } from '../shell/role-context'

beforeEach(() => window.localStorage.clear())

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({ ...signedInAs(ann), ...extra })
const employer = () => window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
const hiddenRoute = 'GET /api/profile/hidden-orgs'
const org = (slug: string, name: string, city = 'Казань') => ({ slug, name, city, hidden_at: '2026-10-03T12:00:00Z' })

describe('privacy mode "anonymously"', () => {
  it('offers the fourth mode between "nobody" and "organizations" and saves it', async () => {
    const user = userEvent.setup()
    const { called } = stubApi(
      signedIn({ 'GET /api/profile': reply(200, ownPage()), [hiddenRoute]: reply(200, { items: [] }), 'PUT /api/profile/privacy': reply(200, ownPage({ ...profile, visibility: 'anonymous' })) }),
    )
    renderApp('/profile')
    const modes = await screen.findAllByRole('radio')
    expect(modes.map((r) => r.getAttribute('value'))).toEqual(['hidden', 'anonymous', 'orgs', 'public'])
    const anonymous = screen.getByRole('radio', { name: /^Анонимно/ })
    expect(anonymous).toHaveAccessibleName(/Имя откроется организации, когда вы откликнетесь/)
    await user.click(anonymous)
    expect(await screen.findByText('Приватность сохранена')).toBeInTheDocument()
    expect(called('PUT', '/api/profile/privacy')[0].body).toEqual({ visibility: 'anonymous', open_to_offers: true })
  })

  it('reminds the owner that organizations see only the scientific side', async () => {
    stubApi(signedIn({ 'GET /api/profile': reply(200, ownPage({ ...profile, visibility: 'anonymous' })), [hiddenRoute]: reply(200, { items: [] }) }))
    renderApp('/profile')
    expect(await screen.findByText(/Режим «Анонимно»: организации видят только научный облик/)).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /^Анонимно/ })).toBeChecked()
  })
})

describe('hide from organizations', () => {
  const open = (extra: Record<string, Route> = {}) => stubApi(signedIn({ 'GET /api/profile': reply(200, ownPage()), ...extra }))

  it('lists the organizations the profile is hidden from, and says so when there are none', async () => {
    open({ [hiddenRoute]: reply(200, { items: [] }) })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    expect(await panel.findByText('Пока ни от одной организации профиль не скрыт.')).toBeInTheDocument()
    expect(panel.getByText(/Публичную страницу по ссылке может открыть и тот, кто не вошёл/)).toBeInTheDocument()
  })

  it('shows the hidden list with a button to bring each organization back', async () => {
    const user = userEvent.setup()
    const { called } = open({
      [hiddenRoute]: reply(200, { items: [org('institut-a', 'Институт А'), org('institut-b', 'Институт Б', 'Томск')] }),
      'DELETE /api/profile/hidden-orgs/institut-a': reply(200, { items: [org('institut-b', 'Институт Б', 'Томск')] }),
    })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    expect(await panel.findByText('Институт А')).toBeInTheDocument()
    expect(panel.getByText('Томск')).toBeInTheDocument()
    await user.click(panel.getByRole('button', { name: 'Снова показывать профиль организации Институт А' }))
    expect(await screen.findByText('Организация «Институт А» снова видит профиль', { selector: '.toast *' })).toBeInTheDocument()
    expect(called('DELETE', '/api/profile/hidden-orgs/institut-a')).toHaveLength(1)
    await waitFor(() => expect(panel.queryByText('Институт А')).not.toBeInTheDocument())
    expect(panel.getByText('Институт Б')).toBeInTheDocument()
  })

  it('finds an organization by name and hides the profile from it', async () => {
    const user = userEvent.setup()
    const found = [
      { slug: 'institut-a', name: 'Институт А', city: 'Казань' },
      { slug: 'institut-b', name: 'Институт Б', city: 'Томск' },
    ]
    const { called } = open({
      [hiddenRoute]: reply(200, { items: [org('institut-b', 'Институт Б', 'Томск')] }),
      'GET /api/organizations?limit=8&q=%D0%B8%D0%BD%D1%81%D1%82': reply(200, { items: found }),
      'PUT /api/profile/hidden-orgs/institut-a': reply(200, { items: [org('institut-a', 'Институт А'), org('institut-b', 'Институт Б', 'Томск')] }),
    })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    await user.type(await panel.findByRole('searchbox', { name: /Найти организацию/ }), 'инст')
    const results = await panel.findByRole('list', { name: 'Найти организацию' })
    // Уже скрытая помечена и не имеет кнопки; у другой кнопка есть.
    expect(within(results).getByText('Уже скрыто')).toBeInTheDocument()
    expect(within(results).queryByRole('button', { name: 'Скрыть профиль от организации Институт Б' })).not.toBeInTheDocument()
    await user.click(within(results).getByRole('button', { name: 'Скрыть профиль от организации Институт А' }))
    expect(await screen.findByText('Профиль скрыт от организации «Институт А»', { selector: '.toast *' })).toBeInTheDocument()
    expect(called('PUT', '/api/profile/hidden-orgs/institut-a')).toHaveLength(1)
  })

  it('does not search for fewer than two letters and says when nothing is found', async () => {
    const user = userEvent.setup()
    const { calls } = open({ [hiddenRoute]: reply(200, { items: [] }), 'GET /api/organizations?*': reply(200, { items: [] }) })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    const box = await panel.findByRole('searchbox', { name: /Найти организацию/ })
    await user.type(box, 'и')
    await new Promise((r) => setTimeout(r, 450))
    expect(calls.some((c) => c.path.startsWith('/api/organizations'))).toBe(false)
    await user.type(box, 'ы')
    expect(await panel.findByText('Организаций с таким названием нет.')).toBeInTheDocument()
  })

  it('says why hiding failed', async () => {
    const user = userEvent.setup()
    open({
      [hiddenRoute]: reply(200, { items: [] }),
      'GET /api/organizations?*': reply(200, { items: [{ slug: 'institut-a', name: 'Институт А', city: 'Казань' }] }),
      'PUT /api/profile/hidden-orgs/institut-a': apiError(409, 'too_many_hidden_orgs', 'Список полон'),
    })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    await user.type(await panel.findByRole('searchbox', { name: /Найти организацию/ }), 'институт')
    await user.click(await panel.findByRole('button', { name: 'Скрыть профиль от организации Институт А' }))
    expect(await screen.findByText('Не удалось изменить список', { selector: '.toast *' })).toBeInTheDocument()
    expect(screen.getByText('Список полон', { selector: '.toast *' })).toBeInTheDocument()
  })

  it('reports a failed search and a failed list', async () => {
    const user = userEvent.setup()
    open({ [hiddenRoute]: reply(200, { items: [] }), 'GET /api/organizations?*': apiError(500, 'internal', 'Сбой') })
    renderApp('/profile')
    const panel = within(await screen.findByRole('region', { name: 'Скрыть от организаций' }))
    await user.type(await panel.findByRole('searchbox', { name: /Найти организацию/ }), 'институт')
    expect(await panel.findByText('Не удалось найти организации')).toBeInTheDocument()
  })

  it('offers to retry when the list does not load', async () => {
    let fail = true
    open({ [hiddenRoute]: () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { items: [org('institut-a', 'Институт А')] })) })
    renderApp('/profile')
    expect(await screen.findByRole('heading', { name: 'Не удалось загрузить список организаций' })).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByText('Институт А')).toBeInTheDocument()
  })
})

describe('anonymous card of a scientist', () => {
  const page = () => stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, maskedPage()) }))

  it('shows the scientific side under the label and no name, place of work or résumé', async () => {
    page()
    renderApp(`/scientists/${PROFILE_ID}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Кандидат наук, 1.4.4, R2, h-index 12' })).toBeInTheDocument()
    expect(screen.getByText('Анонимный кандидат', { selector: '.profile-headline' })).toBeInTheDocument()
    expect(screen.getByText(/Учёный скрыл имя\./)).toBeInTheDocument()
    expect(screen.getByText('Открыт к предложениям', { selector: '.tag' })).toBeInTheDocument()
    const degree = within(screen.getByRole('region', { name: 'Степень и звание' }))
    expect(degree.getByText('Кандидат наук')).toBeInTheDocument()
    expect(degree.getByText('Физическая химия')).toBeInTheDocument()
    expect(degree.getByText('Доцент')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Уровень' })).getByText('R2')).toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Метрики' })).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['РИНЦ 12', 'Scopus 9'])
    const works = within(screen.getByRole('region', { name: 'Работы' }))
    expect(works.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['14 публикаций', '2 гранта'])
    expect(works.getByText(/3 статьи в журналах Q1–Q2/)).toBeInTheDocument()
    expect(screen.getByText('Резюме откроется вместе с именем.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Скачать резюме/ })).not.toBeInTheDocument()
    for (const hidden of ['О себе', 'Образование', 'Опыт работы', 'Публикации', 'Идентификаторы и метрики']) {
      expect(screen.queryByRole('heading', { name: hidden })).not.toBeInTheDocument()
    }
  })

  it('titles the tab "Анонимный кандидат", not with a name', async () => {
    page()
    renderApp(`/scientists/${PROFILE_ID}`)
    await screen.findByRole('heading', { level: 1 })
    await waitFor(() => expect(document.title).toContain('Анонимный кандидат'))
  })

  it('lets an employer invite the anonymous scientist and explains that the name opens later', async () => {
    const user = userEvent.setup()
    employer()
    stubApi(
      signedIn({
        [`GET /api/scientists/${PROFILE_ID}`]: reply(200, maskedPage()),
        [`GET /api/scientists/${PROFILE_ID}/offer-targets`]: reply(200, { items: [{ id: 'v1', title: 'Вакансия', org_name: 'Институт', org_slug: 'inst', unit_name: '', deadline: null, offered: false, applied: false }] }),
      }),
    )
    renderApp(`/scientists/${PROFILE_ID}`)
    await user.click(await screen.findByRole('button', { name: 'Пригласить на вакансию: Кандидат наук, 1.4.4, R2, h-index 12' }))
    const dialog = await screen.findByRole('dialog', { name: 'Пригласить на вакансию' })
    expect(within(dialog).getByText(/Учёный скрыл имя\. Приглашать его можно/)).toBeInTheDocument()
  })

  it('works without the counts and level of an old server answer', async () => {
    const { counts, career_level, quartiles, ...bare } = maskedProfile
    void counts
    void career_level
    void quartiles
    stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, maskedPage({ ...bare, label: undefined, degree: { ...bare.degree, level: 'none' }, academic_title: 'none', specialties: [], h_index: { rsci: null, scopus: null, wos: null, scholar: null }, open_to_offers: false })) }))
    renderApp(`/scientists/${PROFILE_ID}`)
    expect(await screen.findByRole('heading', { level: 1, name: 'Анонимный кандидат' })).toBeInTheDocument()
    expect(screen.getByText('Степень и звание пока не указаны.')).toBeInTheDocument()
    expect(screen.getByText('h-index не указан.')).toBeInTheDocument()
    expect(screen.getByText('Записей о работах нет.')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Уровень' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Научные специальности' })).not.toBeInTheDocument()
  })

  it('shows a link to nothing when only one quartile line applies', async () => {
    stubApi(signedIn({ [`GET /api/scientists/${PROFILE_ID}`]: reply(200, maskedPage({ ...maskedProfile, quartiles: { q12_total: 1, q12_recent: 0, recent_from: 2022, year: 2024 }, counts: { publications: 1, grants: 0, patents: 3 } })) }))
    renderApp(`/scientists/${PROFILE_ID}`)
    const works = within(await screen.findByRole('region', { name: 'Работы' }))
    expect(works.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['1 публикация', '3 патента или программы'])
    expect(works.getByText(/1 статья в журналах Q1–Q2/)).toBeInTheDocument()
  })
})

describe('anonymous card in the catalog and in sent offers', () => {
  const masked = { ...catalogCard, id: PROFILE_ID, name: '', headline: '', city: '', region: '', masked: true, label: 'Кандидат наук, 1.4.4, R2, h-index 15', career_level: 2, degree: 'candidate' as const, academic_title: 'none' as const }

  it('sets the card out under the label with the level and a "name hidden" mark', async () => {
    stubApi(signedIn({ 'GET /api/scientists?*': reply(200, catalogResult([masked, catalogCard])) }))
    renderApp('/scientists')
    const link = await screen.findByRole('link', { name: 'Кандидат наук, 1.4.4, R2, h-index 15' })
    expect(link).toHaveAttribute('href', `/scientists/${PROFILE_ID}`)
    const card = within(link.closest('article')!)
    expect(card.getByText('Имя скрыто')).toBeInTheDocument()
    expect(card.getByText('R2')).toBeInTheDocument()
    expect(card.queryByText('Новосибирск')).not.toBeInTheDocument()
    // Обычная карточка рядом осталась прежней.
    expect(screen.getByRole('link', { name: 'Елена Орлова' })).toBeInTheDocument()
  })

  it('names an anonymous invitee by the label in the sent list', async () => {
    employer()
    const row = { ...sentOffer, scientist: { profile_id: PROFILE_ID, name: '', masked: true, label: 'Кандидат наук, 1.4.4, R2, h-index 15' } }
    const noLabel = { ...sentOffer, id: 'o2', scientist: { profile_id: PROFILE_ID, name: '', masked: true } }
    stubApi(signedIn({ 'GET /api/my/sent-offers?*': reply(200, sentList([row, noLabel])) }))
    renderApp('/sent-offers')
    expect(await screen.findByRole('link', { name: 'Кандидат наук, 1.4.4, R2, h-index 15' })).toHaveAttribute('href', `/scientists/${PROFILE_ID}`)
    expect(screen.getByRole('link', { name: 'Анонимный кандидат' })).toBeInTheDocument()
    expect(screen.getAllByText('Имя скрыто')).toHaveLength(2)
  })
})

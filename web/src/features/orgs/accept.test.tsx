import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, signedInAs, stubApi } from '../../test/api'
import { press } from '../../test/forms'
import { lab, mine, org, SLUG } from '../../test/orgs'
import { renderApp } from '../../test/render'
import { formatDate, kindLabel, longName, roleLabel, siteLabel, unitKindLabel, unitKindShort } from './labels'

const lookup = (over: Record<string, unknown> = {}) =>
  reply(200, { organization: { slug: SLUG, name: org.name }, role: 'unit_head', unit_name: lab.name, email: 'anna@example.ru', email_matches: null, ...over })

describe('invitation page', () => {
  it('explains the invitation and asks a visitor to sign in with the right address', async () => {
    const api = stubApi({ 'POST /api/invitations/lookup': lookup() })
    renderApp('/invitations/accept?token=abc+def')
    expect(await screen.findByText(`Вас приглашают в «${org.name}»: Руководитель подразделения. Подразделение: ${lab.name}.`)).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Приглашение в организацию' })).toBeInTheDocument()
    expect(screen.getByText(/войдите в аккаунт с почтой anna@example.ru/)).toBeInTheDocument()
    // Ссылка для входа возвращает на эту же страницу, код из письма сохраняется целиком.
    const login = within(screen.getByRole('main')).getByRole('link', { name: 'Войти' })
    expect(login.getAttribute('href')).toBe(`/login?next=${encodeURIComponent('/invitations/accept?token=abc%20def')}`)
    expect(within(screen.getByRole('main')).getByRole('link', { name: 'Зарегистрироваться' })).toHaveAttribute('href', '/register')
    expect(screen.getByText(/Оно также будет ждать вас в разделе «Организация»/)).toBeInTheDocument()
    expect(api.called('POST', '/api/invitations/lookup')[0].body).toEqual({ token: 'abc def' })
  })

  it('shows the invitation without a unit line when the role has no unit', async () => {
    stubApi({ 'POST /api/invitations/lookup': lookup({ unit_name: null, role: 'hr' }) })
    renderApp('/invitations/accept?token=t')
    expect(await screen.findByText(`Вас приглашают в «${org.name}»: Кадровик.`)).toBeInTheDocument()
  })

  it('tells a person signed in under another address to switch accounts', async () => {
    stubApi({ ...signedInAs(), 'POST /api/invitations/lookup': lookup({ email_matches: false }) })
    renderApp('/invitations/accept?token=t')
    expect(await screen.findByRole('alert')).toHaveTextContent('Приглашение отправлено на anna@example.ru, а вы вошли с другой почтой')
    expect(screen.queryByRole('button', { name: 'Принять приглашение' })).not.toBeInTheDocument()
  })

  it('lets the right person accept, and brings them to the organization', async () => {
    const api = stubApi({
      ...signedInAs(),
      'POST /api/invitations/lookup': lookup({ email_matches: true }),
      'POST /api/invitations/accept': reply(200, { slug: SLUG, name: org.name, role: 'unit_head' }),
      [`GET /api/organizations/${SLUG}`]: reply(200, { organization: org, units: [], viewer: { role: 'unit_head', can_edit_organization: false, can_manage_members: false, can_manage_units: false, editable_units: [] } }),
      'GET /api/my/organizations': mine(),
    })
    const { router } = renderApp('/invitations/accept?token=secret-token')
    await userEvent.click(await screen.findByRole('button', { name: 'Принять приглашение' }))
    expect(await screen.findByText(`Вы теперь в организации «${org.name}»`)).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.pathname).toBe(`/my-organization/${SLUG}`))
    expect(api.called('POST', '/api/invitations/accept')[0].body).toEqual({ token: 'secret-token' })
  })

  it('says so when the person already works there', async () => {
    stubApi({
      ...signedInAs(),
      'POST /api/invitations/lookup': lookup({ email_matches: true }),
      'POST /api/invitations/accept': apiError(409, 'already_member', 'Вы уже работаете в этой организации'),
    })
    renderApp('/invitations/accept?token=t')
    await userEvent.click(await screen.findByRole('button', { name: 'Принять приглашение' }))
    expect(await screen.findByText('Вы уже работаете в этой организации.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Перейти к организации' })).toHaveAttribute('href', `/my-organization/${SLUG}`)
    expect(screen.queryByRole('button', { name: 'Принять приглашение' })).not.toBeInTheDocument()
  })

  it('shows any other failure above the button and lets the person try again', async () => {
    let n = 0
    stubApi({
      ...signedInAs(),
      'POST /api/invitations/lookup': lookup({ email_matches: true }),
      'POST /api/invitations/accept': () => (n++ === 0 ? apiError(400, 'invalid_invitation', 'Приглашение устарело, отозвано или уже принято. Попросите отправить новое') : reply(200, { slug: SLUG, name: org.name, role: 'hr' })),
      [`GET /api/organizations/${SLUG}`]: reply(500),
      'GET /api/my/organizations': mine(),
    })
    renderApp('/invitations/accept?token=t')
    await userEvent.click(await screen.findByRole('button', { name: 'Принять приглашение' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Приглашение устарело')
    expect(screen.getByRole('button', { name: 'Принять приглашение' })).toBeInTheDocument()
  })

  it('says the link is incomplete when there is no code', async () => {
    stubApi()
    renderApp('/invitations/accept')
    expect(await screen.findByRole('heading', { level: 1, name: 'Приглашение не работает' })).toBeInTheDocument()
    expect(screen.getByText(/нет кода из письма/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Организация' })).toHaveAttribute('href', '/my-organization')
  })

  it('says the invitation no longer works when it is old, withdrawn or used', async () => {
    stubApi({ 'POST /api/invitations/lookup': apiError(400, 'invalid_invitation', 'Приглашение устарело') })
    renderApp('/invitations/accept?token=old')
    expect(await screen.findByRole('heading', { level: 1, name: 'Приглашение не работает' })).toBeInTheDocument()
    expect(screen.getByText(/Попросите владельца организации отправить новое/)).toBeInTheDocument()
  })

  it('holds the place while loading and offers a retry when the server fails', async () => {
    let n = 0
    stubApi({ 'POST /api/invitations/lookup': () => (n++ === 0 ? reply(500) : lookup()) })
    renderApp('/invitations/accept?token=t')
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось открыть приглашение')
    await press('Проверить ещё раз')
    expect(await screen.findByRole('heading', { level: 1, name: 'Приглашение в организацию' })).toBeInTheDocument()
  })

  it('waits to learn who is signed in before it looks the invitation up', async () => {
    const api = stubApi({ 'GET /api/auth/me': () => new Promise(() => {}) as never })
    renderApp('/invitations/accept?token=t')
    expect(screen.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(api.called('POST', '/api/invitations/lookup')).toHaveLength(0)
  })
})

describe('labels', () => {
  it('names kinds and roles, and falls back to the raw value for ones it does not know', () => {
    expect(kindLabel('university')).toBe('Вуз')
    expect(kindLabel('flying-circus')).toBe('flying-circus')
    expect(unitKindLabel('shared_facility')).toBe('ЦКП (центр коллективного пользования)')
    expect(unitKindLabel('club')).toBe('club')
    expect(unitKindShort('shared_facility')).toBe('ЦКП')
    expect(unitKindShort('club')).toBe('club')
    expect(roleLabel('hr')).toBe('Кадровик')
    expect(roleLabel('boss')).toBe('boss')
  })

  it('shows only the domain of a site and marks very long names', () => {
    expect(siteLabel('https://sikm.example.ru/ru/about?x=1')).toBe('sikm.example.ru')
    expect(siteLabel('не адрес')).toBe('не адрес')
    expect(longName('Институт')).toBeUndefined()
    expect(longName('я'.repeat(81))).toBe(true)
  })

  it('formats dates in Russian and ignores garbage', () => {
    expect(formatDate('2026-10-09T10:00:00Z')).toMatch(/9 октября 2026/)
    expect(formatDate('вчера')).toBe('')
  })
})


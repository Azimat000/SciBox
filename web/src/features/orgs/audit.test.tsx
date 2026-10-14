import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { press } from '../../test/forms'
import { lab, members, orgRoutes, SLUG, viewers } from '../../test/orgs'
import { renderApp } from '../../test/render'
import type { AuditEntry } from './api'
import { auditDay, auditTime, describeAudit, quoted } from './auditText'

const path = `/my-organization/${SLUG}/audit`
const api = `/api/organizations/${SLUG}/audit`
const me = { ...ann, id: members[0].user_id, name: 'Анна Смирнова' }
const anna = { id: members[0].user_id, name: 'Анна Смирнова' }
const boris = { id: members[1].user_id, name: 'Борис Кадров' }
const VACANCY = '88888888-8888-4888-8888-888888888888'
const APPLICATION = '99999999-9999-4999-8999-999999999999'

let seq = 100
const entry = (over: Partial<AuditEntry>): AuditEntry => ({
  id: ++seq, at: '2026-10-08T11:32:00Z', action: 'vacancy.published', actor: anna, subject: null,
  target: { kind: '', id: null, label: '' }, details: {}, ...over,
})

const asOwner = (extra: Record<string, Route> = {}) => ({ ...signedInAs(me), ...orgRoutes(viewers.owner), ...extra })
const page = (items: AuditEntry[], next: number | null = null) => reply(200, { items, next })

describe('audit log page', () => {
  it('adds the tab for the owner only', async () => {
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page([]) }))
    renderApp(`/my-organization/${SLUG}`)
    const tabs = within(await screen.findByRole('navigation', { name: 'Разделы управления' }))
    expect(tabs.getAllByRole('link').map((a) => a.textContent)).toEqual(['Данные', 'Подразделения', 'Сотрудники', 'Журнал'])
  })

  it('hides the tab from other staff and explains when they open the address', async () => {
    stubApi({ ...signedInAs(me), ...orgRoutes(viewers.hr) })
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 2, name: 'Здесь нет прав' })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: 'Данные' })).toHaveLength(2) // вкладка и выход из тупика
    expect(within(screen.getByRole('navigation', { name: 'Разделы управления' })).queryByRole('link', { name: 'Журнал' })).not.toBeInTheDocument()
  })

  it('lists entries by day with the time in Moscow, the object, the person and who did it', async () => {
    const items = [
      entry({ action: 'vacancy.published', target: { kind: 'vacancy', id: VACANCY, label: 'Старший научный сотрудник' }, at: '2026-10-08T11:32:00Z' }),
      entry({ action: 'member.role_changed', actor: anna, subject: boris, details: { from: 'hr', to: 'unit_head' }, at: '2026-10-08T09:05:00Z' }),
      entry({ action: 'application.file_downloaded', actor: boris, subject: { id: 'x', name: 'Иван Петров' }, details: { kind: 'cv' },
        target: { kind: 'application', id: APPLICATION, label: 'Старший научный сотрудник' }, at: '2026-10-07T21:30:00Z' }),
    ]
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page(items) }))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 3, name: '8 октября 2026' })).toBeInTheDocument()
    // 21:30 UTC 7 октября — уже 8 октября, 00:30 по Москве.
    const day8 = within(screen.getByRole('region', { name: '8 октября 2026' }))
    expect(day8.getAllByRole('listitem')).toHaveLength(3)
    expect(screen.queryByRole('region', { name: '7 октября 2026' })).not.toBeInTheDocument()
    const rows = day8.getAllByRole('listitem')

    expect(within(rows[0]).getByText('14:32')).toBeInTheDocument()
    expect(within(rows[0]).getByText('Вакансия опубликована')).toBeInTheDocument()
    expect(within(rows[0]).getByRole('link', { name: '«Старший научный сотрудник»' })).toHaveAttribute('href', `/vacancies/${VACANCY}`)
    expect(within(rows[0]).getByText(/Анна Смирнова/)).toBeInTheDocument()
    expect(within(rows[0]).getByText('это вы')).toBeInTheDocument()

    expect(within(rows[1]).getByText('Изменена роль сотрудника')).toBeInTheDocument()
    expect(within(rows[1]).getByText('Сотрудник: Борис Кадров · Кадровик → Руководитель подразделения')).toBeInTheDocument()

    expect(within(rows[2]).getByText('Скачан файл отклика')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Кандидат: Иван Петров · Резюме')).toBeInTheDocument()
    expect(within(rows[2]).getByRole('link', { name: '«Старший научный сотрудник»' })).toHaveAttribute('href', `/candidates/${APPLICATION}`)
    expect(within(rows[2]).queryByText('это вы')).not.toBeInTheDocument()
    expect(within(rows[2]).getByText('00:30')).toBeInTheDocument()
  })

  it('says how many applications went into an exported file', async () => {
    const items = [
      entry({ id: 9, action: 'vacancy.applications_exported', details: { rows: '3', scope: 'vacancy' }, target: { kind: 'vacancy', id: VACANCY, label: 'Старший научный сотрудник' } }),
      entry({ id: 8, action: 'vacancy.applications_exported', details: { rows: '12', scope: 'list' }, target: null }),
      entry({ id: 7, action: 'vacancy.applications_exported', details: {}, target: null }),
    ]
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page(items) }))
    renderApp(path)
    expect(await screen.findAllByText('Отклики выгружены в файл')).toHaveLength(3)
    expect(screen.getByText('3 отклика')).toBeInTheDocument()
    expect(screen.getByText('12 откликов')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '«Старший научный сотрудник»' })).toHaveAttribute('href', `/vacancies/${VACANCY}`)
  })

  it('starts a new day heading when the day changes', async () => {
    const items = [
      entry({ at: '2026-10-08T11:00:00Z' }),
      entry({ at: '2026-10-07T11:00:00Z', action: 'vacancy.closed' }),
    ]
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page(items) }))
    renderApp(path)
    await screen.findByRole('heading', { level: 3, name: '8 октября 2026' })
    expect(screen.getByRole('heading', { level: 3, name: '7 октября 2026' })).toBeInTheDocument()
  })

  it('says what is recorded when the log is empty', async () => {
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page([]) }))
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 3, name: 'В журнале пока пусто' })).toBeInTheDocument()
    expect(screen.getByText(/Записи появятся, когда сотрудники начнут работать/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Показать ещё' })).not.toBeInTheDocument()
  })

  it('shows loading, then an error with retry', async () => {
    let n = 0
    stubApi(asOwner({ [`GET ${api}?limit=30`]: () => (n++ === 0 ? reply(500) : page([entry({})])) }))
    renderApp(path)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить журнал')
    await press('Проверить ещё раз')
    expect(await screen.findByText('Вакансия опубликована')).toBeInTheDocument()
  })

  it('loads the next page from the cursor and keeps the earlier entries', async () => {
    const first = Array.from({ length: 3 }, (_, i) => entry({ id: 50 - i, action: 'vacancy.closed', target: { kind: 'vacancy', id: VACANCY, label: `Вакансия ${i + 1}` } }))
    const second = [entry({ id: 47, action: 'vacancy.archived', target: { kind: 'vacancy', id: VACANCY, label: 'Старая вакансия' } })]
    const { calls } = stubApi(asOwner({
      [`GET ${api}?limit=30`]: page(first, 48),
      [`GET ${api}?limit=30&before=48`]: page(second, null),
    }))
    renderApp(path)
    expect(await screen.findByText('«Вакансия 1»')).toBeInTheDocument()
    await press('Показать ещё')
    expect(await screen.findByText('Вакансия перенесена в архив')).toBeInTheDocument()
    expect(screen.getByText('«Вакансия 3»')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Показать ещё' })).not.toBeInTheDocument()
    expect(calls.filter((c) => c.path.startsWith(api))).toHaveLength(2)
  })

  it('keeps the entries and offers a retry when the next page fails', async () => {
    let fail = true
    stubApi(asOwner({
      [`GET ${api}?limit=30`]: page([entry({ id: 9, target: { kind: 'vacancy', id: VACANCY, label: 'Первая' } })], 9),
      [`GET ${api}?limit=30&before=9`]: () => (fail ? reply(500) : page([entry({ id: 8, action: 'vacancy.closed' })])),
    }))
    renderApp(path)
    await screen.findByText('«Первая»')
    await press('Показать ещё')
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить следующие записи')
    expect(screen.getByText('«Первая»')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByText('Вакансия закрыта')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })

  it('survives odd data: an unknown action, a deleted account, no actor, a long title and a stranger object', async () => {
    const long = 'Очень длинное название вакансии '.repeat(8).trim()
    const items = [
      entry({ action: 'robot.arrived', actor: null }),
      entry({ action: 'member.left', actor: { id: 'z', name: 'Аккаунт удалён' }, subject: { id: 'z', name: 'Аккаунт удалён' } }),
      entry({ action: 'vacancy.reopened', target: { kind: 'vacancy', id: null, label: long } }),
      entry({ action: 'application.invited', details: { kind: 'new_kind' }, subject: boris, target: { kind: 'application', id: APPLICATION, label: 'Вакансия' } }),
    ]
    stubApi(asOwner({ [`GET ${api}?limit=30`]: page(items) }))
    renderApp(path)
    expect(await screen.findByText('Действие «robot.arrived»')).toBeInTheDocument()
    expect(screen.getByText('Система')).toBeInTheDocument()
    expect(screen.getAllByText('Аккаунт удалён').length).toBeGreaterThan(0)
    // Объект без страницы показывается текстом, а не ссылкой.
    expect(screen.getByText(`«${long}»`).closest('a')).toBeNull()
    expect(screen.getByText('Кандидат: Борис Кадров · new_kind')).toBeInTheDocument()
  })

  it('shows a refusal of the server as the error state', async () => {
    stubApi(asOwner({ [`GET ${api}?limit=30`]: apiError(403, 'forbidden', 'У вас нет прав на это действие') }))
    renderApp(path)
    expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить журнал')
    expect(screen.getByRole('alert')).toHaveTextContent('У вас нет прав на это действие')
  })
})

describe('describeAudit', () => {
  const slug = 'inst'
  it('names the cleared unit head differently from the assigned one', () => {
    const base = entry({ action: 'unit.head_changed', target: { kind: 'unit', id: lab.id, label: lab.name } })
    expect(describeAudit({ ...base, subject: boris }, slug)).toMatchObject({ what: 'Назначен руководитель подразделения', subject: 'Руководитель: Борис Кадров', target: { to: `/organizations/${slug}/units/${lab.id}` } })
    expect(describeAudit(base, slug)).toMatchObject({ what: 'Руководитель подразделения снят', subject: null })
  })

  it('links a sent offer to the list of sent offers and builds an invitation detail', () => {
    expect(describeAudit(entry({ action: 'offer.sent', target: { kind: 'offer', id: VACANCY, label: 'Вакансия' } }), slug).target?.to).toBe('/sent-offers')
    // Приглашённый с анонимным профилем: имени нет, его место занимает «Анонимный соискатель» (D-199)
    const hidden = { id: '00000000-0000-0000-0000-000000000000', name: '', anonymous: true }
    expect(describeAudit(entry({ action: 'offer.sent', subject: hidden, target: { kind: 'offer', id: VACANCY, label: 'Вакансия' } }), slug).subject).toBe('Соискатель: Анонимный соискатель')
    expect(describeAudit(entry({ action: 'invitation.sent', details: { role: 'hr', email: 'a***@msu.ru' } }), slug).detail).toBe('Кадровик, a***@msu.ru')
    expect(describeAudit(entry({ action: 'invitation.sent', details: { role: 'hr' } }), slug).detail).toBe('Кадровик')
    expect(describeAudit(entry({ action: 'invitation.sent', details: {} }), slug).detail).toBeNull()
    expect(describeAudit(entry({ action: 'member.role_changed', details: { from: 'hr' } }), slug).detail).toBeNull()
    expect(describeAudit(entry({ action: 'member.joined', details: { role: 'weird' } }), slug).detail).toBe('weird')
  })

  it('turns the guillemets inside a title into low quotation marks', () => {
    expect(quoted('Руководитель ЦКП «Электронная микроскопия»')).toBe('«Руководитель ЦКП „Электронная микроскопия“»')
    expect(quoted('Простое название')).toBe('«Простое название»')
  })

  it('formats a broken time as empty', () => {
    expect(auditDay('not a date')).toBe('')
    expect(auditTime('not a date')).toBe('')
    expect(auditTime('2026-10-08T21:00:00Z')).toBe('00:00')
  })
})

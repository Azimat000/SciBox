import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { ann, apiError, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { renderApp } from '../../test/render'

beforeEach(() => window.localStorage.clear())

const section = async () => within(await screen.findByRole('region', { name: 'Письма' }))
const toast = (text: string) => screen.findByText(text, { selector: '.toast *' })

describe('mail settings in the account', () => {
  const setup = (settings: { email_new_vacancies: boolean; email_deadlines: boolean }, extra: Record<string, Route> = {}) =>
    stubApi({ ...signedInAs(ann), 'GET /api/notification-settings': reply(200, settings), ...extra })

  it('shows what the person gets and what cannot be switched off', async () => {
    setup({ email_new_vacancies: true, email_deadlines: false })
    renderApp('/account')
    const mail = await section()
    await waitFor(() => expect(mail.getByRole('checkbox', { name: /Новые вакансии по сохранённым поискам/ })).toBeChecked())
    expect(mail.getByRole('checkbox', { name: /Напоминания о сроке подачи/ })).not.toBeChecked()
    expect(mail.getByText('Как часто писать, вы выбираете у каждого поиска.')).toBeInTheDocument()
    expect(mail.getByText(/Письма об откликах, приглашениях и решениях отключить нельзя/)).toBeInTheDocument()
  })

  it('saves a switch at once, sending both values, and thanks', async () => {
    const user = userEvent.setup()
    const { calls } = setup(
      { email_new_vacancies: true, email_deadlines: true },
      { 'PUT /api/notification-settings': (call) => reply(200, call.body) },
    )
    renderApp('/account')
    const mail = await section()
    const box = await waitFor(() => {
      const el = mail.getByRole('checkbox', { name: /Напоминания о сроке подачи/ })
      expect(el).toBeChecked()
      return el
    })
    await user.click(box)
    expect(await toast('Настройки писем сохранены')).toBeInTheDocument()
    expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ email_new_vacancies: true, email_deadlines: false })
    expect(mail.getByRole('checkbox', { name: /Напоминания о сроке подачи/ })).not.toBeChecked()
    await user.click(mail.getByRole('checkbox', { name: /Новые вакансии по сохранённым поискам/ }))
    await waitFor(() => expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(2))
    expect(calls.filter((c) => c.method === 'PUT')[1].body).toEqual({ email_new_vacancies: false, email_deadlines: false })
  })

  it('shows the chosen value while saving and goes back with a message when the server refuses', async () => {
    const user = userEvent.setup()
    setup({ email_new_vacancies: true, email_deadlines: true }, { 'PUT /api/notification-settings': apiError(500, 'internal', 'Сбой на сервере') })
    renderApp('/account')
    const mail = await section()
    const box = await waitFor(() => {
      const el = mail.getByRole('checkbox', { name: /Новые вакансии по сохранённым поискам/ })
      expect(el).toBeChecked()
      return el
    })
    await user.click(box)
    expect(await toast('Не удалось сохранить настройки')).toBeInTheDocument()
    expect(screen.getByText('Сбой на сервере', { selector: '.toast *' })).toBeInTheDocument()
    await waitFor(() => expect(mail.getByRole('checkbox', { name: /Новые вакансии по сохранённым поискам/ })).toBeChecked())
  })

  it('holds the place while loading', async () => {
    stubApi({ ...signedInAs(ann), 'GET /api/notification-settings': () => new Promise(() => {}) as never })
    renderApp('/account')
    const mail = await section()
    expect(mail.getByRole('status', { name: 'Загрузка' })).toBeInTheDocument()
    expect(mail.queryByRole('checkbox')).not.toBeInTheDocument()
  })

  it('says so when the settings do not load, and retries', async () => {
    const user = userEvent.setup()
    let fail = true
    setup({ email_new_vacancies: true, email_deadlines: true }, {
      'GET /api/notification-settings': () => (fail ? apiError(500, 'internal', 'Сбой') : reply(200, { email_new_vacancies: true, email_deadlines: true })),
    })
    renderApp('/account')
    const mail = await section()
    expect(await mail.findByRole('alert')).toHaveTextContent('Не удалось загрузить настройки писем')
    fail = false
    await user.click(mail.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await mail.findByRole('checkbox', { name: /Новые вакансии по сохранённым поискам/ })).toBeChecked()
  })
})

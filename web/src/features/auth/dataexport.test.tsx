import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiError, fileReply, reply, securityApi, stubApi, type Route } from '../../test/api'
import { renderApp } from '../../test/render'
import { waitLabel } from './labels'

const reauthNeeded = apiError(403, 'reauth_required', 'Для этого действия введите пароль ещё раз')

async function openSection(routes: Record<string, Route> = {}) {
  const api = stubApi({ ...securityApi(), ...routes })
  renderApp('/account/security')
  const heading = await screen.findByRole('heading', { level: 2, name: 'Ваши данные' })
  return { api, section: heading.closest('section') as HTMLElement }
}

describe('download of own data', () => {
  let saved: { name: string; href: string }[]
  beforeEach(() => {
    saved = []
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:test-archive', revokeObjectURL: vi.fn() }))
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push({ name: this.download, href: this.href })
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it('explains what is inside, what is not, and the limits', async () => {
    const { section } = await openSection()
    expect(within(section).getByText(/Один архив со всем, что сервис хранит о вас/)).toBeInTheDocument()
    expect(within(section).getByText(/Рекомендательные письма о вас в архив не входят/)).toBeInTheDocument()
    expect(within(section).getByRole('button', { name: 'Скачать архив' })).toBeEnabled()
  })

  it('save the archive under the name the server gave and say so', async () => {
    const { api, section } = await openSection({ 'GET /api/account/export': fileReply('scibox-data-2026-10-09.zip', 'PK') })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    expect(await screen.findByText('Архив скачан')).toBeInTheDocument()
    expect(saved).toEqual([{ name: 'scibox-data-2026-10-09.zip', href: 'blob:test-archive' }])
    expect(api.called('GET', '/api/account/export')).toHaveLength(1)
  })

  it('use a plain name when the server gives none', async () => {
    const { section } = await openSection({
      'GET /api/account/export': () => ({ status: 200, file: { name: '', content: 'PK' } }),
    })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]?.name).toBe('scibox-data.zip')
  })

  it('ask for the password once, then download', async () => {
    let asked = false
    const { api, section } = await openSection({
      'GET /api/account/export': () => {
        if (asked) return fileReply('scibox-data-2026-10-09.zip', 'PK')
        asked = true
        return reauthNeeded
      },
      'POST /api/account/reauth': reply(204),
    })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    const dialog = await screen.findByRole('dialog', { name: 'Подтвердите, что это вы' })
    await userEvent.type(within(dialog).getByLabelText(/^Пароль/), 'correct horse battery')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Подтвердить' }))
    expect(await screen.findByText('Архив скачан')).toBeInTheDocument()
    expect(api.called('GET', '/api/account/export')).toHaveLength(2)
    expect(saved).toHaveLength(1)
  })

  it('tell when the daily limit is reached and when to come back', async () => {
    const { section } = await openSection({
      'GET /api/account/export': apiError(429, 'rate_limited', 'Слишком много', { retry_after: 3 * 3600 + 5 }),
    })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    expect(await within(section).findByText('Три архива за сутки уже скачаны. Следующий можно получить через 4 ч.')).toBeInTheDocument()
    expect(saved).toHaveLength(0)
  })

  it('fall back to the general text when the limit has no wait time', async () => {
    const { section } = await openSection({ 'GET /api/account/export': apiError(429, 'rate_limited', 'Подождите немного') })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    expect(await within(section).findByText('Слишком много попыток. Подождите немного и попробуйте снова')).toBeInTheDocument()
  })

  it('name the problem and let the person try again', async () => {
    let fail = true
    const { section } = await openSection({
      'GET /api/account/export': () => {
        if (!fail) return fileReply('scibox-data-2026-10-09.zip', 'PK')
        fail = false
        return apiError(500, 'internal', 'Что-то сломалось на сервере. Попробуйте ещё раз')
      },
    })
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    expect(await within(section).findByText('Не удалось собрать архив')).toBeInTheDocument()
    expect(within(section).getByText('Что-то сломалось на сервере. Попробуйте ещё раз')).toBeInTheDocument()
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    await waitFor(() => expect(saved).toHaveLength(1))
  })

  it('say the server is unreachable when it does not answer', async () => {
    const { api, section } = await openSection()
    api.fetch.mockRejectedValueOnce(new TypeError('network'))
    await userEvent.click(within(section).getByRole('button', { name: 'Скачать архив' }))
    expect(await within(section).findByText('Не удалось собрать архив')).toBeInTheDocument()
  })
})

describe('waitLabel', () => {
  it.each([
    [1, '1 мин'],
    [59, '1 мин'],
    [61, '2 мин'],
    [3599, '1 ч'],
    [3600, '1 ч'],
    [3601, '2 ч'],
    [21 * 3600, '21 ч'],
    [0, '1 мин'],
  ])('%i seconds -> %s', (seconds, label) => {
    expect(waitLabel(seconds)).toBe(label)
  })
})

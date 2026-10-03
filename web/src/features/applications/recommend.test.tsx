import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { apiError, reply, stubApi, type Route } from '../../test/api'
import { fill, press } from '../../test/forms'
import { renderApp } from '../../test/render'
import type { RecommendInfo } from './api'

const TOKEN = 'abc_DEF-123'
const path = `/recommend?token=${TOKEN}`
const info: RecommendInfo = {
  referee_name: 'Андрей Козлов',
  relation: 'научный руководитель',
  applicant_name: 'Анна Смирнова',
  vacancy_title: 'Старший научный сотрудник',
  org_name: 'Сибирский институт',
  status: 'pending',
  expires_at: '2026-11-02T09:00:00Z',
}
const lookup = (i: RecommendInfo = info): Record<string, Route> => ({ 'POST /api/recommendations/lookup': reply(200, { request: i }) })
const fileInput = () => document.querySelector<HTMLInputElement>('input[type="file"]')!
const pdf = (name = 'Письмо.pdf', size = 1000) => new File([new Uint8Array(size)], name, { type: 'application/pdf' })
const ready = () => screen.findByLabelText(/^Письмо/)

describe('recommendation page', () => {
  it('asks the referee for a letter without any sign-in', async () => {
    const { calls } = stubApi(lookup())
    renderApp(path)
    expect(await screen.findByRole('heading', { level: 1, name: 'Анна Смирнова просит рекомендательное письмо' })).toBeInTheDocument()
    expect(screen.getByText('Позиция «Старший научный сотрудник», Сибирский институт.')).toBeInTheDocument()
    expect(screen.getByText('Андрей Козлов, вы указаны как «научный руководитель».')).toBeInTheDocument()
    expect(screen.getByText(/Регистрироваться не нужно\. Письмо увидит только организация/)).toBeInTheDocument()
    expect(calls.find((c) => c.path === '/api/recommendations/lookup')!.body).toEqual({ token: TOKEN })
  })

  it('addresses a referee without a stated relation more simply', async () => {
    stubApi(lookup({ ...info, relation: '' }))
    renderApp(path)
    expect(await screen.findByText('Андрей Козлов, вас указали в качестве рекомендателя.')).toBeInTheDocument()
  })

  it('needs the link from the mail', () => {
    const { calls } = stubApi()
    renderApp('/recommend')
    expect(screen.getByRole('heading', { name: 'В адресе нет ссылки' })).toBeInTheDocument()
    expect(calls.filter((c) => c.path.startsWith('/api/recommendations'))).toHaveLength(0)
  })

  it.each([
    [404, 'invalid_link', 'Ссылка недействительна. Возможно, вам прислали новую: откройте последнее письмо'],
    [410, 'link_expired', 'Срок ссылки вышел. Попросите кандидата отправить просьбу ещё раз'],
    [410, 'application_withdrawn', 'Кандидат отозвал отклик, поэтому письмо уже не нужно. Спасибо'],
  ])('explains a dead link (%i %s)', async (status, code, message) => {
    stubApi({ 'POST /api/recommendations/lookup': apiError(status, code, message) })
    renderApp(path)
    expect(await screen.findByText(message)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отправить письмо' })).not.toBeInTheDocument()
  })

  it('offers to retry when the server does not answer', async () => {
    let down = true
    stubApi({ 'POST /api/recommendations/lookup': () => (down ? reply(500) : reply(200, { request: info })) })
    renderApp(path)
    expect(await screen.findByText('Не удалось открыть ссылку')).toBeInTheDocument()
    down = false
    await userEvent.click(screen.getByRole('button', { name: 'Проверить ещё раз' }))
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent('Анна Смирнова просит рекомендательное письмо')
  })

  it('sends a typed letter and thanks the referee', async () => {
    const { calls } = stubApi({ ...lookup(), 'POST /api/recommendations/submit': reply(200, { status: 'received' }) })
    renderApp(path)
    await ready()
    await fill(/^Письмо/, 'Рекомендую без оговорок.')
    await press('Отправить письмо')
    expect(await screen.findByRole('heading', { name: 'Спасибо, письмо отправлено' })).toBeInTheDocument()
    expect(screen.getByText('Организация его получила. Это окно можно закрыть.')).toBeInTheDocument()
    expect(calls.find((c) => c.path === '/api/recommendations/submit')!.body).toEqual({ token: TOKEN, text: 'Рекомендую без оговорок.', __files: [] })
  })

  it('sends a PDF without any text', async () => {
    const { calls } = stubApi({ ...lookup(), 'POST /api/recommendations/submit': reply(200, { status: 'received' }) })
    renderApp(path)
    await ready()
    await userEvent.upload(fileInput(), pdf('Письмо Козлова.pdf'))
    expect(screen.getByText('Письмо Козлова.pdf')).toBeInTheDocument()
    await press('Отправить письмо')
    expect(await screen.findByRole('heading', { name: 'Спасибо, письмо отправлено' })).toBeInTheDocument()
    expect(calls.find((c) => c.path === '/api/recommendations/submit')!.body).toEqual({ token: TOKEN, text: '', __files: ['Письмо Козлова.pdf'] })
  })

  it('does not send an empty letter', async () => {
    const { calls } = stubApi(lookup())
    renderApp(path)
    await ready()
    await press('Отправить письмо')
    expect(await screen.findByText('Напишите несколько абзацев или приложите PDF. Можно и то и другое.', { selector: '.field-message' })).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST' && c.path.endsWith('/submit'))).toHaveLength(0)
  })

  it('refuses a file that is not a PDF or is too big', async () => {
    stubApi(lookup())
    renderApp(path)
    await ready()
    await userEvent.upload(fileInput(), new File(['x'], 'фото.png', { type: 'image/png' }), { applyAccept: false })
    expect(screen.getByRole('alert')).toHaveTextContent('Нужен файл PDF')
    await userEvent.upload(fileInput(), pdf('Огромный.pdf', 11 * 1024 * 1024))
    expect(screen.getByRole('alert')).toHaveTextContent('Файл больше 10 МБ')
    await userEvent.upload(fileInput(), pdf('Нормальный.pdf'))
    await userEvent.click(screen.getByRole('button', { name: 'Убрать файл' }))
    expect(screen.queryByText('Нормальный.pdf')).not.toBeInTheDocument()
  })

  it('shows what the server says about the letter and a general failure', async () => {
    let answer = apiError(422, 'validation_failed', 'Проверьте поля формы', { fields: { text: 'Письмо длиннее 12000 знаков. Приложите его PDF-файлом', file: 'Файл не похож на PDF' } })
    stubApi({ ...lookup(), 'POST /api/recommendations/submit': () => answer })
    renderApp(path)
    await ready()
    await fill(/^Письмо/, 'Рекомендую.')
    await press('Отправить письмо')
    expect(await screen.findByText('Письмо длиннее 12000 знаков. Приложите его PDF-файлом')).toBeInTheDocument()
    expect(screen.getByText('Файл не похож на PDF')).toBeInTheDocument()
    answer = apiError(409, 'already_answered', 'На эту ссылку уже ответили')
    await press('Отправить письмо')
    expect(await screen.findByText('Не удалось отправить письмо')).toBeInTheDocument()
    expect(screen.getByText('На эту ссылку уже ответили')).toBeInTheDocument()
  })

  it('declines after confirmation', async () => {
    const { calls } = stubApi({ ...lookup(), 'POST /api/recommendations/decline': reply(200, { status: 'declined' }) })
    renderApp(path)
    await ready()
    await press('Отказаться')
    const dialog = screen.getByRole('dialog', { name: 'Отказаться от письма?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Отмена' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    await press('Отказаться')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отказаться' }))
    expect(await screen.findByRole('heading', { name: 'Вы отказались от письма' })).toBeInTheDocument()
    expect(calls.find((c) => c.path === '/api/recommendations/decline')!.body).toEqual({ token: TOKEN })
  })

  it('reports a failed decline and keeps the form', async () => {
    stubApi({ ...lookup(), 'POST /api/recommendations/decline': apiError(409, 'already_answered', 'На эту ссылку уже ответили') })
    renderApp(path)
    await ready()
    await press('Отказаться')
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отказаться' }))
    expect(await screen.findByText('На эту ссылку уже ответили')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Отправить письмо' })).toBeInTheDocument()
  })

  it.each([
    ['received', 'Письмо уже получено', 'Вы уже ответили по этой ссылке, спасибо. Второй раз она не сработает.'],
    ['declined', 'Вы уже отказались', 'Ссылка сработала один раз. Если передумали, попросите кандидата отправить новую.'],
  ] as const)('shows a thank-you instead of the form when the link was already used (%s)', async (status, title, text) => {
    stubApi(lookup({ ...info, status }))
    renderApp(path)
    expect(await screen.findByRole('heading', { name: title })).toBeInTheDocument()
    expect(screen.getByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Отправить письмо' })).not.toBeInTheDocument()
  })
})

import { screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ann, reply, signedInAs, stubApi, type Route } from '../../test/api'
import { application, candidate, candidateList, counts, list, staffApplication, summary } from '../../test/applications'
import { chatRefs, unreadCounts } from '../../test/chat'
import { mineList, offer, OFFER_ID, sentList, sentOffer } from '../../test/offers'
import { renderApp } from '../../test/render'
import { ROLE_STORAGE_KEY } from '../shell/role-context'

// Кнопки входа в переписку (срез 34): «Написать», пока людей в переписке не было, иначе «Перейти к переписке» с числом новых;
// без переписки кнопки нет. Везде, где перечислено в CHAT.md «Экраны».

const signedIn = (extra: Record<string, Route> = {}): Record<string, Route> => ({
  ...signedInAs(ann),
  'GET /api/notifications/unread-count': reply(200, { unread: 0 }),
  'GET /api/chats/unread-count': reply(200, unreadCounts.none()),
  ...extra,
})

afterEach(() => window.localStorage.clear())

const open = chatRefs.openWithUnread()
const fresh = chatRefs.notStarted()
const closed = chatRefs.closed()

describe('chat entry buttons', () => {
  it('my applications: open with unread, write, closed and none', async () => {
    const items = [
      { ...summary, id: 'a1', chat: open },
      { ...summary, id: 'a2', chat: fresh },
      { ...summary, id: 'a3', chat: closed },
      { ...summary, id: 'a4', chat: null },
      { ...summary, id: 'a5' },
    ]
    stubApi(signedIn({ 'GET /api/applications?*': reply(200, list(items)) }))
    renderApp('/applications')
    const going = await screen.findByRole('link', { name: 'Перейти к переписке, новых: 1' })
    expect(going).toHaveAttribute('href', `/chats/${open.id}`)
    expect(within(going).getByText('1')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Написать' })).toHaveAttribute('href', `/chats/${fresh.id}`)
    expect(screen.getByRole('link', { name: 'Перейти к переписке' })).toHaveAttribute('href', `/chats/${closed.id}`)
    expect(screen.getAllByRole('link', { name: /Написать|Перейти к переписке/ })).toHaveLength(3)
  })

  it('application page of the scientist', async () => {
    stubApi(signedIn({ [`GET /api/applications/${application.id}`]: reply(200, { application: { ...application, chat: fresh } }) }))
    renderApp(`/applications/${application.id}`)
    expect(await screen.findByRole('link', { name: 'Написать' })).toHaveAttribute('href', `/chats/${fresh.id}`)
  })

  it('application page without a chat has no button', async () => {
    stubApi(signedIn({ [`GET /api/applications/${application.id}`]: reply(200, { application }) }))
    renderApp(`/applications/${application.id}`)
    await screen.findByRole('heading', { level: 1 })
    expect(screen.queryByRole('link', { name: /Написать|Перейти к переписке/ })).not.toBeInTheDocument()
  })

  it('candidate card and candidates list of the organization', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    stubApi(
      signedIn({
        [`GET /api/applications/${staffApplication.id}`]: reply(200, { application: { ...staffApplication, chat: open } }),
        'GET /api/my/candidates?*': reply(200, candidateList([{ ...candidate, chat: closed }], { counts: counts({ sent: 1 }) })),
        'GET /api/my/candidate-vacancies': reply(200, { items: [] }),
      }),
    )
    const { router } = renderApp(`/candidates/${staffApplication.id}`)
    const aside = (await screen.findByRole('heading', { name: 'Решение по отклику' })).closest('aside') as HTMLElement
    expect(within(aside).getByRole('link', { name: 'Перейти к переписке, новых: 1' })).toHaveAttribute('href', `/chats/${open.id}`)
    await router.navigate('/candidates')
    expect(await screen.findByRole('link', { name: 'Перейти к переписке' })).toHaveAttribute('href', `/chats/${closed.id}`)
  })

  it('offers of the scientist: list and offer page', async () => {
    stubApi(
      signedIn({
        'GET /api/offers?*': reply(200, mineList([{ ...offer, chat: fresh }])),
        [`GET /api/offers/${OFFER_ID}`]: reply(200, { offer: { ...offer, chat: open } }),
      }),
    )
    const { router } = renderApp('/offers')
    expect(await screen.findByRole('link', { name: 'Написать' })).toHaveAttribute('href', `/chats/${fresh.id}`)
    await router.navigate(`/offers/${OFFER_ID}`)
    expect(await screen.findByRole('link', { name: 'Перейти к переписке, новых: 1' })).toHaveAttribute('href', `/chats/${open.id}`)
  })

  it('sent offers of the organization', async () => {
    window.localStorage.setItem(ROLE_STORAGE_KEY, 'employer')
    stubApi(signedIn({ 'GET /api/my/sent-offers?*': reply(200, sentList([{ ...sentOffer, chat: open }])) }))
    renderApp('/sent-offers')
    expect(await screen.findByRole('link', { name: 'Перейти к переписке, новых: 1' })).toHaveAttribute('href', `/chats/${open.id}`)
  })
})

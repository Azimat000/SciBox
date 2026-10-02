import { render } from '@testing-library/react'
import { createMemoryRouter, type RouteObject } from 'react-router'
import { App } from '../app/App'
import { createQueryClient } from '../app/queryClient'
import { routes as appRoutes } from '../app/routes'

/** Рисует приложение на нужном адресе со свежим кешем запросов. */
export function renderApp(path = '/', routes: RouteObject[] = appRoutes) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const queryClient = createQueryClient()
  return { router, ...render(<App router={router} queryClient={queryClient} />) }
}

/** Ответ сервера с JSON-телом. */
export function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

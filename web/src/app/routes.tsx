import type { RouteObject } from 'react-router'
import { NotFoundPage } from '../features/status/NotFoundPage'
import { StatusPage } from '../features/status/StatusPage'
import { CrashPage } from './CrashPage'
import { Layout } from './Layout'

export const routes: RouteObject[] = [
  {
    element: <Layout />,
    errorElement: <CrashPage />,
    children: [
      { index: true, element: <StatusPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

import type { RouteObject } from 'react-router'
import { ComingSoonPage } from '../features/shell/ComingSoonPage'
import { comingSoonPaths } from '../features/shell/nav'
import { NotFoundPage } from '../features/status/NotFoundPage'
import { StatusPage } from '../features/status/StatusPage'
import { StyleguidePage } from '../features/styleguide/StyleguidePage'
import { CrashPage } from './CrashPage'
import { Layout } from './Layout'

export const routes: RouteObject[] = [
  {
    element: <Layout />,
    errorElement: <CrashPage />,
    children: [
      { index: true, element: <StatusPage /> },
      { path: 'styleguide', element: <StyleguidePage /> },
      ...comingSoonPaths.map((path) => ({ path, element: <ComingSoonPage /> })),
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

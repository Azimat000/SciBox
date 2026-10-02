import type { RouteObject } from 'react-router'
import { AccountPage } from '../features/auth/AccountPage'
import { ConfirmEmailPage } from '../features/auth/ConfirmEmailPage'
import { ForgotPasswordPage } from '../features/auth/ForgotPasswordPage'
import { LoginPage } from '../features/auth/LoginPage'
import { PrivacyPage } from '../features/auth/PrivacyPage'
import { RegisterPage } from '../features/auth/RegisterPage'
import { ResetPasswordPage } from '../features/auth/ResetPasswordPage'
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
      { path: 'login', element: <LoginPage /> },
      { path: 'register', element: <RegisterPage /> },
      { path: 'forgot-password', element: <ForgotPasswordPage /> },
      { path: 'reset-password', element: <ResetPasswordPage /> },
      { path: 'confirm-email', element: <ConfirmEmailPage /> },
      { path: 'account', element: <AccountPage /> },
      { path: 'privacy', element: <PrivacyPage /> },
      { path: 'styleguide', element: <StyleguidePage /> },
      ...comingSoonPaths.map((path) => ({ path, element: <ComingSoonPage /> })),
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

import type { RouteObject } from 'react-router'
import { AccountPage } from '../features/auth/AccountPage'
import { ConfirmEmailPage } from '../features/auth/ConfirmEmailPage'
import { ForgotPasswordPage } from '../features/auth/ForgotPasswordPage'
import { LoginPage } from '../features/auth/LoginPage'
import { PrivacyPage } from '../features/auth/PrivacyPage'
import { RegisterPage } from '../features/auth/RegisterPage'
import { ResetPasswordPage } from '../features/auth/ResetPasswordPage'
import { AcceptInvitationPage } from '../features/orgs/AcceptInvitationPage'
import { CreateOrganizationPage } from '../features/orgs/CreateOrganizationPage'
import { ManageDataPage } from '../features/orgs/ManageDataPage'
import { ManageLayout } from '../features/orgs/ManageLayout'
import { ManageMembersPage } from '../features/orgs/ManageMembersPage'
import { ManageUnitsPage } from '../features/orgs/ManageUnitsPage'
import { MyOrganizationsPage } from '../features/orgs/MyOrganizationsPage'
import { OrganizationPage } from '../features/orgs/OrganizationPage'
import { OrganizationsPage } from '../features/orgs/OrganizationsPage'
import { UnitEditPage } from '../features/orgs/UnitEditPage'
import { UnitPage } from '../features/orgs/UnitPage'
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
      { path: 'organizations', element: <OrganizationsPage /> },
      { path: 'organizations/new', element: <CreateOrganizationPage /> },
      { path: 'organizations/:slug', element: <OrganizationPage /> },
      { path: 'organizations/:slug/units/:unitId', element: <UnitPage /> },
      { path: 'my-organization', element: <MyOrganizationsPage /> },
      {
        path: 'my-organization/:slug',
        element: <ManageLayout />,
        children: [
          { index: true, element: <ManageDataPage /> },
          { path: 'units', element: <ManageUnitsPage /> },
          { path: 'units/new', element: <UnitEditPage /> },
          { path: 'units/:unitId', element: <UnitEditPage /> },
          { path: 'members', element: <ManageMembersPage /> },
        ],
      },
      { path: 'invitations/accept', element: <AcceptInvitationPage /> },
      { path: 'styleguide', element: <StyleguidePage /> },
      ...comingSoonPaths.map((path) => ({ path, element: <ComingSoonPage /> })),
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

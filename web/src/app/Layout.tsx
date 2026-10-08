import { Outlet } from 'react-router'
import { DeletionBanner } from '../features/auth/DeletionBanner'
import { ReauthProvider } from '../features/auth/ReauthProvider'
import { RoleProvider } from '../features/shell/RoleProvider'
import { SiteFooter } from '../features/shell/SiteFooter'
import { SiteHeader } from '../features/shell/SiteHeader'
import { t } from '../i18n'
import { ToastProvider } from '../ui/ToastProvider'
import { useRouteTitle } from './pageTitle'

export function Layout() {
  useRouteTitle()
  return (
    <RoleProvider>
      <ToastProvider>
        <ReauthProvider>
          <a className="skip-link" href="#main">
            {t.shell.skipToContent}
          </a>
          <SiteHeader />
          <DeletionBanner />
          <main id="main" tabIndex={-1}>
            <Outlet />
          </main>
          <SiteFooter />
        </ReauthProvider>
      </ToastProvider>
    </RoleProvider>
  )
}

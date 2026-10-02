import { Outlet } from 'react-router'
import { RoleProvider } from '../features/shell/RoleProvider'
import { SiteFooter } from '../features/shell/SiteFooter'
import { SiteHeader } from '../features/shell/SiteHeader'
import { t } from '../i18n'
import { ToastProvider } from '../ui/ToastProvider'

export function Layout() {
  return (
    <RoleProvider>
      <ToastProvider>
        <a className="skip-link" href="#main">
          {t.shell.skipToContent}
        </a>
        <SiteHeader />
        <main id="main" tabIndex={-1}>
          <Outlet />
        </main>
        <SiteFooter />
      </ToastProvider>
    </RoleProvider>
  )
}

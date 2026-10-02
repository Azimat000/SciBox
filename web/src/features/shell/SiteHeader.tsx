import { useEffect, useState } from 'react'
import { Link, NavLink, useLocation } from 'react-router'
import { productName } from '../../config/product'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { CloseIcon, MenuIcon } from '../../ui/icons'
import { useMe } from '../auth/api'
import { AccountBlock, UserMenu } from '../auth/UserMenu'
import { useSignOut } from '../auth/useSignOut'
import { navFor } from './nav'
import { RoleSwitch } from './RoleSwitch'
import { useRole } from './useRole'
import './SiteHeader.css'

export function SiteHeader() {
  const { role } = useRole()
  const { user, isLoading } = useMe()
  const signOut = useSignOut()
  const items = navFor(role)
  const { pathname } = useLocation()
  // Меню открыто «на этой странице»: перешли на другую, и оно закрылось само
  const [openAt, setOpenAt] = useState<string | null>(null)
  const menuOpen = openAt === pathname
  const setMenuOpen = (open: boolean) => setOpenAt(open ? pathname : null)

  useEffect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpenAt(null)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [menuOpen])

  const links = items.map((item) => (
    <NavLink key={item.to} to={item.to} className="nav-link">
      {item.label}
    </NavLink>
  ))

  return (
    <header className="site-header">
      <div className="site-header-bar">
        <Link to="/" className="wordmark">
          {productName}
        </Link>

        <nav className="nav-desktop" aria-label={t.shell.mainNav}>
          {links}
        </nav>

        <div className="header-tools">
          <div className="header-role">
            <RoleSwitch />
          </div>
          {/* Пока неизвестно, вошёл ли человек, держим место, чтобы шапка не прыгала */}
          {isLoading ? (
            <span className="header-auth-placeholder" aria-hidden="true" />
          ) : user ? (
            <div className="header-signin">
              <UserMenu user={user} />
            </div>
          ) : (
            <div className="header-auth header-signin">
              <ButtonLink to="/login" variant="secondary" size="sm">
                {t.shell.signIn}
              </ButtonLink>
              <ButtonLink to="/register" size="sm">
                {t.shell.register}
              </ButtonLink>
            </div>
          )}
          <button
            type="button"
            className="menu-button"
            aria-expanded={menuOpen}
            aria-controls="mobile-menu"
            aria-label={menuOpen ? t.common.closeMenu : t.common.openMenu}
            onClick={() => setMenuOpen(!menuOpen)}
          >
            {menuOpen ? <CloseIcon size={24} /> : <MenuIcon size={24} />}
          </button>
        </div>
      </div>

      <div id="mobile-menu" className="mobile-menu" hidden={!menuOpen}>
        <nav aria-label={t.shell.mainNav}>{links}</nav>
        <div className="mobile-menu-role">
          <RoleSwitch />
        </div>
        {user ? (
          <AccountBlock user={user} signOut={() => signOut.mutate()} signingOut={signOut.isPending} />
        ) : (
          !isLoading && (
            <div className="mobile-menu-auth">
              <ButtonLink to="/login" variant="secondary">
                {t.shell.signIn}
              </ButtonLink>
              <ButtonLink to="/register">{t.shell.register}</ButtonLink>
            </div>
          )
        )}
      </div>
    </header>
  )
}

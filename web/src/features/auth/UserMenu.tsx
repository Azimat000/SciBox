import { useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { ChevronDownIcon } from '../../ui/icons'
import type { User } from './api'
import { useSignOut } from './useSignOut'
import './UserMenu.css'

/** Имя человека в шапке; по нажатию открывается «Настройки аккаунта» и «Выйти». */
export function UserMenu({ user }: { user: User }) {
  const { pathname } = useLocation()
  // Меню открыто «на этой странице»: перешли на другую, и оно закрылось само
  const [openAt, setOpenAt] = useState<string | null>(null)
  const open = openAt === pathname
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const signOut = useSignOut()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setOpenAt(null)
      buttonRef.current?.focus()
    }
    const onPointer = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpenAt(null)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onPointer)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onPointer)
    }
  }, [open])

  return (
    <div className="user-menu" ref={rootRef}>
      <button
        type="button"
        ref={buttonRef}
        className="user-menu-button"
        aria-expanded={open}
        aria-controls="user-menu-panel"
        aria-label={`${t.shell.accountMenu}: ${user.name}`}
        onClick={() => setOpenAt(open ? null : pathname)}
      >
        <span className="user-menu-name">{user.name}</span>
        <ChevronDownIcon size={16} />
      </button>
      <div id="user-menu-panel" className="user-menu-panel" hidden={!open}>
        <AccountBlock user={user} signOut={() => signOut.mutate()} signingOut={signOut.isPending} compact />
      </div>
    </div>
  )
}

/** Имя, почта и две команды: общий кусок для выпадающего и мобильного меню. */
export function AccountBlock({
  user,
  signOut,
  signingOut,
  compact = false,
}: {
  user: User
  signOut: () => void
  signingOut: boolean
  /** В выпадающем меню на компьютере кнопка мельче; в мобильном меню она в полный рост, под палец. */
  compact?: boolean
}) {
  return (
    <div className="account-block">
      <div className="account-block-who">
        <p className="account-block-name">{user.name}</p>
        <p className="account-block-email">{user.email}</p>
      </div>
      <Link to="/profile" className="account-block-link">
        {t.profile.menuLink}
      </Link>
      <Link to="/account" className="account-block-link">
        {t.shell.accountSettings}
      </Link>
      <Button variant="secondary" size={compact ? 'sm' : 'md'} loading={signingOut} onClick={signOut}>
        {t.shell.signOut}
      </Button>
    </div>
  )
}

import type { ReactNode } from 'react'
import './auth.css'

type AuthPageProps = {
  title: string
  lead?: string
  children: ReactNode
  /** Шире обычного: для настроек и политики. */
  wide?: boolean
  /** Вкладки раздела под заголовком (настройки). */
  tabs?: ReactNode
}

/** Общая рамка страниц аккаунта: заголовок, короткое пояснение и колонка для формы. */
export function AuthPage({ title, lead, children, wide = false, tabs }: AuthPageProps) {
  return (
    <div className="page page-narrow auth-page">
      <h1>{title}</h1>
      {tabs}
      {lead && <p className="lead">{lead}</p>}
      <div className={wide ? 'auth-body auth-body-wide' : 'auth-body'}>{children}</div>
    </div>
  )
}

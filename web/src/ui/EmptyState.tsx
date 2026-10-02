import type { ReactNode } from 'react'
import './EmptyState.css'

type EmptyStateProps = {
  title: string
  text?: string
  /** 'error' — что-то сломалось и нужно сообщить сразу; 'empty' — просто пока ничего нет. */
  tone?: 'empty' | 'error'
  /** Действие, которое выводит человека из тупика. */
  action?: ReactNode
  headingLevel?: 2 | 3
}

/** Пустое место: ничего не найдено, ничего ещё нет, что-то сломалось. Всегда говорит, что делать дальше. */
export function EmptyState({ title, text, tone = 'empty', action, headingLevel = 3 }: EmptyStateProps) {
  const Heading = `h${headingLevel}` as const
  return (
    <div className="empty" data-tone={tone} role={tone === 'error' ? 'alert' : undefined}>
      <svg className="empty-art" viewBox="0 0 96 72" width="96" height="72" fill="none" aria-hidden="true" focusable="false">
        <path d="M26 6h34l12 12v48H26z" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
        <path d="M60 6v12h12" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
        {tone === 'error' ? (
          <path d="M49 28v16M49 53h.01" stroke="currentColor" strokeWidth="3.5" strokeLinecap="round" />
        ) : (
          <>
            {/* маркер проведён под строкой текста, как карандашом по странице */}
            <path d="M33 43.5h30" stroke="var(--mark)" strokeWidth="7" strokeLinecap="round" />
            <path d="M33 30h30M33 43h22M33 56h16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </>
        )}
      </svg>
      <Heading className="empty-title">{title}</Heading>
      {text && <p className="empty-text">{text}</p>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  )
}

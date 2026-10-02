import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { CheckIcon } from './icons'
import './Tag.css'

type TagProps = { tone?: 'neutral' | 'accent'; children: ReactNode }

/** Короткая метка-факт: «Конкурс», «ППС». Не нажимается. */
export function Tag({ tone = 'neutral', children }: TagProps) {
  return <span className={`tag tag-${tone}`}>{children}</span>
}

type ChipProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'aria-pressed'> & { pressed: boolean }

/** Фильтр-переключатель: включён или выключен, состояние видно и без цвета (галочка). */
export function FilterChip({ pressed, children, className, ...rest }: ChipProps) {
  return (
    <button type="button" className={['chip', className ?? ''].filter(Boolean).join(' ')} aria-pressed={pressed} {...rest}>
      {pressed && <CheckIcon size={16} />}
      {children}
    </button>
  )
}

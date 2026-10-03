import { useState } from 'react'
import { t } from '../../i18n'
import { Button, type ButtonSize, type ButtonVariant } from '../../ui/Button'
import { useMe } from '../auth/api'
import { useRole } from '../shell/useRole'
import { InviteModal } from './InviteModal'

/**
 * «Пригласить на вакансию»: только в режиме «Нанимаю» и только вошедшему. Решает ли он вправе приглашать, скажет сервер:
 * окно покажет вакансии, которые человек ведёт, или объяснит, что их нет.
 */
export function InviteButton({ profileId, name, size = 'md', variant = 'secondary' }: { profileId: string; name: string; size?: ButtonSize; variant?: ButtonVariant }) {
  const { role } = useRole()
  const { user } = useMe()
  const [open, setOpen] = useState(false)
  if (role !== 'employer' || !user) return null
  return (
    <>
      <Button size={size} variant={variant} onClick={() => setOpen(true)} aria-label={`${t.offers.invite.button}: ${name}`}>
        {t.offers.invite.button}
      </Button>
      {open && <InviteModal profileId={profileId} name={name} onClose={() => setOpen(false)} />}
    </>
  )
}

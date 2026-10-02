import { t } from '../../i18n'
import type { Role } from './role-context'
import { useRole } from './useRole'
import './RoleSwitch.css'

const options: { role: Role; label: string }[] = [
  { role: 'seeker', label: t.shell.roleSeeker },
  { role: 'employer', label: t.shell.roleEmployer },
]

/** Два режима одного аккаунта. Активный отмечен маркером и весом, не только цветом. */
export function RoleSwitch() {
  const { role, setRole } = useRole()
  return (
    <div className="role-switch" role="group" aria-label={t.shell.roleLabel}>
      {options.map((o) => (
        <button
          key={o.role}
          type="button"
          className="role-option"
          aria-pressed={role === o.role}
          onClick={() => setRole(o.role)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

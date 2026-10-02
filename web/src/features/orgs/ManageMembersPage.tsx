import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type FormEvent } from 'react'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import { Select, type Option } from '../../ui/Select'
import { ChevronDownIcon } from '../../ui/icons'
import { Tag } from '../../ui/Tag'
import { TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { useMe } from '../auth/api'
import { describeError, fieldErrorsOf } from '../auth/errors'
import { useForm } from '../auth/useForm'
import { checkEmail, compact } from '../auth/validation'
import { changeRole, invite, refreshOrganizations, removeMember, revokeInvitation, roles, useMembers, type Member, type PendingInvitation } from './api'
import { ConfirmModal } from './ConfirmModal'
import { formatDate, roleLabel, roleOptions } from './labels'
import { useManageContext } from './manageContext'
import { LoadFailed, PageSkeleton } from './states'

/** Сотрудники и приглашения. Только для владельца: здесь личные почты и права. */
export function ManageMembersPage() {
  const { slug, view, viewer } = useManageContext()
  const members = useMembers(slug, viewer.can_manage_members)

  if (!viewer.can_manage_members) {
    return (
      <EmptyState
        headingLevel={2}
        title={t.orgs.manage.noAccessTitle}
        text={t.orgs.manage.noAccessText}
        action={<ButtonLink to={`/my-organization/${slug}`}>{t.orgs.manage.tabData}</ButtonLink>}
      />
    )
  }
  if (members.isPending) return <PageSkeleton />
  if (members.isError) return <LoadFailed title={t.orgs.members.loadError} error={members.error} onRetry={() => void members.refetch()} />

  const vacantUnits: Option[] = view.units.filter((u) => u.head_name === null).map((u) => ({ value: u.id, label: u.name }))

  return (
    <>
      <p className="manage-intro">{t.orgs.members.lead}</p>

      <section className="manage-section" aria-labelledby="members-invite">
        <h2 id="members-invite">{t.orgs.members.inviteTitle}</h2>
        <InviteForm slug={slug} vacantUnits={vacantUnits} />
      </section>

      <section className="manage-section" aria-labelledby="members-list">
        <h2 id="members-list">{t.orgs.members.listTitle}</h2>
        <div>
          {members.data.members.map((m) => (
            <MemberRow key={m.user_id} slug={slug} member={m} />
          ))}
        </div>
        <dl className="role-legend">
          {roles.map((r) => (
            <div key={r}>
              <dt>{roleLabel(r)}</dt>
              <dd>{t.orgs.roleDescriptions[r]}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="manage-section" aria-labelledby="members-pending">
        <h2 id="members-pending">{t.orgs.members.pendingTitle}</h2>
        {members.data.invitations.length === 0 ? (
          <p className="manage-note">{t.orgs.members.pendingNone}</p>
        ) : (
          members.data.invitations.map((i) => <PendingRow key={i.id} slug={slug} invitation={i} />)
        )}
      </section>
    </>
  )
}

function InviteForm({ slug, vacantUnits }: { slug: string; vacantUnits: readonly Option[] }) {
  const client = useQueryClient()
  const toast = useToast()
  const formRef = useRef<HTMLFormElement>(null)
  const send = useMutation({
    mutationFn: (v: { email: string; role: string; unit: string }) =>
      invite(slug, { email: v.email.trim(), role: v.role, unit_id: v.role === 'unit_head' && v.unit ? v.unit : null }),
    onSuccess: async (inv) => {
      await refreshOrganizations(client, slug)
      set('email', '')
      set('unit', '')
      toast.show({ kind: 'success', title: t.orgs.members.sent(inv.email) })
    },
  })
  const { values, set, errors, validate } = useForm({ email: '', role: '', unit: '' }, send.error, formRef)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const found = compact({
      email: checkEmail(values.email),
      role: values.role === '' ? t.orgs.errors.roleRequired : undefined,
    })
    if (validate(found)) send.mutate(values)
  }
  const formError = send.error && Object.keys(fieldErrorsOf(send.error)).length === 0 ? describeError(send.error) : undefined

  return (
    <form className="invite-form" onSubmit={submit} ref={formRef} noValidate>
      {formError && (
        <Alert kind="error" className="invite-wide">
          {formError}
        </Alert>
      )}
      <TextField
        label={t.orgs.members.email}
        hint={t.orgs.members.emailHint}
        type="email"
        name="email"
        autoComplete="off"
        inputMode="email"
        autoCapitalize="none"
        spellCheck={false}
        value={values.email}
        onChange={(e) => set('email', e.target.value)}
        error={errors.email}
        className="invite-wide"
        required
      />
      <Select
        label={t.orgs.members.roleField}
        name="role"
        placeholder={t.orgs.members.rolePlaceholder}
        options={roleOptions}
        value={values.role}
        onChange={(e) => set('role', e.target.value)}
        error={errors.role}
        required
      />
      {values.role === 'unit_head' && (
        <Select
          label={t.orgs.members.unit}
          hint={t.orgs.members.unitHint}
          name="unit"
          placeholder={t.orgs.members.unitNone}
          options={vacantUnits}
          value={values.unit}
          onChange={(e) => set('unit', e.target.value)}
          error={errors.unit_id}
          optional
        />
      )}
      <Button type="submit" loading={send.isPending}>
        {t.orgs.members.send}
      </Button>
    </form>
  )
}

function MemberRow({ slug, member }: { slug: string; member: Member }) {
  const client = useQueryClient()
  const toast = useToast()
  const { user } = useMe()
  const [removing, setRemoving] = useState(false)
  const isYou = user?.id === member.user_id

  const change = useMutation({
    mutationFn: (role: string) => changeRole(slug, member.user_id, role),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.members.roleChanged })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.orgs.members.roleChangeFailed, text: describeError(err) }),
  })
  const remove = useMutation({
    mutationFn: () => removeMember(slug, member.user_id),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.members.removed })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.orgs.members.removeFailed, text: describeError(err) }),
    onSettled: () => setRemoving(false),
  })

  return (
    <div className="member-row">
      <div>
        <p className="member-name">
          {member.name} {isYou && <Tag>{t.orgs.members.you}</Tag>}
        </p>
        <p className="member-email">{member.email}</p>
      </div>
      <div className="select-wrap">
        <select
          className="control role-select"
          aria-label={t.orgs.members.changeRole(member.name)}
          value={member.role}
          disabled={change.isPending}
          onChange={(e) => change.mutate(e.target.value)}
        >
          {roleOptions.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <ChevronDownIcon className="select-chevron" size={18} />
      </div>
      <div className="row-actions">
        {!isYou && (
          <Button variant="quiet" size="sm" onClick={() => setRemoving(true)}>
            {t.orgs.members.remove}
          </Button>
        )}
      </div>
      <ConfirmModal
        open={removing}
        title={t.orgs.members.removeTitle(member.name)}
        text={t.orgs.members.removeText}
        confirmLabel={t.orgs.members.removeConfirm}
        pending={remove.isPending}
        onConfirm={() => remove.mutate()}
        onClose={() => setRemoving(false)}
      />
    </div>
  )
}

function PendingRow({ slug, invitation }: { slug: string; invitation: PendingInvitation }) {
  const client = useQueryClient()
  const toast = useToast()
  const revoke = useMutation({
    mutationFn: () => revokeInvitation(slug, invitation.id),
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.members.revoked })
    },
    onError: (err) => toast.show({ kind: 'error', title: t.orgs.members.revokeFailed, text: describeError(err) }),
  })
  const again = useMutation({
    mutationFn: () => invite(slug, { email: invitation.email, role: invitation.role, unit_id: invitation.unit_id }),
    onSuccess: async (inv) => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: t.orgs.members.sent(inv.email) })
    },
    onError: (err) => toast.show({ kind: 'error', title: describeError(err) }),
  })
  return (
    <div className="mine-row">
      <div className="mine-row-main">
        <p className="member-name">{invitation.email}</p>
        <p className="mine-row-line">
          <Tag tone="accent">{roleLabel(invitation.role)}</Tag>
          {invitation.unit_name && <span>{t.orgs.members.pendingUnit(invitation.unit_name)}</span>}
          <span className="num">{t.orgs.members.pendingUntil(formatDate(invitation.expires_at))}</span>
        </p>
      </div>
      <div className="mine-row-actions">
        <Button variant="secondary" size="sm" loading={again.isPending} onClick={() => again.mutate()}>
          {t.orgs.members.again}
        </Button>
        <Button variant="quiet" size="sm" loading={revoke.isPending} onClick={() => revoke.mutate()}>
          {t.orgs.members.revoke}
        </Button>
      </div>
    </div>
  )
}

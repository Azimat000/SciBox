import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router'
import { t } from '../../i18n'
import { ButtonLink } from '../../ui/Button'
import { EmptyState } from '../../ui/EmptyState'
import type { Option } from '../../ui/Select'
import { useToast } from '../../ui/useToast'
import { createUnit, refreshOrganizations, setUnitHead, updateUnit, useMembers } from './api'
import { useManageContext } from './manageContext'
import { LoadFailed, PageSkeleton } from './states'
import { UnitForm, type UnitFormValues } from './UnitForm'

/** Добавление и правка подразделения (адрес без номера: новое). Руководителя назначает только владелец. */
export function UnitEditPage() {
  const { slug, view, viewer } = useManageContext()
  const { unitId } = useParams()
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const members = useMembers(slug, viewer.can_manage_members)

  const unit = unitId ? view.units.find((u) => u.id === unitId) : undefined
  const allowed = unitId ? viewer.can_manage_units || viewer.editable_units.includes(unitId) : viewer.can_manage_units
  const initialHead = unit?.head_user_id ?? ''

  const save = useMutation({
    mutationFn: async ({ fields, head }: UnitFormValues) => {
      const saved = unitId ? await updateUnit(slug, unitId, fields) : await createUnit(slug, fields)
      if (viewer.can_manage_units && head !== initialHead) await setUnitHead(slug, saved.id, head || null)
      return saved
    },
    onSuccess: async () => {
      await refreshOrganizations(client, slug)
      toast.show({ kind: 'success', title: unitId ? t.orgs.units.saved : t.orgs.units.created })
      void navigate(`/my-organization/${slug}/units`)
    },
  })

  if (unitId && !unit) {
    return (
      <EmptyState
        headingLevel={2}
        title={t.orgs.units.notFoundTitle}
        text={t.orgs.units.notFoundText}
        action={<ButtonLink to={`/my-organization/${slug}/units`}>{t.orgs.units.back}</ButtonLink>}
      />
    )
  }
  if (!allowed) {
    return (
      <EmptyState
        headingLevel={2}
        title={t.orgs.manage.noAccessTitle}
        text={t.orgs.units.headOnlyOwner}
        action={<ButtonLink to={`/my-organization/${slug}/units`}>{t.orgs.units.back}</ButtonLink>}
      />
    )
  }
  if (viewer.can_manage_members && members.isPending) return <PageSkeleton />
  if (viewer.can_manage_members && members.isError) {
    return <LoadFailed title={t.orgs.members.loadError} error={members.error} onRetry={() => void members.refetch()} />
  }

  // Список сотрудников есть только у владельца (он один его видит): без него поля «Руководитель» нет.
  const headOptions: Option[] | undefined = members.data?.members.map((m) => ({ value: m.user_id, label: m.name }))

  return (
    <>
      <p className="manage-note">
        <Link to={`/my-organization/${slug}/units`}>{t.orgs.units.back}</Link>
      </p>
      <h2 className="manage-subtitle">{unit ? t.orgs.units.editTitle : t.orgs.units.newTitle}</h2>
      <UnitForm
        key={unitId ?? 'new'}
        initial={{
          name: unit?.name ?? '',
          kind: unit?.kind ?? '',
          description: unit?.description ?? '',
          topics: unit?.topics ?? [],
          head: initialHead,
        }}
        submitLabel={unit ? t.orgs.units.save : t.orgs.units.create}
        pending={save.isPending}
        error={save.error}
        headOptions={headOptions}
        headName={unit?.head_name}
        onSubmit={(v) => save.mutate(v)}
      />
    </>
  )
}

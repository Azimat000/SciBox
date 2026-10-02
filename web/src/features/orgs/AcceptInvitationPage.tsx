import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { t } from '../../i18n'
import { Alert } from '../../ui/Alert'
import { Button, ButtonLink } from '../../ui/Button'
import { Skeleton } from '../../ui/Skeleton'
import { useToast } from '../../ui/useToast'
import { AuthPage } from '../auth/AuthPage'
import { describeError } from '../auth/errors'
import { acceptInvitation, refreshOrganizations, useInvitation } from './api'
import { roleLabel } from './labels'
import { LoadFailed } from './states'
import './orgs.css'

/** Страница по ссылке из письма-приглашения: что за организация и какая роль, и кнопка «Принять». */
export function AcceptInvitationPage() {
  const token = useSearchParams()[0].get('token') ?? ''
  const client = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const query = useInvitation(token)
  const accept = useMutation({
    mutationFn: () => acceptInvitation(token),
    onSuccess: async (joined) => {
      await refreshOrganizations(client, joined.slug)
      toast.show({ kind: 'success', title: t.orgs.mine.accepted(joined.name) })
      void navigate(`/my-organization/${joined.slug}`)
    },
  })

  if (!token) return <Invalid text={t.orgs.accept.noToken} />
  if (query.isPending) {
    return (
      <AuthPage title={t.orgs.accept.title}>
        <div role="status" aria-busy="true" aria-label={t.common.loading}>
          <Skeleton width="70%" height="1.25rem" />
        </div>
      </AuthPage>
    )
  }
  if (query.isError) {
    if (query.error instanceof ApiError && query.error.code === 'invalid_invitation') return <Invalid text={t.orgs.accept.invalidText} />
    return <LoadFailed title={t.orgs.accept.loadError} error={query.error} onRetry={() => void query.refetch()} />
  }

  const inv = query.data
  const next = encodeURIComponent(`/invitations/accept?token=${encodeURIComponent(token)}`)
  const alreadyMember = accept.error instanceof ApiError && accept.error.code === 'already_member'

  return (
    <AuthPage
      title={t.orgs.accept.title}
      lead={t.orgs.accept.lead(inv.organization.name, roleLabel(inv.role)) + (inv.unit_name ? ` ${t.orgs.accept.unit(inv.unit_name)}` : '')}
    >
      {inv.email_matches === null && (
        <>
          <Alert kind="info">{t.orgs.accept.needLogin(inv.email)}</Alert>
          <div className="auth-form">
            <ButtonLink to={`/login?next=${next}`}>{t.orgs.accept.login}</ButtonLink>
            <ButtonLink to="/register" variant="secondary">
              {t.orgs.accept.register}
            </ButtonLink>
          </div>
          <p className="auth-note">{t.orgs.accept.afterRegister}</p>
        </>
      )}
      {inv.email_matches === false && <Alert kind="error">{t.orgs.accept.wrongEmail(inv.email)}</Alert>}
      {inv.email_matches === true &&
        (alreadyMember ? (
          <Alert kind="info" action={<Link to={`/my-organization/${inv.organization.slug}`}>{t.orgs.accept.toOrganization}</Link>}>
            {t.orgs.accept.already}
          </Alert>
        ) : (
          <div className="auth-form">
            {accept.error && <Alert kind="error">{describeError(accept.error)}</Alert>}
            <Button loading={accept.isPending} onClick={() => accept.mutate()}>
              {t.orgs.accept.accept}
            </Button>
          </div>
        ))}
    </AuthPage>
  )
}

function Invalid({ text }: { text: string }) {
  return (
    <AuthPage title={t.orgs.accept.invalidTitle} lead={text}>
      <div className="auth-status">
        <ButtonLink to="/my-organization" variant="secondary">
          {t.nav.myOrganization}
        </ButtonLink>
      </div>
    </AuthPage>
  )
}

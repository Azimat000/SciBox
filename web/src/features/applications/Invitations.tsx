import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Tag } from '../../ui/Tag'
import { useToast } from '../../ui/useToast'
import { describeError } from '../auth/errors'
import { refreshNotifications } from '../notifications/refresh'
import { ConfirmModal } from '../orgs/ConfirmModal'
import { acceptProposal, answerInvitation, cancelInvitation, refreshApplications, type Invitation } from './api'
import { ProposeModal, ReplyModal } from './AnswerModals'
import { dateText, invStatusLabel, kindLabel, momentText } from './labels'
import './review.css'
import { mailtoHref } from '../../lib/mailto'
import { isSafeHttp } from '../../lib/safeLink'

export type Role = 'applicant' | 'staff'

/** Место собеседования пишет организация; ссылкой оно становится, только если по адресу видно, куда он ведёт (T-117). */
const isHttpUrl = (s: string) => !/\s/.test(s) && isSafeHttp(s)

/** Приглашения отклика. Обе стороны видят их одинаково; кнопки у каждой стороны свои. */
export function InvitationList({ appId, invitations, role }: { appId: string; invitations: Invitation[]; role: Role }) {
  const i = t.applications.invitation
  return (
    <ul className="inv-list" aria-label={role === 'staff' ? i.title : i.titleApplicant}>
      {invitations.map((inv) => (
        <Item key={inv.id} appId={appId} inv={inv} role={role} />
      ))}
    </ul>
  )
}

function Item({ appId, inv, role }: { appId: string; inv: Invitation; role: Role }) {
  const i = t.applications.invitation
  // Ждущее ответа приглашение соискателю выделено: ему нужно действие.
  const tone = role === 'applicant' && inv.can_answer ? 'accent' : 'neutral'
  return (
    <li className="inv-item" data-cancelled={inv.status === 'cancelled' || undefined}>
      <div className="inv-head">
        <h3 className="inv-kind">{kindLabel(inv.kind)}</h3>
        <Tag tone={tone}>{invStatusLabel(inv.status)}</Tag>
      </div>
      <Details inv={inv} />
      {inv.answer && <Answer inv={inv} role={role} />}
      <p className="inv-sent">{i.sentAt(dateText(inv.created_at))}</p>
      <InvitationActions appId={appId} inv={inv} />
    </li>
  )
}

/**
 * Кнопки приглашения по правам inv (can_answer, can_accept_proposal, can_cancel) и окна к ним. Одни и те же на странице
 * отклика и в переписке (срез 35); onChanged зовётся после успешного действия, когда списки уже перечитаны.
 */
export function InvitationActions({ appId, inv, onChanged }: { appId: string; inv: Invitation; onChanged?: () => void }) {
  const i = t.applications.invitation
  const client = useQueryClient()
  const toast = useToast()
  const [confirmCancel, setConfirmCancel] = useState(false)
  const [modal, setModal] = useState<'propose' | 'reply' | null>(null)
  const refresh = () => Promise.all([refreshApplications(client), refreshNotifications(client)])
  const fail = (err: unknown) => toast.show({ kind: 'error', title: i.actionFailed, text: describeError(err) })
  const done = async (title: string) => {
    await refresh()
    toast.show({ kind: 'success', title })
    onChanged?.()
  }

  const confirm = useMutation({
    mutationFn: () => answerInvitation(appId, inv.id, { action: 'confirm' }),
    onSuccess: () => done(i.confirmDone),
    onError: fail,
  })
  const accept = useMutation({
    mutationFn: () => acceptProposal(appId, inv.id),
    onSuccess: () => done(i.acceptDone),
    onError: fail,
  })
  const cancel = useMutation({
    mutationFn: () => cancelInvitation(appId, inv.id),
    onSuccess: async () => {
      setConfirmCancel(false)
      await done(i.cancelDone)
    },
    onError: (err) => {
      setConfirmCancel(false)
      fail(err)
    },
  })

  if (!(inv.can_answer || inv.can_accept_proposal || inv.can_cancel)) return null
  return (
    <>
      <div className="inv-actions">
        {inv.can_answer && inv.kind === 'interview' && (
          <>
            <Button size="sm" loading={confirm.isPending} onClick={() => confirm.mutate()}>
              {i.confirm}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => setModal('propose')}>
              {i.propose}
            </Button>
          </>
        )}
        {inv.can_answer && inv.kind === 'request_contacts' && (
          <Button size="sm" onClick={() => setModal('reply')}>
            {i.reply}
          </Button>
        )}
        {inv.can_accept_proposal && (
          <Button size="sm" loading={accept.isPending} onClick={() => accept.mutate()}>
            {i.acceptProposal}
          </Button>
        )}
        {inv.can_cancel && (
          <Button size="sm" variant="quiet" onClick={() => setConfirmCancel(true)}>
            {i.cancel}
          </Button>
        )}
      </div>
      {modal === 'propose' && <ProposeModal appId={appId} invitationId={inv.id} onClose={() => setModal(null)} onDone={onChanged} />}
      {modal === 'reply' && <ReplyModal appId={appId} invitationId={inv.id} onClose={() => setModal(null)} onDone={onChanged} />}
      <ConfirmModal
        open={confirmCancel}
        title={i.cancelTitle}
        text={i.cancelText}
        confirmLabel={i.cancel}
        pending={cancel.isPending}
        onConfirm={() => cancel.mutate()}
        onClose={() => setConfirmCancel(false)}
      />
    </>
  )
}

/** Факты приглашения: когда, где, контакты, сообщение организации. */
export function Details({ inv }: { inv: Invitation }) {
  const i = t.applications.invitation
  return (
    <div className="inv-body">
      {inv.kind === 'interview' && inv.starts_at && (
        <dl className="inv-facts">
          <dt>{i.when}</dt>
          <dd>
            <time dateTime={inv.starts_at}>{momentText(inv.starts_at)}</time>
          </dd>
          <dt>{i.where}</dt>
          <dd>
            {inv.place_kind === 'online' ? `${i.online}: ` : `${i.onsite}: `}
            {inv.place_kind === 'online' && isHttpUrl(inv.place) ? (
              <a href={inv.place} target="_blank" rel="noopener noreferrer">
                {inv.place}
              </a>
            ) : (
              inv.place
            )}
          </dd>
        </dl>
      )}
      {inv.kind === 'contacts' && (
        <dl className="inv-facts">
          {inv.contact_name && (
            <>
              <dt>{i.contactPerson}</dt>
              <dd>{inv.contact_name}</dd>
            </>
          )}
          {inv.contact_email && (
            <>
              <dt>{i.email}</dt>
              <dd>
                <a href={mailtoHref(inv.contact_email)}>{inv.contact_email}</a>
              </dd>
            </>
          )}
          {inv.contact_phone && (
            <>
              <dt>{i.phone}</dt>
              <dd>
                <a href={`tel:${inv.contact_phone.replace(/[^\d+]/g, '')}`}>{inv.contact_phone}</a>
              </dd>
            </>
          )}
        </dl>
      )}
      {inv.message && (
        <p className="inv-message">
          <span>{i.message}: </span>
          {inv.message}
        </p>
      )}
    </div>
  )
}

/** Что ответил кандидат. Для собеседования вид ответа понятен по состоянию приглашения. */
export function Answer({ inv, role }: { inv: Invitation; role: Role }) {
  const i = t.applications.invitation
  const a = inv.answer!
  const lines: string[] = []
  if (inv.kind === 'interview') {
    if (inv.status === 'proposed' && a.proposed_at) lines.push(i.answerProposed(momentText(a.proposed_at)))
    else if (a.proposed_at) lines.push(role === 'staff' ? i.answerAcceptedProposalStaff : i.answerAcceptedProposal)
    else if (inv.status === 'confirmed') lines.push(i.answerConfirmed)
  }
  if (a.contact) lines.push(i.answerContact(a.contact))
  if (a.time) lines.push(i.answerTime(a.time))
  if (a.note) lines.push(i.answerNote(a.note))
  if (lines.length === 0) return null
  return (
    <div className="inv-answer">
      <p className="inv-answer-head">{role === 'staff' ? i.answerHeading : i.answerHeadingApplicant}</p>
      {lines.map((line) => (
        <p key={line}>{line}</p>
      ))}
    </div>
  )
}

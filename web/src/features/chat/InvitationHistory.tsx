import { t } from '../../i18n'
import { mailtoHref } from '../../lib/mailto'
import { isSafeHttp } from '../../lib/safeLink'
import type { Side } from './api'
import { momentText, type OldInvitation } from './oldInvitation'
import '../applications/review.css'

// Старые приглашения внутри отклика (до D-256): только для чтения, без кнопок.

/** Место собеседования писала организация; ссылкой оно становится, только если по адресу видно, куда он ведёт (T-117). */
const isHttpUrl = (s: string) => !/\s/.test(s) && isSafeHttp(s)

/** Карточка старого приглашения: когда, где, контакты, сообщение организации и ответ кандидата. */
export function OldInvitationCard({ inv, side }: { inv: OldInvitation; side: Side }) {
  return (
    <div className="chat-event-card">
      <Details inv={inv} />
      {inv.answer && <Answer inv={inv} side={side} />}
    </div>
  )
}

function Details({ inv }: { inv: OldInvitation }) {
  const i = t.chat.oldInvitation
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
              <dd>{inv.contact_phone}</dd>
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
function Answer({ inv, side }: { inv: OldInvitation; side: Side }) {
  const i = t.chat.oldInvitation
  const a = inv.answer!
  const lines: string[] = []
  if (inv.kind === 'interview') {
    if (inv.status === 'proposed' && a.proposed_at) lines.push(i.answerProposed(momentText(a.proposed_at)))
    else if (a.proposed_at) lines.push(side === 'org' ? i.answerAcceptedProposalStaff : i.answerAcceptedProposal)
    else if (inv.status === 'confirmed') lines.push(i.answerConfirmed)
  }
  if (a.contact) lines.push(i.answerContact(a.contact))
  if (a.time) lines.push(i.answerTime(a.time))
  if (a.note) lines.push(i.answerNote(a.note))
  if (lines.length === 0) return null
  return (
    <div className="inv-answer">
      <p className="inv-answer-head">{side === 'org' ? i.answerHeading : i.answerHeadingApplicant}</p>
      {lines.map((line) => (
        <p key={line}>{line}</p>
      ))}
    </div>
  )
}

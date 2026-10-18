import { useState } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'
import { Tag } from '../../ui/Tag'
import { OldInvitationCard } from './InvitationHistory'
import { momentText, oldKindLabel, oldStatusLabel } from './oldInvitation'
import { AnswerForm } from '../offers/OfferPage'
import type { ChatHeader, Message } from './api'
import { COLLAPSE_AT, effectiveInvitation, effectiveOffer, field, momentField } from './events'
import { MessageText } from './MessageText'
import { messageTime } from './conversation'

type Props = {
  message: Message
  items: readonly Message[]
  chat: ChatHeader
  /** После ответа на приглашение: переписка перечитывается. */
  onChanged: () => void
}

/** Длинное письмо или записка сворачивается; «Показать полностью» раскрывает. */
function Quote({ label, text }: { label: string; text: string }) {
  const [open, setOpen] = useState(false)
  if (!text) return null
  const long = text.length > COLLAPSE_AT
  return (
    <div className="chat-event-quote">
      <p className="chat-event-quote-label">{label}</p>
      <p className="chat-event-quote-text" data-clamped={long && !open ? 'true' : undefined}>
        <MessageText text={text} />
      </p>
      {long && (
        <button type="button" className="chat-event-more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? t.chat.line.collapse : t.chat.line.expand}
        </button>
      )}
    </div>
  )
}

/** Системная строка ленты: событие по отклику или приглашению, при необходимости с кнопками ответа (D-214, CHAT.md). */
export function SystemLine({ message, items, chat, onChanged }: Props) {
  const l = t.chat.line
  const side = chat.side
  const type = message.event?.type ?? ''
  const time = <time className="chat-event-time num" dateTime={message.created_at}>{messageTime(message.created_at)}</time>
  const frame = (title: string, body?: React.ReactNode, tag?: React.ReactNode, extra?: string) => (
    <div className="chat-event" data-type={type} data-extra={extra}>
      <div className="chat-event-head">
        <p className="chat-event-title">{title}</p>
        {tag}
        {time}
      </div>
      {body}
    </div>
  )

  switch (type) {
    case 'application.sent':
      return frame(l.application.sent[side], <Quote label={l.coverLetter} text={field(message, 'cover_letter')} />)
    case 'application.viewed':
      return frame(l.application.viewed[side])
    case 'application.withdrawn':
      return frame(l.application.withdrawn[side])
    case 'application.decided': {
      const accepted = field(message, 'decision') === 'accepted'
      return frame(l.application.decided, <Quote label={l.note} text={field(message, 'note')} />, <Tag tone={accepted ? 'accent' : 'neutral'}>{accepted ? l.application.accepted : l.application.rejected}</Tag>)
    }
    // Приглашения внутри отклика были до D-256: старые строки показываются как история, без кнопок.
    case 'invitation.created': {
      const inv = effectiveInvitation(items, message)
      if (!inv) return frame(l.unknown)
      return frame(l.invitation.created(oldKindLabel(inv.kind).toLowerCase()), <OldInvitationCard inv={inv} side={side} />, <Tag>{oldStatusLabel(inv.status)}</Tag>, 'card')
    }
    case 'invitation.answered': {
      const answer = field(message, 'answer')
      const key = answer === 'proposed' ? 'proposed' : answer === 'answered' ? 'answered' : 'confirmed'
      const lines: string[] = []
      const proposed = momentField(message, 'proposed_at')
      const i = t.chat.oldInvitation
      if (proposed) lines.push(i.answerProposed(momentText(proposed)))
      const contact = field(message, 'contact')
      if (contact) lines.push(i.answerContact(contact))
      const when = field(message, 'time')
      if (when) lines.push(i.answerTime(when))
      const note = field(message, 'note')
      if (note) lines.push(i.answerNote(note))
      return frame(
        l.invitation[key][side],
        lines.length > 0 ? (
          <div className="chat-event-lines">
            {lines.map((line) => (
              <p key={line}>
                <MessageText text={line} />
              </p>
            ))}
          </div>
        ) : undefined,
      )
    }
    case 'invitation.proposal_accepted': {
      const at = momentField(message, 'starts_at')
      return frame(l.invitation.accepted, at ? <p className="chat-event-lines">{momentText(at)}</p> : undefined)
    }
    case 'invitation.cancelled': {
      const kind = field(message, 'kind')
      return frame(kind ? l.invitation.cancelledKind(oldKindLabel(kind).toLowerCase()) : l.invitation.cancelled)
    }
    case 'offer.created': {
      const offer = effectiveOffer(items, message, side, chat.can_write)
      if (!offer) return frame(l.unknown)
      return frame(
        l.offer.created[side],
        <OfferCard offerId={offer.id} text={offer.message} canAnswer={offer.canAnswer} onChanged={onChanged} />,
        offer.canAnswer ? <Tag tone="accent">{l.offer.waits}</Tag> : undefined,
        'card',
      )
    }
    case 'offer.answered': {
      const declined = field(message, 'answer') === 'declined'
      return frame(declined ? l.offer.declined[side] : l.offer.interested[side], <Quote label={side === 'scientist' ? l.offer.yourNote : l.offer.theirNote} text={field(message, 'note')} />)
    }
    case 'offer.cancelled':
      return frame(l.offer.cancelled)
    case 'chat.blocked':
      return frame(l.blocked[side])
    case 'chat.unblocked':
      return frame(l.unblocked[side])
    default:
      return frame(l.unknown)
  }
}

/** Приглашение на вакансию: текст организации и кнопка ответа; ответ в том же окне, что на странице приглашения. */
function OfferCard({ offerId, text, canAnswer, onChanged }: { offerId: string; text: string; canAnswer: boolean; onChanged: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="chat-event-card">
      <Quote label={t.chat.line.offer.message} text={text} />
      {canAnswer && (
        <div className="inv-actions">
          <Button size="sm" onClick={() => setOpen(true)}>
            {t.chat.line.offer.answer}
          </Button>
        </div>
      )}
      <Modal open={open} onClose={() => setOpen(false)} title={t.chat.line.offer.answerTitle}>
        <AnswerForm
          offerId={offerId}
          onDone={() => {
            setOpen(false)
            onChanged()
          }}
        />
      </Modal>
    </div>
  )
}

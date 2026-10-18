import { Link } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Tag } from '../../ui/Tag'
import { InviteButton } from '../offers/InviteButton'
import { anonymousLabel } from '../profile/labels'
import type { CatalogCard } from './api'
import { degreeLabel, titleLabel } from './labels'
import './catalog.css'

/** Учёный в списке каталога, набранный как запись в содержании журнала: имя, должность, ряд фактов, специальности. */
type Props = {
  card: CatalogCard
  headingLevel?: 2 | 3
  /** С какого года считаются «свежие» статьи в Q1–Q2 (из ответа каталога). */
  recentFrom: number
}

export function ScientistEntry({ card, headingLevel = 2, recentFrom }: Props) {
  const c = t.catalog
  const Heading = `h${headingLevel}` as const
  const place = c.place(card.city, card.region)
  // Анонимная карточка: вместо имени название из научных данных, без должности и города (D-199)
  const title = card.anonymous
    ? anonymousLabel({ degree: card.degree, title: card.academic_title, codes: card.specialties.map((s) => s.code), level: card.level ?? 0, hIndex: card.h_index })
    : card.name
  // У анонимной карточки степень, звание, уровень и h-index уже в названии: в строке фактов они не повторяются
  const facts: string[] = []
  if (!card.anonymous && card.degree !== 'none') facts.push(degreeLabel(card.degree))
  if (!card.anonymous && card.academic_title !== 'none') facts.push(titleLabel(card.academic_title))
  if (place) facts.push(place)
  if (!card.anonymous && card.h_index !== null) facts.push(c.hIndex(card.h_index))
  if (card.publications > 0) facts.push(c.publications(card.publications, plural(card.publications, c.publicationForms)))
  if (card.q12_total > 0) {
    const total = c.q12(card.q12_total, plural(card.q12_total, c.q12Forms))
    facts.push(card.q12_recent > 0 ? `${total}, ${c.q12Recent(card.q12_recent, recentFrom)}` : total)
  }
  return (
    <article className="entry sci-entry">
      <Heading className="entry-title">
        <Link to={`/scientists/${card.id}`}>{title}</Link>
      </Heading>
      {card.headline && <p className="entry-org">{card.headline}</p>}
      {card.anonymous && (
        <div className="entry-side sci-side">
          <div className="sci-tags">
            <Tag>{t.profile.anonymous.badge}</Tag>
          </div>
        </div>
      )}
      {facts.length > 0 && (
        <ul className="entry-facts" aria-label={c.facts}>
          {facts.map((fact, i) => (
            <li key={fact}>{i === 0 && !card.anonymous && card.degree !== 'none' ? <strong>{fact}</strong> : fact}</li>
          ))}
        </ul>
      )}
      <div className="entry-footer sci-footer">
        {card.specialties.length > 0 && (
          <ul className="sci-specs" aria-label={c.specialties}>
            {card.specialties.map((s) => (
              <li key={s.code}>
                <Tag>
                  <span className="num">{s.code}</span> {s.name}
                </Tag>
              </li>
            ))}
          </ul>
        )}
        <InviteButton profileId={card.id} name={card.anonymous ? t.profile.anonymous.name : card.name} size="sm" />
      </div>
    </article>
  )
}

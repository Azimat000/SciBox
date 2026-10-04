import { Link } from 'react-router'
import { t } from '../../i18n'
import { plural } from '../../lib/plural'
import { Tag } from '../../ui/Tag'
import { InviteButton } from '../offers/InviteButton'
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
  const facts: string[] = []
  if (card.degree !== 'none') facts.push(degreeLabel(card.degree))
  if (card.academic_title !== 'none') facts.push(titleLabel(card.academic_title))
  if (place) facts.push(place)
  if (card.h_index !== null) facts.push(c.hIndex(card.h_index))
  if (card.publications > 0) facts.push(c.publications(card.publications, plural(card.publications, c.publicationForms)))
  if (card.q12_total > 0) {
    const total = c.q12(card.q12_total, plural(card.q12_total, c.q12Forms))
    facts.push(card.q12_recent > 0 ? `${total}, ${c.q12Recent(card.q12_recent, recentFrom)}` : total)
  }
  return (
    <article className="entry sci-entry">
      <Heading className="entry-title">
        <Link to={`/scientists/${card.id}`}>{card.name}</Link>
      </Heading>
      <p className="entry-org">{card.headline}</p>
      {card.open_to_offers && (
        <div className="entry-side sci-side">
          <Tag tone="accent">{c.openToOffers}</Tag>
        </div>
      )}
      {facts.length > 0 && (
        <ul className="entry-facts" aria-label={c.facts}>
          {facts.map((fact, i) => (
            <li key={fact}>{i === 0 && card.degree !== 'none' ? <strong>{fact}</strong> : fact}</li>
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
        <InviteButton profileId={card.id} name={card.name} size="sm" />
      </div>
    </article>
  )
}

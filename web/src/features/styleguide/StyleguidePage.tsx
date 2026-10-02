import { useState, type ReactNode } from 'react'
import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Combobox } from '../../ui/Combobox'
import { Deadline } from '../../ui/Deadline'
import { EmptyState } from '../../ui/EmptyState'
import { Modal } from '../../ui/Modal'
import { ResultsHeader } from '../../ui/ResultsHeader'
import { SearchBar } from '../../ui/SearchBar'
import { Select } from '../../ui/Select'
import { VacancyListSkeleton } from '../../ui/Skeleton'
import { FilterChip, Tag } from '../../ui/Tag'
import { TextArea, TextField } from '../../ui/TextField'
import { useToast } from '../../ui/useToast'
import { VacancyEntry } from '../../ui/VacancyEntry'
import { cities, entries, filterChips, formats, minimalEntry, swatches, worstEntry } from './demo'
import './styleguide.css'

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="sg-section">
      <h2>{title}</h2>
      {children}
    </section>
  )
}

/** Служебная страница со всеми деталями интерфейса. В покрытие тестами не входит (docs/TESTING.md). */
export function StyleguidePage() {
  const s = t.styleguide
  const toast = useToast()
  const [city, setCity] = useState<string | null>('nsk')
  const [empty, setEmpty] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [region, setRegion] = useState('')
  const [pressed, setPressed] = useState<string[]>([filterChips[0]])
  const [modal, setModal] = useState(false)
  const [now] = useState(() => new Date())
  const inDays = (n: number) => {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + n)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  return (
    <div className="page styleguide">
      <h1>{s.title}</h1>
      <p className="lead">{s.lead}</p>

      <Section title={s.colors}>
        <ul className="sg-swatches">
          {swatches.map((sw) => (
            <li key={sw.name}>
              <span className="sg-swatch" style={{ background: `var(${sw.name})` }} />
              <code>{sw.name}</code>
              <span>{sw.note}</span>
            </li>
          ))}
        </ul>
      </Section>

      <Section title={s.type}>
        <div className="sg-type">
          <h3 className="sg-type-title">{s.typeTitle}</h3>
          <p className="lead">{s.typeLead}</p>
          <p>{s.typeUi}</p>
          <p className="num">{s.typeNums}</p>
        </div>
      </Section>

      <Section title={s.buttons}>
        <div className="sg-row">
          <Button>{s.btnPrimary}</Button>
          <Button variant="secondary">{s.btnSecondary}</Button>
          <Button variant="quiet">{s.btnQuiet}</Button>
          <Button variant="danger">{s.btnDanger}</Button>
          <Button disabled>{s.btnDisabled}</Button>
          <Button loading>{s.btnLoading}</Button>
          <Button size="sm">{s.btnSmall}</Button>
          <Button size="sm" variant="secondary">
            {s.btnSmall}
          </Button>
        </div>
      </Section>

      <Section title={s.fields}>
        <div className="sg-grid">
          <TextField label={s.fieldName} hint={s.fieldNameHint} placeholder={s.fieldNamePlaceholder} required />
          <TextField label={s.fieldError} defaultValue="ivanov@" error={s.fieldErrorText} />
          <TextField label={s.fieldDisabled} defaultValue="Институт прикладной оптики" disabled />
          <Select label={s.fieldSelect} options={formats} placeholder="" optional />
          <Combobox label={s.fieldCombo} hint={s.fieldComboHint} options={cities} value={city} onChange={setCity} />
          <Combobox label={s.fieldComboEmpty} options={cities} value={empty} onChange={setEmpty} placeholder={s.comboPlaceholder} />
          <TextArea className="sg-wide" label={s.fieldText} hint={s.fieldTextHint} optional />
        </div>
      </Section>

      <Section title={s.searchBar}>
        <SearchBar query={query} onQueryChange={setQuery} regions={cities} region={region} onRegionChange={setRegion} onSubmit={() => toast.show({ title: query || s.toastInfoTitle })} />
        <div className="sg-gap">
          <ResultsHeader count={entries.length} sortedBy={s.searchSortedBy} />
        </div>
      </Section>

      <Section title={s.chips}>
        <div className="sg-row" role="group" aria-label={s.chips}>
          {filterChips.map((chip) => (
            <FilterChip
              key={chip}
              pressed={pressed.includes(chip)}
              onClick={() => setPressed(pressed.includes(chip) ? pressed.filter((c) => c !== chip) : [...pressed, chip])}
            >
              {chip}
            </FilterChip>
          ))}
        </div>
        <div className="sg-row" aria-label={s.tagsLabel} role="group">
          <Tag>{t.vacancy.competition}</Tag>
          <Tag>ППС</Tag>
          <Tag tone="accent">R3</Tag>
        </div>
      </Section>

      <Section title={s.deadlines}>
        <div className="sg-grid sg-grid-4">
          <div>
            <h3 className="sg-sub">{s.deadlineNear}</h3>
            <Deadline date={inDays(7)} now={now} />
          </div>
          <div>
            <h3 className="sg-sub">{s.deadlineFar}</h3>
            <Deadline date={inDays(43)} now={now} />
          </div>
          <div>
            <h3 className="sg-sub">{s.deadlineLast}</h3>
            <Deadline date={inDays(0)} now={now} />
          </div>
          <div>
            <h3 className="sg-sub">{s.deadlineGone}</h3>
            <Deadline date={inDays(-3)} now={now} />
          </div>
        </div>
      </Section>

      <Section title={s.entries}>
        <h3 className="sg-sub">{s.entriesNormal}</h3>
        <div className="sg-list">
          {entries.map((e) => (
            <VacancyEntry key={e.to} {...e} />
          ))}
        </div>
        <h3 className="sg-sub">{s.entriesWorst}</h3>
        <div className="sg-list">
          <VacancyEntry {...worstEntry} />
        </div>
        <h3 className="sg-sub">{s.entriesMin}</h3>
        <div className="sg-list">
          <VacancyEntry {...minimalEntry} />
        </div>
        <h3 className="sg-sub">{s.loadingState}</h3>
        <div className="sg-list">
          <VacancyListSkeleton count={2} />
        </div>
      </Section>

      <Section title={s.emptyStates}>
        <div className="sg-grid">
          <EmptyState
            title={s.emptyNothingTitle}
            text={s.emptyNothingText}
            action={<Button variant="secondary">{s.emptyNothingAction}</Button>}
          />
          <EmptyState
            tone="error"
            title={s.emptyErrorTitle}
            text={s.emptyErrorText}
            action={<Button>{t.common.retry}</Button>}
          />
        </div>
      </Section>

      <Section title={s.overlays}>
        <div className="sg-row">
          <Button variant="secondary" onClick={() => setModal(true)}>
            {s.openModal}
          </Button>
          <Button variant="secondary" onClick={() => toast.show({ title: s.toastInfoTitle })}>
            {s.toastInfo}
          </Button>
          <Button
            variant="secondary"
            onClick={() => toast.show({ kind: 'success', title: s.toastSuccessTitle, text: s.toastSuccessText })}
          >
            {s.toastSuccess}
          </Button>
          <Button variant="secondary" onClick={() => toast.show({ kind: 'error', title: s.toastErrorTitle, text: s.toastErrorText })}>
            {s.toastError}
          </Button>
        </div>
        <Modal
          open={modal}
          onClose={() => setModal(false)}
          title={s.modalTitle}
          actions={
            <>
              <Button variant="quiet" onClick={() => setModal(false)}>
                {t.common.cancel}
              </Button>
              <Button variant="danger" onClick={() => setModal(false)}>
                {s.modalConfirm}
              </Button>
            </>
          }
        >
          <p>{s.modalText}</p>
        </Modal>
      </Section>
    </div>
  )
}

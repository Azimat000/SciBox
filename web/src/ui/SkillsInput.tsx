import { useState, type ClipboardEvent, type KeyboardEvent } from 'react'
import { t } from '../i18n'
import { Button } from './Button'
import { Field } from './Field'
import { CloseIcon } from './icons'
import { addSkills, clean } from './skills'
import './SkillsInput.css'

type Props = {
  label: string
  hint?: string
  placeholder?: string
  error?: string
  value: readonly string[]
  onChange: (next: string[]) => void
  /** Сколько можно добавить всего. */
  max: number
  /** Длина одного навыка в знаках. */
  maxLength: number
}

const separators = /[,;\n]/

/**
 * Список коротких фраз: человек пишет навык и нажимает Enter, запятую или «Добавить»; навык встаёт меткой с крестиком.
 * Несколько навыков через запятую можно вставить разом. Недописанное слово добавляется, когда поле теряет фокус,
 * чтобы не пропасть при сохранении формы.
 */
export function SkillsInput({ label, hint, placeholder, error, value, onChange, max, maxLength }: Props) {
  const [draft, setDraft] = useState('')
  const full = value.length >= max

  const commit = (parts: string[]) => {
    const next = addSkills(value, parts, max)
    if (next.length !== value.length) onChange(next)
  }
  const flush = () => {
    if (clean(draft) === '') return
    commit([draft])
    setDraft('')
  }
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || e.key !== 'Enter') return
    e.preventDefault()
    flush()
  }
  // Запятую ловим в самом тексте, а не по клавише: экранные клавиатуры телефонов клавиш не сообщают.
  const onType = (text: string) => {
    if (!separators.test(text)) {
      setDraft(text)
      return
    }
    const parts = text.split(separators)
    setDraft((parts.pop() ?? '').trimStart())
    commit(parts)
  }
  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text')
    if (!separators.test(text)) return
    e.preventDefault()
    commit((draft + text).split(separators))
    setDraft('')
  }

  return (
    <Field label={label} hint={hint} error={error} optional>
      {(control) => (
        <div className="skills">
          <div className="skills-entry">
            <input
              className="control"
              {...control}
              value={draft}
              maxLength={maxLength}
              placeholder={full ? t.ui.skillsFull : placeholder}
              disabled={full}
              onChange={(e) => onType(e.target.value)}
              onKeyDown={onKeyDown}
              onPaste={onPaste}
              onBlur={flush}
              enterKeyHint="done"
            />
            <Button type="button" variant="secondary" disabled={full || clean(draft) === ''} onClick={flush}>
              {t.ui.skillAdd}
            </Button>
          </div>
          {value.length > 0 && (
            <ul className="skills-list" aria-label={t.ui.skillsList(label)}>
              {value.map((s) => (
                <li key={s} className="skill-chip">
                  <span className="skill-name">{s}</span>
                  <button type="button" className="skill-remove" aria-label={t.ui.skillRemove(s)} onClick={() => onChange(value.filter((x) => x !== s))}>
                    <CloseIcon size={16} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          {value.length > 0 && <p className="skills-count num">{t.ui.skillsCount(value.length, max)}</p>}
        </div>
      )}
    </Field>
  )
}

import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useId, useState, type ChangeEvent, type ClipboardEvent, type KeyboardEvent } from 'react'
import { t } from '../i18n'
import { Button } from './Button'
import { Field } from './Field'
import { CloseIcon } from './icons'
import { addSkills, clean, skillKey, type SkillSearch, type SkillSuggestion } from './skills'
import './Combobox.css'
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
  /** Подсказки из справочника; без них поле работает как простой список фраз. */
  search?: SkillSearch
  /** Чем различаются справочники (научные и общие навыки): часть ключа кеша подсказок. */
  searchKey?: string
}

const separators = /[,;\n]/
const DEBOUNCE_MS = 250

type Option = { kind: 'found'; item: SkillSuggestion } | { kind: 'own'; name: string }

/**
 * Список коротких фраз: человек пишет навык, под полем появляются подсказки из справочника (начало слова, словоформы,
 * опечатки); выбранное встаёт меткой с крестиком. Нет нужного: последняя строка «Не нашли? Добавить «…»» добавляет
 * написанное как своё. Enter без выбранной подсказки тоже добавляет написанное, запятая и вставка списка через запятую
 * делят на навыки, недописанное добавляется, когда поле теряет фокус (не пропадёт при сохранении формы).
 * Шаблон WAI-ARIA 1.2: фокус остаётся в поле, текущая строка сообщается через aria-activedescendant.
 */
export function SkillsInput({ label, hint, placeholder, error, value, onChange, max, maxLength, search, searchKey }: Props) {
  const listId = useId()
  const [draft, setDraft] = useState('')
  const [debounced, setDebounced] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const full = value.length >= max
  const query = clean(draft)

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(query), DEBOUNCE_MS)
    return () => window.clearTimeout(timer)
  }, [query])

  const found = useQuery({
    queryKey: ['skill-suggest', searchKey ?? '', debounced],
    queryFn: ({ signal }) => (search as SkillSearch)(debounced, signal),
    enabled: search !== undefined && debounced !== '',
    staleTime: 5 * 60_000,
    retry: false,
    placeholderData: keepPreviousData,
  })

  const chosen = new Set(value.map(skillKey))
  const options: Option[] = []
  if (search && query !== '') {
    for (const item of found.data?.items ?? []) {
      if (!chosen.has(skillKey(item.name))) options.push({ kind: 'found', item })
    }
    // «Не нашли?» появляется, когда справочник ответил на этот запрос: пока он ищет, строка прыгала бы вместе с подсказками.
    const settled = debounced === query && !found.isFetching
    const ownKey = skillKey(query)
    const taken = chosen.has(ownKey) || options.some((o) => o.kind === 'found' && skillKey(o.item.name) === ownKey)
    if (settled && !taken) options.push({ kind: 'own', name: query })
  }
  const shown = open && options.length > 0
  const activeIndex = active >= options.length ? options.length - 1 : active
  const optionId = (i: number) => `${listId}-o${i}`

  const commit = (parts: string[]) => {
    const next = addSkills(value, parts, max)
    if (next.length !== value.length) onChange(next)
  }
  const reset = () => {
    setDraft('')
    setDebounced('')
    setActive(-1)
    setOpen(false)
  }
  const pick = (o: Option) => {
    commit([o.kind === 'found' ? o.item.name : o.name])
    reset()
  }
  // Написанное точно как название или синоним из справочника получает название сразу, а не после сохранения.
  const canonical = (text: string) => {
    if (debounced !== text || !found.data) return text
    const key = skillKey(text)
    const hit = found.data.items.find((s) => skillKey(s.name) === key || (s.via !== undefined && skillKey(s.via) === key))
    return hit ? hit.name : text
  }
  const flush = () => {
    if (query === '') return
    commit([canonical(query)])
    reset()
  }
  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) return
    switch (e.key) {
      case 'ArrowDown':
        if (options.length === 0) return
        e.preventDefault()
        if (!open) setOpen(true)
        else setActive(Math.min(activeIndex + 1, options.length - 1))
        return
      case 'ArrowUp':
        if (!shown) return
        e.preventDefault()
        setActive(Math.max(activeIndex - 1, -1))
        return
      case 'Escape':
        if (!open) return
        e.preventDefault()
        setOpen(false)
        setActive(-1)
        return
      case 'Enter':
        e.preventDefault()
        if (shown && activeIndex >= 0) pick(options[activeIndex])
        else flush()
        return
    }
  }
  // Запятую ловим в самом тексте, а не по клавише: экранные клавиатуры телефонов клавиш не сообщают.
  const onType = (e: ChangeEvent<HTMLInputElement>) => {
    const text = e.target.value
    setActive(-1)
    if (!separators.test(text)) {
      setDraft(text)
      setOpen(true)
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
    reset()
  }

  let status = ''
  if (open && search && query !== '') {
    if (found.isError) status = t.ui.skillsError
    else if (found.isFetching && !found.data) status = t.ui.skillsSearching
    else if (found.isSuccess && debounced === query && found.data.items.length === 0) status = t.ui.skillsNothing
    else if (found.isSuccess && debounced === query && found.data.fuzzy) status = t.ui.skillsFuzzy
  }

  return (
    <Field label={label} hint={hint} error={error} optional>
      {(control) => (
        <div className="skills">
          <div className="skills-entry">
            <div className="skills-search combo">
              <input
                className="control"
                {...control}
                {...(search
                  ? {
                      role: 'combobox',
                      'aria-autocomplete': 'list' as const,
                      'aria-expanded': shown,
                      'aria-controls': listId,
                      'aria-activedescendant': shown && activeIndex >= 0 ? optionId(activeIndex) : undefined,
                    }
                  : {})}
                autoComplete="off"
                value={draft}
                maxLength={maxLength}
                placeholder={full ? t.ui.skillsFull : placeholder}
                disabled={full}
                onChange={onType}
                onKeyDown={onKeyDown}
                onPaste={onPaste}
                onBlur={() => {
                  setOpen(false)
                  flush()
                }}
                enterKeyHint="done"
              />
              {search && (
                <ul id={listId} role="listbox" aria-label={t.ui.skillsSuggestions(label)} aria-busy={found.isFetching} className="combo-list skill-suggest" hidden={!shown}>
                  {options.map((o, i) => (
                    <li
                      key={o.kind === 'found' ? `f:${o.item.name}` : 'own'}
                      id={optionId(i)}
                      role="option"
                      aria-selected={i === activeIndex}
                      data-active={i === activeIndex || undefined}
                      className={o.kind === 'own' ? 'combo-option skill-option skill-option-own' : 'combo-option skill-option'}
                      // mousedown, а не click: иначе поле потеряет фокус раньше выбора
                      onMouseDown={(e) => {
                        e.preventDefault()
                        pick(o)
                      }}
                      onMouseMove={() => setActive(i)}
                    >
                      {o.kind === 'found' ? (
                        <>
                          <span className="skill-option-name">{o.item.name}</span>
                          <span className="skill-option-meta">{o.item.via ? t.ui.skillsVia(o.item.via, o.item.group) : o.item.group}</span>
                        </>
                      ) : (
                        <span className="skill-option-name">{t.ui.skillsAddOwn(o.name)}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <Button type="button" variant="secondary" disabled={full || query === ''} onClick={flush}>
              {t.ui.skillAdd}
            </Button>
          </div>
          {search && (
            <p className="skills-status" role="status">
              {status}
            </p>
          )}
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

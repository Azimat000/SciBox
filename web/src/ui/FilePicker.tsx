import { useId, useRef, useState, type ChangeEvent } from 'react'
import { Button } from './Button'
import { sizeText } from '../lib/fileSize'
import { CloseIcon, FileIcon, PlusIcon } from './icons'
import './FilePicker.css'

export type FilePickerMessages = {
  tooBig: (name: string) => string
  notPdf: (name: string) => string
  tooMany: string
}

type Props = {
  label: string
  hint?: string
  files: readonly File[]
  onChange: (files: File[]) => void
  addLabel: string
  removeLabel: (name: string) => string
  /** Текст вместо списка, пока файлов нет. */
  emptyText?: string
  max: number
  maxBytes: number
  messages: FilePickerMessages
  /** Ошибка от сервера или от формы. */
  error?: string
  /** Можно выбрать несколько файлов сразу. */
  multiple?: boolean
}

const isPdf = (f: File) => f.type === 'application/pdf' || f.name.toLowerCase().endsWith('.pdf')

/**
 * Выбор PDF-файлов: кнопка «Добавить», список выбранных с кнопкой «Убрать». Лишнее (не PDF, слишком большое, сверх
 * числа) отвергается сразу с понятной причиной, а не после отправки формы.
 */
export function FilePicker({ label, hint, files, onChange, addLabel, removeLabel, emptyText, max, maxBytes, messages, error, multiple = true }: Props) {
  const id = useId()
  const input = useRef<HTMLInputElement>(null)
  const [problem, setProblem] = useState<string | undefined>(undefined)

  const pick = (ev: ChangeEvent<HTMLInputElement>) => {
    const chosen = Array.from(ev.target.files ?? [])
    ev.target.value = '' // тот же файл можно выбрать снова
    const next = [...files]
    let found: string | undefined
    for (const f of chosen) {
      if (!isPdf(f)) found = messages.notPdf(f.name)
      else if (f.size > maxBytes) found = messages.tooBig(f.name)
      else if (next.length >= max) found = messages.tooMany
      else next.push(f)
    }
    setProblem(found)
    onChange(next)
  }
  const remove = (i: number) => {
    setProblem(undefined)
    onChange(files.filter((_, j) => j !== i))
  }
  const shown = problem ?? error
  const full = files.length >= max

  return (
    <div className="file-picker" role="group" aria-labelledby={`${id}-label`} data-invalid={shown ? 'true' : undefined}>
      <p className="field-label" id={`${id}-label`}>
        {label}
      </p>
      {hint && <p className="field-hint">{hint}</p>}
      {files.length > 0 ? (
        <ul className="file-list">
          {files.map((f, i) => (
            <li key={`${f.name}-${i}`} className="file-row">
              <FileIcon size={18} />
              <span className="file-name">{f.name}</span>
              <span className="file-size num">{sizeText(f.size)}</span>
              <button type="button" className="file-remove" aria-label={removeLabel(f.name)} onClick={() => remove(i)}>
                <CloseIcon size={16} />
              </button>
            </li>
          ))}
        </ul>
      ) : (
        emptyText && <p className="file-empty">{emptyText}</p>
      )}
      <input
        ref={input}
        id={id}
        type="file"
        accept="application/pdf,.pdf"
        multiple={multiple}
        className="visually-hidden"
        tabIndex={-1}
        onChange={pick}
        aria-hidden="true"
      />
      <div>
        <Button variant="secondary" size="sm" disabled={full} onClick={() => input.current?.click()}>
          <PlusIcon size={16} />
          {addLabel}
        </Button>
      </div>
      {shown && (
        <p className="field-message" role="alert">
          {shown}
        </p>
      )}
    </div>
  )
}

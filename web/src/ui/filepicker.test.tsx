import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { FilePicker } from './FilePicker'

const messages = { tooBig: (n: string) => `«${n}» велик`, notPdf: (n: string) => `«${n}» не PDF`, tooMany: 'Слишком много' }

function Harness({ max = 2, maxBytes = 5000, error, multiple }: { max?: number; maxBytes?: number; error?: string; multiple?: boolean }) {
  const [files, setFiles] = useState<File[]>([])
  return (
    <FilePicker
      label="Файлы"
      hint="Только PDF"
      files={files}
      onChange={setFiles}
      addLabel="Добавить PDF"
      removeLabel={(n) => `Убрать ${n}`}
      emptyText="Файлов нет"
      max={max}
      maxBytes={maxBytes}
      messages={messages}
      error={error}
      multiple={multiple}
    />
  )
}

const input = () => document.querySelector<HTMLInputElement>('input[type="file"]')!
const pdf = (name: string, size = 100, type = 'application/pdf') => new File([new Uint8Array(size)], name, { type })

describe('FilePicker', () => {
  it('shows the empty text and the hint, then lists chosen files with their sizes', async () => {
    render(<Harness />)
    expect(screen.getByText('Файлов нет')).toBeInTheDocument()
    expect(screen.getByText('Только PDF')).toBeInTheDocument()
    await userEvent.upload(input(), [pdf('Один.pdf', 2048), pdf('Два.pdf', 100)])
    expect(screen.getByText('Один.pdf')).toBeInTheDocument()
    expect(screen.getByText('2 КБ')).toBeInTheDocument()
    expect(screen.getByText('1 КБ')).toBeInTheDocument()
    expect(screen.queryByText('Файлов нет')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить PDF' })).toBeDisabled()
  })

  it('accepts a PDF by its extension when the browser gives no type', async () => {
    render(<Harness />)
    await userEvent.upload(input(), pdf('СКАН.PDF', 100, ''), { applyAccept: false })
    expect(screen.getByText('СКАН.PDF')).toBeInTheDocument()
  })

  it('rejects what is not a PDF, too big, or beyond the limit, and says why', async () => {
    render(<Harness max={1} />)
    await userEvent.upload(input(), pdf('картинка.png', 100, 'image/png'), { applyAccept: false })
    expect(screen.getByRole('alert')).toHaveTextContent('«картинка.png» не PDF')
    expect(screen.getByText('Файлов нет')).toBeInTheDocument()

    await userEvent.upload(input(), pdf('большой.pdf', 6000))
    expect(screen.getByRole('alert')).toHaveTextContent('«большой.pdf» велик')

    // Берём хорошие и лишние вместе: хорошие остаются, на лишнее есть причина.
    await userEvent.upload(input(), [pdf('Первый.pdf'), pdf('Второй.pdf')])
    expect(screen.getByText('Первый.pdf')).toBeInTheDocument()
    expect(screen.queryByText('Второй.pdf')).not.toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Слишком много')
  })

  it('removes a file, clears the message and allows adding again', async () => {
    render(<Harness max={1} />)
    await userEvent.upload(input(), pdf('Первый.pdf'))
    await userEvent.upload(input(), pdf('Второй.pdf'))
    expect(screen.getByRole('alert')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Убрать Первый.pdf' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Добавить PDF' })).toBeEnabled()
    await userEvent.upload(input(), pdf('Первый.pdf'))
    expect(screen.getByText('Первый.pdf')).toBeInTheDocument()
  })

  it('shows an error from the server and prefers the fresh local problem', async () => {
    render(<Harness error="Сервер: файл повреждён" max={1} />)
    expect(screen.getByRole('alert')).toHaveTextContent('Сервер: файл повреждён')
    await userEvent.upload(input(), pdf('картинка.png', 100, 'image/png'), { applyAccept: false })
    expect(screen.getByRole('alert')).toHaveTextContent('«картинка.png» не PDF')
  })

  it('opens the system dialog from the button', async () => {
    render(<Harness multiple={false} />)
    let opened = false
    input().addEventListener('click', (e) => {
      opened = true
      e.preventDefault()
    })
    await userEvent.click(screen.getByRole('button', { name: 'Добавить PDF' }))
    expect(opened).toBe(true)
    expect(input().multiple).toBe(false)
  })
})

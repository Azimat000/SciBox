import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Modal } from './Modal'
import { ToastProvider } from './ToastProvider'
import { useToast } from './useToast'

function ModalDemo({ withActions = true, empty = false }: { withActions?: boolean; empty?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Открыть
      </button>
      <button type="button">Соседняя</button>
      <Modal
        open={open}
        onClose={() => setOpen(false)}
        title="Удалить?"
        actions={
          withActions ? (
            <>
              <button type="button" onClick={() => setOpen(false)}>
                Отмена
              </button>
              <button type="button">Удалить</button>
            </>
          ) : undefined
        }
      >
        {empty ? <p>Только текст</p> : <input aria-label="Причина" />}
      </Modal>
    </>
  )
}

describe('Modal', () => {
  it('is absent while closed', () => {
    render(<ModalDemo />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens as a labelled modal dialog and focuses the first field', async () => {
    render(<ModalDemo />)
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    const dialog = screen.getByRole('dialog', { name: 'Удалить?' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByLabelText('Причина')).toHaveFocus()
    expect(document.body.style.overflow).toBe('hidden')
  })

  it('closes on Escape, unlocks scroll and returns focus to the opener', async () => {
    render(<ModalDemo />)
    const opener = screen.getByRole('button', { name: 'Открыть' })
    await userEvent.click(opener)
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.body.style.overflow).toBe('')
    expect(opener).toHaveFocus()
  })

  it('closes with the X button and with a click on the backdrop, but not on the panel', async () => {
    render(<ModalDemo />)
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    await userEvent.click(screen.getByRole('dialog'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Закрыть' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    await userEvent.click(screen.getByRole('dialog').parentElement!)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps Tab inside the dialog in both directions', async () => {
    render(<ModalDemo />)
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    expect(screen.getByLabelText('Причина')).toHaveFocus()
    await userEvent.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus()
    // с первого элемента назад: на последний
    await userEvent.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Удалить' })).toHaveFocus()
    // с последнего вперёд: на первый
    await userEvent.tab()
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus()
    await userEvent.tab()
    expect(screen.getByLabelText('Причина')).toHaveFocus()
    expect(screen.getByRole('button', { name: 'Соседняя' })).not.toHaveFocus()
  })

  it('handles a dialog with only the close button', async () => {
    render(<ModalDemo empty withActions={false} />)
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    // в содержимом нечего фокусировать: фокус на самой панели
    expect(screen.getByRole('dialog')).toHaveFocus()
    await userEvent.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus()
    await userEvent.tab()
    expect(screen.getByRole('button', { name: 'Закрыть' })).toHaveFocus()
  })

  it('focuses the panel when it has nothing to focus, and Tab goes nowhere', () => {
    render(
      <Modal open onClose={() => {}} title="Пусто">
        <p>Текст</p>
      </Modal>,
    )
    // кнопка «Закрыть» всегда есть, поэтому панель не нужна; убираем её, чтобы проверить ветку
    screen.getByRole('button', { name: 'Закрыть' }).remove()
    const dialog = screen.getByRole('dialog')
    dialog.focus()
    const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    document.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
  })

  it('ignores other keys', async () => {
    render(<ModalDemo />)
    await userEvent.click(screen.getByRole('button', { name: 'Открыть' }))
    await userEvent.keyboard('a')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })
})

function ToastDemo({ input }: { input: Parameters<ReturnType<typeof useToast>['show']>[0] }) {
  const toast = useToast()
  return (
    <button type="button" onClick={() => toast.show(input)}>
      Показать
    </button>
  )
}

function renderToast(input: Parameters<ReturnType<typeof useToast>['show']>[0]) {
  return render(
    <ToastProvider>
      <ToastDemo input={input} />
    </ToastProvider>,
  )
}

describe('Toast', () => {
  afterEach(() => vi.useRealTimers())

  it('shows an info message as a polite status', async () => {
    renderToast({ title: 'Сохранено', text: 'Черновик на месте' })
    await userEvent.click(screen.getByRole('button', { name: 'Показать' }))
    const toast = screen.getByRole('status')
    expect(toast).toHaveTextContent('Сохранено')
    expect(toast).toHaveTextContent('Черновик на месте')
    expect(screen.getByRole('region', { name: 'Уведомления' })).toBeInTheDocument()
  })

  it('shows an error as an alert and keeps it until dismissed', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderToast({ kind: 'error', title: 'Не отправлено' })
    await userEvent.click(screen.getByRole('button', { name: 'Показать' }))
    expect(screen.getByRole('alert')).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(screen.getByRole('alert')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Скрыть уведомление' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('hides success messages by itself after the default time', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderToast({ kind: 'success', title: 'Отправлено' })
    await userEvent.click(screen.getByRole('button', { name: 'Показать' }))
    expect(screen.getByRole('status')).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(5900)
    })
    expect(screen.getByRole('status')).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(200)
    })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('honours a custom duration', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderToast({ title: 'Коротко', duration: 1000 })
    await userEvent.click(screen.getByRole('button', { name: 'Показать' }))
    act(() => {
      vi.advanceTimersByTime(1100)
    })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('keeps at most three messages', async () => {
    renderToast({ title: 'Ещё', kind: 'error' })
    const button = screen.getByRole('button', { name: 'Показать' })
    for (let i = 0; i < 5; i++) await userEvent.click(button)
    expect(screen.getAllByRole('alert')).toHaveLength(3)
  })

  it('returns an id that can be dismissed, and clears timers on unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const held: { api?: ReturnType<typeof useToast> } = {}
    function Grab() {
      const toast = useToast()
      useEffect(() => {
        held.api = toast
      }, [toast])
      return null
    }
    const { unmount } = render(
      <ToastProvider>
        <Grab />
      </ToastProvider>,
    )
    let id = 0
    act(() => {
      id = held.api!.show({ title: 'Один' })
      held.api!.show({ title: 'Два' })
    })
    expect(screen.getAllByRole('status')).toHaveLength(2)
    act(() => held.api!.dismiss(id))
    expect(screen.getAllByRole('status')).toHaveLength(1)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('refuses to work outside the provider', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<ToastDemo input={{ title: 'x' }} />)).toThrow('ToastProvider')
    error.mockRestore()
  })
})

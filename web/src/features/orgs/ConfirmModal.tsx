import { t } from '../../i18n'
import { Button } from '../../ui/Button'
import { Modal } from '../../ui/Modal'

type Props = {
  open: boolean
  title: string
  text: string
  confirmLabel: string
  pending: boolean
  onConfirm: () => void
  onClose: () => void
}

/** Подтверждение необратимого действия: удалить, убрать, выйти. */
export function ConfirmModal({ open, title, text, confirmLabel, pending, onConfirm, onClose }: Props) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title}
      actions={
        <>
          <Button variant="quiet" onClick={onClose}>
            {t.common.cancel}
          </Button>
          <Button variant="danger" loading={pending} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <p>{text}</p>
    </Modal>
  )
}

import { useCallback, useEffect, useRef } from 'react'
import { useBlocker } from 'react-router'
import { t } from '../i18n'
import { Button } from './Button'
import { Modal } from './Modal'

/**
 * Не даёт потерять начатую форму: при уходе на другую страницу сайта спрашивает «Уйти без сохранения?»,
 * при закрытии вкладки или перезагрузке просит браузер показать его стандартный вопрос.
 * Перед переходом после удачного сохранения вызовите release(), иначе сайт спросит и тогда.
 */
export function useLeaveGuard(dirty: boolean) {
  const released = useRef(false)
  const blocker = useBlocker(
    useCallback(
      ({ currentLocation, nextLocation }) => dirty && !released.current && currentLocation.pathname !== nextLocation.pathname,
      [dirty],
    ),
  )

  useEffect(() => {
    if (!dirty) return
    function onBeforeUnload(e: BeforeUnloadEvent) {
      if (released.current) return
      e.preventDefault()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const release = useCallback(() => {
    released.current = true
  }, [])

  const blocked = blocker.state === 'blocked'
  const stay = () => blocker.reset?.()
  const leave = () => blocker.proceed?.()
  const prompt = (
    <Modal
      open={blocked}
      onClose={stay}
      title={t.ui.leaveTitle}
      actions={
        <>
          <Button variant="quiet" onClick={leave}>
            {t.ui.leaveConfirm}
          </Button>
          <Button onClick={stay}>{t.ui.leaveStay}</Button>
        </>
      }
    >
      <p>{t.ui.leaveText}</p>
    </Modal>
  )
  return { prompt, release }
}

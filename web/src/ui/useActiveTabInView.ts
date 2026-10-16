import { useLayoutEffect, useRef } from 'react'
import { useLocation } from 'react-router'

/**
 * Ряд вкладок на телефоне листается вбок без полосы прокрутки. Открытая вкладка (aria-current="page")
 * может оказаться за краем, и человек не видит, где он. После каждого перехода ряд один раз доводит её до середины,
 * в том числе когда вкладки появляются позже страницы (после загрузки данных). Свою прокрутку человека не трогает.
 */
export function useActiveTabInView<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const done = useRef<string | null>(null)
  const { key } = useLocation()
  useLayoutEffect(() => {
    const nav = ref.current
    if (done.current === key || !nav) return
    done.current = key
    if (nav.scrollWidth <= nav.clientWidth) return
    const tab = nav.querySelector<HTMLElement>('[aria-current="page"]')
    if (!tab) return
    const box = nav.getBoundingClientRect()
    const own = tab.getBoundingClientRect()
    nav.scrollLeft += own.left - box.left - (box.width - own.width) / 2
  })
  return ref
}

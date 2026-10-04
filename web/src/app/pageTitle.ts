import { useEffect, useLayoutEffect } from 'react'
import { useLocation, useMatches } from 'react-router'
import { productName } from '../config/product'

/** «Вакансии — SciBox»; без названия страницы только имя продукта. */
export function fullTitle(title: string | undefined): string {
  return title ? `${title} — ${productName}` : productName
}

/**
 * Заголовок вкладки по адресу: берётся из `handle.title` самого глубокого маршрута.
 * Ставится в useLayoutEffect, то есть раньше обычных эффектов страниц: страница с данными
 * (вакансия, учёный) потом поставит своё название через usePageTitle.
 */
export function useRouteTitle(): void {
  const matches = useMatches()
  const { pathname } = useLocation()
  const routeTitle = [...matches].reverse().map((m) => (m.handle as { title?: string } | undefined)?.title).find(Boolean)
  useLayoutEffect(() => {
    document.title = fullTitle(routeTitle)
  }, [routeTitle, pathname])
}

/** Название страницы с данными: имя учёного, вакансии, организации. Пока данных нет, ничего не меняет. */
export function usePageTitle(title: string | undefined): void {
  useEffect(() => {
    if (title) document.title = fullTitle(title)
  }, [title])
}

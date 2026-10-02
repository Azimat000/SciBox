import { useOutletContext } from 'react-router'
import type { OrgView, Viewer } from './api'

/** Что страницы управления получают от рамки: адрес организации, её страница и права вошедшего (он точно сотрудник). */
export type ManageContext = { slug: string; view: OrgView; viewer: Viewer }

export const useManageContext = () => useOutletContext<ManageContext>()

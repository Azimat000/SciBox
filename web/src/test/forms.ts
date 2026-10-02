import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

/** Находит поле по подписи (подпись может заканчиваться на «· обязательно»). */
export const field = (label: string | RegExp) => screen.getByLabelText(label)

/** Печатает в поле, найденное по подписи. */
export async function fill(label: string | RegExp, text: string) {
  await userEvent.type(field(label), text)
}

/** Нажимает кнопку по имени. */
export async function press(name: string | RegExp) {
  await userEvent.click(screen.getByRole('button', { name }))
}

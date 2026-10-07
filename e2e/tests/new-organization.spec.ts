import { expect, type Page } from '@playwright/test'
import { registerAndConfirm, switchRole, test, uniqueEmail, watchServerErrors } from './helpers'

// Дорожка «я организация»: новый человек регистрируется, создаёт организацию,
// публикует вакансию, и гость находит её в поиске.

test('новая организация публикует вакансию, гость находит её поиском @phone', async ({ page, browser, serverErrors }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const orgName = `Институт палеоклимата ${suffix}`
  const title = `Научный сотрудник: керны озёрных отложений ${suffix}`

  await test.step('регистрация', async () => {
    await registerAndConfirm(page, `Павел Ермаков ${suffix}`, uniqueEmail('employer'))
  })

  await test.step('создание организации', async () => {
    await switchRole(page, 'Нанимаю')
    await page.goto('/organizations/new')
    await page.getByLabel('Название', { exact: false }).first().fill(orgName)
    await page.getByLabel('Тип организации').selectOption({ index: 1 })
    await page.getByLabel('Город').fill('Томск')
    await page.getByRole('button', { name: 'Создать организацию' }).click()
    await expect(page.getByText('Организация создана')).toBeVisible()
  })

  await test.step('вакансия: заполнить и опубликовать', async () => {
    await page.goto('/my-vacancies/new')
    await page.getByRole('button', { name: 'Научный работник' }).click()
    await page.getByLabel(/^Должность/).selectOption({ label: 'Научный сотрудник' })
    await page.getByLabel('Название вакансии').fill(title)
    await page.getByLabel('Аннотация').fill('Ищем сотрудника для разбора кернов озёрных отложений Сибири и реконструкции климата голоцена.')
    await page.getByLabel('Описание', { exact: false }).first().fill('Полевые выезды летом, лаборатория изотопного анализа, группа из шести человек.')
    await page.getByLabel('Направление или проект', { exact: false }).fill('Палеоклимат голоцена')
    await pickFromCombobox(page, 'Научные специальности', 'Геоэкология')
    await page.getByLabel('Уровень исследователя').selectOption({ index: 2 })
    await page.getByLabel('Формат работы').selectOption({ label: 'Очно' })
    await pickFromCombobox(page, 'Регион', 'Томская')
    await page.getByLabel('Город').fill('Томск')
    await page.getByLabel('Ставка').selectOption({ index: 1 })
    await page.getByLabel(/^Договор/).selectOption({ label: 'Бессрочный договор' })
    await page.getByRole('button', { name: 'Сохранить и опубликовать' }).click()
    await expect(page.getByText('Вакансия опубликована')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible()
  })

  await test.step('гость находит вакансию поиском', async () => {
    const guest = await (await browser.newContext()).newPage()
    const guestErrors = watchServerErrors(guest)
    await guest.goto('/vacancies')
    await guest.getByRole('searchbox').first().fill(`керны озёрных отложений ${suffix}`)
    await guest.getByRole('searchbox').first().press('Enter')
    await expect(guest.getByRole('link', { name: title, exact: true })).toBeVisible()
    await guest.getByRole('link', { name: title, exact: true }).click()
    await expect(guest.getByText(orgName).first()).toBeVisible()
    await guest.context().close()
    expect([...serverErrors, ...guestErrors]).toEqual([])
  })
})

/** Поле с подсказками: напечатать начало и выбрать первую подходящую строку. */
async function pickFromCombobox(page: Page, label: string, text: string): Promise<void> {
  const box = page.getByRole('combobox', { name: label, exact: true })
  await box.fill(text)
  await page.getByRole('option', { name: new RegExp(text) }).first().click()
}

import { expect } from '@playwright/test'
import { newPassword, registerAndConfirm, signIn, test, uniqueEmail, watchServerErrors } from './helpers'

// Срез 20 (T-10): сменил пароль на одном устройстве — на другом устройстве сеанс завершён.
// «Другое устройство» — отдельный контекст браузера со своими cookie.

test('смена пароля завершает сеанс на другом устройстве @phone', async ({ page, browser }) => {
  const suffix = Date.now().toString(36).slice(-5)
  const email = uniqueEmail('security')
  const changed = 'e2e-changed-2026'

  await test.step('регистрация на первом устройстве', async () => {
    await registerAndConfirm(page, `Ирина Белова ${suffix}`, email)
  })

  const laptop = await browser.newContext()
  const other = await laptop.newPage()
  const otherErrors = watchServerErrors(other)

  await test.step('вход на втором устройстве, оба сеанса видны в списке', async () => {
    await signIn(other, email, newPassword)
    await page.goto('/account/security')
    const sessions = page.getByRole('region', { name: 'Где вы вошли' })
    await expect(sessions.getByRole('listitem')).toHaveCount(2)
    await expect(sessions.getByText('это устройство')).toBeVisible()
  })

  await test.step('смена пароля на первом устройстве', async () => {
    await page.getByLabel('Текущий пароль', { exact: false }).fill(newPassword)
    await page.getByLabel('Новый пароль', { exact: false }).fill(changed)
    await page.getByRole('button', { name: 'Сменить пароль' }).click()
    await expect(page.getByText('Пароль изменён')).toBeVisible()
  })

  await test.step('второе устройство вышло из аккаунта', async () => {
    await other.goto('/account/security')
    await expect(other).toHaveURL(/\/login\?next=\/account\/security/)
    await expect(other.getByText('Войдите, чтобы открыть эту страницу.')).toBeVisible()
  })

  await test.step('первое устройство осталось, в списке один сеанс', async () => {
    await page.reload()
    await expect(page.getByText('Аккаунт открыт только здесь.')).toBeVisible()
  })

  await test.step('новый пароль работает на втором устройстве', async () => {
    await signIn(other, email, changed)
  })

  expect(otherErrors, 'сервер ответил ошибкой второму устройству').toEqual([])
  await laptop.close()
})

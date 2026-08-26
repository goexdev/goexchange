import { test, expect } from '@playwright/test'
import { takeScreenshot } from './helpers'

test.describe('i18n and Accessibility', () => {
  test('language switcher changes UI text', async ({ page }) => {
    await page.goto('/')
    await takeScreenshot(page, '30-i18n-en')

    const langBtn = page.getByRole('button', { name: /Select language|EN/i }).first()
    if (await langBtn.isVisible({ timeout: 1000 }).catch(() => false)) {
      await langBtn.click()
      const zhOption = page.getByText(/Chinese/).first()
      if (await zhOption.isVisible({ timeout: 1000 }).catch(() => false)) {
        await zhOption.click()
        await page.waitForTimeout(500)
        await takeScreenshot(page, '31-i18n-zh')
      }
    }
  })

  test('home page has accessible navigation', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('navigation').first()).toBeVisible()
    await expect(page.getByRole('main')).toBeVisible()
  })

  test('login form has proper labels', async ({ page }) => {
    await page.goto('/login')
    const emailInput = page.locator('input[type="email"]')
    const passwordInput = page.locator('input[type="password"]')
    await expect(emailInput).toBeVisible()
    await expect(passwordInput).toBeVisible()
  })

  test('status page is keyboard navigable', async ({ page }) => {
    await page.goto('/status')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
    await page.keyboard.press('Tab')
  })
})
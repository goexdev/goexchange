import { Page, expect } from '@playwright/test'

export const TEST_USERS = {
  regular: {
    email: 'newtest@goexchange.local',
    password: 'testpass123',
  },
}

export async function login(page: Page, email: string, password: string) {
  // Clear any previous auth state
  await page.context().clearCookies()
  try {
    await page.evaluate(() => {
      localStorage.clear()
      sessionStorage.clear()
    })
  } catch {}
  await page.goto('/login')
  await page.waitForLoadState('domcontentloaded')
  await page.fill('input[type="email"]', email)
  await page.fill('input[type="password"]', password)
  await page.click('button[type="submit"]')
  await page.waitForURL((url) => !url.toString().includes('/login'), { timeout: 15000 })
  await page.waitForTimeout(1000)
}

export async function logout(page: Page) {
  const logoutBtn = page.getByRole('button', { name: /logout/i })
  if (await logoutBtn.isVisible({ timeout: 1000 }).catch(() => false)) {
    await logoutBtn.click()
    await page.waitForURL('**/login', { timeout: 5000 })
  } else {
    await page.context().clearCookies()
  }
}

export async function takeScreenshot(page: Page, name: string) {
  await page.screenshot({
    path: `e2e/screenshots/${name}.png`,
    fullPage: true,
  })
}

export async function expectToast(page: Page, message: string | RegExp) {
  const toast = page.locator('[role="status"], .toast, [data-testid="toast"]')
  await expect(toast).toContainText(message, { timeout: 5000 })
}
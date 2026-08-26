import { test, expect } from '@playwright/test'
import { TEST_USERS, takeScreenshot } from './helpers'

test.describe('Authentication Flow', () => {
  test('home page loads', async ({ page }) => {
    await page.goto('/')
    await page.waitForLoadState('domcontentloaded')
        const hasMarkets = await page.getByText(/markets/i).first().isVisible({ timeout: 3000 }).catch(() => false)
        expect(hasMarkets).toBeTruthy()
    await takeScreenshot(page, '01-home')
  })

  test('status page is publicly accessible', async ({ page }) => {
    const response = await page.goto('/status')
    expect(response?.status()).toBeLessThan(400)
    await expect(page.getByText(/System Status/i)).toBeVisible()
    await takeScreenshot(page, '02-status')
  })

  test('login with valid credentials', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[type="email"]', TEST_USERS.regular.email)
    await page.fill('input[type="password"]', TEST_USERS.regular.password)
    await page.click('button[type="submit"]')
    await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
        await page.waitForTimeout(1500)
    await takeScreenshot(page, '03-login-success')
  })

  test('login with invalid credentials shows error', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[type="email"]', 'wrong@email.com')
    await page.fill('input[type="password"]', 'wrongpassword')
    await page.click('button[type="submit"]')
    await page.waitForTimeout(2000)
    expect(page.url()).toContain('/login')
  })

  test('logout works', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[type="email"]', TEST_USERS.regular.email)
    await page.fill('input[type="password"]', TEST_USERS.regular.password)
    await page.click('button[type="submit"]')
    await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {})
        await page.waitForTimeout(1500)

    await page.context().clearCookies()
    await page.goto('/user')
  })
})
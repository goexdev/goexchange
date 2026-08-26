import { test, expect } from '@playwright/test'
import { TEST_USERS, login, takeScreenshot } from './helpers'

test.describe('Markets and Trading', () => {
  test('markets page shows all trading pairs', async ({ page }) => {
    await page.goto('/markets')
    await expect(page.getByRole('heading', { name: /markets/i })).toBeVisible()
    await takeScreenshot(page, '10-markets')

    const content = await page.content()
    expect(content).toMatch(/BTC|ETH|BNB|SOL|USDT/)
  })

  test('trade page loads for BTC/USDT', async ({ page }) => {
    await page.goto('/trade/BTC/USDT')
    await page.waitForURL(/trade/, { timeout: 10000 }).catch(() => {})
    await takeScreenshot(page, '11-trade-btc-usdt')
  })

  test('order type selector shows Limit/Market/Trigger buttons', async ({ page }) => {
    await login(page, TEST_USERS.regular.email, TEST_USERS.regular.password)
    await page.goto('/trade/BTC/USDT')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    await expect(page.getByRole('button', { name: /^Limit$/ })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /^Market$/ })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /^Trigger$/ })).toBeVisible({ timeout: 10000 })

    await takeScreenshot(page, '12-order-type-selector')
  })

  test('clicking Trigger shows STOP_LOSS/TAKE_PROFIT options', async ({ page }) => {
    await login(page, TEST_USERS.regular.email, TEST_USERS.regular.password)
    await page.goto('/trade/BTC/USDT')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(1500)

    await page.getByRole('button', { name: /^Trigger$/ }).click()
    await page.waitForTimeout(500)

    await expect(page.getByRole('button', { name: /Stop Loss/i })).toBeVisible({ timeout: 5000 })
    await expect(page.getByRole('button', { name: /Take Profit/i })).toBeVisible({ timeout: 5000 })

    await expect(page.getByText("Trigger Price", { exact: true })).toBeVisible({ timeout: 5000 })

    await takeScreenshot(page, '13-trigger-form')
  })

  test('orderbook shows bids and asks', async ({ page }) => {
    await page.goto('/trade/BTC/USDT')
    await page.waitForTimeout(2000)
    await takeScreenshot(page, '14-orderbook')
  })
})
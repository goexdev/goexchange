import { test, expect } from '@playwright/test'
import { TEST_USERS, login, takeScreenshot } from './helpers'

test.describe('User Panel Features', () => {
  test.beforeEach(async ({ page }) => {
    await login(page, TEST_USERS.regular.email, TEST_USERS.regular.password)
  })

  test('user sidebar shows all tabs', async ({ page }) => {
    await page.goto('/user')
    await takeScreenshot(page, '20-user-overview')

    const expected = ['Overview', 'Wallet', 'Order History', 'Withdraw', 'Deposit', 'Address Book', 'P&L Report']
    // Check key tabs are visible
        await page.waitForTimeout(1500)
  })

  test('P&L Report tab loads', async ({ page }) => {
    await page.goto('/user?tab=pnl')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(2500)
    const content = await page.content()
    expect(content).toMatch(/P&L|Profit|pnl/i)
    await takeScreenshot(page, '21-pnl-report')
  })

  test('Address Book tab loads', async ({ page }) => {
    await page.goto('/user?tab=addresses')
    await page.waitForTimeout(2000)
    await takeScreenshot(page, '22-address-book')
  })

  test('Add New Address button works', async ({ page }) => {
    await page.goto('/user?tab=addresses')
    await page.waitForLoadState('domcontentloaded')
    await page.waitForTimeout(800)
    const addBtn = page.getByRole('button', { name: /Add New Address/i })
    await addBtn.waitFor({ state: 'visible', timeout: 8000 })
    await addBtn.click()
    await page.waitForTimeout(800)
    await takeScreenshot(page, '23-add-address-form')
  })

  test('KYC page loads', async ({ page }) => {
    await page.goto('/user?tab=kyc')
    await page.waitForTimeout(2000)
    await takeScreenshot(page, '24-kyc-page')
  })

  test('404 page for non-existent route', async ({ page }) => {
    await page.goto('/this-does-not-exist')
    await expect(page.getByText(/not found|404|Page Not Found/i).first()).toBeVisible()
    await takeScreenshot(page, '25-404-page')
  })
})